package providerplane

import (
	"diffractllm/internal/core"
	"errors"
	"maps"
	"sync"
	"sync/atomic"
)

var ErrNoCredential = errors.New("providerplane: no credential serves this model")

type snapshot struct {
	providerCredentials map[core.Provider][]*core.Credential
}

func buildCredSnapshot(credentials []*core.Credential) *snapshot {
	tempSnap := snapshot{
		providerCredentials: make(map[core.Provider][]*core.Credential),
	}

	for _, cred := range credentials {
		tempSnap.providerCredentials[cred.Provider] = append(tempSnap.providerCredentials[cred.Provider], cred)
	}
	return &tempSnap
}

type ProviderPlane struct {
	credSnapshot atomic.Pointer[snapshot]
	mu           sync.Mutex
}

func NewProviderPlane(cred []*core.Credential) *ProviderPlane {
	temp := ProviderPlane{}
	tempsnapshot := buildCredSnapshot(cred)
	temp.credSnapshot.Store(tempsnapshot)
	return &temp
}

func (plane *ProviderPlane) Candidates(key core.CatalogKey) []*core.Credential {
	snap := plane.credSnapshot.Load()
	if snap == nil {
		return nil
	}
	bucket := snap.providerCredentials[key.Provider]
	candidates := make([]*core.Credential, 0, len(bucket))
	for _, cred := range bucket {
		if cred.CheckValidity() && cred.CheckModel(key.ModelName) {
			candidates = append(candidates, cred)
		}
	}
	return candidates
}

func (plane *ProviderPlane) Credentials(provider core.Provider) []*core.Credential {
	snap := plane.credSnapshot.Load()
	if snap == nil {
		return nil
	}
	return snap.providerCredentials[provider]
}

func (plane *ProviderPlane) clone() *snapshot {
	old := plane.credSnapshot.Load()
	if old == nil {
		return &snapshot{
			providerCredentials: make(map[core.Provider][]*core.Credential),
		}
	}
	return &snapshot{
		providerCredentials: maps.Clone(old.providerCredentials),
	}
}

func (plane *ProviderPlane) UpsertCredential(cred *core.Credential) error {
	if err := cred.Validate(); err != nil {
		return err
	}

	plane.mu.Lock()
	defer plane.mu.Unlock()

	next := plane.clone()
	bucket := next.providerCredentials[cred.Provider]

	replaced := make([]*core.Credential, 0, len(bucket)+1)
	found := false
	for _, existing := range bucket {
		if existing.ID == cred.ID {
			replaced = append(replaced, cred)
			found = true
			continue
		}
		replaced = append(replaced, existing)
	}

	if !found {
		replaced = append(replaced, cred)
	}

	next.providerCredentials[cred.Provider] = replaced
	plane.credSnapshot.Store(next)
	return nil
}

func (plane *ProviderPlane) RemoveCredential(provider core.Provider, id string) error {
	plane.mu.Lock()
	defer plane.mu.Unlock()

	next := plane.clone()
	bucket := next.providerCredentials[provider]

	kept := make([]*core.Credential, 0, len(bucket))
	for _, existing := range bucket {
		if existing.ID != id {
			kept = append(kept, existing)
		}
	}

	if len(kept) == len(bucket) {
		return ErrNoCredential
	}

	if len(kept) == 0 {
		delete(next.providerCredentials, provider)
	} else {
		next.providerCredentials[provider] = kept
	}
	plane.credSnapshot.Store(next)
	return nil
}

func (plane *ProviderPlane) Replace(credentials []*core.Credential) {
	plane.mu.Lock()
	defer plane.mu.Unlock()
	plane.credSnapshot.Store(buildCredSnapshot(credentials))
}

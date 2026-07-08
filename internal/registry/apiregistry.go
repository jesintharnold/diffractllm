package registry

import (
	"diffractllm/internal/core"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
)

type APIRegistry struct {
	registry atomic.Pointer[map[string]*core.ModelAPIRegistry]
	mu       sync.Mutex
}

func NewAPIRegistry(apidetails []*core.ModelAPIRegistry) *APIRegistry {
	lookup := make(map[string]*core.ModelAPIRegistry, len(apidetails))
	for _, api := range apidetails {
		lookup[api.ID] = api
	}
	r := &APIRegistry{}
	r.registry.Store(&lookup)
	return r
}

func (r *APIRegistry) LookupAPIRegistry(id string) (*core.ModelAPIRegistry, bool) {
	api, ok := (*r.registry.Load())[id]
	return api, ok
}

func (r *APIRegistry) UpsertToAPIRegistry(apiDetails *core.ModelAPIRegistry) error {
	if apiDetails == nil || apiDetails.ID == "" {
		return fmt.Errorf("api registry with a non-empty ID is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	old := *r.registry.Load()
	next := make(map[string]*core.ModelAPIRegistry, len(old)+1)
	maps.Copy(next, old)
	next[apiDetails.ID] = apiDetails
	r.registry.Store(&next)
	return nil
}

func (r *APIRegistry) RemoveFromAPIRegistry(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	old := *r.registry.Load()
	if _, ok := old[id]; !ok {
		return fmt.Errorf("api registry %q not found", id)
	}

	next := make(map[string]*core.ModelAPIRegistry, len(old))
	maps.Copy(next, old)
	delete(next, id)
	r.registry.Store(&next)
	return nil
}

func (r *APIRegistry) ReplaceAPIRegistry(apidetails []*core.ModelAPIRegistry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lookup := make(map[string]*core.ModelAPIRegistry, len(apidetails))
	for _, api := range apidetails {
		lookup[api.ID] = api
	}
	r.registry.Store(&lookup)
}

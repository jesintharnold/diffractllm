package registry

import (
	"diffractllm/internal/core"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
)

type modelSnapshot struct {
	ModelLookup     map[core.ModelKey][]*core.Deployment
	Deployments     []*core.Deployment
	ModelPoolLookup map[string]*core.ModelPool
	Pools           []*core.ModelPool
}

func buildDeployments(apiDetails *core.ModelAPIRegistry, states *core.StateManager) []*core.Deployment {
	deployments := make([]*core.Deployment, 0, len(apiDetails.AllowedModels))
	for _, modelName := range apiDetails.AllowedModels {
		if modelName == "" {
			continue
		}
		id := apiDetails.ID + "/" + modelName
		deployments = append(deployments, &core.Deployment{
			ID:            id,
			ModelProvider: apiDetails.Provider,
			ModelName:     modelName,
			APIRegistryID: apiDetails.ID,
			State:         states.Acquire(id, 1),
		})
	}
	return deployments
}

func buildRegistrySnapshot(apiregistry []*core.ModelAPIRegistry, pools []*core.ModelPool, states *core.StateManager) *modelSnapshot {
	modelLookup := make(map[core.ModelKey][]*core.Deployment)
	allDeployments := make([]*core.Deployment, 0)

	for _, apiDetails := range apiregistry {
		deployments := buildDeployments(apiDetails, states)
		for _, deployment := range deployments {
			key := deployment.Key()
			modelLookup[key] = append(modelLookup[key], deployment)
			allDeployments = append(allDeployments, deployment)
		}
	}

	poolLookup := make(map[string]*core.ModelPool, len(pools))
	for _, pool := range pools {
		poolLookup[pool.Name] = pool
	}

	return &modelSnapshot{
		ModelLookup:     modelLookup,
		Deployments:     allDeployments,
		ModelPoolLookup: poolLookup,
		Pools:           pools,
	}
}

type ModelRegistry struct {
	Registry atomic.Pointer[modelSnapshot]
	states   *core.StateManager
	mu       sync.Mutex
}

func NewModelRegistry(apiregistry []*core.ModelAPIRegistry, pools []*core.ModelPool, states *core.StateManager) *ModelRegistry {
	registry := &ModelRegistry{states: states}
	registry.Registry.Store(buildRegistrySnapshot(apiregistry, pools, states))
	return registry
}

func (m *ModelRegistry) LookupModel(key core.ModelKey) ([]*core.Deployment, bool) {
	deployments, ok := m.Registry.Load().ModelLookup[key]
	return deployments, ok
}

func (m *ModelRegistry) LookupPool(name string) (*core.ModelPool, bool) {
	pool, ok := m.Registry.Load().ModelPoolLookup[name]
	return pool, ok
}

func (m *ModelRegistry) SyncDeployments(apiDetails *core.ModelAPIRegistry) error {
	if apiDetails == nil || apiDetails.ID == "" {
		return fmt.Errorf("api registry with a non-empty ID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldregistry := m.Registry.Load()
	olddeployments := make([]*core.Deployment, 0)
	newdeployments := buildDeployments(apiDetails, m.states)
	newRegistryDeployments := make([]*core.Deployment, 0)
	filterbuckets := make(map[core.ModelKey]struct{}, len(olddeployments)+len(newdeployments))
	newReleaseIDs := make(map[string]struct{}, len(newdeployments))
	for _, odeployment := range oldregistry.Deployments {
		if odeployment.APIRegistryID == apiDetails.ID {
			olddeployments = append(olddeployments, odeployment)
			filterbuckets[odeployment.Key()] = struct{}{}
		} else {
			newRegistryDeployments = append(newRegistryDeployments, odeployment)
		}
	}

	//New buckets from new deployments
	newDeploymentlookups := make(map[core.ModelKey][]*core.Deployment, len(newdeployments))
	for _, ndeployment := range newdeployments {
		filterbuckets[ndeployment.Key()] = struct{}{}
		newDeploymentlookups[ndeployment.Key()] = append(newDeploymentlookups[ndeployment.Key()], ndeployment)
		newReleaseIDs[ndeployment.ID] = struct{}{}
	}

	// Creating a copy of the old entire map
	newLookups := make(map[core.ModelKey][]*core.Deployment, len(oldregistry.ModelLookup)+len(newDeploymentlookups))
	maps.Copy(newLookups, oldregistry.ModelLookup)

	for modelkey := range filterbuckets {
		tempBucketDeployments := make([]*core.Deployment, 0, len(oldregistry.ModelLookup[modelkey])+len(newDeploymentlookups[modelkey]))
		for _, tempDeployments := range oldregistry.ModelLookup[modelkey] {
			if tempDeployments.APIRegistryID != apiDetails.ID {
				tempBucketDeployments = append(tempBucketDeployments, tempDeployments)
			}
		}

		tempBucketDeployments = append(tempBucketDeployments, newDeploymentlookups[modelkey]...)
		if len(tempBucketDeployments) == 0 {
			delete(newLookups, modelkey)
		} else {
			newLookups[modelkey] = tempBucketDeployments
		}
	}

	// Now purely for deployments alone
	newRegistryDeployments = append(newRegistryDeployments, newdeployments...)
	m.Registry.Store(&modelSnapshot{
		ModelLookup:     newLookups,
		Deployments:     newRegistryDeployments,
		ModelPoolLookup: oldregistry.ModelPoolLookup,
		Pools:           oldregistry.Pools,
	})

	// Release the IDs which are not present in the new list
	for _, deployment := range olddeployments {
		if _, ok := newReleaseIDs[deployment.ID]; !ok {
			m.states.Release(deployment.ID)
		}
	}

	return nil
}

func (m *ModelRegistry) RemoveDeployments(registryID string) error {

	if registryID == "" {
		return fmt.Errorf("Invalid API Registry ID is given")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldregistry := m.Registry.Load()
	altdeployments := make([]*core.Deployment, 0)

	filteredbuckets := make(map[core.ModelKey]struct{}, 0)

	removedDeployments := make([]*core.Deployment, 0)
	for _, deployment := range oldregistry.Deployments {
		if deployment.APIRegistryID != registryID {
			altdeployments = append(altdeployments, deployment)
		} else {
			filteredbuckets[deployment.Key()] = struct{}{}
			removedDeployments = append(removedDeployments, deployment)
		}
	}

	if len(removedDeployments) == 0 {
		return fmt.Errorf("api registry %q not found", registryID)
	}

	newLookup := make(map[core.ModelKey][]*core.Deployment, len(oldregistry.ModelLookup))
	maps.Copy(newLookup, oldregistry.ModelLookup)

	for key := range filteredbuckets {
		tempDeployments := make([]*core.Deployment, 0)
		for _, dep := range newLookup[key] {
			if dep.APIRegistryID != registryID {
				tempDeployments = append(tempDeployments, dep)
			}
		}
		if len(tempDeployments) == 0 {
			delete(newLookup, key)
		} else {
			newLookup[key] = tempDeployments
		}
	}

	m.Registry.Store(&modelSnapshot{
		ModelLookup:     newLookup,
		Deployments:     altdeployments,
		ModelPoolLookup: oldregistry.ModelPoolLookup,
		Pools:           oldregistry.Pools,
	})

	for _, dep := range removedDeployments {
		m.states.Release(dep.ID)
	}
	return nil
}

func (m *ModelRegistry) Replace(apiregistry []*core.ModelAPIRegistry, pools []*core.ModelPool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Registry.Store(buildRegistrySnapshot(apiregistry, pools, m.states))
}

func (m *ModelRegistry) AddPool(pool *core.ModelPool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.Registry.Load()
	if _, exists := old.ModelPoolLookup[pool.Name]; exists {
		return fmt.Errorf("pool %q already exists", pool.Name)
	}

	newLookup := make(map[string]*core.ModelPool, len(old.ModelPoolLookup)+1)
	maps.Copy(newLookup, old.ModelPoolLookup)
	newLookup[pool.Name] = pool

	newPools := make([]*core.ModelPool, len(old.Pools)+1)
	copy(newPools, old.Pools)
	newPools[len(newPools)-1] = pool

	m.Registry.Store(&modelSnapshot{
		ModelLookup:     old.ModelLookup,
		Deployments:     old.Deployments,
		ModelPoolLookup: newLookup,
		Pools:           newPools,
	})
	return nil
}

func (m *ModelRegistry) UpdatePool(pool *core.ModelPool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.Registry.Load()
	idx := -1
	for i, p := range old.Pools {
		if p.ID == pool.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("model pool with ID %s not found", pool.ID)
	}

	oldname := old.Pools[idx].Name

	newPools := make([]*core.ModelPool, len(old.Pools))
	copy(newPools, old.Pools)
	newPools[idx] = pool

	newpoolLookups := make(map[string]*core.ModelPool, len(old.ModelPoolLookup))
	maps.Copy(newpoolLookups, old.ModelPoolLookup)

	if oldname != pool.Name {
		delete(newpoolLookups, oldname)
	}
	newpoolLookups[pool.Name] = pool
	m.Registry.Store(&modelSnapshot{
		ModelLookup:     old.ModelLookup,
		Deployments:     old.Deployments,
		ModelPoolLookup: newpoolLookups,
		Pools:           newPools,
	})
	return nil
}

func (m *ModelRegistry) DeletePool(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.Registry.Load()
	idx := -1
	for i, p := range old.Pools {
		if p.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("pool with ID %s not found", id)
	}
	name := old.Pools[idx].Name

	newPools := make([]*core.ModelPool, 0, len(old.Pools)-1)
	newPools = append(newPools, old.Pools[:idx]...)
	newPools = append(newPools, old.Pools[idx+1:]...)

	newLookup := make(map[string]*core.ModelPool, len(old.ModelPoolLookup))
	maps.Copy(newLookup, old.ModelPoolLookup)
	delete(newLookup, name)

	m.Registry.Store(&modelSnapshot{
		ModelLookup:     old.ModelLookup,
		Deployments:     old.Deployments,
		ModelPoolLookup: newLookup,
		Pools:           newPools,
	})
	return nil
}

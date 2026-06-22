package core

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

type Deployment struct {
	ID            string
	ModelProvider Provider
	ModelName     string
	BaseModelName string
	IsActive      bool
	Endpoint      string

	Timeout    int
	Credential *Credential
	State      *DeploymentState

	HealthCheck    bool
	HealthEndpoint string
}

func (d *Deployment) Key() ModelKey {
	return ModelKey{Provider: d.ModelProvider, ModelName: d.ModelName}
}

type ModelPool struct {
	ID           string         `json:"id"`
	Name         string         `json:"name" binding:"required"`
	LBType       LBkind         `json:"lb_type" binding:"required"`
	AllowedModel []AllowedModel `json:"allowed_models" binding:"required"`
	IsActive     bool           `json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type registrySnapshot struct {
	ModelLookup     map[ModelKey][]*Deployment
	Deployments     []*Deployment
	ModelPoolLookup map[string]*ModelPool
	Pools           []*ModelPool
}

func buildRegistrySnapshot(deployments []*Deployment, pools []*ModelPool) *registrySnapshot {
	tempLookup := make(map[ModelKey][]*Deployment, 0)
	for _, deployment := range deployments {
		key := deployment.Key()
		tempLookup[key] = append(tempLookup[key], deployment)
	}

	poolLookup := make(map[string]*ModelPool, len(pools))
	for _, pool := range pools {
		poolLookup[pool.Name] = pool
	}

	return &registrySnapshot{
		ModelLookup:     tempLookup,
		Deployments:     deployments,
		ModelPoolLookup: poolLookup,
		Pools:           pools,
	}
}

type ModelRegistry struct {
	Registry atomic.Pointer[registrySnapshot]
	mu       sync.Mutex
}

func NewModelRegistry(deployments []*Deployment, pools []*ModelPool) *ModelRegistry {
	snapshot := &ModelRegistry{}
	snapshot.Registry.Store(buildRegistrySnapshot(deployments, pools))
	return snapshot
}

func (m *ModelRegistry) LookupModel(key ModelKey) ([]*Deployment, bool) {
	deployments, ok := m.Registry.Load().ModelLookup[key]
	return deployments, ok
}

func (m *ModelRegistry) LookupPool(name string) (*ModelPool, bool) {
	pool, ok := m.Registry.Load().ModelPoolLookup[name]
	return pool, ok
}

func (m *ModelRegistry) AddModel(deployment *Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	addkey := deployment.Key()

	oldmap := m.Registry.Load()

	var newmap map[ModelKey][]*Deployment
	if _, ok := oldmap.ModelLookup[addkey]; ok {
		newmap = make(map[ModelKey][]*Deployment, len(oldmap.ModelLookup))
	} else {
		newmap = make(map[ModelKey][]*Deployment, len(oldmap.ModelLookup)+1)
	}
	maps.Copy(newmap, oldmap.ModelLookup)

	oldDeployment := oldmap.ModelLookup[addkey]
	newDeployment := make([]*Deployment, len(oldDeployment)+1)
	copy(newDeployment, oldDeployment)
	newDeployment[len(newDeployment)-1] = deployment
	newmap[addkey] = newDeployment

	newlookups := make([]*Deployment, len(oldmap.Deployments)+1)
	copy(newlookups, oldmap.Deployments)
	newlookups[len(newlookups)-1] = deployment

	m.Registry.Store(&registrySnapshot{
		ModelLookup:     newmap,
		Deployments:     newlookups,
		Pools:           oldmap.Pools,
		ModelPoolLookup: oldmap.ModelPoolLookup,
	})
	return nil
}

func (m *ModelRegistry) UpdateModel(deployment *Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := m.Registry.Load()
	oldmap := snapshot.ModelLookup
	findKey := deployment.Key()

	mapIndex := -1
	for index, deploy := range oldmap[findKey] {
		if deploy.ID == deployment.ID {
			mapIndex = index
			break
		}
	}

	if mapIndex == -1 {
		return fmt.Errorf("deployment with ID %s not found in model map", deployment.ID)
	}

	newmap := make(map[ModelKey][]*Deployment, len(oldmap))
	maps.Copy(newmap, oldmap)

	newDeployments := make([]*Deployment, len(oldmap[findKey]))
	copy(newDeployments, oldmap[findKey])
	newDeployments[mapIndex] = deployment
	newmap[findKey] = newDeployments

	globalIndex := -1
	for index, deploy := range snapshot.Deployments {
		if deploy.ID == deployment.ID {
			globalIndex = index
			break
		}
	}

	if globalIndex == -1 {
		return fmt.Errorf("deployment with ID %s not found in global list", deployment.ID)
	}

	newlookups := make([]*Deployment, len(snapshot.Deployments))
	copy(newlookups, snapshot.Deployments)
	newlookups[globalIndex] = deployment
	m.Registry.Store(&registrySnapshot{
		ModelLookup:     newmap,
		Deployments:     newlookups,
		Pools:           snapshot.Pools,
		ModelPoolLookup: snapshot.ModelPoolLookup,
	})

	return nil
}

func (m *ModelRegistry) DeleteModel(modelkey ModelKey, id string) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := m.Registry.Load()
	oldmap := snapshot.ModelLookup

	mapFound := false
	newDeployments := make([]*Deployment, 0, len(oldmap[modelkey]))
	for _, deployment := range oldmap[modelkey] {
		if deployment.ID == id {
			mapFound = true
			continue
		}
		newDeployments = append(newDeployments, deployment)
	}

	if !mapFound {
		return fmt.Errorf("deployment with ID %s not found in model map", id)
	}

	newmap := make(map[ModelKey][]*Deployment, len(oldmap))
	maps.Copy(newmap, oldmap)
	if len(newDeployments) == 0 {
		delete(newmap, modelkey)
	} else {
		newmap[modelkey] = newDeployments
	}

	globalFound := false
	newlookups := make([]*Deployment, 0, len(snapshot.Deployments))
	for _, deployment := range snapshot.Deployments {
		if deployment.ID == id {
			globalFound = true
			continue
		}
		newlookups = append(newlookups, deployment)
	}

	if !globalFound {
		return fmt.Errorf("deployment with ID %s not found in global list", id)
	}

	m.Registry.Store(&registrySnapshot{
		ModelLookup:     newmap,
		Deployments:     newlookups,
		Pools:           snapshot.Pools,
		ModelPoolLookup: snapshot.ModelPoolLookup,
	})

	return nil
}

func (m *ModelRegistry) Replace(deployments []*Deployment, pools []*ModelPool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Registry.Store(buildRegistrySnapshot(deployments, pools))
}

func (m *ModelRegistry) AddPool(pool *ModelPool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.Registry.Load()
	if _, exists := old.ModelPoolLookup[pool.Name]; exists {
		return fmt.Errorf("pool %q already exists", pool.Name)
	}

	newLookup := make(map[string]*ModelPool, len(old.ModelPoolLookup)+1)
	maps.Copy(newLookup, old.ModelPoolLookup)
	newLookup[pool.Name] = pool

	newPools := make([]*ModelPool, len(old.Pools)+1)
	copy(newPools, old.Pools)
	newPools[len(newPools)-1] = pool

	m.Registry.Store(&registrySnapshot{
		ModelLookup:     old.ModelLookup,
		Deployments:     old.Deployments,
		ModelPoolLookup: newLookup,
		Pools:           newPools,
	})
	return nil
}

func (m *ModelRegistry) UpdatePool(pool *ModelPool) error {
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
		return fmt.Errorf("Model pool with ID %s not found", pool.ID)
	}

	oldname := old.Pools[idx].Name

	newPools := make([]*ModelPool, len(old.Pools))
	copy(newPools, old.Pools)
	newPools[idx] = pool

	newpoolLookups := make(map[string]*ModelPool, len(old.ModelPoolLookup))
	maps.Copy(newpoolLookups, old.ModelPoolLookup)

	if oldname != pool.Name {
		delete(newpoolLookups, oldname)
	}
	newpoolLookups[pool.Name] = pool
	m.Registry.Store(&registrySnapshot{
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

	newPools := make([]*ModelPool, 0, len(old.Pools)-1)
	newPools = append(newPools, old.Pools[:idx]...)
	newPools = append(newPools, old.Pools[idx+1:]...)

	newLookup := make(map[string]*ModelPool, len(old.ModelPoolLookup))
	maps.Copy(newLookup, old.ModelPoolLookup)
	delete(newLookup, name)

	m.Registry.Store(&registrySnapshot{
		ModelLookup:     old.ModelLookup,
		Deployments:     old.Deployments,
		ModelPoolLookup: newLookup,
		Pools:           newPools,
	})
	return nil
}

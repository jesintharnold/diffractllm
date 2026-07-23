package registry

import (
	"diffractllm/internal/core"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
)

type deploymentSnapshot struct {
	ModelLookup map[core.ModelKey][]*core.Deployment
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

func buildDeploymentSnapshot(apiRegistries []*core.ModelAPIRegistry, states *core.StateManager) *deploymentSnapshot {
	modelLookup := make(map[core.ModelKey][]*core.Deployment)
	for _, apiDetails := range apiRegistries {
		for _, deployment := range buildDeployments(apiDetails, states) {
			key := deployment.Key()
			modelLookup[key] = append(modelLookup[key], deployment)
		}
	}
	return &deploymentSnapshot{ModelLookup: modelLookup}
}

type DeploymentRegistry struct {
	state  atomic.Pointer[deploymentSnapshot]
	states *core.StateManager
	mu     sync.Mutex
}

func NewDeploymentRegistry(apiRegistries []*core.ModelAPIRegistry, states *core.StateManager) *DeploymentRegistry {
	registry := &DeploymentRegistry{states: states}
	registry.state.Store(buildDeploymentSnapshot(apiRegistries, states))
	return registry
}

func (registry *DeploymentRegistry) LookupModel(key core.ModelKey) ([]*core.Deployment, bool) {
	deployments, found := registry.state.Load().ModelLookup[key]
	return deployments, found
}

func (registry *DeploymentRegistry) SyncDeployments(apiDetails *core.ModelAPIRegistry) error {
	if apiDetails == nil || apiDetails.ID == "" {
		return fmt.Errorf("api registry with a non-empty ID is required")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	old := registry.state.Load()
	next := make(map[core.ModelKey][]*core.Deployment, len(old.ModelLookup))
	maps.Copy(next, old.ModelLookup)
	removedIDs := make(map[string]struct{})

	for key, bucket := range next {
		filtered := make([]*core.Deployment, 0, len(bucket))
		for _, deployment := range bucket {
			if deployment.APIRegistryID == apiDetails.ID {
				removedIDs[deployment.ID] = struct{}{}
				continue
			}
			filtered = append(filtered, deployment)
		}
		if len(filtered) == 0 {
			delete(next, key)
		} else if len(filtered) != len(bucket) {
			next[key] = filtered
		}
	}

	for _, deployment := range buildDeployments(apiDetails, registry.states) {
		key := deployment.Key()
		next[key] = append(next[key], deployment)
		delete(removedIDs, deployment.ID)
	}

	registry.state.Store(&deploymentSnapshot{ModelLookup: next})
	for id := range removedIDs {
		registry.states.Release(id)
	}
	return nil
}

func (registry *DeploymentRegistry) RemoveDeployments(apiRegistryID string) error {
	if apiRegistryID == "" {
		return fmt.Errorf("api registry ID is required")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	old := registry.state.Load()
	next := make(map[core.ModelKey][]*core.Deployment, len(old.ModelLookup))
	maps.Copy(next, old.ModelLookup)
	removedIDs := make([]string, 0)

	for key, bucket := range next {
		filtered := make([]*core.Deployment, 0, len(bucket))
		for _, deployment := range bucket {
			if deployment.APIRegistryID == apiRegistryID {
				removedIDs = append(removedIDs, deployment.ID)
				continue
			}
			filtered = append(filtered, deployment)
		}
		if len(filtered) == 0 {
			delete(next, key)
		} else if len(filtered) != len(bucket) {
			next[key] = filtered
		}
	}
	if len(removedIDs) == 0 {
		return fmt.Errorf("api registry %q not found", apiRegistryID)
	}

	registry.state.Store(&deploymentSnapshot{ModelLookup: next})
	for _, id := range removedIDs {
		registry.states.Release(id)
	}
	return nil
}

func (registry *DeploymentRegistry) Replace(apiRegistries []*core.ModelAPIRegistry) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.state.Store(buildDeploymentSnapshot(apiRegistries, registry.states))
}

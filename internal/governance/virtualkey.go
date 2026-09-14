package governance

import (
	"diffractllm/internal/core"

	"maps"

	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type VirtualKeyMap map[string]*core.VirtualKey

type VirtualkeyCache struct {
	virtual  atomic.Pointer[VirtualKeyMap]
	mu       sync.Mutex
	LastSync time.Time
	logger   *zap.Logger
}

func (vk *VirtualkeyCache) LookupVkey(key string) (*core.VirtualKey, bool) {
	v := vk.virtual.Load()
	if v == nil {
		return nil, false
	}
	entry, exists := (*v)[key]
	return entry, exists
}

func (vk *VirtualkeyCache) LoadVirtualKeys(vdata []*core.VirtualKey) {
	tempVkey := make(VirtualKeyMap, len(vdata))
	for _, vkey := range vdata {
		tempVkey[vkey.Key] = vkey
	}
	vk.mu.Lock()
	defer vk.mu.Unlock()
	vk.virtual.Store(&tempVkey)
	vk.LastSync = time.Now()
	vk.logger.Debug("virtual key cache hot-swapped", zap.Int("keys", len(tempVkey)))
}

func (vk *VirtualkeyCache) UpsertVirtualKey(key *core.VirtualKey) {
	if key == nil {
		return
	}
	vk.mu.Lock()
	defer vk.mu.Unlock()
	next := vk.clone()
	next[key.Key] = key
	vk.virtual.Store(&next)
}

func (vk *VirtualkeyCache) DeleteVirtualKeyByID(id string) bool {
	vk.mu.Lock()
	defer vk.mu.Unlock()
	next := vk.clone()
	for key, entry := range next {
		if entry.ID == id {
			delete(next, key)
			vk.virtual.Store(&next)
			return true
		}
	}
	return false
}

func (vk *VirtualkeyCache) clone() VirtualKeyMap {
	old := vk.virtual.Load()
	if old == nil {
		return make(VirtualKeyMap)
	}
	return maps.Clone(*old)
}

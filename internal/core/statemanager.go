package core

import (
	"sync"
	"sync/atomic"
	"time"
)

type DeploymentState struct {
	Alive              atomic.Int32
	TripUntil          atomic.Int64
	activeConnections  atomic.Int32
	ConsecutiveFails   atomic.Int32
	ConsecutiveSuccess atomic.Int32
	lastSeen           atomic.Int64
}

func (d *DeploymentState) AddConnection() {
	d.activeConnections.Add(1)
}

func (d *DeploymentState) RemoveConnection() {
	if d.activeConnections.Load() > 0 {
		d.activeConnections.Add(-1)
	}
}

func (d *DeploymentState) GetConnections() int32 {
	return d.activeConnections.Load()
}

func (d *DeploymentState) SetAliveState(alive int32) {
	d.Alive.Store(alive)
}

func (d *DeploymentState) SetTripUntil(s time.Duration) {
	if s > 0 {
		futureTime := time.Now().Add(s).Unix()
		d.TripUntil.Store(futureTime)
	}
}

func (d *DeploymentState) IsHealthy() bool {
	if d.Alive.Load() == 0 {
		return false
	}
	if t := d.TripUntil.Load(); t > time.Now().Unix() {
		return false
	}
	return true
}

type StateManager struct {
	States map[string]*DeploymentState
	mu     sync.RWMutex
}

func NewStateManager() *StateManager {
	return &StateManager{
		States: make(map[string]*DeploymentState),
	}
}

func (m *StateManager) Acquire(id string, alive int32) *DeploymentState {
	now := time.Now().Unix()
	m.mu.RLock()
	if s, ok := m.States[id]; ok {
		m.mu.RUnlock()
		s.lastSeen.Store(now)
		return s
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.States[id]; ok {
		s.lastSeen.Store(now)
		return s
	}

	s := &DeploymentState{}
	s.SetAliveState(alive)
	s.lastSeen.Store(now)
	m.States[id] = s
	return s
}

func (m *StateManager) Release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.States, id)
}

// func (m *StateManager) Sweep(grace time.Duration) {
// 	cutoff := time.Now().Add(-grace).UnixNano()
// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	for id, s := range m.States {
// 		if s.lastSeen.Load() < cutoff {
// 			delete(m.States, id)
// 		}
// 	}
// }

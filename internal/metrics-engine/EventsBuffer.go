package metricsengine

import (
	"sync"
	"sync/atomic"
)

const DefaultEventsBufferCapacity = 5000

type EventsBuffer struct {
	lock        sync.Mutex
	events      []*Event
	maxCapacity int
	dropped     atomic.Int64
}

func NewEventsBuffer(maxCapacity int) *EventsBuffer {
	if maxCapacity <= 0 {
		maxCapacity = DefaultEventsBufferCapacity
	}
	return &EventsBuffer{
		events:      make([]*Event, 0, maxCapacity),
		maxCapacity: maxCapacity,
	}
}

func (b *EventsBuffer) Append(e *Event) bool {
	b.lock.Lock()
	if len(b.events) >= b.maxCapacity {
		b.lock.Unlock()
		b.dropped.Add(1)
		return false
	}
	b.events = append(b.events, e)
	b.lock.Unlock()
	return true
}

func (b *EventsBuffer) Drain() []*Event {
	b.lock.Lock()
	out := b.events
	b.events = make([]*Event, 0, cap(out))
	b.lock.Unlock()
	return out
}

func (b *EventsBuffer) Len() int {
	b.lock.Lock()
	defer b.lock.Unlock()
	return len(b.events)
}

func (b *EventsBuffer) DroppedCount() int64 {
	return b.dropped.Load()
}

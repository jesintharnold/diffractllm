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

// Append returns the buffer length after the append, or ok=false when full.
func (b *EventsBuffer) Append(e *Event) (n int, ok bool) {
	b.lock.Lock()
	if len(b.events) >= b.maxCapacity {
		b.lock.Unlock()
		b.dropped.Add(1)
		return 0, false
	}
	b.events = append(b.events, e)
	n = len(b.events)
	b.lock.Unlock()
	return n, true
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

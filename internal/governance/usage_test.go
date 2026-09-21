package governance

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---------- fixtures owned by this file ----------

func uRec(cost int64) UsageRecord {
	return UsageRecord{
		ClientID: "client-1", BudgetID: "budget-1", Backend: "openai",
		ModelName: "gpt-4o", InputTokens: 10, OutputTokens: 5,
		Cost: cost, RequestedAt: time.Now(),
	}
}
func TestNewUsageBufferCapacity(t *testing.T) {
	tests := []struct {
		name string
		give int
		want int
	}{
		{name: "zero falls back to the default", give: 0, want: DefaultUsageBufferCapacity},
		{name: "negative falls back to the default", give: -5, want: DefaultUsageBufferCapacity},
		{name: "explicit capacity is honoured", give: 50, want: 50},
		{name: "one", give: 1, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ub := NewUsageBuffer(tc.give, zap.NewNop())
			assert.Equal(t, tc.want, ub.maxCapacity)
			assert.Zero(t, ub.Len())
			assert.Zero(t, ub.DroppedCount())
		})
	}
}

// The backing slice is preallocated but capped, so a 100k buffer does not
// reserve 100k records up front.
func TestNewUsageBufferPreallocationIsBounded(t *testing.T) {
	assert.Equal(t, 4096, cap(NewUsageBuffer(DefaultUsageBufferCapacity, zap.NewNop()).records))
	assert.Equal(t, 10, cap(NewUsageBuffer(10, zap.NewNop()).records))
}

func TestAppendAndLen(t *testing.T) {
	ub := NewUsageBuffer(10, zap.NewNop())

	ub.Append(uRec(1))
	ub.Append(uRec(2))

	assert.Equal(t, 2, ub.Len())
	assert.Zero(t, ub.DroppedCount())
}

// At capacity the record is dropped and counted. The budget total stays correct
// because it is a separate counter; only the per-request ledger loses a row.
func TestAppendAtCapacityDrops(t *testing.T) {
	ub := NewUsageBuffer(3, zap.NewNop())

	for i := range 3 {
		ub.Append(uRec(int64(i)))
	}
	require.Equal(t, 3, ub.Len())
	require.Zero(t, ub.DroppedCount(), "exactly at capacity is still accepted")

	ub.Append(uRec(99))
	assert.Equal(t, 3, ub.Len(), "the buffer must not grow past its cap")
	assert.EqualValues(t, 1, ub.DroppedCount())

	ub.Append(uRec(100))
	assert.EqualValues(t, 2, ub.DroppedCount(), "every drop is counted")
}

// Draining frees the room, so drops stop once the flush has run.
func TestDrainMakesRoomAgain(t *testing.T) {
	ub := NewUsageBuffer(2, zap.NewNop())
	ub.Append(uRec(1))
	ub.Append(uRec(2))
	ub.Append(uRec(3))
	require.EqualValues(t, 1, ub.DroppedCount())

	drained := ub.Drain()
	require.Len(t, drained, 2)

	ub.Append(uRec(4))
	assert.Equal(t, 1, ub.Len())
	assert.EqualValues(t, 1, ub.DroppedCount(), "the drop count is cumulative, not per window")
}

func TestDrain(t *testing.T) {
	ub := NewUsageBuffer(10, zap.NewNop())
	ub.Append(uRec(1))
	ub.Append(uRec(2))
	ub.Append(uRec(3))

	got := ub.Drain()
	require.Len(t, got, 3)
	assert.EqualValues(t, 1, got[0].Cost, "insertion order is preserved")
	assert.EqualValues(t, 3, got[2].Cost)

	assert.Zero(t, ub.Len(), "draining empties the buffer")
	assert.Empty(t, ub.Drain(), "a second drain has nothing")
}

func TestDrainEmptyBuffer(t *testing.T) {
	assert.Empty(t, NewUsageBuffer(10, zap.NewNop()).Drain())
}

// Drain hands the caller the slice it was accumulating into and installs a new
// one, so records appended afterwards cannot appear in what was already handed
// out - which is what makes the re-enqueue path on a failed flush safe.
func TestDrainDetachesTheReturnedSlice(t *testing.T) {
	ub := NewUsageBuffer(10, zap.NewNop())
	ub.Append(uRec(1))

	got := ub.Drain()
	require.Len(t, got, 1)

	ub.Append(uRec(2))

	require.Len(t, got, 1, "the drained slice must not see later appends")
	assert.EqualValues(t, 1, got[0].Cost)
	assert.Equal(t, 1, ub.Len())
}

func TestUsageBufferConcurrentAppendAndDrain(t *testing.T) {
	ub := NewUsageBuffer(100_000, zap.NewNop())

	const writers, each = 8, 500
	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			for range each {
				ub.Append(uRec(1))
			}
		}()
	}

	// Drain concurrently and keep a running total: no record may be lost or
	// counted twice.
	var collected int
	stop := make(chan struct{})
	drained := make(chan int, 1)
	go func() {
		total := 0
		for {
			select {
			case <-stop:
				total += len(ub.Drain())
				drained <- total
				return
			default:
				total += len(ub.Drain())
			}
		}
	}()

	wg.Wait()
	close(stop)
	collected = <-drained

	assert.Equal(t, writers*each, collected)
	assert.Zero(t, ub.Len())
	assert.Zero(t, ub.DroppedCount())
}

func TestDroppedCountIsReadableWhileAppending(t *testing.T) {
	ub := NewUsageBuffer(1, zap.NewNop())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			ub.Append(uRec(1))
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			_ = ub.DroppedCount()
			_ = ub.Len()
		}
	}()
	wg.Wait()

	assert.EqualValues(t, 199, ub.DroppedCount())
	assert.Equal(t, 1, ub.Len())
}

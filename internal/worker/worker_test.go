package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingGroup(t *testing.T, interval time.Duration) (*Group, *atomic.Int32) {
	t.Helper()
	var runs atomic.Int32
	g := NewGroup("test")
	require.NoError(t, g.Add(&Job{Name: "job", Interval: interval, Run: func(context.Context) (Detail, error) {
		runs.Add(1)
		return nil, nil
	}}))
	require.NoError(t, g.Start(context.Background()))
	t.Cleanup(func() { _ = g.Shutdown(context.Background()) })
	return g, &runs
}

// A paused job skips its ticks but still runs when triggered by hand.
func TestPausedJobOnlyRunsOnTrigger(t *testing.T) {
	g, runs := countingGroup(t, 10*time.Millisecond)
	require.NoError(t, g.SetPaused("job", true))
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int32(0), runs.Load(), "no scheduled runs while paused")

	require.NoError(t, g.Trigger("job"))
	assert.Eventually(t, func() bool { return runs.Load() == 1 }, time.Second, 5*time.Millisecond)
}

// A crowd of admins pressing the same button: runs never overlap, and the pile-up collapses to the run
// in flight plus at most one queued behind it.
func TestConcurrentTriggersCoalesce(t *testing.T) {
	var running, runs atomic.Int32
	var overlapped atomic.Bool
	release := make(chan struct{})

	g := NewGroup("test")
	require.NoError(t, g.Add(&Job{Name: "job", Interval: time.Hour, Run: func(context.Context) (Detail, error) {
		if running.Add(1) > 1 {
			overlapped.Store(true)
		}
		runs.Add(1)
		<-release
		running.Add(-1)
		return nil, nil
	}}))
	require.NoError(t, g.Start(context.Background()))
	t.Cleanup(func() { _ = g.Shutdown(context.Background()) })

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, g.Trigger("job"))
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	close(release)

	assert.Eventually(t, func() bool { return runs.Load() >= 1 && running.Load() == 0 }, time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond) // anything left over would run now
	assert.LessOrEqual(t, runs.Load(), int32(2), "100 triggers must not become 100 runs")
	assert.False(t, overlapped.Load(), "a job never runs twice at once")
}

// Every by-name call reports a missing job the same way, so a caller can tell it from a failure.
func TestUnknownJobIsReportedByName(t *testing.T) {
	g, _ := countingGroup(t, time.Hour)

	assert.ErrorIs(t, g.Trigger("missing"), ErrUnknownJob)
	assert.ErrorIs(t, g.Reschedule("missing", time.Second), ErrUnknownJob)
	assert.ErrorIs(t, g.SetPaused("missing", true), ErrUnknownJob)
	assert.NoError(t, g.Trigger("job"))
}

// A running job picks up a new interval without a restart.
func TestRescheduleTakesEffectLive(t *testing.T) {
	g, runs := countingGroup(t, time.Hour)
	require.NoError(t, g.Reschedule("job", 10*time.Millisecond))
	assert.Eventually(t, func() bool { return runs.Load() >= 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, 10*time.Millisecond, g.Stats()[0].Interval)

	assert.Error(t, g.Reschedule("job", 0))
	assert.Error(t, g.Reschedule("missing", time.Second))
}

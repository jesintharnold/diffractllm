package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func countingGroup(t *testing.T, interval time.Duration) (*Group, *atomic.Int32) {
	t.Helper()
	var runs atomic.Int32
	g := NewGroup("test", zap.NewNop())
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

// A running job picks up a new interval without a restart.
func TestRescheduleTakesEffectLive(t *testing.T) {
	g, runs := countingGroup(t, time.Hour)
	require.NoError(t, g.Reschedule("job", 10*time.Millisecond))
	assert.Eventually(t, func() bool { return runs.Load() >= 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, 10*time.Millisecond, g.Stats()[0].Interval)

	assert.Error(t, g.Reschedule("job", 0))
	assert.Error(t, g.Reschedule("missing", time.Second))
}

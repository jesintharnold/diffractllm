package worker

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Detail map[string]any

type JobStats struct {
	Name                string        `json:"name"`
	Interval            time.Duration `json:"interval"`
	Running             bool          `json:"running"`
	LastRunAt           time.Time     `json:"last_run_at,omitzero"`
	LastSuccessAt       time.Time     `json:"last_success_at,omitzero"`
	LastDuration        time.Duration `json:"last_duration"`
	LastError           string        `json:"last_error,omitempty"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	LastDetail          Detail        `json:"last_detail,omitempty"`
}

type Job struct {
	Name       string
	Interval   time.Duration
	RunAtStart bool
	RunAtStop  bool
	Run        func(ctx context.Context) (Detail, error)
	mu         sync.Mutex
	stats      JobStats
	trigger    chan struct{}
}

func (j *Job) Stats() JobStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stats
}

func (j *Job) markRunning(start time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stats.Name = j.Name
	j.stats.Interval = j.Interval
	j.stats.Running = true
	j.stats.LastRunAt = start
}

func (j *Job) validate(groupName string) error {
	switch {
	case j == nil:
		return fmt.Errorf("worker %s: nil job", groupName)
	case j.Name == "":
		return fmt.Errorf("worker %s: job needs a name", groupName)
	case j.Run == nil:
		return fmt.Errorf("worker %s: job %q has no Run", groupName, j.Name)
	case j.Interval <= 0:
		return fmt.Errorf("worker %s: job %q needs a positive interval", groupName, j.Name)
	}
	return nil
}

func (j *Job) markDone(start time.Time, detail Detail, err error, took time.Duration) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stats.Running = false
	j.stats.LastDuration = took
	j.stats.LastDetail = detail

	if err != nil {
		j.stats.LastError = err.Error()
		j.stats.ConsecutiveFailures++
		return
	}
	j.stats.LastSuccessAt = start
	j.stats.LastError = ""
	j.stats.ConsecutiveFailures = 0
}

func (j *Job) exec(ctx context.Context, log *zap.Logger) error {
	start := time.Now()
	j.markRunning(start)
	var (
		detail Detail
		err    error
	)

	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
				log.Error("job panicked", zap.String("job", j.Name), zap.Any("panic", p), zap.String("stack", string(debug.Stack())))
			}
		}()
		detail, err = j.Run(ctx)
	}()

	timeTaken := time.Since(start)
	j.markDone(start, detail, err, timeTaken)
	if err != nil {
		log.Error("job failed", zap.String("job", j.Name), zap.Error(err))
	}
	return err
}

func (j *Job) loop(ctx context.Context, stop <-chan struct{}, log *zap.Logger) {
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			j.exec(ctx, log)
		case <-j.trigger:
			j.exec(ctx, log)
			ticker.Reset(j.Interval)
		case <-stop:
			if j.RunAtStop {
				_ = j.exec(ctx, log)
			}
			return
		}
	}

}

// -------------------- GROUP ----------------
type Group struct {
	name   string
	logger *zap.Logger

	mu      sync.Mutex
	jobs    map[string]*Job
	started bool

	stop chan struct{}
	wg   sync.WaitGroup
}

func NewGroup(name string, logger *zap.Logger) *Group {
	return &Group{
		name:   name,
		logger: logger,
		jobs:   make(map[string]*Job),
	}
}

func (g *Group) Add(jobs ...*Job) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return fmt.Errorf("worker %s: Add after Start", g.name)
	}

	for _, job := range jobs {
		if err := job.validate(g.name); err != nil {
			return err
		}
		if _, exists := g.jobs[job.Name]; exists {
			return fmt.Errorf("worker %s: duplicate job %q", g.name, job.Name)
		}

		job.trigger = make(chan struct{}, 1)
		job.stats.Name = job.Name
		job.stats.Interval = job.Interval
		g.jobs[job.Name] = job
	}
	return nil
}

func (g *Group) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return fmt.Errorf("worker %s: already started", g.name)
	}
	g.started = true
	g.stop = make(chan struct{})

	jobs := make([]*Job, 0, len(g.jobs))
	for _, job := range g.jobs {
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b *Job) int { return strings.Compare(a.Name, b.Name) })
	g.mu.Unlock()

	for _, job := range jobs {
		if job.RunAtStart {
			if err := job.exec(ctx, g.logger); err != nil {
				return fmt.Errorf("worker %s: job %q failed at start: %w", g.name, job.Name, err)
			}
		}
	}

	for _, job := range jobs {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			job.loop(ctx, g.stop, g.logger)
		}()
	}

	g.logger.Info("workers started", zap.String("group", g.name), zap.Int("jobs", len(jobs)))
	return nil
}

func (g *Group) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = false
	close(g.stop)
	g.mu.Unlock()

	exited := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(exited)
	}()

	select {
	case <-exited:
		g.logger.Info("workers stopped", zap.String("group", g.name))
		return nil
	case <-ctx.Done():
		return fmt.Errorf("worker %s: did not stop in time: %w", g.name, ctx.Err())
	}
}
func (g *Group) Trigger(name string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	j, ok := g.jobs[name]
	if !ok {
		return fmt.Errorf("worker %s: no job named %q", g.name, name)
	}

	// Send the trigger
	select {
	case j.trigger <- struct{}{}:
	default:
	}
	return nil
}

func (g *Group) Stats() []JobStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]JobStats, 0, len(g.jobs))
	for _, job := range g.jobs {
		out = append(out, job.Stats())
	}
	slices.SortFunc(out, func(a, b JobStats) int { return strings.Compare(a.Name, b.Name) })
	return out
}

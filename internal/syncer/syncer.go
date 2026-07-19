package syncer

import (
	"diffractllm/internal/governance"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Config struct {
	UsageFlushInterval   time.Duration
	BudgetFlushInterval  time.Duration
	BudgetWindowInterval time.Duration
	PriceSyncInterval    time.Duration
}

type Job struct {
	name string
	fn   func()
	stop chan struct{}
	done chan struct{}
}

type Syncer struct {
	governance *governance.Governance
	mu         sync.Mutex
	logger     *zap.Logger
	Jobs       map[string]*Job
	cfg        *Config
}

func NewSyncer(gov *governance.Governance, log *zap.Logger) *Syncer {
	return &Syncer{
		governance: gov,
		logger:     log,
		Jobs:       make(map[string]*Job, 3),
		cfg: &Config{
			UsageFlushInterval:   10 * time.Second,
			BudgetFlushInterval:  10 * time.Second,
			BudgetWindowInterval: 10 * time.Second,
			PriceSyncInterval:    60 * time.Second,
		},
	}
}

func (s *Syncer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger.Info("Starting all syncer background jobs")
	s.startJobsLocked()
	return nil
}

func (s *Syncer) Stop() {
	s.mu.Lock()
	tempJobs := s.Jobs
	s.Jobs = make(map[string]*Job)
	s.mu.Unlock()
	s.logger.Info("Stopping all syncer background jobs", zap.Int("job_count", len(tempJobs)))
	for _, job := range tempJobs {
		close(job.stop)
	}
	for _, job := range tempJobs {
		<-job.done
	}

	s.governance.FlushBudgetUsage()
	s.governance.FlushUsageHistory()
	s.logger.Info("All syncer background jobs stopped successfully")
}

func (s *Syncer) StopJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.Jobs[name]
	if !ok {
		return fmt.Errorf("syncer: no jobs found for - %s", name)
	}
	delete(s.Jobs, name)
	s.logger.Info("Stopping syncer job", zap.String("job_name", name))
	close(job.stop)
	<-job.done
	return nil
}

func (s *Syncer) ReconfigureJob(name string, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("syncer: invalid interval %v for job - %s", interval, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tempjob, ok := s.Jobs[name]
	if !ok {
		return fmt.Errorf("syncer: no job found with this name - %s", name)
	}
	close(tempjob.stop)
	<-tempjob.done
	s.spawnJob(name, interval, tempjob.fn)
	return nil
}

func (s *Syncer) Reconfigure(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old := *s.cfg
	s.cfg = &cfg

	if cfg.UsageFlushInterval != old.UsageFlushInterval {
		s.restartJobLocked("usage_flush", cfg.UsageFlushInterval, s.governance.FlushUsageHistory)
	}
	if cfg.BudgetFlushInterval != old.BudgetFlushInterval {
		s.restartJobLocked("budget_flush", cfg.BudgetFlushInterval, s.governance.FlushBudgetUsage)
	}
	if cfg.BudgetWindowInterval != old.BudgetWindowInterval {
		s.restartJobLocked("budget_window", cfg.BudgetWindowInterval, s.governance.TrackBudgetWindow)
	}
}

func (s *Syncer) restartJobLocked(name string, interval time.Duration, fn func()) {
	if job, ok := s.Jobs[name]; ok {
		close(job.stop)
		<-job.done
		delete(s.Jobs, name)
	}
	if interval > 0 {
		s.spawnJob(name, interval, fn)
	}
	s.logger.Info("Syncer job interval changed", zap.String("job_name", name), zap.Duration("interval", interval))
}

func (s *Syncer) spawnJob(name string, interval time.Duration, fn func()) {
	job := &Job{
		fn:   fn,
		name: name,
		stop: make(chan struct{}, 1),
		done: make(chan struct{}, 1),
	}
	s.Jobs[name] = job
	go func() {
		defer close(job.done)
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("Syncer job goroutine crashed",
					zap.String("job_name", name),
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
				)
			}
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.logger.Debug("Syncer job spawned", zap.String("job_name", name), zap.Duration("interval", interval))
		for {
			select {
			case <-ticker.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							s.logger.Error("Syncer job execution panicked",
								zap.String("job_name", name),
								zap.Any("panic", r),
								zap.String("stack", string(debug.Stack())),
							)
						}
					}()
					fn()
				}()
			case <-job.stop:
				s.logger.Debug("Syncer job loop exiting", zap.String("job_name", name))
				return
			}
		}
	}()
}

func (s *Syncer) startJobsLocked() {
	if s.cfg.UsageFlushInterval > 0 {
		s.spawnJob("usage_flush", s.cfg.UsageFlushInterval, s.governance.FlushUsageHistory)
	}
	if s.cfg.BudgetFlushInterval > 0 {
		s.spawnJob("budget_flush", s.cfg.BudgetFlushInterval, s.governance.FlushBudgetUsage)
	}
	if s.cfg.BudgetWindowInterval > 0 {
		s.spawnJob("budget_window", s.cfg.BudgetWindowInterval, s.governance.TrackBudgetWindow)
	}
}

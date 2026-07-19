package syncer

import (
	"diffractllm/internal/governance"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

type syncConfig struct {
	UsageFlushInterval   time.Duration
	BudgetFlushInterval  time.Duration
	BudgetWindowInterval time.Duration
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
	wg         sync.WaitGroup
	Jobs       map[string]*Job
	cfg        *syncConfig
}

func NewSyncer(gov *governance.Governance, log *zap.Logger) *Syncer {
	return &Syncer{
		governance: gov,
		logger:     log,
		Jobs:       make(map[string]*Job, 3),
		cfg: &syncConfig{
			UsageFlushInterval:   60 * time.Second,
			BudgetFlushInterval:  60 * time.Second,
			BudgetWindowInterval: 30 * time.Second,
		},
	}
}

func (s *Syncer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawnJob("usage_flush")
	return nil
}

func (s *Syncer) Stop() {
	s.mu.Lock()
	tempJobs := s.Jobs
	for _, job := range tempJobs {
		close(job.done)
	}
	s.Jobs = make(map[string]*Job)
	s.mu.Unlock()

	for _, job := range tempJobs {
		<-job.done
	}
	return
}

func (s *Syncer) StopJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.Jobs[name]
	if !ok {
		return fmt.Errorf("syncer: no jobs found for - %s", name)
	}

	close(job.stop)
	<-job.done
	delete(s.Jobs, name)
	return nil
}

func (s *Syncer) ReconfigureJob(name string, interval time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tempjob, ok := s.Jobs[name]
	if !ok {
		return fmt.Errorf("syncer: no job found with this name - %s", name)
	}
	close(tempjob.done)
	<-tempjob.done
	s.spawnJob(name, interval, tempjob.fn)
	return nil
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
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fn()
			case <-job.stop:
				return
			}
		}
	}()
}
 
//go:build ignore

// Excluded alongside modelcatalog.go: the lifecycle here drives that file's
// ModelCatalog. The Start/Stop/loop structure is sound and comes back as-is in
// the catalog pass.

package modelcatalog

import (
	"context"
	config "diffractllm/configs"
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
)

const (
	JobModelSync   = "model_sync"
	JobPricingSync = "pricing_sync"
)

func (c *ModelCatalog) Start() error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("model catalog: already started")
	}
	cfg := c.cfg
	c.mu.Unlock()

	if err := c.LoadModels(); err != nil {
		return fmt.Errorf("model catalog start: %w", err)
	}
	if err := c.loadPricing(); err != nil {
		return fmt.Errorf("model catalog start: %w", err)
	}

	done := make(chan struct{})

	c.mu.Lock()
	c.started, c.done = true, done
	c.mu.Unlock()

	c.wg.Add(2)
	go c.loop(done, JobModelSync, cfg.SyncInterval, c.LoadModels)
	go c.loop(done, JobPricingSync, cfg.SyncInterval, c.LoadBasePricing)

	c.logger.Info("model catalog started",
		zap.Duration("interval", cfg.SyncInterval),
		zap.String("source", cfg.SourceName))
	return nil
}

func (c *ModelCatalog) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	done := c.done
	c.done = nil

	c.mu.Unlock()

	close(done)

	exited := make(chan struct{})
	go func() { c.wg.Wait(); close(exited) }()

	select {
	case <-exited:
		c.logger.Info("model catalog stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("model catalog: loops did not stop in time: %w", ctx.Err())
	}
}

func (c *ModelCatalog) Reconfigure(ctx context.Context, cfg config.ModelCatalogConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("model catalog reconfigure: %w", err)
	}
	if err := c.Stop(ctx); err != nil {
		return fmt.Errorf("model catalog reconfigure: %w", err)
	}

	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()

	return c.Start()
}

func (c *ModelCatalog) loadPricing() error {
	if err := c.LoadBasePricing(); err != nil {
		return err
	}
	return c.LoadCustomPricing()
}

func (c *ModelCatalog) loop(done <-chan struct{}, name string, interval time.Duration, fn func() error) {
	defer c.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("model catalog loop crashed",
				zap.String("job", name), zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						c.logger.Error("model catalog sync panicked",
							zap.String("job", name), zap.Any("panic", r),
							zap.String("stack", string(debug.Stack())))
					}
				}()
				if err := fn(); err != nil {
					c.logger.Error("model catalog sync failed, serving previous snapshot",
						zap.String("job", name), zap.Error(err))
				}
			}()
		case <-done:
			c.logger.Debug("model catalog loop exiting", zap.String("job", name))
			return
		}
	}
}

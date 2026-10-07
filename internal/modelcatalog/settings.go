package modelcatalog

import (
	"errors"
	"fmt"
	"time"

	config "diffractllm/configs"
	"diffractllm/internal/dbstore"

	"go.uber.org/zap"
)

const (
	MissingPriceReject     = "reject"
	MissingPriceChargeZero = "charge_zero"
)

const (
	LedgerUnit      = "nano-USD"
	DisplayCurrency = "USD"
)

const (
	defaultFetchTimeout = 10 * time.Second
	maxFetchTimeout     = 5 * time.Minute
	maxSyncInterval     = 7 * 24 * time.Hour
)

type Settings struct {
	AutoSync        bool   `json:"auto_sync"`
	IntervalSeconds int64  `json:"interval_seconds"`
	TimeoutSeconds  int64  `json:"timeout_seconds"`
	MissingPrice    string `json:"missing_price"`
	LedgerUnit      string `json:"ledger_unit"`
	DisplayCurrency string `json:"display_currency"`
}

var ErrInvalidSettings = errors.New("invalid catalog settings")

func (s Settings) Interval() time.Duration { return time.Duration(s.IntervalSeconds) * time.Second }
func (s Settings) Timeout() time.Duration  { return time.Duration(s.TimeoutSeconds) * time.Second }

func (s Settings) Validate() error {
	switch {
	case s.Interval() < config.MinSyncInterval || s.Interval() > maxSyncInterval:
		return fmt.Errorf("%w: interval must be between %s and %s", ErrInvalidSettings, config.MinSyncInterval, maxSyncInterval)
	case s.Timeout() < time.Second || s.Timeout() > maxFetchTimeout:
		return fmt.Errorf("%w: timeout must be between 1s and %s", ErrInvalidSettings, maxFetchTimeout)
	case s.MissingPrice != MissingPriceReject && s.MissingPrice != MissingPriceChargeZero:
		return fmt.Errorf("%w: missing_price must be %q or %q", ErrInvalidSettings, MissingPriceReject, MissingPriceChargeZero)
	case s.LedgerUnit != "" && s.LedgerUnit != LedgerUnit, s.DisplayCurrency != "" && s.DisplayCurrency != DisplayCurrency:
		return fmt.Errorf("%w: ledger_unit and display_currency are fixed at %s and %s", ErrInvalidSettings, LedgerUnit, DisplayCurrency)
	}
	return nil
}

func (s Settings) withFixed() Settings {
	s.LedgerUnit, s.DisplayCurrency = LedgerUnit, DisplayCurrency
	return s
}

func (c *ModelCatalog) defaultSettings() Settings {
	return Settings{
		AutoSync:        true,
		IntervalSeconds: int64(c.cfg.SyncInterval / time.Second),
		TimeoutSeconds:  int64(defaultFetchTimeout / time.Second),
		MissingPrice:    MissingPriceReject,
	}.withFixed()
}

func (c *ModelCatalog) Settings() Settings {
	if s := c.settings.Load(); s != nil {
		return *s
	}
	return c.defaultSettings()
}

func (c *ModelCatalog) MissingPrice() string { return c.Settings().MissingPrice }

func (c *ModelCatalog) loadSettings() error {
	row, err := c.store.GetCatalogSettings()
	if err != nil {
		return err
	}
	if row == nil {
		s := c.defaultSettings()
		if err := c.store.SaveCatalogSettings(toRow(s)); err != nil {
			return err
		}
		c.settings.Store(&s)
		return nil
	}
	s := Settings{
		AutoSync:        row.AutoSync,
		IntervalSeconds: row.IntervalSeconds,
		TimeoutSeconds:  row.TimeoutSeconds,
		MissingPrice:    row.MissingPrice,
	}.withFixed()
	if err := s.Validate(); err != nil {
		c.logger.Warn("saved catalog settings are invalid, using defaults", zap.Error(err))
		s = c.defaultSettings()
	}
	c.settings.Store(&s)
	return nil
}

func (c *ModelCatalog) UpdateSettings(next Settings) (Settings, error) {
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	next = next.withFixed()

	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()

	if err := c.store.SaveCatalogSettings(toRow(next)); err != nil {
		return Settings{}, err
	}
	if c.workers != nil {
		if err := c.workers.Reschedule(JobCatalogSync, next.Interval()); err != nil {
			return Settings{}, fmt.Errorf("stored, not yet live: %w", err)
		}
		if err := c.workers.SetPaused(JobCatalogSync, !next.AutoSync); err != nil {
			return Settings{}, fmt.Errorf("stored, not yet live: %w", err)
		}
	}
	c.settings.Store(&next)
	c.logger.Info("catalog settings updated",
		zap.Bool("auto_sync", next.AutoSync),
		zap.Duration("interval", next.Interval()),
		zap.Duration("timeout", next.Timeout()),
		zap.String("missing_price", next.MissingPrice))
	return next, nil
}

func toRow(s Settings) dbstore.StoreCatalogSettings {
	return dbstore.StoreCatalogSettings{
		AutoSync:        s.AutoSync,
		IntervalSeconds: s.IntervalSeconds,
		TimeoutSeconds:  s.TimeoutSeconds,
		MissingPrice:    s.MissingPrice,
	}
}

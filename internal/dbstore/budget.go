package dbstore

import (
	"diffractllm/internal/core"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A window shorter than the hourly metrics buckets, or longer than a year, is almost certainly a
// typo; "90m" would otherwise parse as 90 months.
const (
	MinBudgetWindow = time.Hour
	MaxBudgetWindow = 365 * 24 * time.Hour
)

var (
	ErrInvalidBudget   = errors.New("invalid budget")
	ErrBudgetNotFound  = errors.New("budget not found")
	ErrBudgetNameTaken = errors.New("a budget with this name already exists")
	ErrBudgetBound     = errors.New("it is bound to an active key")
)
func parseBudgetWindow(duration string) (time.Duration, error) {
	if duration == "" {
		return 0, fmt.Errorf("%w: budget_duration is required", ErrInvalidBudget)
	}
	d, err := core.ParseDuration(duration)
	if err != nil {
		return 0, fmt.Errorf("%w: budget_duration %q: %v", ErrInvalidBudget, duration, err)
	}
	if d < MinBudgetWindow || d > MaxBudgetWindow {
		return 0, fmt.Errorf("%w: budget_duration %q must be between 1 hour and 365 days", ErrInvalidBudget, duration)
	}
	return d, nil
}

type StoreBudget struct {
	ID                  string        `gorm:"primaryKey;type:text"`
	Name                string        `gorm:"uniqueIndex;not null;type:text"`
	BudgetLimit         int64         `gorm:"not null"`
	BudgetUnit          string        `gorm:"not null;default:'nanodollars'"`
	BudgetDuration      string        `gorm:"not null;type:varchar(10)"`
	Enforce             bool          `gorm:"not null"`
	TotalCost           int64         `gorm:"not null;default:0"`
	RequestCount        int64         `gorm:"not null;default:0"`
	Status              string        `gorm:"not null;default:'unbound';type:text"`
	LastBudgetRefreshAt time.Time     `gorm:"not null"`
	BudgetParseDuration time.Duration `gorm:"-"`
	LastFlushedAt       time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (StoreBudget) TableName() string { return "budgets" }

func (b *StoreBudget) AfterFind(tx *gorm.DB) error {
	if b.BudgetDuration == "" {
		b.BudgetParseDuration = 0
		return nil
	}
	d, err := core.ParseDuration(b.BudgetDuration)
	if err != nil {
		return fmt.Errorf("budget %q: invalid duration %q: %w", b.ID, b.BudgetDuration, err)
	}
	b.BudgetParseDuration = d
	return nil
}

func (b *StoreBudget) ToCore() *core.Budget {
	return &core.Budget{
		ID:                  b.ID,
		Name:                b.Name,
		BudgetLimit:         b.BudgetLimit,
		BudgetUnit:          b.BudgetUnit,
		BudgetDuration:      b.BudgetDuration,
		Enforce:             &b.Enforce,
		TotalSpend:          b.TotalCost,
		RequestCount:        b.RequestCount,
		Status:              b.Status,
		LastBudgetRefreshAt: b.LastBudgetRefreshAt,
		BudgetParseDuration: b.BudgetParseDuration,
		CreatedAt:           b.CreatedAt,
		UpdatedAt:           b.UpdatedAt,
	}
}

func (s *Store) CreateBudget(b core.Budget) (*StoreBudget, error) {
	if b.BudgetLimit <= 0 {
		return nil, fmt.Errorf("%w: budget_limit must be positive nano-USD, got %d", ErrInvalidBudget, b.BudgetLimit)
	}
	if _, err := parseBudgetWindow(b.BudgetDuration); err != nil {
		return nil, err
	}

	unit := b.BudgetUnit
	if unit == "" {
		unit = core.BudgetUnitNanoUSD
	}
	enforce := true
	if b.Enforce != nil {
		enforce = *b.Enforce
	}

	refreshedAt := b.LastBudgetRefreshAt
	if refreshedAt.IsZero() {
		refreshedAt = time.Now().UTC()
	}

	budget := StoreBudget{
		ID:                  uuid.Must(uuid.NewV7()).String(),
		Name:                b.Name,
		BudgetLimit:         b.BudgetLimit,
		BudgetUnit:          unit,
		BudgetDuration:      b.BudgetDuration,
		Enforce:             enforce,
		Status:              "unbound",
		LastBudgetRefreshAt: refreshedAt,
	}

	if err := s.DB.Create(&budget).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, fmt.Errorf("%w: %q", ErrBudgetNameTaken, b.Name)
		}
		return nil, fmt.Errorf("create budget: %w", err)
	}

	return &budget, nil
}

func (s *Store) GetBudget(budgetID string) (*StoreBudget, error) {
	var b StoreBudget
	if err := s.DB.Where("id = ?", budgetID).First(&b).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("budget %q: %w", budgetID, ErrBudgetNotFound)
		}
		return nil, fmt.Errorf("get budget %q: %w", budgetID, err)
	}
	return &b, nil
}

func (s *Store) ListBudgets() ([]StoreBudget, error) {
	var budgets []StoreBudget
	if err := s.DB.Find(&budgets).Error; err != nil {
		return nil, fmt.Errorf("failed to list budgets: %w", err)
	}
	return budgets, nil
}

func (s *Store) UpdateBudget(budget_id string, b core.Budget) (*StoreBudget, error) {

	existingBudget, err := s.GetBudget(budget_id)
	if err != nil {
		return nil, err
	}

	if b.BudgetLimit < 0 {
		return nil, fmt.Errorf("%w: budget_limit must be positive nano-USD, got %d", ErrInvalidBudget, b.BudgetLimit)
	}

	updates := map[string]any{}
	if b.BudgetLimit != 0 && existingBudget.BudgetLimit != b.BudgetLimit {
		updates["budget_limit"] = b.BudgetLimit
	}

	if b.BudgetDuration != "" && existingBudget.BudgetDuration != b.BudgetDuration {
		if _, err := parseBudgetWindow(b.BudgetDuration); err != nil {
			return nil, err
		}
		updates["budget_duration"] = b.BudgetDuration
	}

	if b.Enforce != nil && existingBudget.Enforce != *b.Enforce {
		updates["enforce"] = *b.Enforce
	}

	if len(updates) == 0 {
		return existingBudget, nil
	}

	if err := s.DB.Model(&StoreBudget{}).Where("id = ?", budget_id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update budget: %w", err)
	}

	return s.GetBudget(budget_id)
}

func (s *Store) UpdateBudgetEnforce(budgetID string, enforce bool) error {
	res := s.DB.Model(&StoreBudget{}).Where("id = ?", budgetID).Update("enforce", enforce)
	if res.Error != nil {
		return fmt.Errorf("failed to update enforce for budget %q: %w", budgetID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("budget %q not found", budgetID)
	}
	return nil
}

func (s *Store) DeleteBudget(budgetID string) error {
	b, err := s.GetBudget(budgetID)
	if err != nil {
		return err
	}
	if b.Status == "bound" {
		return fmt.Errorf("cannot delete budget %q: %w", budgetID, ErrBudgetBound)
	}
	return s.DB.Where("id = ?", budgetID).Delete(&StoreBudget{}).Error
}

func (s *Store) FlushBudgetUsage(budgetID string, totalCost, totalRequests int64) error {
	updates := map[string]any{
		"total_cost":      totalCost,
		"request_count":   totalRequests,
		"last_flushed_at": time.Now(),
	}
	return s.DB.Model(&StoreBudget{}).Where("id = ?", budgetID).UpdateColumns(updates).Error
}

func (s *Store) ResetBudgetWindow(budgetID string, resetTime time.Time) error {
	return s.DB.Model(&StoreBudget{}).Where("id = ?", budgetID).UpdateColumns(map[string]any{
		"total_cost":             0,
		"request_count":          0,
		"last_budget_refresh_at": resetTime,
	}).Error
}

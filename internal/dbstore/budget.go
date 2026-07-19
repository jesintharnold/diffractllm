package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreBudget struct {
	ID                  string        `gorm:"primaryKey;type:text"`
	Name                string        `gorm:"uniqueIndex;not null;type:text"`
	BudgetLimit         int64         `gorm:"not null"`
	BudgetUnit          string        `gorm:"not null;default:'microdollars'"`
	BudgetDuration      string        `gorm:"not null;type:varchar(10)"`
	Enforce             bool          `gorm:"not null;default:true"`
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
		Enforce:             b.Enforce,
		TotalSpend:          b.TotalCost,
		RequestCount:        b.RequestCount,
		Status:              b.Status,
		LastBudgetRefreshAt: b.LastBudgetRefreshAt,
		BudgetParseDuration: b.BudgetParseDuration,
	}
}

func (s *Store) CreateBudget(b core.Budget) (*StoreBudget, error) {
	unit := b.BudgetUnit
	if unit == "" {
		unit = "microdollars"
	}
	budget := StoreBudget{
		ID:                  uuid.Must(uuid.NewV7()).String(),
		Name:                b.Name,
		BudgetLimit:         b.BudgetLimit,
		BudgetUnit:          unit,
		BudgetDuration:      b.BudgetDuration,
		Enforce:             b.Enforce,
		Status:              "unbound",
		LastBudgetRefreshAt: b.LastBudgetRefreshAt,
	}

	if err := s.DB.Create(&budget).Error; err != nil {
		return nil, fmt.Errorf("create budget: %w", err)
	}

	return &budget, nil
}

func (s *Store) GetBudget(budgetID string) (*StoreBudget, error) {
	var b StoreBudget
	if err := s.DB.Where("id = ?", budgetID).First(&b).Error; err != nil {
		return nil, fmt.Errorf("budget %q not found: %w", budgetID, err)
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

	updates := map[string]any{}
	if b.BudgetLimit != 0 && existingBudget.BudgetLimit != b.BudgetLimit {
		updates["budget_limit"] = b.BudgetLimit
	}

	if b.BudgetDuration != "" && existingBudget.BudgetDuration != b.BudgetDuration {
		updates["budget_duration"] = b.BudgetDuration
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
		return fmt.Errorf("cannot delete budget %q: currently bound to an active key", budgetID)
	}
	return s.DB.Where("id = ?", budgetID).Delete(&StoreBudget{}).Error
}

func (s *Store) FlushBudgetUsage(budgetID string, spend, requests int64) error {
	updates := map[string]any{
		"total_cost":      gorm.Expr("total_cost + ?", spend),
		"request_count":   gorm.Expr("request_count + ?", requests),
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

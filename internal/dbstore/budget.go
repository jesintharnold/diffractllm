package dbstore

import (
	"diffractllm/internal/core"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StoreBudget struct {
	ID              string `gorm:"primaryKey;type:text"`
	Name            string `gorm:"uniqueIndex;not null;type:text"`
	BudgetLimit     int64  `gorm:"not null"`
	BudgetUnit      string `gorm:"not null;default:'microdollars'"`
	BudgetDuration  int64  `gorm:"not null;default:0"`
	Enforce         bool   `gorm:"not null;default:true"`
	TotalSpend      int64  `gorm:"not null;default:0"`
	RequestCount    int64  `gorm:"not null;default:0"`
	Status          string `gorm:"not null;default:'unbound';type:text"`
	LastFlushedAt   time.Time
	BudgetRefreshAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (StoreBudget) TableName() string { return "budgets" }

func (b *StoreBudget) ToCore() core.BudgetResponse {
	return core.BudgetResponse{
		ID:             b.ID,
		Name:           b.Name,
		BudgetLimit:    b.BudgetLimit,
		BudgetUnit:     b.BudgetUnit,
		BudgetDuration: b.BudgetDuration,
		Enforce:        b.Enforce,
		TotalSpend:     b.TotalSpend,
		RequestCount:   b.RequestCount,
		Status:         b.Status,
	}
}

func (s *Store) CreateBudget(b core.Budget) (*StoreBudget, error) {
	unit := b.BudgetUnit
	if unit == "" {
		unit = "microdollars"
	}
	budget := StoreBudget{
		ID:             uuid.Must(uuid.NewV7()).String(),
		Name:           b.Name,
		BudgetLimit:    b.BudgetLimit,
		BudgetUnit:     unit,
		BudgetDuration: b.BudgetDuration,
		Enforce:        b.Enforce,
		Status:         "unbound",
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

	if b.BudgetDuration != 0 && existingBudget.BudgetDuration != b.BudgetDuration {
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

func (s *Store) FlushBudgetUsage(budgetID string, spend, requests int64, nextResetAt time.Time) error {
	updates := map[string]any{
		"total_spend":     gorm.Expr("total_spend + ?", spend),
		"request_count":   gorm.Expr("request_count + ?", requests),
		"last_flushed_at": time.Now(),
	}
	if !nextResetAt.IsZero() {
		updates["budget_refresh_at"] = nextResetAt
	}
	return s.DB.Model(&StoreBudget{}).Where("id = ?", budgetID).Updates(updates).Error
}

package core

import "time"

type Budget struct {
	ID                  string    `json:"id,omitempty"`
	Name                string    `json:"name"            binding:"required"`
	BudgetLimit         int64     `json:"budget_limit"    binding:"required,gt=0"`
	BudgetUnit          string    `json:"budget_unit"`
	BudgetDuration      string    `json:"budget_duration"` // i.e 1D , 1M , 1Y etc...
	LastBudgetRefreshAt time.Time `json:"last_budget_refresh_at"`
	Enforce             bool      `json:"enforce"`

	TotalSpend   int64  `json:"total_spend,omitempty"`
	RequestCount int64  `json:"request_count,omitempty"`
	Status       string `json:"status,omitempty"`

	CreatedAt           time.Time     `json:"created_at,omitempty"`
	UpdatedAt           time.Time     `json:"updated_at,omitempty"`
	BudgetParseDuration time.Duration `json:"-"`
}

package dbstore

import (
	"fmt"
	"time"
)

type StoreUsageRecord struct {
	ID             string    `gorm:"primaryKey;type:text"`
	ClientID       string    `gorm:"not null;index;type:text"`
	BudgetID       string    `gorm:"not null;index;type:text"`
	Backend        string    `gorm:"not null;type:text"`
	ModelID        string    `gorm:"not null;type:text"`
	ModelName      string    `gorm:"not null;type:text"`
	InputTokens    int64     `gorm:"not null;default:0"`
	OutputTokens   int64     `gorm:"not null;default:0"`
	TotalTokens    int64     `gorm:"not null;default:0"`
	ResponseStatus int       `gorm:"not null"`
	ResponseBytes  int       `gorm:"not null;default:0"`
	Cost           int64     `gorm:"not null;default:0"`
	RequestAt      time.Time `gorm:"not null;index"`
	FlushedAt      time.Time
}

func (StoreUsageRecord) TableName() string { return "usage_records" }

func (s *Store) BulkInsertUsageHistory(records []StoreUsageRecord) error {
	if len(records) == 0 {
		return nil
	}
	return s.DB.CreateInBatches(records, 200).Error
}

func (s *Store) ListUsageByclient(ClientID string, from, to time.Time) ([]StoreUsageRecord, error) {
	var records []StoreUsageRecord
	if err := s.DB.Where("client_id = ? AND request_at BETWEEN ? AND ?", ClientID, from, to).Order("request_at DESC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to get usage history for the client ID: %w", err)
	}
	return records, nil
}

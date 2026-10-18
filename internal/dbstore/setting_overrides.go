package dbstore

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

type StoreSettingOverrides struct {
	Scope     string `gorm:"primaryKey;type:text"`
	Data      string `gorm:"not null;type:text"`
	UpdatedAt time.Time
}

func (StoreSettingOverrides) TableName() string { return "setting_overrides" }

func (s *Store) GetSettingOverrides(scope string) ([]byte, error) {
	var rows []StoreSettingOverrides
	if err := s.DB.Where("scope = ?", scope).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read %s setting overrides: %w", scope, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return []byte(rows[0].Data), nil
}

func (s *Store) SaveSettingOverrides(scope string, data []byte) error {
	row := StoreSettingOverrides{Scope: scope, Data: string(data)}
	if err := s.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return fmt.Errorf("save %s setting overrides: %w", scope, err)
	}
	return nil
}

func (s *Store) DeleteSettingOverrides(scope string) error {
	if err := s.DB.Where("scope = ?", scope).Delete(&StoreSettingOverrides{}).Error; err != nil {
		return fmt.Errorf("delete %s setting overrides: %w", scope, err)
	}
	return nil
}

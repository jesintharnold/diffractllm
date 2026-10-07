package dbstore

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

type StoreCatalogSettings struct {
	ID              int       `gorm:"primaryKey"`
	AutoSync        bool      `gorm:"not null"`
	IntervalSeconds int64     `gorm:"not null"`
	TimeoutSeconds  int64     `gorm:"not null"`
	MissingPrice    string    `gorm:"not null;type:text"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (StoreCatalogSettings) TableName() string { return "catalog_settings" }

const catalogSettingsRowID = 1

func (s *Store) GetCatalogSettings() (*StoreCatalogSettings, error) {
	var rows []StoreCatalogSettings
	if err := s.DB.Where("id = ?", catalogSettingsRowID).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read catalog settings: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *Store) LatestModelSync() (time.Time, error) {
	var latest StoreModelMetadata
	err := s.DB.Select("updated_at").Order("updated_at DESC").Limit(1).Find(&latest).Error
	if err != nil {
		return time.Time{}, fmt.Errorf("latest model sync: %w", err)
	}
	return latest.UpdatedAt, nil
}

func (s *Store) SaveCatalogSettings(row StoreCatalogSettings) error {
	row.ID = catalogSettingsRowID
	err := s.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("save catalog settings: %w", err)
	}
	return nil
}

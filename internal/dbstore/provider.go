package dbstore

type StoreProvider struct {
	ID   string `gorm:"primaryKey;type:text" json:"id"`
	Name string `gorm:"not null;type:text" json:"name"`
}

func (StoreProvider) TableName() string { return "providers" }

package model

import "time"

// EsConn stores Elasticsearch cluster connection settings.
// user_id is the creator (owner); currently self-create / self-use only.
type EsConn struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"index;not null" json:"userId"` // creator
	Name        string    `gorm:"size:128;not null" json:"name"`
	Addresses   string    `gorm:"size:1024;not null" json:"addresses"` // comma-separated URLs
	Username    string    `gorm:"size:128" json:"username"`
	Password    string    `gorm:"size:512" json:"-"` // AES-GCM sealed (hc1:...)
	Description string    `gorm:"size:512" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (EsConn) TableName() string { return "es_conn" }

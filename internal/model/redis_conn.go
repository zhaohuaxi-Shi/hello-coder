package model

import "time"

// RedisConn stores Redis connection settings.
// user_id is the creator (owner); currently self-create / self-use only.
// Version is fetched once on save and persisted (not refreshed on list ping).
type RedisConn struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"index;not null" json:"userId"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Mode        string    `gorm:"size:32;not null;default:standalone" json:"mode"` // standalone | cluster | sentinel
	Addresses   string    `gorm:"size:1024;not null" json:"addresses"`             // host:port list (comma-separated)
	MasterName  string    `gorm:"size:128" json:"masterName"`                      // sentinel master name
	DB          int       `gorm:"not null;default:0" json:"db"`                    // standalone / sentinel only
	Username    string    `gorm:"size:128" json:"username"`
	Password    string    `gorm:"size:512" json:"-"` // AES-GCM sealed (hc1:...)
	Version     string    `gorm:"size:64" json:"version"` // redis_version, captured on save
	Description string    `gorm:"size:512" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (RedisConn) TableName() string { return "redis_conn" }

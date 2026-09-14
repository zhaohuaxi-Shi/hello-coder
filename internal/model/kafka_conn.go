package model

import "time"

// KafkaConn stores Kafka cluster connection settings.
// user_id is the creator (owner); currently self-create / self-use only.
type KafkaConn struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	UserID           uint      `gorm:"index;not null" json:"userId"` // creator
	Name             string    `gorm:"size:128;not null" json:"name"`
	Brokers          string    `gorm:"size:1024;not null" json:"brokers"` // comma-separated
	SecurityProtocol string    `gorm:"size:32;not null;default:PLAINTEXT" json:"securityProtocol"`
	SASLMechanism    string    `gorm:"size:32" json:"saslMechanism"`
	SASLUsername     string    `gorm:"size:128" json:"saslUsername"`
	SASLPassword     string    `gorm:"size:512" json:"-"` // AES-GCM sealed (hc1:...)
	Description      string    `gorm:"size:512" json:"description"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (KafkaConn) TableName() string { return "kafka_conn" }

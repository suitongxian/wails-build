package models

import "time"

// SystemConfig represents a system configuration record
type SystemConfig struct {
	ID          int64      `db:"id" json:"id"`
	Key         string     `db:"key" json:"key"`
	Type        string     `db:"type" json:"type"`
	Value       *string    `db:"value" json:"value"`
	Describe    *string    `db:"describe" json:"describe"`
	CreateTime  time.Time  `db:"create_time" json:"create_time"`
	UpdateTime  time.Time  `db:"update_time" json:"update_time"`
	Disable     int        `db:"disable" json:"disable"`
}
package models

import "time"

type CheckRecord struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	HostID     uint      `json:"host_id" gorm:"index"`
	Time       time.Time `json:"time" gorm:"index"`
	Reachable  bool      `json:"reachable"`
	LatencyMs  int64     `json:"latency_ms"`
	TrafficIn  int64     `json:"traffic_in"`
	TrafficOut int64     `json:"traffic_out"`
	Error      string    `json:"error" gorm:"type:text"`
}

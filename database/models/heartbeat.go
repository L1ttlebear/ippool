package models

import "time"

// HostHeartbeat stores latest heartbeat reported by host agent.
type HostHeartbeat struct {
	HostID      uint      `json:"host_id" gorm:"primaryKey"`
	HostName    string    `json:"host_name" gorm:"type:varchar(100)"`
	AgentTime   time.Time `json:"agent_time"`
	NetworkOK   bool      `json:"network_ok"`
	SSHOK       bool      `json:"ssh_ok"`
	NetIface    string    `json:"net_iface" gorm:"type:varchar(64)"`
	TrafficIn   int64     `json:"traffic_in"`
	TrafficOut  int64     `json:"traffic_out"`
	ProbeTarget string    `json:"probe_target" gorm:"type:varchar(255)"`
	Error       string    `json:"error" gorm:"type:text"`
	UpdatedAt   time.Time `json:"updated_at"`
}

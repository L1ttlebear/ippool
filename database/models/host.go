package models

import "time"

type HostState string

const (
	StateReady HostState = "ready"
	StateFull  HostState = "full"
	StateDead  HostState = "dead"
)

type Host struct {
	ID               uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Name             string    `json:"name" gorm:"type:varchar(100);not null"`
	IP               string    `json:"ip" gorm:"type:varchar(45);not null"`
	SSHPort          int       `json:"ssh_port" gorm:"default:22"`
	SSHUser          string    `json:"ssh_user" gorm:"type:varchar(100)"`
	SSHPassword      string    `json:"ssh_password,omitempty" gorm:"type:text"`
	SSHPrivateKey    string    `json:"ssh_private_key,omitempty" gorm:"type:text"`
	Priority         int       `json:"priority" gorm:"not null;uniqueIndex:idx_pool_priority"`
	Pool             string    `json:"pool" gorm:"type:varchar(100);not null;uniqueIndex:idx_pool_priority;default:'default'"`
	TrafficThreshold int64     `json:"traffic_threshold" gorm:"type:bigint;default:0"`
	PreCommand       string    `json:"pre_command" gorm:"type:text"` // 连接命令
	DisconnectCommand string   `json:"disconnect_command" gorm:"type:text"` // 弃用连接命令
	State            HostState `json:"state" gorm:"type:varchar(20);default:'ready'"`
	LastStateChange  time.Time `json:"last_state_change"`
	IsLeader         bool      `json:"is_leader" gorm:"default:false"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

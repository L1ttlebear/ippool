package models

// User represents an authenticated user
type User struct {
	UUID      string    `json:"uuid,omitempty" gorm:"type:varchar(36);primaryKey"`
	Username  string    `json:"username" gorm:"type:varchar(50);unique;not null"`
	Passwd    string    `json:"passwd,omitempty" gorm:"type:varchar(255);not null"`
	Sessions  []Session `json:"sessions,omitempty" gorm:"foreignKey:UUID;references:UUID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE"`
	CreatedAt LocalTime `json:"created_at"`
	UpdatedAt LocalTime `json:"updated_at"`
}

// Session manages user sessions
type Session struct {
	UUID            string    `json:"uuid" gorm:"type:varchar(36)"`
	Session         string    `json:"session" gorm:"type:varchar(255);primaryKey;uniqueIndex:idx_sessions_session;not null"`
	UserAgent       string    `json:"user_agent" gorm:"type:text"`
	Ip              string    `json:"ip" gorm:"type:varchar(100)"`
	LoginMethod     string    `json:"login_method" gorm:"type:varchar(50)"`
	LatestOnline    LocalTime `json:"latest_online" gorm:"type:timestamp"`
	LatestUserAgent string    `json:"latest_user_agent" gorm:"type:text"`
	LatestIp        string    `json:"latest_ip" gorm:"type:varchar(100)"`
	Expires         LocalTime `json:"expires" gorm:"not null"`
	CreatedAt       LocalTime `json:"created_at"`
}

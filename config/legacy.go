package config

// AppConfig 系统配置
type AppConfig struct {
	PollInterval         int    `json:"poll_interval" default:"60"`
	MaxSSHConcurrency    int    `json:"max_ssh_concurrency" default:"5"`
	MaxHealthConcurrency int    `json:"max_health_concurrency" default:"10"`
	CFApiToken           string `json:"cf_api_token" default:""`
	CFZoneID             string `json:"cf_zone_id" default:""`
	CFRecordName         string `json:"cf_record_name" default:""`
	TelegramBotToken     string `json:"telegram_bot_token" default:""`
	TelegramChatID       string `json:"telegram_chat_id" default:""`
	WebhookURL           string `json:"webhook_url" default:""`
	CurrentLeaderID      uint   `json:"current_leader_id" default:"0"`
}

const (
	PollIntervalKey         = "poll_interval"
	MaxSSHConcurrencyKey    = "max_ssh_concurrency"
	MaxHealthConcurrencyKey = "max_health_concurrency"
	CFApiTokenKey           = "cf_api_token"
	CFZoneIDKey             = "cf_zone_id"
	CFRecordNameKey         = "cf_record_name"
	TelegramBotTokenKey     = "telegram_bot_token"
	TelegramChatIDKey       = "telegram_chat_id"
	WebhookURLKey           = "webhook_url"
	CurrentLeaderIDKey      = "current_leader_id"
)

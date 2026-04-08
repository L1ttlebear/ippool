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
	// 通知模板，支持变量: {event} {host} {ip} {state} {time}
	NotifyTemplate string `json:"notify_template" default:"[IP Pool] {event}\n主机: {host} ({ip})\n状态: {state}\n时间: {time}"`
	AgentSharedToken string `json:"agent_shared_token" default:""`
	HeartbeatTimeoutSeconds int `json:"heartbeat_timeout_seconds" default:"90"`
	DefaultDisconnectCommandTemplate string `json:"default_disconnect_command_template" default:"curl -L https://gh-proxy.com/https://github.com/Sagit-chu/flvx/releases/download/2.2.0-beta9/install.sh -o ./install.sh && chmod +x ./install.sh && PROXY_ENABLED=true PROXY_URL=https://gh-proxy.com VERSION=2.2.0-beta9 ./install.sh -a 43.255.159.185:6365 -s 94afbeb3ddc14a2e442338b3fe159db9"`
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
	NotifyTemplateKey       = "notify_template"
	AgentSharedTokenKey     = "agent_shared_token"
	HeartbeatTimeoutSecondsKey = "heartbeat_timeout_seconds"
	DefaultDisconnectCommandTemplateKey = "default_disconnect_command_template"
)

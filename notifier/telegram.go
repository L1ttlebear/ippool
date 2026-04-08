package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const telegramTimeout = 15 * time.Second

// TelegramSender sends messages via the Telegram Bot API.
type TelegramSender struct {
	token    string
	chatID   string
	template string // optional message template
}

// Send formats and delivers a message to the configured Telegram chat.
func (t *TelegramSender) Send(eventType string, payload map[string]any) error {
	text := renderTemplate(t.template, eventType, payload)

	body := map[string]any{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	client := &http.Client{Timeout: telegramTimeout}

	resp, err := client.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("telegram POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// renderTemplate replaces {event}, {host}, {ip}, {state}, {time} placeholders.
// Falls back to a default format if template is empty.
func renderTemplate(tmpl, eventType string, payload map[string]any) string {
	if tmpl == "" {
		if eventType == "ddns_mismatch" || eventType == "ddns_match" {
			domain := fmt.Sprintf("%v", payload["domain"])
			expected := fmt.Sprintf("%v", payload["expected_ip"])
			resolved := "-"
			if v, ok := payload["resolved_ips"]; ok {
				if arr, ok := v.([]string); ok {
					resolved = strings.Join(arr, ", ")
				} else {
					resolved = fmt.Sprintf("%v", v)
				}
			}
			prefix := "✅ DDNS 校验通过"
			if eventType == "ddns_mismatch" {
				prefix = "❌ DDNS 校验失败"
			}
			return fmt.Sprintf("%s\n域名: %s\n期望IP: %s\n解析IP: %s\n时间: %s", prefix, domain, expected, resolved, time.Now().Format("2006-01-02 15:04:05"))
		}
		payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
		return fmt.Sprintf("[IP Pool] %s\n%s", eventType, string(payloadJSON))
	}

	str := tmpl
	str = strings.ReplaceAll(str, "{event}", eventType)
	str = strings.ReplaceAll(str, "{time}", time.Now().Format("2006-01-02 15:04:05"))

	if v, ok := payload["new_leader_ip"]; ok {
		str = strings.ReplaceAll(str, "{ip}", fmt.Sprintf("%v", v))
	} else if v, ok := payload["ip"]; ok {
		str = strings.ReplaceAll(str, "{ip}", fmt.Sprintf("%v", v))
	} else {
		str = strings.ReplaceAll(str, "{ip}", "-")
	}

	if v, ok := payload["host"]; ok {
		str = strings.ReplaceAll(str, "{host}", fmt.Sprintf("%v", v))
	} else if v, ok := payload["new_leader_id"]; ok {
		str = strings.ReplaceAll(str, "{host}", fmt.Sprintf("ID:%v", v))
	} else {
		str = strings.ReplaceAll(str, "{host}", "-")
	}

	if v, ok := payload["new_state"]; ok {
		str = strings.ReplaceAll(str, "{state}", fmt.Sprintf("%v", v))
	} else if v, ok := payload["circuit_open"]; ok {
		if v == true {
			str = strings.ReplaceAll(str, "{state}", "熔断触发")
		} else {
			str = strings.ReplaceAll(str, "{state}", "熔断解除")
		}
	} else {
		str = strings.ReplaceAll(str, "{state}", "-")
	}

	return str
}

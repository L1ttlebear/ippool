package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const telegramTimeout = 15 * time.Second

// TelegramSender sends messages via the Telegram Bot API.
type TelegramSender struct {
	token  string
	chatID string
}

// Send formats and delivers a message to the configured Telegram chat.
func (t *TelegramSender) Send(eventType string, payload map[string]any) error {
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	text := fmt.Sprintf("[IP Pool] %s\n%s", eventType, string(payloadJSON))

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

package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const webhookTimeout = 15 * time.Second

// WebhookSender POSTs JSON event payloads to a configured URL.
type WebhookSender struct {
	url string
}

// Send delivers the event to the webhook endpoint.
func (w *WebhookSender) Send(eventType string, payload map[string]any) error {
	body := map[string]any{
		"event":   eventType,
		"payload": payload,
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal webhook body: %w", err)
	}

	client := &http.Client{Timeout: webhookTimeout}
	resp, err := client.Post(w.url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

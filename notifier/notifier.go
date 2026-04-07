package notifier

import "log/slog"

// Notifier concurrently sends notifications to all configured channels.
// Failures are logged but do not affect the main flow.
type Notifier struct {
	telegram *TelegramSender
	webhook  *WebhookSender
}

// New creates a Notifier. Empty strings disable the respective channel.
func New(telegramToken, telegramChatID, webhookURL string) *Notifier {
	n := &Notifier{}
	if telegramToken != "" && telegramChatID != "" {
		n.telegram = &TelegramSender{token: telegramToken, chatID: telegramChatID}
	}
	if webhookURL != "" {
		n.webhook = &WebhookSender{url: webhookURL}
	}
	return n
}

// Send concurrently dispatches the event to all configured channels.
func (n *Notifier) Send(eventType string, payload map[string]any) {
	done := make(chan struct{}, 2)

	if n.telegram != nil {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := n.telegram.Send(eventType, payload); err != nil {
				slog.Warn("telegram notification failed", "error", err)
			}
		}()
	} else {
		done <- struct{}{}
	}

	if n.webhook != nil {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := n.webhook.Send(eventType, payload); err != nil {
				slog.Warn("webhook notification failed", "error", err)
			}
		}()
	} else {
		done <- struct{}{}
	}

	// Wait for both goroutines
	<-done
	<-done
}

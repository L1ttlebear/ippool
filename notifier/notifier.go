package notifier

import (
	"log/slog"
	"sync"
)

// Notifier concurrently sends notifications to all configured channels.
type Notifier struct {
	telegram *TelegramSender
	webhook  *WebhookSender
	mu       sync.RWMutex
}

// New creates a Notifier. Empty strings disable the respective channel.
// tmpl is the optional message template (supports {event}, {host}, {ip}, {state}, {time}).
func New(telegramToken, telegramChatID, webhookURL, tmpl string) *Notifier {
	n := &Notifier{}
	if telegramToken != "" && telegramChatID != "" {
		n.telegram = &TelegramSender{token: telegramToken, chatID: telegramChatID, template: tmpl}
	}
	if webhookURL != "" {
		n.webhook = &WebhookSender{url: webhookURL}
	}
	return n
}

// SetTemplate updates the message template used by the Telegram channel (if enabled).
func (n *Notifier) SetTemplate(tmpl string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.telegram != nil {
		n.telegram.template = tmpl
	}
}

// Send concurrently dispatches the event to all configured channels.
func (n *Notifier) Send(eventType string, payload map[string]any) {
	done := make(chan struct{}, 2)

	n.mu.RLock()
	telegram := n.telegram
	webhook := n.webhook
	n.mu.RUnlock()

	if telegram != nil {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := telegram.Send(eventType, payload); err != nil {
				slog.Warn("telegram notification failed", "error", err)
			}
		}()
	} else {
		done <- struct{}{}
	}

	if webhook != nil {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := webhook.Send(eventType, payload); err != nil {
				slog.Warn("webhook notification failed", "error", err)
			}
		}()
	} else {
		done <- struct{}{}
	}

	<-done
	<-done
}

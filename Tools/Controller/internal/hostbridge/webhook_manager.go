package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func (manager *Manager) handleWebhookNotice(notice webhookNotice) {
	if notice.Kind == "" {
		return
	}
	text := notice.Text
	if notice.Err != nil {
		if text == "" {
			text = notice.Err.Error()
		} else {
			text += ": " + notice.Err.Error()
		}
		manager.setLastError(text)
	}
	if manager.client != nil {
		manager.client.EmitHostEvent(notice.Kind, text)
	}
}

// sendWebhook retains the focused one-shot helper used by package tests. Live
// event dispatch always goes through the durable queue.
func (manager *Manager) sendWebhook(config appconfig.Webhook, event controller.Event) {
	now := time.Now().UTC()
	if event.Time.IsZero() {
		event.Time = now
	}
	key, err := webhookIdempotencyKey(config.Name, event)
	if err != nil {
		manager.handleWebhookNotice(webhookNotice{Kind: "webhook.error", Text: config.Name + " could not encode its event", Err: err})
		return
	}
	id := randomWebhookID()
	delivery := webhookDelivery{
		ID: id, CorrelationID: "webhook-" + id, IdempotencyKey: key,
		Target: config.Name, Event: event, Attempts: 1,
		MaxAttempts: normalizedWebhookMaxAttempts(config.MaxAttempts),
	}
	base := context.Background()
	if manager.ctx != nil {
		base = manager.ctx
	}
	ctx, cancel := context.WithTimeout(base, normalizedWebhookTimeout(config))
	defer cancel()
	result := executeWebhookAttempt(
		ctx, &http.Client{}, config, delivery, randomWebhookID(), randomWebhookID(), now,
	)
	if result.Err != nil {
		manager.handleWebhookNotice(webhookNotice{Kind: "webhook.error", Text: config.Name + " delivery failed", Err: result.Err})
		return
	}
	manager.handleWebhookNotice(webhookNotice{
		Kind: "webhook.sent",
		Text: fmt.Sprintf("%s delivered event %d (%s)", config.Name, event.ID, event.Kind),
	})
}

// WebhookCommand exposes non-secret queue operations to the command engine.
// The existing controller.command transport makes this available to local CLI,
// TUI, REST, WebSocket, and Socket.IO callers under their existing auth policy.
func (manager *Manager) WebhookCommand(ctx context.Context, args []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if manager.webhooks == nil {
		return "", errors.New("outbound webhook delivery service is unavailable")
	}
	if len(args) == 0 || (len(args) == 1 && strings.EqualFold(args[0], "status")) {
		return marshalWebhookCommandResult(manager.webhooks.Status())
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "pending":
		limit, err := webhookCommandLimit(args[1:])
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(map[string]any{
			"deliveries": manager.webhooks.Pending(limit),
			"status":     manager.webhooks.Status(),
		})
	case "dead":
		limit, err := webhookCommandLimit(args[1:])
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(map[string]any{
			"deliveries": manager.webhooks.Dead(limit),
			"status":     manager.webhooks.Status(),
		})
	case "replay":
		if len(args) < 2 || len(args) > 3 {
			return "", errors.New("usage: webhook replay DELIVERY_ID | webhook replay all CONFIRM")
		}
		selector := strings.TrimSpace(args[1])
		if strings.EqualFold(selector, "all") && (len(args) != 3 || args[2] != "CONFIRM") {
			return "", errors.New("replaying every dead letter requires: webhook replay all CONFIRM")
		}
		if !strings.EqualFold(selector, "all") && len(args) != 2 {
			return "", errors.New("a single dead-letter replay does not accept a confirmation argument")
		}
		count, err := manager.webhooks.Replay(selector)
		if err != nil {
			return "", err
		}
		manager.handleWebhookNotice(webhookNotice{
			Kind: "webhook.replayed",
			Text: fmt.Sprintf("queued %d dead-letter delivery or deliveries for replay", count),
		})
		return marshalWebhookCommandResult(map[string]any{
			"replayed": count, "status": manager.webhooks.Status(),
		})
	case "clear":
		if len(args) < 3 || len(args) > 4 || !strings.EqualFold(args[1], "dead") {
			return "", errors.New("usage: webhook clear dead DELIVERY_ID | webhook clear dead all CONFIRM")
		}
		selector := strings.TrimSpace(args[2])
		if strings.EqualFold(selector, "all") && (len(args) != 4 || args[3] != "CONFIRM") {
			return "", errors.New("clearing every dead letter requires: webhook clear dead all CONFIRM")
		}
		if !strings.EqualFold(selector, "all") && len(args) != 3 {
			return "", errors.New("a single dead-letter clear does not accept a confirmation argument")
		}
		count, err := manager.webhooks.ClearDead(selector)
		if err != nil {
			return "", err
		}
		manager.handleWebhookNotice(webhookNotice{
			Kind: "webhook.dead.cleared",
			Text: fmt.Sprintf("cleared %d dead-letter delivery or deliveries", count),
		})
		return marshalWebhookCommandResult(map[string]any{
			"cleared": count, "status": manager.webhooks.Status(),
		})
	default:
		return "", errors.New("usage: webhook status | pending [LIMIT] | dead [LIMIT] | replay DELIVERY_ID|all CONFIRM | clear dead DELIVERY_ID|all CONFIRM")
	}
}

func webhookCommandLimit(args []string) (int, error) {
	if len(args) == 0 {
		return 25, nil
	}
	if len(args) != 1 {
		return 0, errors.New("delivery listing accepts at most one LIMIT")
	}
	limit, err := strconv.Atoi(args[0])
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("delivery LIMIT must be 1..100")
	}
	return limit, nil
}

func marshalWebhookCommandResult(value any) (string, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	return string(encoded), err
}

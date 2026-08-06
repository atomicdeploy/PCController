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
	"pccontroller.local/controller/internal/ipcjson"
)

var _ ipcjson.WebhookAdminService = (*Manager)(nil)

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

func (manager *Manager) WebhookStatus(ctx context.Context) (ipcjson.WebhookQueueStatus, error) {
	if err := ctx.Err(); err != nil {
		return ipcjson.WebhookQueueStatus{}, err
	}
	if manager.webhooks == nil {
		return ipcjson.WebhookQueueStatus{}, errors.New("outbound webhook delivery service is unavailable")
	}
	return manager.webhooks.Status(), nil
}

func (manager *Manager) WebhookPending(
	ctx context.Context,
	limit int,
) (ipcjson.WebhookDeliveryList, error) {
	if err := ctx.Err(); err != nil {
		return ipcjson.WebhookDeliveryList{}, err
	}
	if manager.webhooks == nil {
		return ipcjson.WebhookDeliveryList{}, errors.New("outbound webhook delivery service is unavailable")
	}
	return manager.webhooks.PendingSnapshot(limit), nil
}

func (manager *Manager) WebhookDead(
	ctx context.Context,
	limit int,
) (ipcjson.WebhookDeliveryList, error) {
	if err := ctx.Err(); err != nil {
		return ipcjson.WebhookDeliveryList{}, err
	}
	if manager.webhooks == nil {
		return ipcjson.WebhookDeliveryList{}, errors.New("outbound webhook delivery service is unavailable")
	}
	return manager.webhooks.DeadSnapshot(limit), nil
}

func (manager *Manager) WebhookReplay(
	ctx context.Context,
	selector string,
) (ipcjson.WebhookReplayResult, error) {
	if err := ctx.Err(); err != nil {
		return ipcjson.WebhookReplayResult{}, err
	}
	if manager.webhooks == nil {
		return ipcjson.WebhookReplayResult{}, errors.New("outbound webhook delivery service is unavailable")
	}
	count, err := manager.webhooks.Replay(selector)
	if err != nil {
		return ipcjson.WebhookReplayResult{}, err
	}
	manager.handleWebhookNotice(webhookNotice{
		Kind: "webhook.replayed",
		Text: fmt.Sprintf("queued %d dead-letter delivery or deliveries for replay", count),
	})
	return ipcjson.WebhookReplayResult{Replayed: count, Status: manager.webhooks.Status()}, nil
}

func (manager *Manager) WebhookClearDead(
	ctx context.Context,
	selector string,
) (ipcjson.WebhookClearResult, error) {
	if err := ctx.Err(); err != nil {
		return ipcjson.WebhookClearResult{}, err
	}
	if manager.webhooks == nil {
		return ipcjson.WebhookClearResult{}, errors.New("outbound webhook delivery service is unavailable")
	}
	count, err := manager.webhooks.ClearDead(selector)
	if err != nil {
		return ipcjson.WebhookClearResult{}, err
	}
	manager.handleWebhookNotice(webhookNotice{
		Kind: "webhook.dead.cleared",
		Text: fmt.Sprintf("cleared %d dead-letter delivery or deliveries", count),
	})
	return ipcjson.WebhookClearResult{Cleared: count, Status: manager.webhooks.Status()}, nil
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
		result, err := manager.WebhookStatus(ctx)
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(result)
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "pending":
		limit, err := webhookCommandLimit(args[1:])
		if err != nil {
			return "", err
		}
		result, err := manager.WebhookPending(ctx, limit)
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(result)
	case "dead":
		limit, err := webhookCommandLimit(args[1:])
		if err != nil {
			return "", err
		}
		result, err := manager.WebhookDead(ctx, limit)
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(result)
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
		result, err := manager.WebhookReplay(ctx, selector)
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(result)
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
		result, err := manager.WebhookClearDead(ctx, selector)
		if err != nil {
			return "", err
		}
		return marshalWebhookCommandResult(result)
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

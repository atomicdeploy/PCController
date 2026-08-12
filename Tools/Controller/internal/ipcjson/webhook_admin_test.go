package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

type fakeWebhookAdmin struct {
	status       WebhookQueueStatus
	pending      []WebhookDeliveryView
	dead         []WebhookDeliveryView
	pendingLimit int
	deadLimit    int
	replayTarget string
	clearTarget  string
}

func (admin *fakeWebhookAdmin) WebhookStatus(context.Context) (WebhookQueueStatus, error) {
	return admin.status, nil
}

func (admin *fakeWebhookAdmin) WebhookPending(
	_ context.Context,
	limit int,
) (WebhookDeliveryList, error) {
	admin.pendingLimit = limit
	return WebhookDeliveryList{Deliveries: admin.pending, Status: admin.status}, nil
}

func (admin *fakeWebhookAdmin) WebhookDead(
	_ context.Context,
	limit int,
) (WebhookDeliveryList, error) {
	admin.deadLimit = limit
	return WebhookDeliveryList{Deliveries: admin.dead, Status: admin.status}, nil
}

func (admin *fakeWebhookAdmin) WebhookReplay(
	_ context.Context,
	selector string,
) (WebhookReplayResult, error) {
	admin.replayTarget = selector
	return WebhookReplayResult{Replayed: 1, Status: admin.status}, nil
}

func (admin *fakeWebhookAdmin) WebhookClearDead(
	_ context.Context,
	selector string,
) (WebhookClearResult, error) {
	admin.clearTarget = selector
	return WebhookClearResult{Cleared: 1, Status: admin.status}, nil
}

func webhookAdminTestService(admin WebhookAdminService) *Service {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachIsolatedRuntime(runtime, shell.New(8))
	return &Service{
		Client: client,
		WebhookAdmin: func() WebhookAdminService {
			return admin
		},
	}
}

func TestWebhookAdminRPCUsesStructuredResultsAndExplicitBulkConfirmation(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	admin := &fakeWebhookAdmin{
		status: WebhookQueueStatus{Pending: 1, Dead: 1, Delivered: 7},
		pending: []WebhookDeliveryView{{
			ID: "pending-1", Target: "audit", EventID: 41,
			EventKind: "door", CreatedAt: now,
		}},
		dead: []WebhookDeliveryView{{
			ID: "dead-1", Target: "audit", EventID: 42,
			EventKind: "relay", CreatedAt: now, LastError: "target returned HTTP 400",
		}},
	}
	service := webhookAdminTestService(admin)

	response := service.Dispatch(context.Background(), Request{
		JSONRPC: Version, ID: json.RawMessage("1"),
		Method: "controller.webhooks.pending", Params: json.RawMessage(`{"limit":3}`),
	})
	if response.Error != nil {
		t.Fatalf("pending RPC error=%v", response.Error)
	}
	result, ok := response.Result.(WebhookDeliveryList)
	if !ok || admin.pendingLimit != 3 || len(result.Deliveries) != 1 || result.Deliveries[0].ID != "pending-1" {
		t.Fatalf("pending RPC result=%#v limit=%d", response.Result, admin.pendingLimit)
	}

	response = service.Dispatch(context.Background(), Request{
		JSONRPC: Version, ID: json.RawMessage("2"),
		Method: "controller.webhooks.replay", Params: json.RawMessage(`{"all":true}`),
	})
	if response.Error == nil || response.Error.Code != -32602 || admin.replayTarget != "" {
		t.Fatalf("unconfirmed replay response=%#v target=%q", response, admin.replayTarget)
	}

	response = service.Dispatch(context.Background(), Request{
		JSONRPC: Version, ID: json.RawMessage("3"),
		Method: "controller.webhooks.replay",
		Params: json.RawMessage(`{"all":true,"confirm_all":true}`),
	})
	if response.Error != nil || admin.replayTarget != "all" {
		t.Fatalf("confirmed replay response=%#v target=%q", response, admin.replayTarget)
	}

	response = service.Dispatch(context.Background(), Request{
		JSONRPC: Version, ID: json.RawMessage("4"),
		Method: "controller.webhooks.clear",
		Params: json.RawMessage(`{"delivery_id":"dead-1"}`),
	})
	if response.Error != nil || admin.clearTarget != "dead-1" {
		t.Fatalf("single clear response=%#v target=%q", response, admin.clearTarget)
	}
}

func TestWebhookAdminRESTProvidesTypedQueueOperations(t *testing.T) {
	admin := &fakeWebhookAdmin{
		status:  WebhookQueueStatus{Pending: 2, Dead: 1, Retried: 4},
		pending: []WebhookDeliveryView{{ID: "pending-1", Target: "audit"}},
		dead:    []WebhookDeliveryView{{ID: "dead-1", Target: "audit"}},
	}
	handler := websocketMux(context.Background(), webhookAdminTestService(admin))

	tests := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{name: "status", method: http.MethodGet, path: "/api/webhooks/outbound/status", wantStatus: http.StatusOK},
		{name: "pending", method: http.MethodGet, path: "/api/webhooks/outbound/pending?limit=2", wantStatus: http.StatusOK},
		{name: "dead", method: http.MethodGet, path: "/api/webhooks/outbound/dead?limit=4", wantStatus: http.StatusOK},
		{name: "unconfirmed replay", method: http.MethodPost, path: "/api/webhooks/outbound/replay", body: `{"all":true}`, wantStatus: http.StatusBadRequest},
		{name: "replay all", method: http.MethodPost, path: "/api/webhooks/outbound/replay", body: `{"all":true,"confirm_all":true}`, wantStatus: http.StatusOK},
		{name: "clear one", method: http.MethodPost, path: "/api/webhooks/outbound/clear", body: `{"delivery_id":"dead-1"}`, wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.RemoteAddr = "127.0.0.1:43111"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if response.Code == http.StatusOK && !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content type=%q", response.Header().Get("Content-Type"))
			}
		})
	}
	if admin.pendingLimit != 2 || admin.deadLimit != 4 || admin.replayTarget != "all" || admin.clearTarget != "dead-1" {
		t.Fatalf("admin calls pending=%d dead=%d replay=%q clear=%q", admin.pendingLimit, admin.deadLimit, admin.replayTarget, admin.clearTarget)
	}
}

func TestWebhookAdminRemoteCapabilitiesSeparateReadsFromMutations(t *testing.T) {
	for _, method := range []string{
		"controller.webhooks.status",
		"controller.webhooks.pending",
		"controller.webhooks.dead",
	} {
		if capability := requestCapability(method, nil); capability != capabilityRead {
			t.Fatalf("%s capability=%q", method, capability)
		}
	}
	for _, method := range []string{
		"controller.webhooks.replay",
		"controller.webhooks.clear",
	} {
		if capability := requestCapability(method, nil); capability != capabilityIntegrations {
			t.Fatalf("%s capability=%q", method, capability)
		}
	}
}

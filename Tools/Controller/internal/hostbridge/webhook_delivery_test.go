package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

type webhookRoundTripFunc func(*http.Request) (*http.Response, error)

func (function webhookRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestWebhookTemplatePreservesJSONStringsAndObjectsLosslessly(t *testing.T) {
	event := controller.Event{
		ID: 42, Time: time.Date(2026, 8, 2, 8, 9, 10, 123, time.UTC),
		Kind: "message", Text: "He said \"yes\"\nسطر دوم", Source: "board",
		Metadata: map[string]string{"nested": `{"enabled":true}`, "quote": `"`},
	}
	encoded, err := renderWebhookTemplate(
		`{"id":{{id}},"kind":{{kind}},"text":"{{text}}","metadata":{{metadata}},"event":{{event}}}`,
		event,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID       uint64            `json:"id"`
		Kind     string            `json:"kind"`
		Text     string            `json:"text"`
		Metadata map[string]string `json:"metadata"`
		Event    controller.Event  `json:"event"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("rendered body is not JSON: %v\n%s", err, encoded)
	}
	if result.ID != event.ID || result.Kind != event.Kind || result.Text != event.Text ||
		result.Metadata["nested"] != event.Metadata["nested"] || result.Event.Text != event.Text {
		t.Fatalf("rendered body lost data: %#v", result)
	}
	if _, err := renderWebhookTemplate(`{"text":"{{missing}}"}`, event, true); err == nil {
		t.Fatal("unknown placeholder was accepted")
	}
	if _, err := renderWebhookTemplate(`{"text":"{{text}}"`, event, true); err == nil {
		t.Fatal("malformed JSON template was accepted")
	}
}

func TestWebhookTemplateRejectsExpandedBodiesAboveDeliveryLimit(t *testing.T) {
	event := controller.Event{Text: strings.Repeat("x", maxWebhookDeliveryBytes/2)}
	if _, err := renderWebhookTemplate("{{event}}{{event}}{{event}}", event, false); err == nil {
		t.Fatal("placeholder expansion above the delivery limit was accepted")
	} else if !strings.Contains(err.Error(), "rendered webhook body exceeds") {
		t.Fatalf("unexpected expansion error: %v", err)
	}
}

func TestWebhookErrorsAreBoundedAndRemainValidUTF8(t *testing.T) {
	message := boundedWebhookError(errors.New(strings.Repeat("خطا", maxWebhookErrorBytes)))
	if len(message) > maxWebhookErrorBytes {
		t.Fatalf("bounded error length=%d", len(message))
	}
	if !utf8.ValidString(message) {
		t.Fatal("bounded error split a UTF-8 code point")
	}
}

func TestWebhookAttemptAddsCorrelationIdempotencyAndHMACHeaders(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 30, 0, 0, time.UTC)
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(context.Background())
		capturedBody, _ = io.ReadAll(request.Body)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})}
	config := appconfig.Webhook{
		Name: "audit", Enabled: true, URL: "https://example.test/hook?scope=host",
		Method: http.MethodPost, BodyTemplate: `{"text":"{{text}}"}`,
		SigningSecret: "0123456789abcdef",
	}
	delivery := webhookDelivery{
		ID: "delivery-7", CorrelationID: "correlation-7",
		IdempotencyKey: "key-7", Attempts: 2,
		Event: controller.Event{ID: 7, Kind: "door", Text: "opened"},
	}
	result := executeWebhookAttempt(
		context.Background(), client, config, delivery,
		"attempt-2", "nonce-2", now,
	)
	if result.Err != nil || result.Status != http.StatusNoContent {
		t.Fatalf("attempt=%#v", result)
	}
	for header, want := range map[string]string{
		"Idempotency-Key":               "key-7",
		"X-PCController-Delivery-ID":    "delivery-7",
		"X-PCController-Correlation-ID": "correlation-7",
		"X-PCController-Attempt-ID":     "attempt-2",
		"X-PCController-Attempt":        "2",
		"X-PCController-Timestamp":      "1785673800",
		"X-PCController-Nonce":          "nonce-2",
	} {
		if got := captured.Header.Get(header); got != want {
			t.Fatalf("%s=%q want=%q", header, got, want)
		}
	}
	wantSignature := "v1=" + webhookSignature(
		config.SigningSecret, "1785673800", "nonce-2", http.MethodPost,
		"/hook?scope=host", "delivery-7", capturedBody,
	)
	if got := captured.Header.Get("X-PCController-Signature"); got != wantSignature {
		t.Fatalf("signature=%q want=%q", got, wantSignature)
	}
}

func TestWebhookAttemptRejectsRedirectWithoutForwardingSensitiveHeaders(t *testing.T) {
	redirectDestinationCalled := make(chan http.Header, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectDestinationCalled <- request.Header.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL+"/unvalidated", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	config := appconfig.Webhook{
		Name: "redirect", Enabled: true, URL: redirector.URL + "/configured",
		Method:        http.MethodPost,
		Headers:       map[string]string{"Authorization": "Bearer configured-secret"},
		SigningSecret: "0123456789abcdef",
	}
	delivery := webhookDelivery{
		ID: "delivery-redirect", CorrelationID: "correlation-redirect",
		IdempotencyKey: "idempotency-redirect", Attempts: 1,
		Event: controller.Event{ID: 9, Kind: "redirect", Text: "do not forward"},
	}
	var inheritedRedirectPolicyCalled atomic.Bool
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		inheritedRedirectPolicyCalled.Store(true)
		return nil
	}}
	result := executeWebhookAttempt(
		context.Background(), client, config, delivery,
		"attempt-redirect", "nonce-redirect", time.Now().UTC(),
	)
	if result.Status != http.StatusTemporaryRedirect || result.Retryable || result.Err == nil {
		t.Fatalf("redirect result=%#v", result)
	}
	select {
	case headers := <-redirectDestinationCalled:
		t.Fatalf("redirect destination received sensitive webhook headers: %#v", headers)
	case <-time.After(150 * time.Millisecond):
	}
	if inheritedRedirectPolicyCalled.Load() {
		t.Fatal("configured HTTP client redirect policy escaped the webhook no-redirect rule")
	}
}

func TestWebhookAttemptDoesNotExposeTargetURLInTransportErrors(t *testing.T) {
	const secret = "query-secret-must-not-be-persisted"
	client := &http.Client{Transport: webhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	config := appconfig.Webhook{
		Name: "sanitized", Enabled: true,
		URL: "https://example.test/hook?token=" + secret, Method: http.MethodPost,
	}
	result := executeWebhookAttempt(
		context.Background(), client, config,
		webhookDelivery{ID: "delivery-sanitized", IdempotencyKey: "key", Attempts: 1},
		"attempt", "nonce", time.Now().UTC(),
	)
	if result.Err == nil || !result.Retryable {
		t.Fatalf("transport result=%#v", result)
	}
	if strings.Contains(result.Err.Error(), secret) || strings.Contains(result.Err.Error(), config.URL) {
		t.Fatalf("transport error exposed target URL: %v", result.Err)
	}
}

func TestManagerDispatchUsesDurableWebhookQueue(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Clone(context.Background())
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.OutboundWebhooks = []appconfig.Webhook{{
			Name: "manager", Enabled: true, EventKind: "door",
			URL: server.URL, Method: http.MethodPost,
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(8))
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, client, store, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		manager.Close()
	}()
	runtime.PublishHostEvent("door", "opened")
	select {
	case request := <-requests:
		if request.Header.Get("Idempotency-Key") == "" || request.Header.Get("X-PCController-Correlation-ID") == "" {
			t.Fatalf("durable dispatch headers=%#v", request.Header)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manager event was not delivered through the durable queue")
	}
	deadline := time.Now().Add(2 * time.Second)
	for manager.Status().WebhooksDelivered != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("manager status=%#v", manager.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(defaultWebhookQueuePath(store.Path())); err != nil {
		t.Fatalf("durable manager queue state: %v", err)
	}
}

func TestWebhookQueuePersistsWithoutSecretsRecoversAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "webhooks.json")
	config := appconfig.Webhook{
		Name: "recovery", Enabled: true, URL: "https://secret.example.test/hook?token=hidden",
		Method:        http.MethodPost,
		Headers:       map[string]string{"Authorization": "Bearer never-persist-this"},
		SigningSecret: "never-persist-this-secret",
	}
	resolver := func(name string) (appconfig.Webhook, bool) {
		return config, strings.EqualFold(name, config.Name)
	}
	ids := sequentialWebhookIDs()
	first, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: resolver, NewID: ids,
	})
	if err != nil {
		t.Fatal(err)
	}
	event := controller.Event{ID: 88, Kind: "door", Text: "opened"}
	deliveryID, duplicate, err := first.Enqueue(config, event)
	if err != nil || duplicate {
		t.Fatalf("first enqueue id=%q duplicate=%t err=%v", deliveryID, duplicate, err)
	}
	duplicateID, duplicate, err := first.Enqueue(config, event)
	if err != nil || !duplicate || duplicateID != deliveryID {
		t.Fatalf("duplicate enqueue id=%q duplicate=%t err=%v", duplicateID, duplicate, err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{config.URL, config.Headers["Authorization"], config.SigningSecret} {
		if strings.Contains(string(persisted), secret) {
			t.Fatalf("durable queue persisted target secret %q", secret)
		}
	}

	delivered := make(chan string, 1)
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		delivered <- request.Header.Get("Idempotency-Key")
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	restarted, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: resolver, HTTPClient: client, NewID: sequentialWebhookIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	restarted.Start()
	select {
	case key := <-delivered:
		if key == "" {
			t.Fatal("recovered delivery omitted its idempotency key")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("persisted delivery was not recovered after restart")
	}
	waitWebhookStatus(t, restarted, func(status WebhookQueueStatus) bool {
		return status.Delivered == 1 && status.Pending == 0 && status.Completed == 1
	})
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := restarted.Close(closeContext); err != nil {
		t.Fatal(err)
	}

	completed, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: resolver, NewID: sequentialWebhookIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	completedID, duplicate, err := completed.Enqueue(config, event)
	if err != nil || !duplicate || completedID != deliveryID {
		t.Fatalf("completed dedupe id=%q duplicate=%t err=%v", completedID, duplicate, err)
	}
}

func TestWebhookQueueSaturationIsBoundedAndCounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhooks.json")
	config := appconfig.Webhook{Name: "bounded", Enabled: true, URL: "https://example.test", Method: http.MethodPost}
	queue, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: func(string) (appconfig.Webhook, bool) { return config, true },
		MaxPending: 1, NewID: sequentialWebhookIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.Enqueue(config, controller.Event{ID: 1, Kind: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.Enqueue(config, controller.Event{ID: 2, Kind: "two"}); !errors.Is(err, errWebhookQueueFull) {
		t.Fatalf("saturation error=%v", err)
	}
	status := queue.Status()
	if status.Pending != 1 || status.Dropped != 1 {
		t.Fatalf("saturation status=%#v", status)
	}
}

func TestWebhookWorkersPreventSlowTargetHeadOfLineBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhooks.json")
	slow := appconfig.Webhook{Name: "slow", Enabled: true, URL: "https://example.test/slow", Method: http.MethodPost, TimeoutMS: 2000}
	fast := appconfig.Webhook{Name: "fast", Enabled: true, URL: "https://example.test/fast", Method: http.MethodPost, TimeoutMS: 2000}
	slowStarted := make(chan struct{}, 1)
	releaseSlow := make(chan struct{})
	fastDelivered := make(chan struct{}, 1)
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/slow" {
			slowStarted <- struct{}{}
			select {
			case <-releaseSlow:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		} else {
			fastDelivered <- struct{}{}
		}
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	queue, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path,
		Resolve: func(name string) (appconfig.Webhook, bool) {
			if name == slow.Name {
				return slow, true
			}
			return fast, name == fast.Name
		},
		HTTPClient: client, NewID: sequentialWebhookIDs(), Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	queue.Start()
	if _, _, err := queue.Enqueue(slow, controller.Event{ID: 10, Kind: "slow"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow delivery did not start")
	}
	if _, _, err := queue.Enqueue(fast, controller.Event{ID: 11, Kind: "fast"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fastDelivered:
	case <-time.After(time.Second):
		t.Fatal("fast target was blocked behind a slow target")
	}
	close(releaseSlow)
	waitWebhookStatus(t, queue, func(status WebhookQueueStatus) bool {
		return status.Delivered == 2 && status.Pending == 0
	})
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.Close(closeContext); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookShutdownDrainOverridesRetryDelay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhooks.json")
	config := appconfig.Webhook{
		Name: "drain", Enabled: true, URL: "https://example.test/hook",
		Method: http.MethodPost, MaxAttempts: 3,
		RetryInitialMS: 50, RetryMaximumMS: 100,
	}
	var mu sync.Mutex
	requests := make([]*http.Request, 0, 2)
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, request.Clone(context.Background()))
		attempt := len(requests)
		mu.Unlock()
		status := http.StatusNoContent
		header := make(http.Header)
		if attempt == 1 {
			status = http.StatusServiceUnavailable
			header.Set("Retry-After", "3600")
		}
		return &http.Response{
			StatusCode: status, Header: header,
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	queue, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: func(string) (appconfig.Webhook, bool) { return config, true },
		HTTPClient: client, NewID: sequentialWebhookIDs(), RandomFloat: func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatal(err)
	}
	queue.Start()
	if _, _, err := queue.Enqueue(config, controller.Event{ID: 3, Kind: "retry"}); err != nil {
		t.Fatal(err)
	}
	waitWebhookStatus(t, queue, func(status WebhookQueueStatus) bool {
		return status.Retried == 1 && status.Pending == 1
	})
	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := queue.Close(closeContext); err != nil {
		t.Fatal(err)
	}
	status := queue.Status()
	if status.Delivered != 1 || status.Pending != 0 {
		t.Fatalf("drain status=%#v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("drain request count=%d", len(requests))
	}
	if requests[0].Header.Get("Idempotency-Key") != requests[1].Header.Get("Idempotency-Key") ||
		requests[0].Header.Get("X-PCController-Attempt-ID") == requests[1].Header.Get("X-PCController-Attempt-ID") {
		t.Fatal("retry did not preserve idempotency or rotate its attempt ID")
	}
}

func TestWebhookDrainDeadlineCancelsAttemptAndLeavesRestartableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhooks.json")
	config := appconfig.Webhook{
		Name: "restart-after-cancel", Enabled: true,
		URL: "https://example.test/blocked", Method: http.MethodPost,
		TimeoutMS: 5000, MaxAttempts: 3,
	}
	started := make(chan struct{}, 1)
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	resolver := func(string) (appconfig.Webhook, bool) { return config, true }
	queue, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: resolver, HTTPClient: client,
		NewID: sequentialWebhookIDs(), Workers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	queue.Start()
	if _, _, err := queue.Enqueue(config, controller.Event{ID: 12, Kind: "blocked"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocked delivery did not start")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := queue.Close(closeContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain deadline error=%v", err)
	}
	if status := queue.Status(); status.Pending != 1 || status.InFlight != 0 {
		t.Fatalf("cancelled queue status=%#v", status)
	}
	recovered, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path, Resolve: resolver, NewID: sequentialWebhookIDs(), Workers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status := recovered.Status(); status.Pending != 1 || status.Dead != 0 {
		t.Fatalf("recovered cancelled state=%#v", status)
	}
}

func TestMalformedWebhookTargetMovesToDeadLettersAndCanReplayOrClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhooks.json")
	var valid atomic.Bool
	config := appconfig.Webhook{Name: "repairable", Enabled: true, URL: "://malformed", Method: http.MethodPost, MaxAttempts: 1}
	client := &http.Client{Transport: webhookRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("")), Request: request,
		}, nil
	})}
	queue, err := newWebhookDeliveryQueue(webhookQueueOptions{
		Path: path,
		Resolve: func(string) (appconfig.Webhook, bool) {
			resolved := config
			if valid.Load() {
				resolved.URL = "https://example.test/repaired"
			}
			return resolved, true
		},
		HTTPClient: client, NewID: sequentialWebhookIDs(),
	})
	if err != nil {
		t.Fatal(err)
	}
	queue.Start()
	deliveryID, _, err := queue.Enqueue(config, controller.Event{ID: 4, Kind: "malformed"})
	if err != nil {
		t.Fatal(err)
	}
	waitWebhookStatus(t, queue, func(status WebhookQueueStatus) bool { return status.Dead == 1 })
	dead := queue.Dead(10)
	if len(dead) != 1 || dead[0].ID != deliveryID || !strings.Contains(dead[0].LastError, "absolute HTTP(S)") {
		t.Fatalf("dead letters=%#v", dead)
	}
	manager := &Manager{webhooks: queue}
	listed, err := manager.WebhookCommand(context.Background(), []string{"dead", "10"})
	if err != nil || !strings.Contains(listed, deliveryID) || strings.Contains(listed, config.URL) {
		t.Fatalf("dead-letter command output=%q err=%v", listed, err)
	}
	if _, err := manager.WebhookCommand(context.Background(), []string{"replay", "all"}); err == nil {
		t.Fatal("bulk replay did not require explicit confirmation")
	}
	valid.Store(true)
	if output, err := manager.WebhookCommand(context.Background(), []string{"replay", deliveryID}); err != nil || !strings.Contains(output, `"replayed": 1`) {
		t.Fatalf("replay output=%q err=%v", output, err)
	}
	waitWebhookStatus(t, queue, func(status WebhookQueueStatus) bool {
		return status.Delivered == 1 && status.Dead == 0
	})
	valid.Store(false)
	secondID, _, err := queue.Enqueue(config, controller.Event{ID: 5, Kind: "clear"})
	if err != nil {
		t.Fatal(err)
	}
	waitWebhookStatus(t, queue, func(status WebhookQueueStatus) bool { return status.Dead == 1 })
	if _, err := manager.WebhookCommand(context.Background(), []string{"clear", "dead", "all"}); err == nil {
		t.Fatal("bulk dead-letter clear did not require explicit confirmation")
	}
	if output, err := manager.WebhookCommand(context.Background(), []string{"clear", "dead", secondID}); err != nil || !strings.Contains(output, `"cleared": 1`) {
		t.Fatalf("clear output=%q err=%v", output, err)
	}
	if queue.Status().Dead != 0 {
		t.Fatal("dead letter remained after explicit clear")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.Close(closeContext); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookRetryAndRetryAfterPrimitivesAreDeterministic(t *testing.T) {
	queue := &webhookDeliveryQueue{randomFloat: func() float64 { return 0.5 }}
	if delay := queue.retryDelay(webhookDelivery{Attempts: 4, RetryInitialMS: 100, RetryMaximumMS: 1000}); delay != 800*time.Millisecond {
		t.Fatalf("retry delay=%s", delay)
	}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if delay := parseWebhookRetryAfter("17", now); delay != 17*time.Second {
		t.Fatalf("delta Retry-After=%s", delay)
	}
	if delay := parseWebhookRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); delay != 90*time.Second {
		t.Fatalf("date Retry-After=%s", delay)
	}
	for _, status := range []int{408, 425, 429, 500, 503} {
		if !webhookStatusRetryable(status) {
			t.Fatalf("HTTP %d was not retryable", status)
		}
	}
	for _, status := range []int{400, 401, 404, 409} {
		if webhookStatusRetryable(status) {
			t.Fatalf("HTTP %d was unexpectedly retryable", status)
		}
	}
}

func waitWebhookStatus(
	t *testing.T,
	queue *webhookDeliveryQueue,
	accept func(WebhookQueueStatus) bool,
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := queue.Status()
		if accept(status) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("webhook status did not converge: %#v", status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func sequentialWebhookIDs() func() string {
	var mu sync.Mutex
	next := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		next++
		return "test-id-" + strconv.Itoa(next)
	}
}

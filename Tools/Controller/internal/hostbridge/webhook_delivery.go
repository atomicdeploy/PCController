package hostbridge

import (
	"bytes"
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/netpolicy"
)

const (
	webhookQueueSchema          = 1
	defaultWebhookMaxPending    = 1024
	defaultWebhookMaxDead       = 512
	defaultWebhookMaxCompleted  = 2048
	defaultWebhookWorkers       = 8
	defaultWebhookMaxAttempts   = 6
	defaultWebhookTimeout       = 5 * time.Second
	defaultWebhookRetryInitial  = 500 * time.Millisecond
	defaultWebhookRetryMaximum  = 30 * time.Second
	webhookCompletedRetention   = 24 * time.Hour
	maxWebhookDeliveryBytes     = 256 << 10
	maxWebhookQueueStateBytes   = 16 << 20
	maxWebhookResponseBodyBytes = 64 << 10
)

var (
	errWebhookQueueClosed = errors.New("outbound webhook queue is closing")
	errWebhookQueueFull   = errors.New("outbound webhook queue is full")
)

// webhookDelivery contains only durable delivery metadata. Target URLs,
// credentials, configured headers, templates, and signing secrets remain in the
// validated host configuration and are resolved by target name for each attempt.
type webhookDelivery struct {
	ID             string           `json:"id"`
	CorrelationID  string           `json:"correlation_id"`
	IdempotencyKey string           `json:"idempotency_key"`
	Target         string           `json:"target"`
	Event          controller.Event `json:"event"`
	CreatedAt      time.Time        `json:"created_at"`
	NextAttemptAt  time.Time        `json:"next_attempt_at"`
	LastAttemptAt  time.Time        `json:"last_attempt_at,omitempty"`
	LastAttemptID  string           `json:"last_attempt_id,omitempty"`
	Attempts       int              `json:"attempts"`
	MaxAttempts    int              `json:"max_attempts"`
	RetryInitialMS int              `json:"retry_initial_ms"`
	RetryMaximumMS int              `json:"retry_maximum_ms"`
	LastStatus     int              `json:"last_status,omitempty"`
	LastError      string           `json:"last_error,omitempty"`
}

type webhookCompletedDelivery struct {
	IdempotencyKey string    `json:"idempotency_key"`
	DeliveryID     string    `json:"delivery_id"`
	DeliveredAt    time.Time `json:"delivered_at"`
}

type webhookQueueCounters struct {
	Enqueued      uint64 `json:"enqueued"`
	Delivered     uint64 `json:"delivered"`
	Retried       uint64 `json:"retried"`
	DeadLettered  uint64 `json:"dead_lettered"`
	Dropped       uint64 `json:"dropped"`
	Duplicates    uint64 `json:"duplicates"`
	DeadDiscarded uint64 `json:"dead_discarded"`
}

type webhookQueueState struct {
	Schema    int                        `json:"schema"`
	Pending   []webhookDelivery          `json:"pending,omitempty"`
	Dead      []webhookDelivery          `json:"dead,omitempty"`
	Completed []webhookCompletedDelivery `json:"completed,omitempty"`
	Counters  webhookQueueCounters       `json:"counters"`
}

// WebhookQueueStatus is safe to expose over the existing command/RPC surface.
// It intentionally excludes endpoint URLs, headers, bodies, and secrets.
type WebhookQueueStatus struct {
	Pending       int        `json:"pending"`
	Dead          int        `json:"dead"`
	Completed     int        `json:"completed_dedupe_records"`
	Enqueued      uint64     `json:"enqueued"`
	Delivered     uint64     `json:"delivered"`
	Retried       uint64     `json:"retried"`
	DeadLettered  uint64     `json:"dead_lettered"`
	Dropped       uint64     `json:"dropped"`
	Duplicates    uint64     `json:"duplicates"`
	DeadDiscarded uint64     `json:"dead_discarded"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	InFlight      int        `json:"in_flight"`
	Closing       bool       `json:"closing"`
}

// WebhookDeliveryView is a bounded, non-secret inspection record.
type WebhookDeliveryView struct {
	ID             string     `json:"id"`
	CorrelationID  string     `json:"correlation_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Target         string     `json:"target"`
	EventID        uint64     `json:"event_id"`
	EventKind      string     `json:"event_kind"`
	CreatedAt      time.Time  `json:"created_at"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	LastAttemptID  string     `json:"last_attempt_id,omitempty"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LastStatus     int        `json:"last_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type webhookNotice struct {
	Kind string
	Text string
	Err  error
}

type webhookQueueOptions struct {
	Path         string
	Resolve      func(string) (appconfig.Webhook, bool)
	HTTPClient   *http.Client
	Now          func() time.Time
	RandomFloat  func() float64
	NewID        func() string
	Notice       func(webhookNotice)
	MaxPending   int
	MaxDead      int
	MaxCompleted int
	Workers      int
}

type webhookDeliveryQueue struct {
	path         string
	resolve      func(string) (appconfig.Webhook, bool)
	httpClient   *http.Client
	now          func() time.Time
	randomFloat  func() float64
	newID        func() string
	notice       func(webhookNotice)
	maxPending   int
	maxDead      int
	maxCompleted int
	workers      int

	mu               sync.Mutex
	state            webhookQueueState
	started          bool
	closing          bool
	forceStop        bool
	inFlight         map[string]bool
	activeCancels    map[string]context.CancelFunc
	persistencePause time.Time
	wake             chan struct{}
	done             chan struct{}
	workerWait       sync.WaitGroup
}

type webhookAttemptResult struct {
	Status     int
	Retryable  bool
	RetryAfter time.Duration
	Err        error
}

func defaultWebhookQueuePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "state", "outbound-webhooks.json")
}

func newWebhookDeliveryQueue(options webhookQueueOptions) (*webhookDeliveryQueue, error) {
	if strings.TrimSpace(options.Path) == "" || options.Resolve == nil {
		return nil, errors.New("webhook delivery queue requires a state path and target resolver")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RandomFloat == nil {
		options.RandomFloat = cryptoRandomFloat
	}
	if options.NewID == nil {
		options.NewID = randomWebhookID
	}
	if options.MaxPending <= 0 {
		options.MaxPending = defaultWebhookMaxPending
	}
	if options.MaxDead <= 0 {
		options.MaxDead = defaultWebhookMaxDead
	}
	if options.MaxCompleted <= 0 {
		options.MaxCompleted = defaultWebhookMaxCompleted
	}
	if options.Workers <= 0 {
		options.Workers = defaultWebhookWorkers
	}
	queue := &webhookDeliveryQueue{
		path: options.Path, resolve: options.Resolve, httpClient: options.HTTPClient,
		now: options.Now, randomFloat: options.RandomFloat, newID: options.NewID,
		notice: options.Notice, maxPending: options.MaxPending,
		maxDead: options.MaxDead, maxCompleted: options.MaxCompleted,
		workers: options.Workers, inFlight: make(map[string]bool),
		activeCancels: make(map[string]context.CancelFunc),
		wake:          make(chan struct{}, options.Workers), done: make(chan struct{}),
		state: webhookQueueState{Schema: webhookQueueSchema},
	}
	if err := queue.load(); err != nil {
		return nil, err
	}
	return queue, nil
}

func (queue *webhookDeliveryQueue) Start() {
	queue.mu.Lock()
	if queue.started {
		queue.mu.Unlock()
		return
	}
	queue.started = true
	queue.mu.Unlock()
	queue.workerWait.Add(queue.workers)
	for worker := 0; worker < queue.workers; worker++ {
		go func() {
			defer queue.workerWait.Done()
			queue.run()
		}()
	}
	go func() {
		queue.workerWait.Wait()
		close(queue.done)
	}()
	queue.signalAll()
}

func (queue *webhookDeliveryQueue) Enqueue(
	target appconfig.Webhook,
	event controller.Event,
) (string, bool, error) {
	now := queue.now().UTC()
	key, err := webhookIdempotencyKey(target.Name, event)
	if err != nil {
		return "", false, err
	}
	if event.Time.IsZero() {
		event.Time = now
	}
	event.Metadata = cloneStringMap(event.Metadata)
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", false, fmt.Errorf("encode webhook event: %w", err)
	}
	if len(encoded) > maxWebhookDeliveryBytes {
		queue.mu.Lock()
		queue.state.Counters.Dropped++
		_ = queue.persistLocked()
		queue.mu.Unlock()
		return "", false, fmt.Errorf("webhook event exceeds %d bytes", maxWebhookDeliveryBytes)
	}
	queue.mu.Lock()
	if queue.closing {
		queue.mu.Unlock()
		return "", false, errWebhookQueueClosed
	}
	if existing := queue.deliveryForKeyLocked(key); existing != "" {
		before := cloneWebhookQueueState(queue.state)
		queue.state.Counters.Duplicates++
		if err := queue.persistLocked(); err != nil {
			queue.state = before
			queue.mu.Unlock()
			return "", false, err
		}
		queue.mu.Unlock()
		queue.emit(webhookNotice{
			Kind: "webhook.duplicate",
			Text: fmt.Sprintf("%s event %d is already represented by delivery %s", target.Name, event.ID, existing),
		})
		return existing, true, nil
	}
	if len(queue.state.Pending) >= queue.maxPending {
		before := cloneWebhookQueueState(queue.state)
		queue.state.Counters.Dropped++
		persistErr := queue.persistLocked()
		if persistErr != nil {
			queue.state = before
		}
		queue.mu.Unlock()
		if persistErr != nil {
			return "", false, fmt.Errorf("%w; record saturation: %v", errWebhookQueueFull, persistErr)
		}
		return "", false, errWebhookQueueFull
	}
	id := queue.newID()
	delivery := webhookDelivery{
		ID: id, CorrelationID: "webhook-" + id, IdempotencyKey: key,
		Target: target.Name, Event: event, CreatedAt: now, NextAttemptAt: now,
		MaxAttempts:    normalizedWebhookMaxAttempts(target.MaxAttempts),
		RetryInitialMS: int(normalizedWebhookRetryInitial(target).Milliseconds()),
		RetryMaximumMS: int(normalizedWebhookRetryMaximum(target).Milliseconds()),
	}
	before := cloneWebhookQueueState(queue.state)
	queue.state.Pending = append(queue.state.Pending, delivery)
	queue.state.Counters.Enqueued++
	if err := queue.persistLocked(); err != nil {
		queue.state = before
		queue.mu.Unlock()
		return "", false, fmt.Errorf("persist outbound webhook delivery: %w", err)
	}
	queue.mu.Unlock()
	queue.signal()
	queue.emit(webhookNotice{
		Kind: "webhook.queued",
		Text: fmt.Sprintf("%s queued event %d (%s) as delivery %s", target.Name, event.ID, event.Kind, id),
	})
	return id, false, nil
}

func (queue *webhookDeliveryQueue) Status() WebhookQueueStatus {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	result := WebhookQueueStatus{
		Pending: len(queue.state.Pending), Dead: len(queue.state.Dead),
		Completed: len(queue.state.Completed), Enqueued: queue.state.Counters.Enqueued,
		Delivered: queue.state.Counters.Delivered, Retried: queue.state.Counters.Retried,
		DeadLettered: queue.state.Counters.DeadLettered, Dropped: queue.state.Counters.Dropped,
		Duplicates:    queue.state.Counters.Duplicates,
		DeadDiscarded: queue.state.Counters.DeadDiscarded, Closing: queue.closing,
		InFlight: len(queue.inFlight),
	}
	for index := range queue.state.Pending {
		candidate := queue.state.Pending[index].NextAttemptAt
		if result.NextAttemptAt == nil || candidate.Before(*result.NextAttemptAt) {
			copyTime := candidate
			result.NextAttemptAt = &copyTime
		}
	}
	return result
}

func (queue *webhookDeliveryQueue) Pending(limit int) []WebhookDeliveryView {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return webhookDeliveryViews(queue.state.Pending, limit, true)
}

func (queue *webhookDeliveryQueue) Dead(limit int) []WebhookDeliveryView {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return webhookDeliveryViews(queue.state.Dead, limit, false)
}

func (queue *webhookDeliveryQueue) Replay(selector string) (int, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return 0, errors.New("dead-letter delivery ID is required")
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closing {
		return 0, errWebhookQueueClosed
	}
	indexes := make([]int, 0)
	for index := range queue.state.Dead {
		if strings.EqualFold(selector, "all") || queue.state.Dead[index].ID == selector {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) == 0 {
		return 0, fmt.Errorf("dead-letter delivery %q was not found", selector)
	}
	if len(queue.state.Pending)+len(indexes) > queue.maxPending {
		return 0, fmt.Errorf("replay requires %d queue slots but only %d are available", len(indexes), queue.maxPending-len(queue.state.Pending))
	}
	before := cloneWebhookQueueState(queue.state)
	now := queue.now().UTC()
	selected := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		selected[index] = true
		delivery := queue.state.Dead[index]
		delivery.Attempts = 0
		delivery.LastAttemptAt = time.Time{}
		delivery.LastAttemptID = ""
		delivery.LastStatus = 0
		delivery.LastError = ""
		delivery.NextAttemptAt = now
		queue.state.Pending = append(queue.state.Pending, delivery)
	}
	remaining := queue.state.Dead[:0]
	for index, delivery := range queue.state.Dead {
		if !selected[index] {
			remaining = append(remaining, delivery)
		}
	}
	queue.state.Dead = remaining
	if err := queue.persistLocked(); err != nil {
		queue.state = before
		return 0, err
	}
	queue.signalLocked()
	return len(indexes), nil
}

func (queue *webhookDeliveryQueue) ClearDead(selector string) (int, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return 0, errors.New("dead-letter delivery ID is required")
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	before := cloneWebhookQueueState(queue.state)
	removed := 0
	remaining := queue.state.Dead[:0]
	for _, delivery := range queue.state.Dead {
		if strings.EqualFold(selector, "all") || delivery.ID == selector {
			removed++
			continue
		}
		remaining = append(remaining, delivery)
	}
	if removed == 0 {
		return 0, fmt.Errorf("dead-letter delivery %q was not found", selector)
	}
	queue.state.Dead = remaining
	if err := queue.persistLocked(); err != nil {
		queue.state = before
		return 0, err
	}
	return removed, nil
}

func (queue *webhookDeliveryQueue) BeginDrain() {
	queue.mu.Lock()
	queue.closing = true
	queue.mu.Unlock()
	queue.signalAll()
}

func (queue *webhookDeliveryQueue) Close(ctx context.Context) error {
	queue.BeginDrain()
	queue.mu.Lock()
	started := queue.started
	queue.mu.Unlock()
	if !started {
		return nil
	}
	queue.signal()
	select {
	case <-queue.done:
		return nil
	case <-ctx.Done():
		queue.mu.Lock()
		queue.forceStop = true
		cancels := make([]context.CancelFunc, 0, len(queue.activeCancels))
		for _, cancel := range queue.activeCancels {
			cancels = append(cancels, cancel)
		}
		queue.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		queue.signalAll()
		<-queue.done
		return ctx.Err()
	}
}

func (queue *webhookDeliveryQueue) run() {
	for {
		delivery, wait, stop := queue.nextDelivery()
		if stop {
			return
		}
		if delivery == nil {
			if wait < 0 {
				<-queue.wake
				continue
			}
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-queue.wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			continue
		}
		queue.attempt(delivery.ID)
	}
}

func (queue *webhookDeliveryQueue) nextDelivery() (*webhookDelivery, time.Duration, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.forceStop {
		return nil, 0, true
	}
	if len(queue.state.Pending) == 0 {
		if queue.closing {
			return nil, 0, true
		}
		return nil, -1, false
	}
	if len(queue.inFlight) >= len(queue.state.Pending) {
		return nil, -1, false
	}
	now := queue.now().UTC()
	if queue.persistencePause.After(now) {
		return nil, queue.persistencePause.Sub(now), false
	}
	selected := -1
	for index := range queue.state.Pending {
		if queue.inFlight[queue.state.Pending[index].ID] {
			continue
		}
		if selected < 0 || queue.state.Pending[index].NextAttemptAt.Before(queue.state.Pending[selected].NextAttemptAt) {
			selected = index
		}
	}
	if selected < 0 {
		return nil, -1, false
	}
	delivery := queue.state.Pending[selected]
	if !queue.closing && delivery.NextAttemptAt.After(now) {
		return nil, delivery.NextAttemptAt.Sub(now), false
	}
	queue.inFlight[delivery.ID] = true
	return &delivery, 0, false
}

func (queue *webhookDeliveryQueue) attempt(deliveryID string) {
	defer queue.releaseInFlight(deliveryID)
	now := queue.now().UTC()
	attemptID := queue.newID()
	queue.mu.Lock()
	index := queue.pendingIndexLocked(deliveryID)
	if index < 0 || queue.forceStop {
		queue.mu.Unlock()
		return
	}
	before := cloneWebhookQueueState(queue.state)
	queue.state.Pending[index].Attempts++
	queue.state.Pending[index].LastAttemptAt = now
	queue.state.Pending[index].LastAttemptID = attemptID
	queue.state.Pending[index].LastError = ""
	queue.state.Pending[index].LastStatus = 0
	if err := queue.persistLocked(); err != nil {
		queue.state = before
		queue.persistencePause = now.Add(time.Second)
		queue.mu.Unlock()
		queue.emit(webhookNotice{Kind: "webhook.persistence.error", Text: "could not record an outbound webhook attempt", Err: err})
		return
	}
	delivery := queue.state.Pending[index]
	queue.mu.Unlock()

	target, ok := queue.resolve(delivery.Target)
	result := webhookAttemptResult{}
	if !ok || !target.Enabled {
		result.Err = fmt.Errorf("target configuration %q is unavailable or disabled", delivery.Target)
	} else {
		timeout := normalizedWebhookTimeout(target)
		requestContext, cancel := context.WithTimeout(context.Background(), timeout)
		queue.mu.Lock()
		if queue.forceStop {
			queue.mu.Unlock()
			cancel()
			return
		}
		queue.activeCancels[deliveryID] = cancel
		queue.mu.Unlock()
		result = executeWebhookAttempt(
			requestContext, queue.httpClient, target, delivery, attemptID,
			queue.newID(), now,
		)
		cancel()
		queue.mu.Lock()
		delete(queue.activeCancels, deliveryID)
		queue.mu.Unlock()
	}
	queue.finishAttempt(delivery.ID, result)
}

func (queue *webhookDeliveryQueue) finishAttempt(
	deliveryID string,
	result webhookAttemptResult,
) {
	now := queue.now().UTC()
	queue.mu.Lock()
	index := queue.pendingIndexLocked(deliveryID)
	if index < 0 {
		queue.mu.Unlock()
		return
	}
	before := cloneWebhookQueueState(queue.state)
	delivery := queue.state.Pending[index]
	delivery.LastStatus = result.Status
	if result.Err != nil {
		delivery.LastError = result.Err.Error()
	}
	notice := webhookNotice{}
	if result.Err == nil && result.Status >= 200 && result.Status < 300 {
		queue.state.Pending = append(queue.state.Pending[:index], queue.state.Pending[index+1:]...)
		queue.state.Counters.Delivered++
		queue.state.Completed = append(queue.state.Completed, webhookCompletedDelivery{
			IdempotencyKey: delivery.IdempotencyKey,
			DeliveryID:     delivery.ID,
			DeliveredAt:    now,
		})
		queue.pruneCompletedLocked(now)
		notice = webhookNotice{
			Kind: "webhook.sent",
			Text: fmt.Sprintf("%s delivered event %d (%s) as %s", delivery.Target, delivery.Event.ID, delivery.Event.Kind, delivery.ID),
		}
	} else if !result.Retryable || delivery.Attempts >= delivery.MaxAttempts {
		queue.state.Pending = append(queue.state.Pending[:index], queue.state.Pending[index+1:]...)
		queue.state.Counters.DeadLettered++
		if len(queue.state.Dead) >= queue.maxDead {
			queue.state.Dead = append([]webhookDelivery(nil), queue.state.Dead[1:]...)
			queue.state.Counters.DeadDiscarded++
		}
		queue.state.Dead = append(queue.state.Dead, delivery)
		notice = webhookNotice{
			Kind: "webhook.dead",
			Text: fmt.Sprintf("%s delivery %s moved to dead letters after %d attempt(s)", delivery.Target, delivery.ID, delivery.Attempts),
			Err:  result.Err,
		}
	} else {
		delay := queue.retryDelay(delivery)
		if result.RetryAfter > delay {
			delay = result.RetryAfter
		}
		delivery.NextAttemptAt = now.Add(delay)
		queue.state.Pending[index] = delivery
		queue.state.Counters.Retried++
		notice = webhookNotice{
			Kind: "webhook.retry",
			Text: fmt.Sprintf("%s delivery %s will retry in %s after attempt %d", delivery.Target, delivery.ID, delay.Round(time.Millisecond), delivery.Attempts),
			Err:  result.Err,
		}
	}
	if err := queue.persistLocked(); err != nil {
		queue.state = before
		queue.persistencePause = now.Add(time.Second)
		queue.mu.Unlock()
		queue.emit(webhookNotice{Kind: "webhook.persistence.error", Text: "could not record an outbound webhook result", Err: err})
		return
	}
	queue.mu.Unlock()
	queue.emit(notice)
	queue.signal()
}

func executeWebhookAttempt(
	ctx context.Context,
	client *http.Client,
	config appconfig.Webhook,
	delivery webhookDelivery,
	attemptID string,
	nonce string,
	now time.Time,
) webhookAttemptResult {
	method := strings.ToUpper(strings.TrimSpace(config.Method))
	target, body, err := prepareWebhookPayload(config, delivery.Event)
	if err != nil {
		return webhookAttemptResult{Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return webhookAttemptResult{Err: fmt.Errorf("build target request: %w", err)}
	}
	for key, value := range config.Headers {
		request.Header.Set(key, value)
	}
	if len(body) != 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Idempotency-Key", delivery.IdempotencyKey)
	request.Header.Set("X-PCController-Delivery-ID", delivery.ID)
	request.Header.Set("X-PCController-Correlation-ID", delivery.CorrelationID)
	request.Header.Set("X-PCController-Attempt-ID", attemptID)
	request.Header.Set("X-PCController-Attempt", strconv.Itoa(delivery.Attempts))
	if secret := config.SigningSecret; secret != "" {
		timestamp := strconv.FormatInt(now.Unix(), 10)
		request.Header.Set("X-PCController-Timestamp", timestamp)
		request.Header.Set("X-PCController-Nonce", nonce)
		request.Header.Set(
			"X-PCController-Signature",
			"v1="+webhookSignature(secret, timestamp, nonce, method, request.URL.RequestURI(), delivery.ID, body),
		)
	}
	// A redirect target has not passed the configured webhook's URL policy and
	// must never inherit authentication, signature, correlation, attempt, or
	// idempotency headers. Treat the first 3xx response as the target's final
	// response; operators can configure the canonical destination explicitly.
	redirectSafeClient := *client
	redirectSafeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := redirectSafeClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errors.New("target request timed out")
		} else {
			var targetError *url.Error
			if errors.As(err, &targetError) && targetError.Err != nil {
				err = targetError.Err
			}
			err = fmt.Errorf("target request failed: %w", err)
		}
		return webhookAttemptResult{Retryable: true, Err: err}
	}
	if response == nil {
		return webhookAttemptResult{Retryable: true, Err: errors.New("target returned no HTTP response")}
	}
	if response.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxWebhookResponseBodyBytes))
		_ = response.Body.Close()
	}
	result := webhookAttemptResult{Status: response.StatusCode}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return result
	}
	result.Retryable = webhookStatusRetryable(response.StatusCode)
	result.RetryAfter = parseWebhookRetryAfter(response.Header.Get("Retry-After"), now)
	result.Err = fmt.Errorf("target returned HTTP %d", response.StatusCode)
	return result
}

func prepareWebhookPayload(
	config appconfig.Webhook,
	event controller.Event,
) (string, []byte, error) {
	method := strings.ToUpper(strings.TrimSpace(config.Method))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return "", nil, fmt.Errorf("unsupported webhook method %q", config.Method)
	}
	parsed, err := netpolicy.ParseHTTPURL(config.URL, "webhook target")
	if err != nil {
		return "", nil, err
	}
	if method == http.MethodGet || method == http.MethodDelete {
		query := parsed.Query()
		query.Set("kind", event.Kind)
		query.Set("text", event.Text)
		query.Set("id", strconv.FormatUint(event.ID, 10))
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil, nil
	}
	if strings.TrimSpace(config.BodyTemplate) == "" {
		encoded, err := json.Marshal(event)
		return parsed.String(), encoded, err
	}
	contentType := config.Headers["Content-Type"]
	if contentType == "" {
		for key, value := range config.Headers {
			if strings.EqualFold(key, "Content-Type") {
				contentType = value
				break
			}
		}
	}
	jsonBody := contentType == "" || strings.Contains(strings.ToLower(contentType), "json")
	encoded, err := renderWebhookTemplate(config.BodyTemplate, event, jsonBody)
	if err != nil {
		return "", nil, err
	}
	return parsed.String(), encoded, nil
}

func renderWebhookTemplate(
	template string,
	event controller.Event,
	jsonBody bool,
) ([]byte, error) {
	if len(template) > maxWebhookDeliveryBytes {
		return nil, fmt.Errorf("webhook body template exceeds %d bytes", maxWebhookDeliveryBytes)
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return nil, err
	}
	stringsByName := map[string]string{
		"id": strconv.FormatUint(event.ID, 10), "kind": event.Kind,
		"text": event.Text, "source": event.Source,
		"time":  event.Time.UTC().Format(time.RFC3339Nano),
		"event": string(eventJSON), "metadata": string(metadataJSON),
	}
	rawByName := map[string][]byte{
		"id":   []byte(strconv.FormatUint(event.ID, 10)),
		"kind": mustJSONMarshal(event.Kind), "text": mustJSONMarshal(event.Text),
		"source": mustJSONMarshal(event.Source),
		"time":   mustJSONMarshal(event.Time.UTC().Format(time.RFC3339Nano)),
		"event":  eventJSON, "metadata": metadataJSON,
	}
	var result bytes.Buffer
	writeBytes := func(value []byte) error {
		if len(value) > maxWebhookDeliveryBytes-result.Len() {
			return fmt.Errorf("rendered webhook body exceeds %d bytes", maxWebhookDeliveryBytes)
		}
		_, _ = result.Write(value)
		return nil
	}
	writeString := func(value string) error {
		if len(value) > maxWebhookDeliveryBytes-result.Len() {
			return fmt.Errorf("rendered webhook body exceeds %d bytes", maxWebhookDeliveryBytes)
		}
		_, _ = result.WriteString(value)
		return nil
	}
	inString, escaped := false, false
	for index := 0; index < len(template); {
		if template[index] == '{' && index+1 < len(template) && template[index+1] == '{' {
			end := strings.Index(template[index+2:], "}}")
			if end < 0 {
				return nil, errors.New("webhook body template contains an unterminated placeholder")
			}
			end += index + 2
			name := strings.TrimSpace(template[index+2 : end])
			plain, ok := stringsByName[name]
			if !ok {
				return nil, fmt.Errorf("webhook body template placeholder %q is unsupported", name)
			}
			if jsonBody {
				if inString {
					quoted := mustJSONMarshal(plain)
					if err := writeBytes(quoted[1 : len(quoted)-1]); err != nil {
						return nil, err
					}
				} else {
					if err := writeBytes(rawByName[name]); err != nil {
						return nil, err
					}
				}
			} else {
				if err := writeString(plain); err != nil {
					return nil, err
				}
			}
			index = end + 2
			continue
		}
		current := template[index]
		if result.Len() >= maxWebhookDeliveryBytes {
			return nil, fmt.Errorf("rendered webhook body exceeds %d bytes", maxWebhookDeliveryBytes)
		}
		_ = result.WriteByte(current)
		if jsonBody {
			if inString {
				if escaped {
					escaped = false
				} else if current == '\\' {
					escaped = true
				} else if current == '"' {
					inString = false
				}
			} else if current == '"' {
				inString = true
			}
		}
		index++
	}
	encoded := result.Bytes()
	if jsonBody {
		if !json.Valid(encoded) {
			return nil, errors.New("rendered webhook body is not valid JSON")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, encoded); err != nil {
			return nil, err
		}
		encoded = append([]byte(nil), compact.Bytes()...)
	}
	return encoded, nil
}

func webhookSignature(
	secret, timestamp, nonce, method, requestURI, deliveryID string,
	body []byte,
) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp)
	_, _ = io.WriteString(mac, "\n"+nonce)
	_, _ = io.WriteString(mac, "\n"+strings.ToUpper(method))
	_, _ = io.WriteString(mac, "\n"+requestURI)
	_, _ = io.WriteString(mac, "\n"+deliveryID+"\n")
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookStatusRetryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func parseWebhookRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil {
		if seconds < 0 {
			return 0
		}
		return boundedRetryAfter(time.Duration(seconds) * time.Second)
	}
	parsed, err := http.ParseTime(value)
	if err != nil || !parsed.After(now) {
		return 0
	}
	return boundedRetryAfter(parsed.Sub(now))
}

func boundedRetryAfter(value time.Duration) time.Duration {
	if value > 24*time.Hour {
		return 24 * time.Hour
	}
	return value
}

func (queue *webhookDeliveryQueue) retryDelay(delivery webhookDelivery) time.Duration {
	base := time.Duration(delivery.RetryInitialMS) * time.Millisecond
	maximum := time.Duration(delivery.RetryMaximumMS) * time.Millisecond
	if base <= 0 {
		base = defaultWebhookRetryInitial
	}
	if maximum < base {
		maximum = defaultWebhookRetryMaximum
	}
	for count := 1; count < delivery.Attempts && base < maximum; count++ {
		if base > maximum/2 {
			base = maximum
			break
		}
		base *= 2
	}
	if base > maximum {
		base = maximum
	}
	random := queue.randomFloat()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	factor := 0.8 + 0.4*random
	return time.Duration(float64(base) * factor)
}

func normalizedWebhookTimeout(config appconfig.Webhook) time.Duration {
	if config.TimeoutMS <= 0 {
		return defaultWebhookTimeout
	}
	return time.Duration(config.TimeoutMS) * time.Millisecond
}

func normalizedWebhookMaxAttempts(value int) int {
	if value <= 0 {
		return defaultWebhookMaxAttempts
	}
	return value
}

func normalizedWebhookRetryInitial(config appconfig.Webhook) time.Duration {
	if config.RetryInitialMS <= 0 {
		return defaultWebhookRetryInitial
	}
	return time.Duration(config.RetryInitialMS) * time.Millisecond
}

func normalizedWebhookRetryMaximum(config appconfig.Webhook) time.Duration {
	initial := normalizedWebhookRetryInitial(config)
	if config.RetryMaximumMS <= 0 {
		if defaultWebhookRetryMaximum < initial {
			return initial
		}
		return defaultWebhookRetryMaximum
	}
	return time.Duration(config.RetryMaximumMS) * time.Millisecond
}

func webhookIdempotencyKey(target string, event controller.Event) (string, error) {
	encoded, err := json.Marshal(struct {
		Target string           `json:"target"`
		Event  controller.Event `json:"event"`
	}{Target: strings.ToLower(strings.TrimSpace(target)), Event: event})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "pcw_" + hex.EncodeToString(digest[:]), nil
}

func webhookDeliveryViews(
	deliveries []webhookDelivery,
	limit int,
	pending bool,
) []WebhookDeliveryView {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	copyDeliveries := append([]webhookDelivery(nil), deliveries...)
	sort.Slice(copyDeliveries, func(left, right int) bool {
		return copyDeliveries[left].CreatedAt.After(copyDeliveries[right].CreatedAt)
	})
	if len(copyDeliveries) > limit {
		copyDeliveries = copyDeliveries[:limit]
	}
	result := make([]WebhookDeliveryView, 0, len(copyDeliveries))
	for _, delivery := range copyDeliveries {
		view := WebhookDeliveryView{
			ID: delivery.ID, CorrelationID: delivery.CorrelationID,
			IdempotencyKey: delivery.IdempotencyKey, Target: delivery.Target,
			EventID: delivery.Event.ID, EventKind: delivery.Event.Kind,
			CreatedAt: delivery.CreatedAt, LastAttemptID: delivery.LastAttemptID,
			Attempts: delivery.Attempts, MaxAttempts: delivery.MaxAttempts,
			LastStatus: delivery.LastStatus, LastError: delivery.LastError,
		}
		if pending {
			next := delivery.NextAttemptAt
			view.NextAttemptAt = &next
		}
		if !delivery.LastAttemptAt.IsZero() {
			last := delivery.LastAttemptAt
			view.LastAttemptAt = &last
		}
		result = append(result, view)
	}
	return result
}

func (queue *webhookDeliveryQueue) pendingIndexLocked(deliveryID string) int {
	for index := range queue.state.Pending {
		if queue.state.Pending[index].ID == deliveryID {
			return index
		}
	}
	return -1
}

func (queue *webhookDeliveryQueue) deliveryForKeyLocked(key string) string {
	for _, delivery := range queue.state.Pending {
		if delivery.IdempotencyKey == key {
			return delivery.ID
		}
	}
	for _, delivery := range queue.state.Dead {
		if delivery.IdempotencyKey == key {
			return delivery.ID
		}
	}
	for _, delivery := range queue.state.Completed {
		if delivery.IdempotencyKey == key {
			return delivery.DeliveryID
		}
	}
	return ""
}

func (queue *webhookDeliveryQueue) pruneCompletedLocked(now time.Time) {
	cutoff := now.Add(-webhookCompletedRetention)
	retained := queue.state.Completed[:0]
	for _, completed := range queue.state.Completed {
		if completed.DeliveredAt.After(cutoff) {
			retained = append(retained, completed)
		}
	}
	queue.state.Completed = retained
	if len(queue.state.Completed) > queue.maxCompleted {
		queue.state.Completed = append(
			[]webhookCompletedDelivery(nil),
			queue.state.Completed[len(queue.state.Completed)-queue.maxCompleted:]...,
		)
	}
}

func (queue *webhookDeliveryQueue) load() error {
	encoded, err := os.ReadFile(queue.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read outbound webhook queue: %w", err)
	}
	if len(encoded) > maxWebhookQueueStateBytes {
		return fmt.Errorf("outbound webhook queue exceeds %d bytes", maxWebhookQueueStateBytes)
	}
	var state webhookQueueState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return fmt.Errorf("decode outbound webhook queue: %w", err)
	}
	if state.Schema != webhookQueueSchema {
		return fmt.Errorf("unsupported outbound webhook queue schema %d", state.Schema)
	}
	if len(state.Pending) > queue.maxPending || len(state.Dead) > queue.maxDead ||
		len(state.Completed) > queue.maxCompleted {
		return errors.New("outbound webhook queue exceeds configured record bounds")
	}
	seenIDs := make(map[string]bool)
	seenKeys := make(map[string]bool)
	for _, delivery := range append(append([]webhookDelivery(nil), state.Pending...), state.Dead...) {
		if delivery.ID == "" || delivery.Target == "" || delivery.IdempotencyKey == "" ||
			delivery.MaxAttempts < 1 || seenIDs[delivery.ID] || seenKeys[delivery.IdempotencyKey] {
			return errors.New("outbound webhook queue contains an invalid or duplicate delivery")
		}
		seenIDs[delivery.ID], seenKeys[delivery.IdempotencyKey] = true, true
	}
	for _, completed := range state.Completed {
		if completed.DeliveryID == "" || completed.IdempotencyKey == "" ||
			seenKeys[completed.IdempotencyKey] {
			return errors.New("outbound webhook queue contains an invalid completed record")
		}
		seenKeys[completed.IdempotencyKey] = true
	}
	queue.state = state
	queue.pruneCompletedLocked(queue.now().UTC())
	return nil
}

func (queue *webhookDeliveryQueue) persistLocked() error {
	queue.state.Schema = webhookQueueSchema
	encoded, err := json.MarshalIndent(queue.state, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxWebhookQueueStateBytes {
		return fmt.Errorf("outbound webhook queue state exceeds %d bytes", maxWebhookQueueStateBytes)
	}
	return writeWebhookQueueFileAtomic(queue.path, encoded, 0o600)
}

func cloneWebhookQueueState(source webhookQueueState) webhookQueueState {
	result := source
	result.Pending = append([]webhookDelivery(nil), source.Pending...)
	for index := range result.Pending {
		result.Pending[index].Event.Metadata = cloneStringMap(source.Pending[index].Event.Metadata)
		result.Pending[index].Event.Payload = append([]byte(nil), source.Pending[index].Event.Payload...)
	}
	result.Dead = append([]webhookDelivery(nil), source.Dead...)
	for index := range result.Dead {
		result.Dead[index].Event.Metadata = cloneStringMap(source.Dead[index].Event.Metadata)
		result.Dead[index].Event.Payload = append([]byte(nil), source.Dead[index].Event.Payload...)
	}
	result.Completed = append([]webhookCompletedDelivery(nil), source.Completed...)
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (queue *webhookDeliveryQueue) releaseInFlight(deliveryID string) {
	queue.mu.Lock()
	delete(queue.inFlight, deliveryID)
	delete(queue.activeCancels, deliveryID)
	finishedDrain := queue.closing && len(queue.state.Pending) == 0
	queue.mu.Unlock()
	if finishedDrain {
		queue.signalAll()
	} else {
		queue.signal()
	}
}

func (queue *webhookDeliveryQueue) signal() {
	select {
	case queue.wake <- struct{}{}:
	default:
	}
}

func (queue *webhookDeliveryQueue) signalAll() {
	for worker := 0; worker < queue.workers; worker++ {
		queue.signal()
	}
}

func (queue *webhookDeliveryQueue) signalLocked() {
	queue.signal()
}

func (queue *webhookDeliveryQueue) emit(notice webhookNotice) {
	if queue.notice != nil && notice.Kind != "" {
		queue.notice(notice)
	}
}

func randomWebhookID() string {
	buffer := make([]byte, 16)
	if _, err := cryptorand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	stamp := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(stamp[:16])
}

func cryptoRandomFloat() float64 {
	var buffer [8]byte
	if _, err := cryptorand.Read(buffer[:]); err != nil {
		return 0.5
	}
	return float64(binary.BigEndian.Uint64(buffer[:])>>11) / float64(uint64(1)<<53)
}

func mustJSONMarshal(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

package appcore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/lamp"
)

const (
	diagnosticsReadyPoll = 500 * time.Millisecond
	diagnosticsDuePoll   = 15 * time.Second
	diagnosticsRetry     = time.Minute
)

type diagnosticsState struct {
	mu       sync.RWMutex
	snapshot diagnostics.Snapshot
	// sources contains both public endpoint documents and the sanitized,
	// projection-only station/AP configuration documents. Only public entries
	// are copied into snapshot.Endpoints and therefore serialized over IPC/API.
	sources map[string]diagnostics.EndpointResult

	fetchMu sync.Mutex

	publishMu sync.Mutex
	subsMu    sync.RWMutex
	subs      map[uint64]chan diagnostics.Snapshot
	nextSubID atomic.Uint64
}

func newDiagnosticsState(deviceURL string) *diagnosticsState {
	return &diagnosticsState{
		snapshot: diagnostics.NewSnapshot(deviceURL),
		sources:  make(map[string]diagnostics.EndpointResult),
		subs:     make(map[uint64]chan diagnostics.Snapshot),
	}
}

// DiagnosticsSnapshot returns a deep, caller-owned copy of the latest parsed
// diagnostic cache. Raw endpoint bytes stay in-process; JSON serialization of
// each Document exposes its redacted SafeRaw representation.
func (c *Core) DiagnosticsSnapshot() diagnostics.Snapshot {
	if c == nil || c.diagnostics == nil {
		return diagnostics.NewSnapshot("")
	}
	return c.diagnostics.current(time.Now().UTC())
}

// RefreshDiagnostics fetches exact canonical ESP endpoints and updates the
// shared parsed cache. With no endpoints it refreshes the complete audited
// registry. A partial snapshot is returned alongside joined endpoint errors.
func (c *Core) RefreshDiagnostics(
	ctx context.Context,
	endpoints ...string,
) (diagnostics.Snapshot, error) {
	if ctx == nil {
		return c.DiagnosticsSnapshot(), lamp.ErrNilContext
	}
	client, epoch, err := c.currentClient()
	if err != nil {
		return c.DiagnosticsSnapshot(), err
	}
	cfg := c.Config()
	endpoints, err = normalizeDiagnosticSelection(cfg.Diagnostics, endpoints)
	if err != nil {
		return c.DiagnosticsSnapshot(), err
	}
	return c.refreshDiagnostics(
		ctx,
		client,
		endpoints,
		func() bool {
			c.configMu.RLock()
			defer c.configMu.RUnlock()
			return c.clientEpoch == epoch
		},
	)
}

func (c *Core) fetchDiagnosticRaw(
	ctx context.Context,
	endpoint string,
) ([]byte, error) {
	if ctx == nil {
		return nil, lamp.ErrNilContext
	}
	cfg := c.Config()
	if err := cfg.Diagnostics.ValidateRequest(http.MethodGet, endpoint); err != nil {
		return nil, err
	}
	client, epoch, err := c.currentClient()
	if err != nil {
		return nil, err
	}

	c.diagnostics.fetchMu.Lock()
	defer c.diagnostics.fetchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.diagnostics.setRefreshing(true)
	defer c.diagnostics.setRefreshing(false)

	attempted := time.Now().UTC()
	data, fetchErr := client.Fetch(ctx, endpoint)
	c.configMu.RLock()
	current := c.clientEpoch == epoch
	c.configMu.RUnlock()
	if !current {
		return nil, ErrStopped
	}
	if fetchErr != nil {
		if !errors.Is(fetchErr, context.Canceled) {
			c.diagnostics.recordFailure(endpoint, attempted, fetchErr)
		}
		return nil, fetchErr
	}
	c.diagnostics.recordSuccess(diagnostics.Parse(endpoint, data, attempted))
	return data, nil
}

// SubscribeDiagnostics publishes whole immutable snapshots. Delivery is
// latest-value-wins, matching Core.Subscribe without mixing device metadata
// churn into lamp state events.
func (c *Core) SubscribeDiagnostics(
	buffer int,
) (<-chan diagnostics.Snapshot, func()) {
	if c.diagnostics == nil {
		channel := make(chan diagnostics.Snapshot)
		close(channel)
		return channel, func() {}
	}
	// Diagnostics are whole snapshots rather than an event stream. Retaining
	// more than the newest value wastes memory and can make slow consumers
	// render stale device information. Keep buffer for API compatibility.
	_ = buffer
	id := c.diagnostics.nextSubID.Add(1)
	channel := make(chan diagnostics.Snapshot, 1)
	c.diagnostics.subsMu.Lock()
	c.diagnostics.subs[id] = channel
	c.diagnostics.subsMu.Unlock()

	var once sync.Once
	return channel, func() {
		once.Do(func() {
			c.diagnostics.subsMu.Lock()
			delete(c.diagnostics.subs, id)
			c.diagnostics.subsMu.Unlock()
		})
	}
}

func normalizeDiagnosticSelection(
	policy interface {
		ValidateRequest(method, endpoint string) error
	},
	endpoints []string,
) ([]string, error) {
	if len(endpoints) == 0 {
		return diagnostics.PollEndpoints(), nil
	}
	result := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if err := policy.ValidateRequest(http.MethodGet, endpoint); err != nil {
			return nil, err
		}
		if !slices.Contains(result, endpoint) {
			result = append(result, endpoint)
		}
	}
	return result, nil
}

func (c *Core) runDiagnostics(
	ctx context.Context,
	client *lamp.Client,
	generation uint64,
) {
	ready := time.NewTicker(diagnosticsReadyPoll)
	defer ready.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ready.C:
			snapshot := c.Snapshot()
			if !snapshot.HTTPReachable && !snapshot.WebSocketOnline {
				continue
			}
			_, _ = c.refreshDiagnosticsForGeneration(ctx, client, generation, true)
			goto periodic
		}
	}

periodic:
	ticker := time.NewTicker(diagnosticsDuePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot := c.Snapshot()
			if !snapshot.HTTPReachable && !snapshot.WebSocketOnline {
				continue
			}
			_, _ = c.refreshDiagnosticsForGeneration(ctx, client, generation, true)
		}
	}
}

func (c *Core) refreshDiagnosticsForGeneration(
	ctx context.Context,
	client *lamp.Client,
	generation uint64,
	dueOnly bool,
) (diagnostics.Snapshot, error) {
	endpoints := diagnostics.PollEndpoints()
	if dueOnly {
		endpoints = c.dueDiagnosticEndpoints(time.Now().UTC())
	}
	if len(endpoints) == 0 {
		return c.DiagnosticsSnapshot(), nil
	}
	return c.refreshDiagnostics(
		ctx,
		client,
		endpoints,
		func() bool {
			c.lifecycleMu.Lock()
			defer c.lifecycleMu.Unlock()
			return c.currentGenerationLocked(generation)
		},
	)
}

func (c *Core) dueDiagnosticEndpoints(now time.Time) []string {
	result := make([]string, 0, len(diagnostics.EndpointSpecs()))
	for _, spec := range diagnostics.EndpointSpecs() {
		if !spec.Auto {
			continue
		}
		current, ok := c.diagnostics.sourceResult(spec.Path)
		switch {
		case !ok || current.LastAttempt.IsZero():
			result = append(result, spec.Path)
		case current.LastSuccess.IsZero():
			if now.Sub(current.LastAttempt) >= diagnosticsRetry {
				result = append(result, spec.Path)
			}
		case now.Sub(current.LastSuccess) >= spec.TTL:
			result = append(result, spec.Path)
		}
	}
	return result
}

func (c *Core) refreshDiagnostics(
	ctx context.Context,
	client *lamp.Client,
	endpoints []string,
	stillCurrent func() bool,
) (diagnostics.Snapshot, error) {
	if c == nil || c.diagnostics == nil {
		return diagnostics.NewSnapshot(""), ErrStopped
	}
	c.diagnostics.fetchMu.Lock()
	defer c.diagnostics.fetchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return c.DiagnosticsSnapshot(), err
	}
	if stillCurrent != nil && !stillCurrent() {
		return c.DiagnosticsSnapshot(), ErrStopped
	}

	c.diagnostics.setRefreshing(true)
	defer c.diagnostics.setRefreshing(false)

	var fetchErrors []error
	for _, endpoint := range endpoints {
		if err := ctx.Err(); err != nil {
			fetchErrors = append(fetchErrors, err)
			break
		}
		attempted := time.Now().UTC()
		data, fetchErr := client.Fetch(ctx, endpoint)
		if stillCurrent != nil && !stillCurrent() {
			return c.DiagnosticsSnapshot(), ErrStopped
		}
		if fetchErr != nil {
			if !errors.Is(fetchErr, context.Canceled) {
				c.diagnostics.recordFailure(endpoint, attempted, fetchErr)
			}
			fetchErrors = append(fetchErrors, fmt.Errorf("%s: %w", endpoint, fetchErr))
			continue
		}
		document := diagnostics.Parse(endpoint, data, attempted)
		c.diagnostics.recordSuccess(document)
	}
	return c.DiagnosticsSnapshot(), errors.Join(fetchErrors...)
}

func (c *Core) resetDiagnostics(deviceURL string) {
	if c == nil {
		return
	}
	if c.diagnostics == nil {
		c.diagnostics = newDiagnosticsState(deviceURL)
		return
	}
	c.diagnostics.reset(deviceURL)
}

func (state *diagnosticsState) current(now time.Time) diagnostics.Snapshot {
	state.mu.RLock()
	result := state.snapshot.Clone()
	state.mu.RUnlock()
	for endpoint, endpointResult := range result.Endpoints {
		if spec, ok := diagnostics.LookupEndpoint(endpoint); ok &&
			!endpointResult.LastSuccess.IsZero() {
			endpointResult.Stale = now.Sub(endpointResult.LastSuccess) >= spec.TTL
			result.Endpoints[endpoint] = endpointResult
		}
	}
	return result
}

func (state *diagnosticsState) setRefreshing(refreshing bool) {
	state.mu.Lock()
	state.snapshot.Refreshing = refreshing
	if refreshing {
		state.snapshot.LastAttempt = time.Now().UTC()
	}
	snapshot := state.snapshot.Clone()
	state.mu.Unlock()
	state.publish(snapshot)
}

func (state *diagnosticsState) recordFailure(
	endpoint string,
	attempted time.Time,
	err error,
) {
	state.mu.Lock()
	result := state.sources[endpoint]
	if result.Document.Endpoint == "" {
		result.Document.Endpoint = endpoint
	}
	result.LastAttempt = attempted
	result.Error = err.Error()
	state.sources[endpoint] = result
	state.snapshot.LastAttempt = attempted
	if diagnostics.IsPublicEndpoint(endpoint) {
		state.snapshot.Endpoints[endpoint] = result
	}
	snapshot := state.snapshot.Clone()
	state.mu.Unlock()
	state.publish(snapshot)
}

func (state *diagnosticsState) recordSuccess(document diagnostics.Document) {
	state.mu.Lock()
	result := diagnostics.EndpointResult{
		Document:    document,
		LastAttempt: document.FetchedAt,
		LastSuccess: document.FetchedAt,
	}
	state.sources[document.Endpoint] = result
	if diagnostics.IsPublicEndpoint(document.Endpoint) {
		state.snapshot.Endpoints[document.Endpoint] = result
	}
	state.snapshot.LastAttempt = document.FetchedAt
	state.snapshot.UpdatedAt = document.FetchedAt
	documents := make(map[string]diagnostics.Document, len(state.sources))
	for endpoint, endpointResult := range state.sources {
		if !endpointResult.LastSuccess.IsZero() {
			documents[endpoint] = endpointResult.Document
		}
	}
	state.snapshot.Device = diagnostics.Project(documents)
	snapshot := state.snapshot.Clone()
	state.mu.Unlock()
	state.publish(snapshot)
}

func (state *diagnosticsState) reset(deviceURL string) {
	state.mu.Lock()
	state.snapshot = diagnostics.NewSnapshot(deviceURL)
	state.sources = make(map[string]diagnostics.EndpointResult)
	snapshot := state.snapshot.Clone()
	state.mu.Unlock()
	state.publish(snapshot)
}

func (state *diagnosticsState) sourceResult(
	endpoint string,
) (diagnostics.EndpointResult, bool) {
	if state == nil {
		return diagnostics.EndpointResult{}, false
	}
	state.mu.RLock()
	result, ok := state.sources[endpoint]
	state.mu.RUnlock()
	return result, ok
}

func (state *diagnosticsState) publish(snapshot diagnostics.Snapshot) {
	// Serialize publishers so a concurrent older enqueue cannot land after a
	// newer one while each subscriber's single slot is being replaced.
	state.publishMu.Lock()
	defer state.publishMu.Unlock()

	state.subsMu.RLock()
	channels := make([]chan diagnostics.Snapshot, 0, len(state.subs))
	for _, channel := range state.subs {
		channels = append(channels, channel)
	}
	state.subsMu.RUnlock()
	for _, channel := range channels {
	drain:
		for {
			select {
			case <-channel:
			default:
				break drain
			}
		}
		select {
		case channel <- snapshot.Clone():
		default:
			// A concurrent receiver cannot make this fail after the complete
			// drain, but keep publication non-blocking by contract.
		}
	}
}

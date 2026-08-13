package hostui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	SurfaceTUI   = "tui"
	SurfaceWebUI = "webui"

	SurfaceLaunchEnsure = "ensure"
	SurfaceLaunchLaunch = "launch"
	SurfaceLaunchFocus  = "focus"

	maximumSurfaceLaunchEntries = 128
	surfaceLaunchEntryLifetime  = 10 * time.Minute
)

// SurfaceLaunchRequest is deliberately not a generic process contract. A
// caller can select only a product-owned surface and a bounded UI destination;
// executable names, arguments, environment variables, and shell text are not
// accepted anywhere on this path.
type SurfaceLaunchRequest struct {
	Surface        string `json:"surface"`
	Mode           string `json:"mode,omitempty"`
	Target         string `json:"target,omitempty"`
	Page           string `json:"page,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// SurfaceLaunchResult distinguishes an accepted operating-system request from
// a live instance confirmation. Starting a terminal or URL handler does not by
// itself prove that a visible window appeared, so Confirmed remains false until
// the application registry provides that evidence.
type SurfaceLaunchResult struct {
	Surface           string    `json:"surface"`
	Requested         string    `json:"requested"`
	Effective         string    `json:"effective"`
	Target            string    `json:"target,omitempty"`
	InstanceID        string    `json:"instance_id,omitempty"`
	Backend           string    `json:"backend,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	IdempotencyKey    string    `json:"idempotency_key"`
	LauncherProcessID int       `json:"launcher_pid,omitempty"`
	Accepted          bool      `json:"accepted"`
	Confirmed         bool      `json:"confirmed"`
	Deduplicated      bool      `json:"deduplicated"`
	At                time.Time `json:"at"`
}

type surfaceLaunchOperation struct {
	fingerprint string
	ready       chan struct{}
	result      SurfaceLaunchResult
	err         error
	createdAt   time.Time
}

// SurfaceLaunchCoordinator supplies concurrent idempotency for launch calls.
// It is intentionally in-memory: process restart creates a new coordinator and
// live-instance discovery remains the authoritative cross-restart dedupe.
type SurfaceLaunchCoordinator struct {
	mu      sync.Mutex
	now     func() time.Time
	launch  func(context.Context, SurfaceLaunchRequest) (SurfaceLaunchResult, error)
	entries map[string]*surfaceLaunchOperation
}

func NewSurfaceLaunchCoordinator(
	launch func(context.Context, SurfaceLaunchRequest) (SurfaceLaunchResult, error),
) *SurfaceLaunchCoordinator {
	return &SurfaceLaunchCoordinator{
		now: time.Now, launch: launch, entries: make(map[string]*surfaceLaunchOperation),
	}
}

func (coordinator *SurfaceLaunchCoordinator) Launch(
	ctx context.Context,
	request SurfaceLaunchRequest,
) (SurfaceLaunchResult, error) {
	if coordinator == nil || coordinator.launch == nil {
		return SurfaceLaunchResult{}, errors.New("application surface launcher is unavailable")
	}
	request, err := NormalizeSurfaceLaunchRequest(request)
	if err != nil {
		return SurfaceLaunchResult{}, err
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey, err = newSurfaceLaunchKey()
		if err != nil {
			return SurfaceLaunchResult{}, err
		}
	}
	fingerprint := strings.Join([]string{
		request.Surface, request.Mode, request.Target, request.Page,
	}, "\x00")
	if ctx == nil {
		ctx = context.Background()
	}

	coordinator.mu.Lock()
	now := coordinator.now().UTC()
	coordinator.pruneLocked(now)
	if previous, ok := coordinator.entries[request.IdempotencyKey]; ok {
		if previous.fingerprint != fingerprint {
			coordinator.mu.Unlock()
			return SurfaceLaunchResult{}, errors.New("idempotency key was already used for a different surface launch")
		}
		ready := previous.ready
		coordinator.mu.Unlock()
		select {
		case <-ctx.Done():
			return SurfaceLaunchResult{}, ctx.Err()
		case <-ready:
			result := previous.result
			result.Deduplicated = true
			return result, previous.err
		}
	}
	if len(coordinator.entries) >= maximumSurfaceLaunchEntries {
		coordinator.mu.Unlock()
		return SurfaceLaunchResult{}, errors.New("application surface launch queue is busy")
	}
	operation := &surfaceLaunchOperation{
		fingerprint: fingerprint, ready: make(chan struct{}), createdAt: now,
	}
	coordinator.entries[request.IdempotencyKey] = operation
	coordinator.mu.Unlock()

	result, launchErr := coordinator.launch(ctx, request)
	result.Surface = request.Surface
	result.Requested = request.Mode
	result.Target = request.Target
	result.IdempotencyKey = request.IdempotencyKey
	result.Deduplicated = false
	if result.At.IsZero() {
		result.At = coordinator.now().UTC()
	}

	coordinator.mu.Lock()
	operation.result = result
	operation.err = launchErr
	close(operation.ready)
	coordinator.mu.Unlock()
	return result, launchErr
}

func NormalizeSurfaceLaunchRequest(request SurfaceLaunchRequest) (SurfaceLaunchRequest, error) {
	request.Surface = strings.ToLower(strings.TrimSpace(request.Surface))
	switch request.Surface {
	case "web":
		request.Surface = SurfaceWebUI
	case SurfaceTUI, SurfaceWebUI:
	default:
		return SurfaceLaunchRequest{}, errors.New("surface must be tui or webui")
	}
	request.Mode = strings.ToLower(strings.TrimSpace(request.Mode))
	if request.Mode == "" {
		request.Mode = SurfaceLaunchEnsure
	}
	switch request.Mode {
	case SurfaceLaunchEnsure, SurfaceLaunchLaunch, SurfaceLaunchFocus:
	default:
		return SurfaceLaunchRequest{}, errors.New("surface launch mode must be ensure, launch, or focus")
	}
	request.Target = strings.TrimSpace(request.Target)
	if request.Target != "" && !instanceIDPattern.MatchString(request.Target) {
		return SurfaceLaunchRequest{}, errors.New("surface launch target must be one exact instance id")
	}
	if request.Mode == SurfaceLaunchLaunch && request.Target != "" {
		return SurfaceLaunchRequest{}, errors.New("launch mode cannot promise a caller-selected instance id")
	}
	request.Page = strings.ToLower(strings.TrimSpace(request.Page))
	if request.Page != "" && (!instancePagePattern.MatchString(request.Page) || strings.Contains(request.Page, "/")) {
		return SurfaceLaunchRequest{}, errors.New("surface launch page is invalid")
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey != "" && !instanceIDPattern.MatchString(request.IdempotencyKey) {
		return SurfaceLaunchRequest{}, errors.New("surface launch idempotency key is invalid")
	}
	return request, nil
}

func newSurfaceLaunchKey() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate surface launch idempotency key: %w", err)
	}
	return "launch:" + hex.EncodeToString(buffer), nil
}

func (coordinator *SurfaceLaunchCoordinator) pruneLocked(now time.Time) {
	for key, entry := range coordinator.entries {
		select {
		case <-entry.ready:
			if now.Sub(entry.createdAt) >= surfaceLaunchEntryLifetime {
				delete(coordinator.entries, key)
			}
		default:
		}
	}
	if len(coordinator.entries) < maximumSurfaceLaunchEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, entry := range coordinator.entries {
		select {
		case <-entry.ready:
			if oldestKey == "" || entry.createdAt.Before(oldest) {
				oldestKey, oldest = key, entry.createdAt
			}
		default:
		}
	}
	if oldestKey != "" {
		delete(coordinator.entries, oldestKey)
	}
}

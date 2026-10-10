package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

// Authority is a scheduler reservation, not a serial-port connection. Ordinary
// live controls remain available to observers unless production is locked or a
// prepared timeline is playing. E-STOP always remains available.
type MediaAuthorityRequest struct {
	ClientID    string `json:"client_id"`
	Operation   string `json:"operation"`
	RequesterID string `json:"requester_id,omitempty"`
	Label       string `json:"-"`
}
type MediaAuthorityClaim struct {
	ClientID    string    `json:"client_id"`
	Label       string    `json:"label"`
	RequestedAt time.Time `json:"requested_at"`
}
type MediaAuthorityStatus struct {
	OwnerID    string                `json:"owner_id"`
	OwnerLabel string                `json:"owner_label"`
	Exclusive  bool                  `json:"exclusive"`
	Revision   uint64                `json:"revision"`
	Pending    []MediaAuthorityClaim `json:"pending"`
}
type MediaAuthorityError struct {
	Kind   string
	Status MediaAuthorityStatus
}

func (err *MediaAuthorityError) Error() string {
	if err.Status.Exclusive {
		return fmt.Sprintf("hardware publishing is exclusively reserved by %s", err.Status.OwnerLabel)
	}
	return fmt.Sprintf("hardware publishing is owned by %s; request a handoff", err.Status.OwnerLabel)
}

type mediaActorKey struct{}

func WithMediaActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, mediaActorKey{}, id)
}

// Caller holds mediaPlayback.mu. Expiry never steals an explicit production
// reservation. Its stable owner must unlock/release it.
func (runtime *Runtime) authorityLocked() MediaAuthorityStatus {
	state := &runtime.mediaPlayback
	if !state.authority.Exclusive && state.authority.OwnerID != "" && !state.received.IsZero() && time.Since(state.received) >= mediaPlaybackLease {
		state.authority.OwnerID, state.authority.OwnerLabel = "", ""
		state.authority.Revision++
	}
	kept := make([]MediaAuthorityClaim, 0, len(state.authority.Pending))
	for _, claim := range state.authority.Pending {
		if time.Since(claim.RequestedAt) < 30*time.Second {
			kept = append(kept, claim)
		}
	}
	state.authority.Pending = kept
	result := state.authority
	result.Pending = append([]MediaAuthorityClaim{}, kept...)
	return result
}
func (runtime *Runtime) MediaAuthority() MediaAuthorityStatus {
	runtime.mediaPlayback.mu.Lock()
	defer runtime.mediaPlayback.mu.Unlock()
	return runtime.authorityLocked()
}
func (runtime *Runtime) authorityAdmissionLocked(id string) error {
	status := runtime.authorityLocked()
	if status.OwnerID != "" && status.OwnerID != id {
		return &MediaAuthorityError{Kind: "authority_conflict", Status: status}
	}
	return nil
}
func (runtime *Runtime) ChangeMediaAuthority(request MediaAuthorityRequest) (MediaAuthorityStatus, error) {
	if strings.TrimSpace(request.ClientID) == "" || len(request.ClientID) > 180 {
		return runtime.MediaAuthority(), errors.New("registered client_id is required")
	}
	state := &runtime.mediaPlayback
	state.operation.Lock()
	defer state.operation.Unlock()
	state.mu.Lock()
	status := runtime.authorityLocked()
	fail := func(err error) (MediaAuthorityStatus, error) { state.mu.Unlock(); return status, err }
	owner := status.OwnerID == request.ClientID
	transfer := ""
	label := request.Label
	switch request.Operation {
	case "request":
		if status.OwnerID == "" {
			transfer = request.ClientID
		} else if !owner {
			if status.Exclusive {
				return fail(&MediaAuthorityError{Kind: "authority_locked", Status: status})
			}
			found := false
			for _, claim := range status.Pending {
				if claim.ClientID == request.ClientID {
					found = true
				}
			}
			if !found {
				if len(status.Pending) >= 16 {
					return fail(errors.New("authority request queue is full"))
				}
				state.authority.Pending = append(state.authority.Pending, MediaAuthorityClaim{ClientID: request.ClientID, Label: label, RequestedAt: time.Now().UTC()})
				state.authority.Revision++
			}
		}
	case "accept", "reject":
		if !owner {
			return fail(&MediaAuthorityError{Kind: "authority_conflict", Status: status})
		}
		if status.Exclusive && request.Operation == "accept" {
			return fail(&MediaAuthorityError{Kind: "authority_locked", Status: status})
		}
		found := false
		for _, claim := range status.Pending {
			if claim.ClientID == request.RequesterID {
				found = true
				label = claim.Label
			}
		}
		if !found {
			return fail(errors.New("handoff request is absent or expired"))
		}
		if request.Operation == "accept" {
			transfer = request.RequesterID
		} else {
			kept := []MediaAuthorityClaim{}
			for _, claim := range status.Pending {
				if claim.ClientID != request.RequesterID {
					kept = append(kept, claim)
				}
			}
			state.authority.Pending = kept
			state.authority.Revision++
		}
	case "release":
		if !owner {
			return fail(&MediaAuthorityError{Kind: "authority_conflict", Status: status})
		}
		if state.snapshot.Playing && time.Since(state.received) < mediaPlaybackLease {
			return fail(errors.New("pause playback before releasing publishing authority"))
		}
		transfer = "release"
	case "lock", "unlock":
		if !owner {
			return fail(&MediaAuthorityError{Kind: "authority_conflict", Status: status})
		}
		state.authority.Exclusive = request.Operation == "lock"
		state.authority.Revision++
	default:
		return fail(errors.New("authority operation must be request, accept, reject, release, lock or unlock"))
	}
	if transfer != "" {
		if state.snapshot.Playing && time.Since(state.received) < mediaPlaybackLease {
			return fail(errors.New("pause playback before accepting a publishing handoff"))
		}
		// Serialize against prepare/update while cancellation and safe output cleanup
		// complete. No new publisher is admitted until the old executor has stopped.
		state.mu.Unlock()
		runtime.stopMediaTimeline()
		plan, board := runtime.MediaTimeline(), runtime.Snapshot()
		// A superseded generation no longer owns outputs on the current board.
		// Retrying its cleanup can never succeed and must not target a replacement
		// session. Current-generation cleanup still has to be acknowledged.
		if plan.StepCount > 0 && board.Connected && plan.Generation == board.ConnectionGeneration {
			if err := runtime.mediaTimelineOff(plan.Generation); err != nil {
				return runtime.MediaAuthority(), fmt.Errorf("handoff safety cleanup was not acknowledged: %w", err)
			}
		}
		state.mu.Lock()
		if transfer == "release" {
			transfer = ""
			label = ""
		}
		state.authority = MediaAuthorityStatus{OwnerID: transfer, OwnerLabel: label, Revision: state.authority.Revision + 1, Pending: []MediaAuthorityClaim{}}
		// A successful transfer starts a new publisher session, even when the
		// stable client ID belongs to a restarted instance of the same app. Do
		// not carry the prior process's sequence/epoch into the fresh lease or
		// its first positive sequence can be rejected as stale.
		state.snapshot = MediaPlaybackSnapshot{MediaPlaybackUpdate: MediaPlaybackUpdate{ClientID: transfer, Rate: 1}}
		state.received = time.Now()
	}
	result := runtime.authorityLocked()
	state.mu.Unlock()
	runtime.PublishStructuredEvent(Event{Kind: "media.authority", Stream: "state", Source: request.ClientID, State: request.Operation,
		Metadata: map[string]string{"owner_id": result.OwnerID, "owner_label": result.OwnerLabel, "exclusive": fmt.Sprint(result.Exclusive), "revision": fmt.Sprint(result.Revision)}})
	return result, nil
}
func (runtime *Runtime) rejectExclusiveMediaControl(ctx context.Context, opcode byte) error {
	if runtime.emergencyStop.Load() || ctx.Value(mediaTimelineContextKey{}) != nil || !hostRecordableOpcode(opcode) {
		return nil
	}
	actor, _ := ctx.Value(mediaActorKey{}).(string)
	status := runtime.MediaAuthority()
	if actor != status.OwnerID && (status.Exclusive || (status.OwnerID != "" && (opcode == native.OpRelayAllOff || opcode == native.OpPWMAllOff))) {
		return &MediaAuthorityError{Kind: "authority_locked", Status: status}
	}
	return nil
}

// Admission for high-level actions (including autonomous MCU programs), before
// an executor detaches its lifetime from the RPC request.
func (runtime *Runtime) CheckMediaControlAuthority(ctx context.Context) error {
	actor, _ := ctx.Value(mediaActorKey{}).(string)
	status := runtime.MediaAuthority()
	if status.Exclusive && actor != status.OwnerID {
		return &MediaAuthorityError{Kind: "authority_locked", Status: status}
	}
	return nil
}

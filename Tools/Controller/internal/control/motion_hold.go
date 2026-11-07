package control

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

const MotionHoldLifetime = 700 * time.Millisecond

type MotionHoldLease struct {
	ID         string    `json:"id"`
	Side       byte      `json:"side"` // Public sides are 1 and 2.
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type motionHoldWriteKey struct{}

// guardMotionHoldWrite transfers authority only at the actual serial write,
// not when a competing command is rejected or waits for its request slot.
func (r *Runtime) guardMotionHoldWrite(ctx context.Context, generation uint64, opcode byte, payload []byte) context.Context {
	if ctx.Value(motionHoldWriteKey{}) != nil {
		return ctx
	}
	mask := byte(0)
	switch opcode {
	case native.OpRelaySide:
		if len(payload) == 2 && payload[0] < 2 {
			mask = 1 << payload[0]
		}
	case native.OpRelaySet:
		if len(payload) == 2 && payload[0] < 4 {
			mask = 1 << (payload[0] / 2)
		}
	case native.OpRelayAllOff, native.OpRelayTest:
		mask = 3
	}
	if mask == 0 {
		return ctx
	}
	return link.WithRequestWriteGuard(ctx, func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.generation != generation {
			return errors.New("motion write owner changed")
		}
		for side := 0; side < 2; side++ {
			if mask&(1<<side) != 0 {
				r.motionHolds[side] = MotionHoldLease{}
			}
		}
		return nil
	})
}

func (r *Runtime) motionHoldValidLocked(lease MotionHoldLease) bool {
	return lease.Side >= 1 && lease.Side <= 2 && lease.ID != "" && r.generation == lease.Generation && r.motionHolds[lease.Side-1].ID == lease.ID
}

func (r *Runtime) StartMotionHold(ctx context.Context, generation uint64, side, direction byte, id string) (MotionHoldLease, error) {
	if side < 1 || side > 2 || direction < 1 || direction > 2 || generation == 0 {
		return MotionHoldLease{}, errors.New("motion hold requires side 1..2, direction 1..2 and a current generation")
	}
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 {
		return MotionHoldLease{}, errors.New("motion hold id must be a fresh 128-bit hex token")
	}
	lease := MotionHoldLease{ID: id, Side: side, Generation: generation, ExpiresAt: time.Now().Add(MotionHoldLifetime)}
	r.mu.Lock()
	if r.motionHoldRevoked[id].After(time.Now()) || r.motionHolds[0].ID == id || r.motionHolds[1].ID == id {
		r.mu.Unlock()
		return MotionHoldLease{}, errors.New("motion hold was released or already started")
	}
	if r.generation != generation || !r.sessionUsableLocked() {
		r.mu.Unlock()
		return MotionHoldLease{}, errors.New("motion hold owner changed or disconnected")
	}
	// Starting a second hold for this side is explicit replacement. The old
	// timer and any queued release retain their old token and cannot stop it.
	r.motionHolds[side-1] = lease
	r.mu.Unlock()
	time.AfterFunc(MotionHoldLifetime, func() { r.expireMotionHold(lease, 0) })
	ctx = context.WithValue(ctx, motionHoldWriteKey{}, true)
	ctx = link.WithRequestWriteGuard(ctx, func() error {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if !r.motionHoldValidLocked(lease) || !time.Now().Before(r.motionHolds[side-1].ExpiresAt) {
			return errors.New("motion hold expired before write")
		}
		return nil
	})
	intent := r.beginMotionIntent(side-1, motionName(direction))
	frame, err := r.requestAtGeneration(ctx, generation, native.OpRelaySide, []byte{side - 1, direction}, native.OpACK)
	if err != nil {
		r.rollbackMotionIntent(side-1, intent)
		return MotionHoldLease{}, err
	}
	r.publishCommandEvidence(acknowledgedCommandEvidence(ctx, native.OpRelaySide, []byte{side - 1, direction}, frame))
	return lease, nil
}

func (r *Runtime) RenewMotionHold(lease MotionHoldLease) (MotionHoldLease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.emergencyStop.Load() {
		return MotionHoldLease{}, ErrEmergencyStopActive
	}
	if !r.motionHoldValidLocked(lease) || !r.sessionUsableLocked() || r.programming.Active || r.programming.RestorationPending || !time.Now().Before(r.motionHolds[lease.Side-1].ExpiresAt) {
		return MotionHoldLease{}, errors.New("motion hold expired or owner changed")
	}
	lease = r.motionHolds[lease.Side-1]
	lease.ExpiresAt = time.Now().Add(MotionHoldLifetime)
	r.motionHolds[lease.Side-1] = lease
	return lease, nil
}

func (r *Runtime) ReleaseMotionHold(ctx context.Context, lease MotionHoldLease) error {
	r.mu.Lock()
	r.revokeMotionHoldLocked(lease.ID)
	if !r.motionHoldValidLocked(lease) {
		r.mu.Unlock()
		return nil
	} // A stale release must never stop a newer intent.
	r.motionHolds[lease.Side-1].ExpiresAt = time.Now()
	r.mu.Unlock()
	return r.stopMotionHold(ctx, lease)
}

func (r *Runtime) revokeMotionHoldLocked(id string) {
	if id == "" {
		return
	}
	if r.motionHoldRevoked == nil {
		r.motionHoldRevoked = map[string]time.Time{}
	}
	now := time.Now()
	for key, until := range r.motionHoldRevoked {
		if !until.After(now) {
			delete(r.motionHoldRevoked, key)
		}
	}
	if len(r.motionHoldRevoked) >= 256 {
		oldest := ""
		var earliest time.Time
		for key, until := range r.motionHoldRevoked {
			if oldest == "" || until.Before(earliest) {
				oldest, earliest = key, until
			}
		}
		delete(r.motionHoldRevoked, oldest)
	}
	r.motionHoldRevoked[id] = now.Add(30 * time.Second)
}

func (r *Runtime) expireMotionHold(lease MotionHoldLease, attempt int) {
	r.mu.RLock()
	if !r.motionHoldValidLocked(lease) {
		r.mu.RUnlock()
		return
	}
	delay := time.Until(r.motionHolds[lease.Side-1].ExpiresAt)
	r.mu.RUnlock()
	if delay > 0 {
		time.AfterFunc(delay, func() { r.expireMotionHold(lease, attempt) })
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.stopMotionHold(ctx, lease); err != nil {
		r.publish("motion.hold.stop_failed", fmt.Sprintf("motion side %d stop is unconfirmed: %v", lease.Side, err), native.Frame{})
		// Safety-only, bounded stop reassertion. Never retry a movement/start.
		if attempt < 2 {
			time.AfterFunc(250*time.Millisecond, func() { r.expireMotionHold(lease, attempt+1) })
		}
	}
}

func (r *Runtime) stopMotionHold(ctx context.Context, lease MotionHoldLease) error {
	ctx = context.WithValue(ctx, emergencySafetyContextKey{}, true)
	ctx = context.WithValue(ctx, motionHoldWriteKey{}, true)
	ctx = link.WithRequestWriteGuard(ctx, func() error {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if !r.motionHoldValidLocked(lease) {
			return errors.New("motion hold release superseded")
		}
		if time.Now().Before(r.motionHolds[lease.Side-1].ExpiresAt) {
			return errors.New("motion hold was renewed")
		}
		return nil
	})
	frame, err := r.requestAtGenerationPriority(ctx, lease.Generation, link.RequestPrioritySafety, native.OpRelaySide, []byte{lease.Side - 1, 0}, native.OpACK)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.motionHoldValidLocked(lease) {
		r.revokeMotionHoldLocked(lease.ID)
		r.motionHolds[lease.Side-1] = MotionHoldLease{}
	}
	r.mu.Unlock()
	r.publishCommandEvidence(acknowledgedCommandEvidence(ctx, native.OpRelaySide, []byte{lease.Side - 1, 0}, frame))
	return nil
}

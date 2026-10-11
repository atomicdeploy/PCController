package control

import (
	"context"
	"errors"

	"pccontroller.local/controller/internal/native"
)

type mirrorSegmentRevisionKey struct{}

// SegmentRevision orders local display intentions before the next rendered
// frame arrives. It is a host ownership guard, never an invented panel value.
func (runtime *Runtime) SegmentRevision() uint64 { return runtime.segmentRevision.Load() }

func (runtime *Runtime) PresentMirrorSegments(ctx context.Context, cells [4]byte, revision uint64) error {
	ctx = context.WithValue(ctx, mirrorSegmentRevisionKey{}, revision)
	return runtime.Command(ctx, native.OpDisplayText, native.ScheduledSegmentRawPayload(cells, 2000))
}

func segmentIntentCommand(opcode byte, payload []byte) bool {
	switch opcode {
	case native.OpMenuSetPage, native.OpMenuAction, native.OpRemoteKeyGesture:
		return true
	case native.OpDisplayText:
		return len(payload) != 0 && payload[0] != 1 // LCD-only is independent
	}
	return false
}

// The guard and wire request hold one lock. Checking a cache and then taking a
// different send lock would still let an accepted user command be overwritten.
func (runtime *Runtime) guardSegmentRequest(ctx context.Context, opcode byte, payload []byte) (func(), error) {
	if !segmentIntentCommand(opcode, payload) {
		return func() {}, nil
	}
	runtime.segmentCommandMu.Lock()
	unlock := runtime.segmentCommandMu.Unlock
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, err
	}
	if revision, mirror := ctx.Value(mirrorSegmentRevisionKey{}).(uint64); mirror {
		if runtime.segmentRevision.Load() != revision {
			unlock()
			return nil, errors.New("new local panel intention superseded mirror lease")
		}
	} else {
		runtime.segmentRevision.Add(1)
	}
	return unlock, nil
}

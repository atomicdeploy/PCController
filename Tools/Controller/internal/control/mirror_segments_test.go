package control

import (
	"context"
	"testing"

	"pccontroller.local/controller/internal/native"
)

func TestMirrorSegmentGuardNeverOverwritesNewLocalIntent(t *testing.T) {
	runtime := &Runtime{}
	ctx := context.Background()
	unlock, err := runtime.guardSegmentRequest(ctx, native.OpDisplayText, []byte{5})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.SegmentRevision() != 1 {
		t.Fatal("local intention was not ordered before rendering")
	}
	unlock()
	stale := context.WithValue(ctx, mirrorSegmentRevisionKey{}, uint64(0))
	if _, err := runtime.guardSegmentRequest(stale, native.OpDisplayText, []byte{5}); err == nil {
		t.Fatal("stale lease replaced a new local request")
	}
	current := context.WithValue(ctx, mirrorSegmentRevisionKey{}, uint64(1))
	unlock, err = runtime.guardSegmentRequest(current, native.OpDisplayText, []byte{5})
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if runtime.SegmentRevision() != 1 {
		t.Fatal("mirror lease masqueraded as a new local intention")
	}
	unlock, err = runtime.guardSegmentRequest(ctx, native.OpDisplayText, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if runtime.SegmentRevision() != 1 {
		t.Fatal("LCD-only operation invalidated segment ownership")
	}
	unlock, err = runtime.guardSegmentRequest(ctx, native.OpMenuAction, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if runtime.SegmentRevision() != 2 {
		t.Fatal("menu action did not relinquish an old panel lease")
	}
}

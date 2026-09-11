package link

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestRequestAlreadyExpiredDoesNotWrite(t *testing.T) {
	port := newFakePort()
	var writes atomic.Int32
	port.onWrite = func([]byte) { writes.Add(1) }
	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := session.Request(ctx, native.OpGetStatus, nil, native.OpStatus); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired request: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatal("expired request reached the port")
	}
}

func TestRequestCancelledBehindRawWriterDoesNotWriteLater(t *testing.T) {
	port := newFakePort()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var writes atomic.Int32
	port.onWrite = func([]byte) {
		if writes.Add(1) == 1 {
			close(entered)
			<-release
		}
	}
	session := NewForPort("TEST", port)
	defer session.Close()
	rawDone := make(chan error, 1)
	go func() { rawDone <- session.WriteRaw([]byte{1}) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("raw writer did not enter")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requestDone := make(chan error, 1)
	go func() {
		_, err := session.Request(ctx, native.OpGetStatus, nil, native.OpStatus)
		requestDone <- err
	}()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.stateMu.RLock()
		pending := len(session.waiters)
		session.stateMu.RUnlock()
		if pending != 0 {
			break
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("request never reserved its sequence")
		}
	}
	cancel()
	select {
	case err := <-requestDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request remained blocked behind raw writer")
	}
	unblock()
	select {
	case err := <-rawDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("raw writer did not finish after release")
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if err := session.writeRawContext(writeCtx, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 2 {
		t.Fatalf("stale request wrote after release: writes=%d, want only two raw writes", writes.Load())
	}
}

type notifyingDTRPort struct {
	*fakePort
	changes chan bool
}

func (port *notifyingDTRPort) SetDTR(value bool) error {
	err := port.fakePort.SetDTR(value)
	port.changes <- value
	return err
}

func TestPulseDTRCancellationRestoresLineAndWriteGate(t *testing.T) {
	port := &notifyingDTRPort{fakePort: newFakePort(), changes: make(chan bool, 2)}
	var writes atomic.Int32
	port.onWrite = func([]byte) { writes.Add(1) }
	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.PulseDTR(ctx, time.Minute) }()
	select {
	case asserted := <-port.changes:
		if !asserted {
			t.Fatal("DTR was not asserted first")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DTR pulse did not start")
	}
	// Raw writes and resets must share the same exclusion gate.
	select {
	case session.writeGate <- struct{}{}:
		<-session.writeGate
		t.Fatal("DTR did not hold the write gate")
	default:
	}
	blocked, release := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer release()
	if err := session.writeRawContext(blocked, []byte{1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live write was not excluded during reset: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatal("write reached the port during DTR reset")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DTR cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DTR did not stop on cancellation")
	}
	select {
	case released := <-port.changes:
		if released {
			t.Fatal("DTR was not restored")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DTR restoration was not observed")
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if err := session.writeRawContext(writeCtx, []byte{2}); err != nil {
		t.Fatalf("write gate not released: %v", err)
	}
	if writes.Load() != 1 {
		t.Fatalf("writes=%d, want only the post-reset write", writes.Load())
	}
}

func TestRequestAcceptsDelayedACKWithinCallerBudget(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		response, err := native.Encode(native.Frame{Opcode: native.OpACK, Seq: request.Seq, Payload: []byte{request.Opcode, 0}})
		if err != nil {
			t.Errorf("encode: %v", err)
			return
		}
		go func() {
			timer := time.NewTimer(650 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				select {
				case port.reads <- response:
				case <-port.closed:
				}
			case <-port.closed:
			}
		}()
	}
	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := session.Command(ctx, native.OpStatusRGB, []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("valid delayed ACK rejected: %v", err)
	}
}

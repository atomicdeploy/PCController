package link

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestLateACKRetainsReceiveTimeAfterRequestCancellation(t *testing.T) {
	port := newFakePort()
	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var request native.Frame
	port.onWrite = func(encoded []byte) {
		var err error
		request, err = native.Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	_, err := session.Request(ctx, native.OpRelayAllOff, nil, native.OpACK)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for _, field := range []string{"seq=", "admission=", "write=", "response=", "read_return_age=", "data_age="} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("missing %s in %v", field, err)
		}
	}
	lateACK, err := native.Encode(native.Frame{Opcode: native.OpACK, Seq: request.Seq, Payload: []byte{native.OpRelayAllOff, 0}})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	port.reads <- lateACK
	select {
	case event := <-session.Events():
		if event.Frame.Opcode != native.OpACK || event.ReceivedAt.Before(before) || event.ReceivedAt.After(time.Now()) {
			t.Fatalf("late ACK lost receive evidence: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("late ACK was swallowed by the canceled waiter")
	}
}

func TestFragmentedACKSurvivesEmptyReadIntervals(t *testing.T) {
	port := newFakePort()
	session := NewForPort("TEST", port)
	defer session.Close()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := native.Encode(native.Frame{Opcode: native.OpACK, Seq: request.Seq, Payload: []byte{request.Opcode, 0}})
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range reply {
			port.reads <- []byte{value}
			port.reads <- nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	frame, err := session.Request(ctx, native.OpRelayAllOff, nil, native.OpACK)
	if err != nil || frame.Opcode != native.OpACK {
		t.Fatalf("partial ACK lost across idle reads: %+v, %v", frame, err)
	}
	select {
	case event := <-session.Events():
		t.Fatalf("partial/idle read produced an error or unsolicited frame: %+v", event)
	default:
	}
}

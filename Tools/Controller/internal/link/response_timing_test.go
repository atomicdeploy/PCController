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

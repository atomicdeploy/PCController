package localapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalSocketBufferPreservesOrderedReplayUnderBackpressure(
	t *testing.T,
) {
	if terminalSocketBuffer != 16 {
		t.Fatalf("terminal socket buffer = %d, want 16", terminalSocketBuffer)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outbound := make(chan terminalOutbound, terminalSocketBuffer)

	for index := 0; index < terminalSocketBuffer; index++ {
		if !queueTerminalOutbound(ctx, outbound, terminalOutbound{
			messageType: websocket.BinaryMessage,
			payload:     []byte{byte(index)},
		}) {
			t.Fatalf("queue rejected frame %d before reaching capacity", index)
		}
	}

	queued := make(chan bool, 1)
	go func() {
		queued <- queueTerminalOutbound(ctx, outbound, terminalOutbound{
			messageType: websocket.BinaryMessage,
			payload:     []byte{byte(terminalSocketBuffer)},
		})
	}()
	select {
	case <-queued:
		t.Fatal("queue did not apply backpressure at its bounded capacity")
	case <-time.After(40 * time.Millisecond):
	}

	var replay []byte
	replay = append(replay, (<-outbound).payload...)
	select {
	case accepted := <-queued:
		if !accepted {
			t.Fatal("backpressured frame was dropped")
		}
	case <-time.After(time.Second):
		t.Fatal("backpressured frame did not resume after one frame drained")
	}
	for index := 1; index <= terminalSocketBuffer; index++ {
		replay = append(replay, (<-outbound).payload...)
	}
	want := make([]byte, terminalSocketBuffer+1)
	for index := range want {
		want[index] = byte(index)
	}
	if !bytes.Equal(replay, want) {
		t.Fatalf("ordered replay = %v, want %v", replay, want)
	}
}

package control

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestStripOrderAllWirePermutations(t *testing.T) {
	for _, firmware := range appconfig.StripColorOrders() {
		for _, desired := range appconfig.StripColorOrders() {
			for _, payload := range [][]byte{{0xFF, 11, 22, 33, 45}, {4, 11, 22, 33, 45}, {0xFD, 0, 11, 22, 33, 44, 55, 66}} {
				original := bytes.Clone(payload)
				got, err := reorderStripPayload(payload, desired, firmware)
				if err != nil {
					t.Fatal(err)
				}
				start, end := 1, 4
				if payload[0] == 0xFD {
					start, end = 2, len(payload)
				}
				for offset := start; offset < end; offset += 3 {
					for slot := 0; slot < 3; slot++ {
						wire := got[offset+strings.IndexByte("RGB", firmware[slot])]
						want := payload[offset+strings.IndexByte("RGB", desired[slot])]
						if wire != want {
							t.Fatalf("%s -> %s wire[%d]=%d want %d", firmware, desired, slot, wire, want)
						}
					}
				}
				if !bytes.Equal(payload, original) || !bytes.Equal(got[:start], original[:start]) || !bytes.Equal(got[end:], original[end:]) {
					t.Fatal("changed source, index or brightness")
				}
			}
		}
	}
}

func TestStripOrderPreservesControlPayloadsAndDefaults(t *testing.T) {
	for _, payload := range [][]byte{nil, {0xFC}, {0xFE, 100}, {0xFD, 0, 1}, {0, 1, 2}} {
		got, err := reorderStripPayload(payload, "RGB", "BRG")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("changed non-color payload %v: %v %v", payload, got, err)
		}
	}
	if _, err := reorderStripPayload([]byte{0xFF, 1, 2, 3, 255}, "RRB", "BRG"); err == nil {
		t.Fatal("accepted invalid order")
	}
	payload := []byte{0xFF, 1, 2, 3, 255}
	got, err := reorderStripPayload(payload, "", "")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("legacy settings changed colors")
	}
}

func TestStripOrderCommandPersistsWithoutActuation(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	config := appconfig.Defaults()
	writes := 0
	failSave := false
	engine := NewCommandEngine(runtime, CommandOptions{
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(update func(*appconfig.Config) error) error {
			candidate := config
			if err := update(&candidate); err != nil {
				return err
			}
			if failSave {
				return errors.New("disk full")
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			writes++
			return nil
		},
	})
	output, err := engine.Execute(context.Background(), "strip order")
	if err != nil || !strings.Contains(output, `"color_order":"BRG"`) || writes != 0 {
		t.Fatalf("query %s %v", output, err)
	}
	output, err = engine.Execute(context.Background(), "strip order grb")
	if err != nil || config.UI.StripColorOrder != "GRB" || writes != 1 {
		t.Fatalf("save %s %v", output, err)
	}
	// Runtime applies the new setting to the actual transport payload, not UI only.
	got, err := runtime.stripWirePayload(context.Background(), native.OpAddressableLED, []byte{0xFF, 11, 22, 33, 255})
	if err != nil || !bytes.Equal(got, []byte{0xFF, 11, 33, 22, 255}) {
		t.Fatalf("runtime mapping: %v %v", got, err)
	}
	if _, err = engine.Execute(context.Background(), "strip order RRR"); err == nil || writes != 1 {
		t.Fatal("invalid order saved")
	}
	failSave = true
	if _, err = engine.Execute(context.Background(), "strip order RGB"); err == nil || config.UI.StripColorOrder != "GRB" {
		t.Fatal("failed save changed active mapping")
	}
	failSave = false
	if _, err = engine.Execute(context.Background(), "strip order previous"); err != nil || config.UI.StripColorOrder != "GBR" {
		t.Fatal("previous did not wrap common-first list")
	}
}

func TestStripOrderFrameSnapshotSurvivesConfigChange(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.UI{StripColorOrder: "RGB", StripFirmwareOrder: "BRG"}
	runtime.stripConfig = func() appconfig.UI { return config }
	ctx := context.WithValue(context.Background(), stripOrderContextKey{}, runtime.stripOutputConfig())
	config.StripColorOrder = "GRB"
	got, err := runtime.stripWirePayload(ctx, native.OpAddressableLED, []byte{0xFD, 0, 11, 22, 33})
	if err != nil || !bytes.Equal(got, []byte{0xFD, 0, 22, 33, 11}) {
		t.Fatalf("frame changed mapping mid-flight: %v %v", got, err)
	}
}

func TestStripOrderFramedSerialOutput(t *testing.T) {
	runtime := New(Options{})
	port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 8)}
	frames := make(chan native.Frame, 8)
	port.handle = func(frame native.Frame) (native.Frame, error) {
		frames <- frame
		return native.Frame{Opcode: native.OpACK, Payload: []byte{frame.Opcode, 0}}, nil
	}
	session := link.NewForPort("strip-order-fixture", port)
	runtime.session, runtime.generation = session, 1
	t.Cleanup(func() { _ = runtime.Close() })
	config := appconfig.Defaults()
	config.UI.StripColorOrder = "GRB"
	engine := NewCommandEngine(runtime, CommandOptions{HostConfig: func() appconfig.Config { return config }})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := engine.Execute(ctx, "strip fill 11 22 33"); err != nil {
		t.Fatal(err)
	}
	if got := <-frames; got.Opcode != native.OpAddressableLED || !bytes.Equal(got.Payload, []byte{0xFF, 11, 33, 22, 255}) {
		t.Fatalf("fill was not remapped on wire: %#v", got)
	}
	if _, err := engine.Execute(ctx, "strip frame 0B1621"); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{{0xFE, 1}, {0xFD, 0, 11, 33, 22}, {0xFC}} {
		if got := <-frames; !bytes.Equal(got.Payload, want) {
			t.Fatalf("staged wire frame %v want %v", got.Payload, want)
		}
	}
}

func TestStripOrderRawChunksLatchUntilShow(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.UI{StripColorOrder: "RGB"}
	runtime.stripConfig = func() appconfig.UI { return config }
	ctx := context.Background()
	_, _ = runtime.stripWirePayload(ctx, native.OpAddressableLED, []byte{0xFD, 0, 11, 22, 33})
	config.StripColorOrder = "GRB"
	got, err := runtime.stripWirePayload(ctx, native.OpAddressableLED, []byte{0xFD, 1, 11, 22, 33})
	if err != nil || !bytes.Equal(got, []byte{0xFD, 1, 22, 33, 11}) {
		t.Fatal("raw frame changed mapping mid-flight")
	}
	_, _ = runtime.stripWirePayload(ctx, native.OpAddressableLED, []byte{0xFC})
	got, err = runtime.stripWirePayload(ctx, native.OpAddressableLED, []byte{0xFD, 0, 11, 22, 33})
	if err != nil || !bytes.Equal(got, []byte{0xFD, 0, 11, 33, 22}) {
		t.Fatal("new frame did not use new mapping")
	}
}

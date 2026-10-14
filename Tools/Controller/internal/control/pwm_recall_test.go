package control

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestPWMRecallOnlyConfirmedNonzeroCurrentOwner(t *testing.T) {
	runtime := New(Options{})
	runtime.session, runtime.generation = &link.Session{}, 3
	values := native.PWMValues{Available: true}
	values.Values[1] = 239
	runtime.PublishPWMValues(values)
	values.Values[1] = 0
	runtime.PublishPWMValues(values)
	if got := runtime.pwmRecallAtGeneration(3, 1); !got.Known || got.Raw != 239 {
		t.Fatalf("Off erased confirmed memory: %+v", got)
	}
	values.Values[1], values.Available = 1000, false
	runtime.PublishPWMValues(values)
	values.Available, values.Values[2] = true, 5000
	runtime.PublishPWMValues(values)
	if got := runtime.pwmRecallAtGeneration(3, 1); got.Raw != 239 {
		t.Fatalf("unavailable/malformed frame changed memory: %+v", got)
	}
	runtime.generation++
	if runtime.pwmRecallAtGeneration(3, 1).Known || runtime.Snapshot().PWMRecall[1].Known {
		t.Fatal("replaced generation exposed old board memory")
	}
	values = native.PWMValues{Available: true}
	runtime.PublishPWMValues(values)
	if runtime.pwmRecallAtGeneration(4, 1).Known {
		t.Fatal("replacement inherited old nonzero")
	}
	values.Values[1] = 17
	runtime.PublishPWMValues(values)
	if got := runtime.pwmRecallAtGeneration(4, 1); got.Raw != 17 || !got.Known {
		t.Fatal(got)
	}
	runtime.session = nil
	if runtime.Snapshot().PWMRecall[1].Known {
		t.Fatal("disconnected memory advertised")
	}
}

func TestPWMRecallTargetsPreserveRawAndAbsolutePercent(t *testing.T) {
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 1)
	config.OnBehavior = "restore"
	for _, test := range []struct {
		verb    string
		current uint16
		memory  PWMRecallValue
		want    uint16
	}{
		{"on", 0, PWMRecallValue{Known: true, Raw: 239}, 239},
		{"toggle", 0, PWMRecallValue{Known: true, Raw: 239}, 239},
		{"on", 984, PWMRecallValue{Known: true, Raw: 239}, 984},
		{"toggle", 984, PWMRecallValue{Known: true, Raw: 239}, 0},
		{"off", 984, PWMRecallValue{Known: true, Raw: 239}, 0},
		{"on", 0, PWMRecallValue{}, 4095},
		{"on", 0, PWMRecallValue{Known: true, Raw: 5000}, 4095},
	} {
		got, err := pwmActionTarget(test.current, test.verb, config, test.memory)
		if err != nil || got != test.want {
			t.Fatalf("%+v got %d %v", test, got, err)
		}
	}
	config.Gamma = 2.4
	if got, _ := pwmActionTarget(0, "on", config, PWMRecallValue{Known: true, Raw: 239}); got != 239 {
		t.Fatal("curve edit changed remembered electrical duty")
	}
	if appconfig.PWMRawFromPercent(100, config) != 4095 {
		t.Fatal("absolute 100 changed")
	}
	config.OnBehavior = "full"
	if got, _ := pwmActionTarget(0, "on", config, PWMRecallValue{Known: true, Raw: 239}); got != 4095 {
		t.Fatal("default full changed")
	}
}

func TestPWMRecallTwoCallersFramedOffOnToggle(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 1)
	config.OnBehavior = "restore"
	values := native.PWMValues{Available: true}
	values.Values[1], values.Values[2] = 239, 777
	port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
	port.handle = func(frame native.Frame) (native.Frame, error) {
		switch frame.Opcode {
		case native.OpPWMGet:
			payload := make([]byte, 34)
			payload[0] = 1
			for i, raw := range values.Values {
				binary.LittleEndian.PutUint16(payload[2+i*2:], raw)
			}
			return native.Frame{Opcode: native.OpPWMValues, Payload: payload}, nil
		case native.OpPWMSet:
			values.Values[frame.Payload[0]] = binary.LittleEndian.Uint16(frame.Payload[1:])
			return native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}}, nil
		}
		return native.Frame{}, errors.New("unexpected command")
	}
	session := link.NewForPort("fixture", port)
	runtime.session, runtime.generation = session, 7
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Independently supplied clients/configs consume one primary memory, not
	// a draft stored by the caller who switched the channel off.
	for _, step := range []struct {
		verb string
		want uint16
	}{{"off", 0}, {"on", 239}, {"toggle", 0}, {"toggle", 239}} {
		got, err := runtime.ApplyPWMAction(ctx, 7, 1, step.verb, config)
		if err != nil {
			t.Fatal(err)
		}
		if got.Values[1] != step.want || got.Values[2] != 777 {
			t.Fatalf("%s %+v", step.verb, got)
		}
		if recall := runtime.Snapshot().PWMRecall[1]; !recall.Known || recall.Raw != 239 {
			t.Fatal(recall)
		}
	}
}

func TestPWMRecallAcknowledgementIsNotReadback(t *testing.T) {
	runtime := New(Options{})
	runtime.session, runtime.generation = &link.Session{}, 3
	runtime.observe(native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}})
	if runtime.pwmRecallAtGeneration(3, 1).Known {
		t.Fatal("ACK invented confirmed brightness")
	}
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 1)
	config.OnBehavior = "restore"
	reads, writes := 0, 0
	_, err := applyPWMActionWithRecallIO(1, "on", config, func() (native.PWMValues, error) {
		reads++
		if reads == 2 {
			return native.PWMValues{}, context.DeadlineExceeded
		}
		return native.PWMValues{Available: true}, nil
	}, func(uint16) error { writes++; return nil }, func() PWMRecallValue { return runtime.pwmRecallAtGeneration(3, 1) })
	var confirmation *PWMActionConfirmationError
	if !errors.As(err, &confirmation) || writes != 1 || runtime.pwmRecallAtGeneration(3, 1).Known {
		t.Fatalf("failed confirmation invented memory: %v writes%d", err, writes)
	}
}

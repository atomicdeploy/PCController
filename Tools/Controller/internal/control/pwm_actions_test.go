package control

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestPWMActionUsesLogicalCurveAndAuthoritativeReadback(t *testing.T) {
	for _, channel := range []int{0, 8} {
		config := appconfig.PWMChannel(appconfig.Defaults().UI, channel)
		for _, verb := range []string{"on", "off", "increase", "decrease"} {
			t.Run(config.Curve+verb, func(t *testing.T) {
				values := native.PWMValues{Available: true}
				values.Values[channel] = appconfig.PWMRawFromPercent(15, config)
				values.Values[15] = 2222
				wantPercent, _ := pwmActionPercent(values.Values[channel], verb, config)
				wantRaw := appconfig.PWMRawFromPercent(wantPercent, config)
				reads, writes := 0, 0
				got, err := applyPWMActionWithIO(byte(channel), verb, config, func() (native.PWMValues, error) { reads++; return values, nil }, func(raw uint16) error { writes++; values.Values[channel] = raw; return nil })
				if err != nil || reads != 2 || writes != 1 || got.Values[channel] != wantRaw || got.Values[15] != 2222 {
					t.Fatalf("reads=%d writes=%d values=%+v err=%v", reads, writes, got, err)
				}
			})
		}
	}
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 0)
	step := 5.5
	config.StepPercent = &step
	up, _ := pwmActionPercent(0, "increase", config)
	if up != step {
		t.Fatalf("configured logical step ignored: %v", up)
	}
	if top, _ := pwmActionPercent(4095, "increase", config); top != 100 {
		t.Fatal("upper clamp failed")
	}
	if bottom, _ := pwmActionPercent(0, "decrease", config); bottom != 0 {
		t.Fatal("lower clamp failed")
	}
	if _, err := pwmActionPercent(0, "invalid", config); err == nil {
		t.Fatal("unadvertised action accepted")
	}
}

func TestPWMActionFailureNeverInventsSuccessfulReadback(t *testing.T) {
	config := appconfig.PWMChannel(appconfig.Defaults().UI, 0)
	for _, mode := range []string{"read-error", "unavailable", "raw-overflow", "write-error", "readback-error"} {
		t.Run(mode, func(t *testing.T) {
			reads, writes := 0, 0
			_, err := applyPWMActionWithIO(0, "increase", config, func() (native.PWMValues, error) {
				reads++
				if mode == "read-error" || mode == "readback-error" && reads == 2 {
					return native.PWMValues{}, context.DeadlineExceeded
				}
				values := native.PWMValues{Available: mode != "unavailable"}
				if mode == "raw-overflow" {
					values.Values[0] = 5000
				}
				return values, nil
			}, func(uint16) error {
				writes++
				if mode == "write-error" {
					return errors.New("write failed")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failure reported success")
			}
			if (mode == "read-error" || mode == "unavailable" || mode == "raw-overflow") && writes != 0 {
				t.Fatal("unsafe write after failed read")
			}
			if mode == "readback-error" && !strings.Contains(err.Error(), "outcome is unknown") {
				t.Fatalf("false output certainty: %v", err)
			}
		})
	}
}

func TestPWMActionGateCancellationAndZeroingPreemption(t *testing.T) {
	runtime := New(Options{})
	release, err := runtime.acquirePWMAction(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	epoch := runtime.pwmActionEpoch.Load()
	active, cleanup, err := runtime.bindPWMActionContext(context.Background(), epoch)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	canceled, alreadyCanceled := context.WithCancel(context.Background())
	alreadyCanceled()
	_, _ = runtime.Request(canceled, native.OpPWMAllOff, nil, native.OpACK)
	select {
	case <-active.Done():
		t.Fatal("already-canceled zeroing request changed live action")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := runtime.Request(ctx, native.OpPWMSet, []byte{0, 1, 0}, native.OpACK); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("gate wait not cancellable: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if _, err := runtime.Request(ctx2, native.OpPWMAllOff, nil, native.OpACK); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("zeroing blocked on held gate: %v", err)
	}
	select {
	case <-active.Done():
	default:
		t.Fatal("zeroing did not cancel the active change")
	}
	if _, _, err := runtime.bindPWMActionContext(context.Background(), epoch); err == nil {
		t.Fatal("superseded queued action accepted")
	}
	if _, err := runtime.Request(ctx2, native.OpStatus, nil, native.OpStatus); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read-only liveness blocked on PWM gate: %v", err)
	}
}

func TestPWMActionRejectsReplacementGenerationBeforeTransport(t *testing.T) {
	runtime := New(Options{})
	runtime.session = &link.Session{}
	runtime.generation = 9
	_, err := runtime.ApplyPWMAction(context.Background(), 8, 0, "increase", appconfig.PWMChannel(appconfig.Defaults().UI, 0))
	if err == nil || !strings.Contains(err.Error(), "generation 8 is no longer active") {
		t.Fatalf("stale action reached transport: %v", err)
	}
	if _, err := pwmActionPercent(4095, "decrease", appconfig.PWMChannelConfig{StepPercent: floatPointer(math.NaN())}); err == nil {
		t.Fatal("invalid step accepted")
	}
}

func floatPointer(v float64) *float64 { return &v }

type pwmActionTestPort struct {
	*reconnectTestPort
	decoder native.Decoder
	replies chan []byte
	pending []byte
	handle  func(native.Frame) (native.Frame, error)
}

func (port *pwmActionTestPort) Read(destination []byte) (int, error) {
	if len(port.pending) == 0 {
		select {
		case port.pending = <-port.replies:
		case <-port.closed:
			return 0, errors.New("fixture closed")
		}
	}
	n := copy(destination, port.pending)
	port.pending = port.pending[n:]
	return n, nil
}

func (port *pwmActionTestPort) Write(data []byte) (int, error) {
	frames, failures := port.decoder.Feed(data)
	if len(failures) > 0 {
		return 0, failures[0]
	}
	for _, frame := range frames {
		response, err := port.handle(frame)
		if err != nil {
			return 0, err
		}
		response.Seq = frame.Seq
		encoded, err := native.Encode(response)
		if err != nil {
			return 0, err
		}
		port.replies <- encoded
	}
	return len(data), nil
}

func TestPWMActionFramedTransportReadbackAndReplacement(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable", true: "replaced during read"}[replaced], func(t *testing.T) {
			runtime := New(Options{})
			config := appconfig.PWMChannel(appconfig.Defaults().UI, 0)
			var mu sync.Mutex
			values := native.PWMValues{Available: true}
			values.Values[0] = appconfig.PWMRawFromPercent(15, config)
			values.Values[1] = 1234
			before := values.Values[0]
			reads, writes := 0, 0
			port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
			port.handle = func(frame native.Frame) (native.Frame, error) {
				mu.Lock()
				defer mu.Unlock()
				if frame.Opcode == native.OpPWMGet {
					reads++
					if replaced && reads == 1 {
						runtime.mu.Lock()
						runtime.generation++
						runtime.mu.Unlock()
					}
					payload := make([]byte, 34)
					payload[0] = 1
					for i, raw := range values.Values {
						binary.LittleEndian.PutUint16(payload[2+i*2:], raw)
					}
					return native.Frame{Opcode: native.OpPWMValues, Payload: payload}, nil
				}
				if frame.Opcode != native.OpPWMSet || len(frame.Payload) != 3 {
					return native.Frame{}, errors.New("unexpected fixture command")
				}
				writes++
				values.Values[frame.Payload[0]] = binary.LittleEndian.Uint16(frame.Payload[1:])
				return native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}}, nil
			}
			session := link.NewForPort("fixture", port)
			runtime.session, runtime.generation = session, 7
			t.Cleanup(func() { _ = session.Close() })
			after := runtime.LatestEventID()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := runtime.ApplyPWMAction(ctx, 7, 0, "increase", config)
			mu.Lock()
			observedReads, observedWrites := reads, writes
			mu.Unlock()
			if replaced {
				if err == nil || observedWrites != 0 {
					t.Fatalf("replacement board written: reads%d writes%d err%v", observedReads, observedWrites, err)
				}
				return
			}
			want := appconfig.PWMRawFromPercent(appconfig.PWMPercentFromRaw(before, config)+1, config)
			if err != nil || observedReads != 2 || observedWrites != 1 || got.Values[0] != want || got.Values[1] != 1234 {
				t.Fatalf("framed action failed: %+v reads%d writes%d err%v", got, observedReads, observedWrites, err)
			}
			event, err := runtime.WaitEvent(ctx, after, "pwm.changed")
			for err == nil && event.Metadata["pwm.0"] != strconv.Itoa(int(want)) {
				event, err = runtime.WaitEvent(ctx, event.ID, "pwm.changed")
			}
			if err != nil || event.Metadata["connection_generation"] != "7" || event.Metadata["pwm.1"] != "1234" || event.Metadata["revision"] == "" {
				t.Fatalf("readback broadcast missing: %+v %v", event, err)
			}
		})
	}
}

func TestPWMAbsoluteCommandUsesConfiguredCurveAndRejectsInvalidPercent(t *testing.T) {
	for _, curve := range []string{"linear", "gamma"} {
		for _, text := range []string{"0", "12.5", "35", "100", "-1", "100.1", "NaN", "+Inf"} {
			t.Run(curve+"/"+text, func(t *testing.T) {
				runtime := New(Options{RequestTimeout: time.Second})
				t.Cleanup(func() { _ = runtime.Close() })
				config := appconfig.Defaults()
				channel := appconfig.PWMChannel(config.UI, 0)
				channel.Curve, channel.Gamma = curve, 2.2
				config.UI.PWMChannels[appconfig.PWMChannelKey(0)] = channel
				var mu sync.Mutex
				writes := 0
				var actual uint16
				port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
				port.handle = func(frame native.Frame) (native.Frame, error) {
					mu.Lock()
					defer mu.Unlock()
					if frame.Opcode != native.OpPWMSet || len(frame.Payload) != 3 || frame.Payload[0] != 0 {
						return native.Frame{}, errors.New("unexpected PWM command frame")
					}
					writes++
					actual = binary.LittleEndian.Uint16(frame.Payload[1:])
					return native.Frame{Opcode: native.OpACK, Payload: []byte{native.OpPWMSet, 0}}, nil
				}
				session := link.NewForPort("fixture", port)
				runtime.session, runtime.generation = session, 7
				t.Cleanup(func() { _ = session.Close() })
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := pwmCommand(ctx, runtime, CommandOptions{HostConfig: func() appconfig.Config { return config }}, []string{"set", "0", text})
				percent, _ := strconv.ParseFloat(text, 64)
				valid := !math.IsNaN(percent) && !math.IsInf(percent, 0) && percent >= 0 && percent <= 100
				mu.Lock()
				defer mu.Unlock()
				if !valid {
					if err == nil || writes != 0 {
						t.Fatalf("invalid percent reached transport: %q writes=%d err=%v", text, writes, err)
					}
				} else if err != nil || writes != 1 || actual != appconfig.PWMRawFromPercent(percent, channel) {
					t.Fatalf("absolute percent conversion/write failed: %q curve=%s raw=%d writes=%d err=%v", text, curve, actual, writes, err)
				}
			})
		}
	}
}

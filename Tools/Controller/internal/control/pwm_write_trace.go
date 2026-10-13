package control

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const pwmTraceCapacity = 64

type pwmTraceOriginKey struct{}
type pwmTraceOrigin struct{ method, actor, transport string }

// Trace provenance is diagnostic only; it never grants media/control authority.
func WithPWMTraceOrigin(ctx context.Context, method, actor, transport string) context.Context {
	return context.WithValue(ctx, pwmTraceOriginKey{}, pwmTraceOrigin{method, actor, transport})
}

type PWMWriteAcknowledgement struct {
	CaptureID            uint64    `json:"capture_id"`
	Sequence             uint64    `json:"sequence"`
	ConnectionGeneration uint64    `json:"connection_generation"`
	Time                 time.Time `json:"time"`
	Channel              byte      `json:"channel"`
	RawValue             uint16    `json:"raw_value"`
	Source               string    `json:"source"`
	ActorID              string    `json:"actor_id,omitempty"`
	Method               string    `json:"method,omitempty"`
	Transport            string    `json:"transport,omitempty"`
	DeviceMicros         uint32    `json:"device_micros,omitempty"`
	Timed                bool      `json:"timed"`
}

type PWMWriteTrace struct {
	CaptureID uint64                    `json:"capture_id"`
	Active    bool                      `json:"active"`
	Channel   byte                      `json:"channel"`
	ExpiresAt time.Time                 `json:"expires_at"`
	Total     uint64                    `json:"total"`
	Dropped   uint64                    `json:"dropped"`
	Records   []PWMWriteAcknowledgement `json:"records"`
}

type pwmWriteTraceState struct {
	mu        sync.Mutex
	epoch     uint64
	channel   byte
	expiresAt time.Time
	total     uint64
	records   [pwmTraceCapacity]PWMWriteAcknowledgement
}

func (runtime *Runtime) StartPWMWriteTrace(channel byte, duration time.Duration) (PWMWriteTrace, error) {
	if channel >= appconfig.PWMChannelCount || duration < time.Millisecond || duration > time.Minute {
		return PWMWriteTrace{}, errors.New("PWM trace requires channel 0..15 and duration_ms 1..60000")
	}
	state := &runtime.pwmTrace
	state.mu.Lock()
	state.epoch++
	state.channel, state.expiresAt, state.total = channel, time.Now().Add(duration), 0
	state.records = [pwmTraceCapacity]PWMWriteAcknowledgement{}
	state.mu.Unlock()
	return runtime.PWMWriteTrace(), nil
}

func (runtime *Runtime) StopPWMWriteTrace() PWMWriteTrace {
	state := &runtime.pwmTrace
	state.mu.Lock()
	state.expiresAt = time.Time{}
	state.mu.Unlock()
	return runtime.PWMWriteTrace()
}

func (runtime *Runtime) PWMWriteTrace() PWMWriteTrace {
	state := &runtime.pwmTrace
	state.mu.Lock()
	defer state.mu.Unlock()
	result := PWMWriteTrace{CaptureID: state.epoch, Active: time.Now().Before(state.expiresAt), Channel: state.channel, ExpiresAt: state.expiresAt, Total: state.total, Records: []PWMWriteAcknowledgement{}}
	first := uint64(0)
	if state.total > pwmTraceCapacity {
		first = state.total - pwmTraceCapacity
		result.Dropped = first
	}
	for index := first; index < state.total; index++ {
		result.Records = append(result.Records, state.records[index%pwmTraceCapacity])
	}
	return result
}

func (runtime *Runtime) pwmTraceAdmission(opcode byte, payload []byte) uint64 {
	if opcode != native.OpPWMSet || len(payload) != 3 || payload[0] >= appconfig.PWMChannelCount || binary.LittleEndian.Uint16(payload[1:]) > appconfig.PWMMaximumValue {
		return 0
	}
	state := &runtime.pwmTrace
	state.mu.Lock()
	defer state.mu.Unlock()
	if payload[0] != state.channel || !time.Now().Before(state.expiresAt) {
		return 0
	}
	return state.epoch
}

func (runtime *Runtime) observePWMWriteAck(ctx context.Context, epoch, generation uint64, opcode byte, payload []byte, frame native.Frame) {
	if epoch == 0 || opcode != native.OpPWMSet || len(payload) != 3 || frame.Opcode != native.OpACK || len(frame.Payload) < 2 || frame.Payload[0] != opcode || frame.Payload[1] != 0 {
		return
	}
	origin, _ := ctx.Value(pwmTraceOriginKey{}).(pwmTraceOrigin)
	source := "unknown"
	if CommandSourceFromContext(ctx) == CommandSourceBackground {
		source = "background"
	} else if origin.method != "" {
		source = "rpc"
	}
	// Bound optional caller metadata; never capture auth, credentials or commands.
	if len(origin.actor) > 128 {
		origin.actor = ""
	}
	if len(origin.method) > 128 {
		origin.method = ""
	}
	if len(origin.transport) > 32 {
		origin.transport = ""
	}
	deviceMicros, timed := native.ResponseDeviceMicros(frame)
	record := PWMWriteAcknowledgement{ConnectionGeneration: generation, Time: time.Now(), Channel: payload[0], RawValue: binary.LittleEndian.Uint16(payload[1:]), Source: source, ActorID: origin.actor, Method: origin.method, Transport: origin.transport, DeviceMicros: deviceMicros, Timed: timed}
	state := &runtime.pwmTrace
	state.mu.Lock()
	if epoch != state.epoch || record.Channel != state.channel || !record.Time.Before(state.expiresAt) {
		state.mu.Unlock()
		return
	}
	state.total++
	record.CaptureID = epoch
	record.Sequence = state.total
	state.records[(state.total-1)%pwmTraceCapacity] = record
	state.mu.Unlock()
	runtime.publishEvent(Event{Kind: "pwm.write.ack", Stream: EventStreamDebug, Source: "host", Target: "diagnostics", Text: "PWM command acknowledged", Frame: native.Frame{Opcode: opcode, Payload: append([]byte(nil), payload...)}, Metadata: map[string]string{
		"capture_id": strconv.FormatUint(epoch, 10),
		"channel":    strconv.Itoa(int(record.Channel)), "raw_value": strconv.Itoa(int(record.RawValue)), "connection_generation": strconv.FormatUint(generation, 10),
		"source": record.Source, "actor_id": record.ActorID, "method": record.Method, "transport": record.Transport, "sequence": strconv.FormatUint(record.Sequence, 10),
		"device_micros": strconv.FormatUint(record.DeviceMicros, 10), "timed": strconv.FormatBool(record.Timed),
	}})
}

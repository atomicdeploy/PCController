package control

import (
	"fmt"
	"strconv"
	"time"

	"pccontroller.local/controller/internal/native"
)

// ObservedStatus is a small coherent cache read, not a device transaction or
// a catalog expansion. Status pushes must not relabel an old board's sample.
func (runtime *Runtime) ObservedStatus() (native.Status, bool, time.Time, uint64, bool) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.status, runtime.haveStatus, runtime.statusUpdated, runtime.generation, runtime.session != nil
}

// PublishPWMValues admits only a complete authoritative readback. Embedders
// already holding that readback need not issue another UART request.
func (runtime *Runtime) PublishPWMValues(values native.PWMValues) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.publishPWMValuesLocked(values)
}

func (runtime *Runtime) publishPWMValuesLocked(values native.PWMValues) {
	for _, value := range values.Values {
		if value > 4095 {
			return
		}
	}
	if values.SelectedChannel > 15 || (runtime.havePWMValues && runtime.pwmValues == values) {
		return
	}
	runtime.pwmValues, runtime.havePWMValues = values, true
	runtime.pwmRevision++
	runtime.pwmUpdated = time.Now()
	metadata := map[string]string{
		"available":             strconv.FormatBool(values.Available),
		"selected_channel":      strconv.Itoa(int(values.SelectedChannel)),
		"revision":              strconv.FormatUint(runtime.pwmRevision, 10),
		"connection_generation": strconv.FormatUint(runtime.generation, 10),
	}
	for channel, value := range values.Values {
		metadata[fmt.Sprintf("pwm.%d", channel)] = strconv.Itoa(int(value))
	}
	runtime.publishEvent(Event{Kind: "pwm.changed", Stream: EventStreamState, Source: "board", Target: "app.clients", Text: "PWM outputs updated", Metadata: metadata})
}

// Retain the trailing compositor frame inside the bounded presentation window.
// No timer reads a device or survives a changed owner.
func (runtime *Runtime) scheduleStatusLEDFlushLocked() {
	if runtime.statusLEDFlush != nil {
		return
	}
	generation := runtime.generation
	delay := time.Until(runtime.statusLEDPublishedAt.Add(statusLEDPresentationInterval))
	if delay < 0 {
		delay = 0
	}
	var scheduled *time.Timer
	scheduled = time.AfterFunc(delay, func() {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		if runtime.statusLEDFlush != scheduled {
			return
		}
		runtime.statusLEDFlush = nil
		if generation != runtime.generation || !runtime.haveStatusLED ||
			(runtime.haveStatusLEDPublished && runtime.statusLEDPublished == runtime.statusLED) {
			return
		}
		state := runtime.statusLED
		runtime.statusLEDPublished, runtime.haveStatusLEDPublished, runtime.statusLEDPublishedAt = state, true, time.Now()
		runtime.publishEvent(Event{Kind: "status_led.changed", Stream: EventStreamState, Source: "board", Target: "app.clients", Text: "Status LED updated", Metadata: map[string]string{
			"red": strconv.Itoa(int(state.Red)), "green": strconv.Itoa(int(state.Green)), "blue": strconv.Itoa(int(state.Blue)),
			"brightness": strconv.Itoa(int(state.Brightness)), "effect": strconv.Itoa(int(state.Effect)), "condition": strconv.Itoa(int(state.Condition)),
			"revision": strconv.FormatUint(runtime.statusLEDRevision, 10), "connection_generation": strconv.FormatUint(generation, 10),
		}})
	})
	runtime.statusLEDFlush = scheduled
}

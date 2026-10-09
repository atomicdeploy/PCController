package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

type DisplayRepeat string

const (
	DisplayRepeatOnce     DisplayRepeat = "once"
	DisplayRepeatLoop     DisplayRepeat = "loop"
	DisplayRepeatInterval DisplayRepeat = "interval"
)

// DisplayRequest is shared by the Go API, JSON-RPC, REST command dispatcher,
// interactive shell, and TUI. Long segment text scrolls automatically; Scroll
// forces the same marquee for text that already fits the four-cell display.
type DisplayRequest struct {
	Target     string        `json:"target"`
	Text       string        `json:"text"`
	SpeedMS    int           `json:"speed_ms,omitempty"`
	DurationMS int           `json:"duration_ms,omitempty"`
	Repeat     DisplayRepeat `json:"repeat,omitempty"`
	IntervalMS int           `json:"interval_ms,omitempty"`
	Scroll     bool          `json:"scroll,omitempty"`
}

type DisplayResult struct {
	Target       string        `json:"target"`
	Text         string        `json:"text"`
	Scrolling    bool          `json:"scrolling"`
	SpeedMS      int           `json:"speed_ms"`
	DurationMS   int           `json:"duration_ms"`
	Repeat       DisplayRepeat `json:"repeat"`
	IntervalMS   int           `json:"interval_ms,omitempty"`
	SegmentCells int           `json:"segment_cells"`
	LCDCells     int           `json:"lcd_cells"`
}

func normalizeDisplayRequest(value DisplayRequest) (DisplayRequest, error) {
	value.Target = strings.ToLower(strings.TrimSpace(value.Target))
	switch value.Target {
	case "segment", "seg":
		value.Target = "segments"
	case "segments", "lcd", "both":
	default:
		return value, errors.New("display target must be segments, lcd, or both")
	}
	if value.SpeedMS == 0 {
		value.SpeedMS = 220
	}
	if value.SpeedMS < 80 || value.SpeedMS > 5000 {
		return value, errors.New("display speed_ms must be 80..5000")
	}
	if value.DurationMS == 0 {
		value.DurationMS = 5000
	}
	if value.DurationMS < 80 || value.DurationMS > int(^uint16(0)) {
		return value, errors.New("display duration_ms must be 80..65535")
	}
	value.Repeat = DisplayRepeat(strings.ToLower(strings.TrimSpace(string(value.Repeat))))
	if value.Repeat == "" {
		value.Repeat = DisplayRepeatOnce
	}
	switch value.Repeat {
	case DisplayRepeatOnce, DisplayRepeatLoop:
		value.IntervalMS = 0
	case DisplayRepeatInterval:
		if value.IntervalMS == 0 {
			value.IntervalMS = 30000
		}
		if value.IntervalMS < 1000 || value.IntervalMS > 255000 || value.IntervalMS%1000 != 0 {
			return value, errors.New("display interval_ms must be a whole 1..255 seconds")
		}
	default:
		return value, errors.New("display repeat must be once, loop, or interval")
	}
	if len(value.Text) > 40 {
		return value, fmt.Errorf("display text is %d bytes, maximum is 40", len(value.Text))
	}
	for _, character := range []byte(value.Text) {
		if character < 0x20 || character > 0x7E {
			return value, errors.New("display text must contain printable ASCII")
		}
	}
	return value, nil
}

func displayRepeatCode(value DisplayRepeat) byte {
	switch value {
	case DisplayRepeatLoop:
		return native.SegmentRepeatLoop
	case DisplayRepeatInterval:
		return native.SegmentRepeatInterval
	default:
		return native.SegmentRepeatOnce
	}
}

func checkedDisplayUint16(value int, name string) (uint16, error) {
	if value < 0 || value > 65535 {
		return 0, fmt.Errorf("display %s %d is outside 0..65535", name, value)
	}
	return uint16(value), nil
}

// PresentDisplay replaces the current host presentation for the selected
// display. Segment timing is MCU-owned; LCD once/interval clearing is retained
// by the host because the legacy LCD payload has no repeat fields.
func (runtime *Runtime) PresentDisplay(
	ctx context.Context,
	request DisplayRequest,
) (DisplayResult, error) {
	request, err := normalizeDisplayRequest(request)
	if err != nil {
		return DisplayResult{}, err
	}
	result := DisplayResult{
		Target: request.Target, Text: request.Text,
		Scrolling: request.Scroll || len(request.Text) > 4,
		SpeedMS:   request.SpeedMS, DurationMS: request.DurationMS,
		Repeat: request.Repeat, IntervalMS: request.IntervalMS,
		SegmentCells: 4, LCDCells: 32,
	}
	if request.Target == "segments" || request.Target == "both" {
		snapshot := runtime.Snapshot()
		if snapshot.Connected && snapshot.Hello.Capabilities&native.CapabilityScheduledSegments == 0 {
			return DisplayResult{}, errors.New("connected firmware does not support scheduled segment messages; flash the current firmware first")
		}
		speedMS, conversionErr := checkedDisplayUint16(request.SpeedMS, "speed_ms")
		if conversionErr != nil {
			return DisplayResult{}, conversionErr
		}
		holdMS, conversionErr := checkedDisplayUint16(request.DurationMS, "duration_ms")
		if conversionErr != nil {
			return DisplayResult{}, conversionErr
		}
		payload, payloadErr := native.ScheduledSegmentPayload(
			native.ScheduledSegmentOptions{
				SpeedMS: speedMS, HoldMS: holdMS,
				IntervalSecond: byte(request.IntervalMS / 1000),
				Repeat:         displayRepeatCode(request.Repeat), ForceScroll: request.Scroll,
			},
			request.Text,
		)
		if payloadErr != nil {
			return DisplayResult{}, payloadErr
		}
		if err := runtime.Command(ctx, native.OpDisplayText, payload); err != nil {
			return DisplayResult{}, err
		}
	}
	if request.Target == "lcd" || request.Target == "both" {
		if err := runtime.presentLCDMessage(ctx, request); err != nil {
			return DisplayResult{}, err
		}
	}
	return result, nil
}

func (runtime *Runtime) presentLCDMessage(ctx context.Context, request DisplayRequest) error {
	runtime.displayMu.Lock()
	if runtime.lcdMessageCancel != nil {
		runtime.lcdMessageCancel()
		runtime.lcdMessageCancel = nil
	}
	runtime.displayMu.Unlock()
	if err := runtime.sendLCDMessage(ctx, request.Text); err != nil {
		return err
	}
	if request.Text == "" || request.Repeat == DisplayRepeatLoop {
		return nil
	}
	scheduleContext, cancel := context.WithCancel(context.Background())
	runtime.displayMu.Lock()
	runtime.lcdMessageCancel = cancel
	runtime.displayMu.Unlock()
	go runtime.runLCDMessageSchedule(scheduleContext, request)
	return nil
}

func (runtime *Runtime) runLCDMessageSchedule(ctx context.Context, request DisplayRequest) {
	for {
		if !waitDisplayTimer(ctx, time.Duration(request.DurationMS)*time.Millisecond) {
			return
		}
		requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = runtime.sendLCDMessage(requestContext, "")
		cancel()
		if request.Repeat != DisplayRepeatInterval {
			return
		}
		if !waitDisplayTimer(ctx, time.Duration(request.IntervalMS)*time.Millisecond) {
			return
		}
		requestContext, cancel = context.WithTimeout(ctx, 3*time.Second)
		_ = runtime.sendLCDMessage(requestContext, request.Text)
		cancel()
	}
}

func waitDisplayTimer(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (runtime *Runtime) sendLCDMessage(ctx context.Context, text string) error {
	payload, err := native.DisplayTextPayload(native.DisplayLCD, 0, text)
	if err != nil {
		return err
	}
	if err := runtime.Command(ctx, native.OpDisplayText, payload); err != nil {
		return err
	}
	line1, line2 := text, ""
	if len(line1) > 16 {
		line2 = line1[16:]
		line1 = line1[:16]
	}
	if len(line2) > 16 {
		line2 = line2[:16]
	}
	if err := runtime.LCDPresenter().RenderPhysical(ctx, lcdLine(line1), lcdLine(line2)); err != nil {
		runtime.PublishStructuredEvent(Event{
			Kind: "lcd.error", Lifecycle: "render", State: "degraded",
			Text: "direct LCD render: " + err.Error(),
		})
	}
	return nil
}

func (runtime *Runtime) cancelDisplaySchedules() {
	runtime.displayMu.Lock()
	if runtime.lcdMessageCancel != nil {
		runtime.lcdMessageCancel()
		runtime.lcdMessageCancel = nil
	}
	runtime.displayMu.Unlock()
}

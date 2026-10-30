package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestLCDTimelineDeferralDoesNotClaimErrorOrConfirmedDisplay(t *testing.T) {
	runtime, _, _ := mediaTimelineFixture(t, false)
	runtime.mediaTimeline.mu.Lock()
	runtime.mediaTimeline.status.State = "playing"
	runtime.mediaTimeline.mu.Unlock()
	err := runtime.rejectMediaTimelineConflict(context.Background(), native.OpDisplayText)
	var busy *ResourceBusyError
	if !errors.As(err, &busy) || busy.Resource != "serial_channel" || busy.Owner != "media_timeline" {
		t.Fatalf("output conflict lost typed ownership: %v", err)
	}
	presenter := NewLCDPresenter(runtime)
	presenter.options.Enabled = true
	presenter.send(0, "waiting", "for playback")
	if presenter.firmwareLines != [2]string{} || presenter.firmwareDevice != "" {
		t.Fatal("deferred presentation was reported as confirmed")
	}
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	deferred := false
	for _, event := range runtime.eventLog {
		if event.Kind == "lcd.error" {
			t.Fatalf("intentional reservation reported as error: %s", event.Text)
		}
		deferred = deferred || event.Kind == "lcd.deferred"
	}
	if !deferred {
		t.Fatal("missing honest deferral event")
	}
}

func TestLCDPromptChangedDuringPriorityRestoresNewestPrompt(t *testing.T) {
	presenter := NewLCDPresenter(nil)
	if err := presenter.Configure(LCDPresentationOptions{
		Enabled: true, Debounce: 20 * time.Millisecond, PriorityHold: time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	presenter.MirrorPrompt("old prompt", "old value")
	if !presenter.ShowPriority("door", "DOOR OPEN", "warning", time.Second) {
		t.Fatal("door priority was rejected")
	}
	presenter.mu.RLock()
	priorityVersion := presenter.version
	presenter.mu.RUnlock()

	presenter.MirrorPrompt("new prompt", "new value")
	presenter.mu.RLock()
	versionAfterPrompt := presenter.version
	presenter.mu.RUnlock()
	if versionAfterPrompt != priorityVersion {
		t.Fatalf("prompt invalidated priority version: %d -> %d", priorityVersion, versionAfterPrompt)
	}

	presenter.restorePrompt(priorityVersion)
	state := presenter.State()
	if state.PriorityKind != "" || state.ActiveLine1 != lcdLine("new prompt") ||
		state.ActiveLine2 != lcdLine("new value") {
		t.Fatalf("restored state=%+v", state)
	}
}

func TestLCDDisconnectClearsOnlyConfirmedTransportState(t *testing.T) {
	lcd := newHostOwnedLCD(func(
		_ context.Context, address, _ byte, _ []byte, _ byte,
	) (native.I2CTransferResult, error) {
		return native.I2CTransferResult{Status: 0, Address: address}, nil
	})
	lcd.sleep = func(time.Duration) {}
	if err := lcd.render(context.Background(), "device", "confirmed", "contents"); err != nil {
		t.Fatal(err)
	}
	presenter := NewLCDPresenter(nil)
	presenter.physical = lcd
	presenter.mu.Lock()
	presenter.prompt = [2]string{lcdLine("prompt"), lcdLine("value")}
	presenter.active = presenter.prompt
	presenter.firmwareLines = [2]string{lcdLine("confirmed"), lcdLine("contents")}
	presenter.firmwareDevice = "device"
	presenter.mu.Unlock()

	presenter.ObserveEvent(Event{Kind: "connection", Lifecycle: "disconnect"})
	state := presenter.State()
	if state.Physical || state.FirmwareMirror || state.Address != 0 {
		t.Fatalf("disconnect retained confirmed transport state: %+v", state)
	}
	if state.ActiveLine1 != lcdLine("prompt") || state.ActiveLine2 != lcdLine("value") {
		t.Fatalf("disconnect discarded desired presentation: %+v", state)
	}
}

func TestLCDOperationalStateIsExplicitEvent(t *testing.T) {
	presenter := NewLCDPresenter(nil)
	if err := presenter.Configure(LCDPresentationOptions{
		Enabled: true, Debounce: 20 * time.Millisecond, PriorityHold: time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	presenter.ObserveEvent(Event{Kind: "program.state", State: "Running", Reason: "macro demo"})
	state := presenter.State()
	if state.PriorityKind != "operational" || state.ActiveLine1 != lcdLine("OPERATION") ||
		state.ActiveLine2 != lcdLine("RUNNING - MACRO ") {
		t.Fatalf("operational presentation=%+v", state)
	}
}

func TestLCDPhysicalErrorReportsOnlyStateChanges(t *testing.T) {
	presenter := NewLCDPresenter(nil)
	missing := errors.New("not detected at 0x27 or 0x3F")
	if !presenter.ReportPhysicalError("LCD", missing) {
		t.Fatal("first missing-LCD state was suppressed")
	}
	if presenter.ReportPhysicalError("LCD", missing) {
		t.Fatal("identical missing-LCD state was repeatedly reported")
	}
	if presenter.ReportPhysicalError("LCD home", context.DeadlineExceeded) {
		t.Fatal("changed error text generated another outage notification")
	}
	presenter.clearConnectionState()
	if presenter.ReportPhysicalError("LCD", missing) {
		t.Fatal("USB reconnect rearmed an undetected LCD")
	}
	if err := presenter.Configure(LCDPresentationOptions{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if presenter.ReportPhysicalError("LCD", missing) {
		t.Fatal("disabling an absent LCD rearmed its notification")
	}
	if !presenter.ReportPhysicalError("LCD", nil) {
		t.Fatal("successful recovery did not clear the error state")
	}
	if !presenter.ReportPhysicalError("LCD", missing) {
		t.Fatal("failure after recovery was not reported")
	}
}

func TestLCDSendSerializationHonorsCallerDeadline(t *testing.T) {
	presenter := NewLCDPresenter(nil)
	if err := presenter.acquireSend(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer presenter.releaseSend()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := presenter.acquireSend(ctx); err == nil {
		t.Fatal("contending LCD send ignored its deadline")
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("deadline-aware LCD serialization took %s", elapsed)
	}
}

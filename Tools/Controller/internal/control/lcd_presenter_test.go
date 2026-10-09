package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

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

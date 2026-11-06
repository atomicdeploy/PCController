package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pccontroller.local/desktop/internal/model"
)

func TestActivityUsesBrowserConsoleVocabularyAndLevels(t *testing.T) {
	setColorCapableTestTerminal(t)
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "always")
	tuiModel, _ := newTestModel(t)

	tuiModel.acceptEvent(model.Event{
		Kind:    model.EventMessage,
		Message: "Automation finished",
	})
	tuiModel.acceptEvent(model.Event{
		Kind:      model.EventBeep,
		BeepCount: 2,
	})
	tuiModel.acceptEvent(model.Event{
		Kind: model.EventRaw,
		Raw:  "temperature: 29",
	})
	tuiModel.acceptSnapshot(model.Snapshot{
		State:           model.StateOff,
		HTTPReachable:   true,
		WebSocketOnline: false,
	}, "websocket")

	want := []struct {
		event activityEvent
		level activityLevel
	}{
		{activityAppStart, activityLevelInfo},
		{activityTransportReady, activityLevelSuccess},
		{activityOutputState, activityLevelSuccess},
		{activityMessageReceived, activityLevelInfo},
		{activityAudioCue, activityLevelInfo},
		{activityDeviceEvent, activityLevelDebug},
		{activityWebSocketDisconnected, activityLevelWarn},
	}
	if len(tuiModel.activity) != len(want) {
		t.Fatalf("activity count = %d, want %d", len(tuiModel.activity), len(want))
	}
	for index, expected := range want {
		entry := tuiModel.activity[index]
		if entry.event != expected.event || entry.level != expected.level {
			t.Errorf(
				"activity[%d] = %s/%s, want %s/%s",
				index,
				entry.event,
				entry.level,
				expected.event,
				expected.level,
			)
		}
	}

	tuiModel.viewport.SetWidth(240)
	tuiModel.viewport.SetHeight(100)
	tuiModel.viewport.GotoTop()
	rendered := tuiModel.viewport.View()
	for _, token := range []string{
		"INFO",
		"SUCCESS",
		"DEBUG",
		"WARN",
		"🚀",
		"💡",
		"📡",
		"app.start",
		"device.state",
		"websocket.disconnected",
	} {
		if !strings.Contains(rendered, token) {
			t.Errorf("activity rendering omitted %q", token)
		}
	}
}

func TestActivitySeverityColorsMatchThemeAndRemainNoColorAccessible(t *testing.T) {
	setColorCapableTestTerminal(t)
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "always")
	tuiModel, _ := newTestModel(t)
	checks := []struct {
		level activityLevel
		want  anyColor
	}{
		{activityLevelDebug, tuiModel.styles.palette.faint},
		{activityLevelInfo, tuiModel.styles.palette.accent},
		{activityLevelSuccess, tuiModel.styles.palette.good},
		{activityLevelWarn, tuiModel.styles.palette.warning},
		{activityLevelError, tuiModel.styles.palette.bad},
	}
	for _, check := range checks {
		gotR, gotG, gotB, gotA := tuiModel.activityLevelColor(check.level).RGBA()
		wantR, wantG, wantB, wantA := check.want.RGBA()
		if gotR != wantR || gotG != wantG || gotB != wantB || gotA != wantA {
			t.Errorf("%s color does not match its theme token", check.level)
		}
	}

	t.Setenv("NO_COLOR", "1")
	noColorModel, _ := newTestModel(t)
	noColorModel.appendActivity(
		activityTransportError,
		activityLevelError,
		"controller unavailable",
	)
	rendered := noColorModel.viewport.View()
	if strings.Contains(rendered, "\x1b[38;") ||
		strings.Contains(rendered, "\x1b[48;") {
		t.Fatalf("NO_COLOR activity contains ANSI colors: %q", rendered)
	}
	rendered = ansi.Strip(rendered)
	for _, token := range []string{"ERROR", "⚠️", "transport.error"} {
		if !strings.Contains(rendered, token) {
			t.Errorf("NO_COLOR activity omitted accessible severity token %q", token)
		}
	}
	if width := lipgloss.Width(formatActivityLevel(activityLevelSuccess)); width == 0 {
		t.Fatal("severity text was not printable")
	}
}

func TestActivityUsesASCIISymbolsWhenEmojiAreDisabled(t *testing.T) {
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "never")
	t.Setenv("NO_EMOJI", "1")

	for event, want := range map[activityEvent]string{
		activityAppStart:           "[START]",
		activityTransportError:     "[WARN]",
		activityOutputState:        "[DEVICE]",
		activityWebSocketConnected: "[WS]",
		activityMessageReceived:    "[MSG]",
		activityAudioCue:           "[AUDIO]",
		activityDiagnosticsLoaded:  "[DIAG]",
	} {
		if got := activitySymbol(event); got != want {
			t.Errorf("activitySymbol(%s) = %q, want %q", event, got, want)
		}
	}
	if got := activitySymbol("unknown"); got != "*" {
		t.Errorf("unknown activity symbol = %q, want ASCII fallback", got)
	}
}

func TestActivityErrorDeduplicationIncludesCanonicalEvent(t *testing.T) {
	tuiModel, _ := newTestModel(t)
	err := testActivityError("offline")

	tuiModel.recordError(activityTransportError, err)
	tuiModel.recordError(activityTransportError, err)
	afterDuplicate := len(tuiModel.activity)
	tuiModel.recordError(activityDiagnosticsError, err)

	if len(tuiModel.activity) != afterDuplicate+1 {
		t.Fatalf("different canonical event was incorrectly deduplicated")
	}
	if got := tuiModel.activity[len(tuiModel.activity)-1].event; got != activityDiagnosticsError {
		t.Fatalf("last event = %s, want %s", got, activityDiagnosticsError)
	}
}

type testActivityError string

func (e testActivityError) Error() string { return string(e) }

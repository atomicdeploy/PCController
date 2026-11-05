package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"rayanlamp.local/app/internal/model"
)

type fakeTransport struct {
	mutex               sync.Mutex
	snapshot            model.Snapshot
	actions             []Action
	err                 error
	events              chan model.Event
	diagnosticResult    DiagnosticResult
	diagnosticErr       error
	diagnosticEndpoints []string
	label               string
}

func (*fakeTransport) Mode() TransportMode { return ModeIPC }
func (f *fakeTransport) Label() string {
	if f.label != "" {
		return f.label
	}
	return "test · named pipe"
}
func (f *fakeTransport) Status(context.Context) (model.Snapshot, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.snapshot, f.err
}
func (f *fakeTransport) Execute(_ context.Context, action Action) (model.Snapshot, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.actions = append(f.actions, action)
	switch action {
	case ActionOn:
		f.snapshot.State = model.StateOn
	case ActionOff:
		f.snapshot.State = model.StateOff
	case ActionToggle:
		if f.snapshot.State == model.StateOn {
			f.snapshot.State = model.StateOff
		} else {
			f.snapshot.State = model.StateOn
		}
	}
	return f.snapshot, f.err
}
func (f *fakeTransport) Events() <-chan model.Event { return f.events }
func (*fakeTransport) Close() error                 { return nil }
func (f *fakeTransport) FetchDiagnostics(
	_ context.Context,
	endpoint string,
) (DiagnosticResult, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.diagnosticEndpoints = append(f.diagnosticEndpoints, endpoint)
	result := f.diagnosticResult
	if result.Endpoint == "" {
		result.Endpoint = endpoint
	}
	if result.Bytes == 0 {
		result.Bytes = len([]byte(result.Content))
	}
	return result, f.diagnosticErr
}

func newTestModel(t *testing.T) (*Model, *fakeTransport) {
	t.Helper()
	transport := &fakeTransport{snapshot: model.Snapshot{
		State:           model.StateOff,
		HTTPReachable:   true,
		WebSocketOnline: true,
		DeviceURL:       "http://rayanlamp.local",
		LastSource:      "websocket",
		LastChanged:     time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC),
	}}
	result, err := NewModel(context.Background(), transport, ModelOptions{
		Poll:       time.Second,
		Timeout:    time.Second,
		Theme:      ThemeDark,
		AltScreen:  true,
		Mouse:      true,
		InitialNow: time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	result.acceptSnapshot(transport.snapshot, "test")
	return result, transport
}

func TestResponsiveViewsStayInsideTerminal(t *testing.T) {
	sizes := []tea.WindowSizeMsg{
		{Width: 24, Height: 8},
		{Width: 40, Height: 12},
		{Width: 80, Height: 24},
		{Width: 120, Height: 40},
	}
	for _, size := range sizes {
		t.Run(
			strings.Join([]string{
				time.Duration(size.Width).String(),
				time.Duration(size.Height).String(),
			}, "x"),
			func(t *testing.T) {
				for _, showAll := range []bool{false, true} {
					model, _ := newTestModel(t)
					model.help.ShowAll = showAll
					_, _ = model.Update(size)
					view := model.View()
					if height := lipgloss.Height(view.Content); height > size.Height {
						t.Fatalf(
							"showAll=%v: view height = %d, terminal height = %d",
							showAll,
							height,
							size.Height,
						)
					}
					if width := lipgloss.Width(view.Content); width > size.Width {
						t.Fatalf(
							"showAll=%v: view width = %d, terminal width = %d",
							showAll,
							width,
							size.Width,
						)
					}
					if !strings.Contains(view.Content, "RAYAN") {
						t.Fatal("view omitted product identity")
					}
				}
			},
		)
	}
}

func TestButtonRegionsAreOneRowNonOverlappingAndInBounds(t *testing.T) {
	model, _ := newTestModel(t)
	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_ = model.View()
	if len(model.buttonRegions) < 4 {
		t.Fatalf("only %d button regions rendered", len(model.buttonRegions))
	}
	for index, current := range model.buttonRegions {
		if current.height != 1 {
			t.Fatalf("button %d height = %d", index, current.height)
		}
		if current.x < 0 || current.y < 0 ||
			current.x+current.width > model.width ||
			current.y+current.height > model.height {
			t.Fatalf("button %d out of bounds: %+v", index, current)
		}
		for previousIndex := 0; previousIndex < index; previousIndex++ {
			previous := model.buttonRegions[previousIndex]
			if regionsOverlap(previous.region, current.region) {
				t.Fatalf(
					"buttons %d and %d overlap: %+v / %+v",
					previousIndex,
					index,
					previous,
					current,
				)
			}
		}
	}
}

func TestKeyboardAndMouseDispatchExactlyOneAction(t *testing.T) {
	model, transport := newTestModel(t)
	updated, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'o', Text: "o"}))
	if updated != model || command == nil || !model.busy {
		t.Fatal("on key did not begin action")
	}
	result := command()
	_, _ = model.Update(result)
	if model.busy {
		t.Fatal("action remained busy after result")
	}

	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_ = model.View()
	var target *actionRegion
	for index := range model.buttonRegions {
		if model.buttonRegions[index].action == ActionOff {
			target = &model.buttonRegions[index]
			break
		}
	}
	if target == nil {
		t.Fatal("off button was not rendered")
	}
	_, command = model.Update(tea.MouseClickMsg{
		X:      target.x,
		Y:      target.y,
		Button: tea.MouseLeft,
	})
	if command == nil {
		t.Fatal("mouse click did not begin action")
	}
	_, _ = model.Update(command())

	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	if len(transport.actions) != 2 ||
		transport.actions[0] != ActionOn ||
		transport.actions[1] != ActionOff {
		t.Fatalf("actions = %v", transport.actions)
	}
}

func TestStaleStatusCannotOverwriteNewerActionState(t *testing.T) {
	tuiModel, _ := newTestModel(t)

	statusCommand := tuiModel.beginStatus("poll")
	if statusCommand == nil {
		t.Fatal("first status request was not started")
	}
	staleResult := statusCommand()

	_, actionCommand := tuiModel.startAction(ActionOn)
	if actionCommand == nil {
		t.Fatal("on action was not started")
	}
	_, _ = tuiModel.Update(actionCommand())
	if tuiModel.snapshot.State != model.StateOn {
		t.Fatalf("state after action = %s, want on", tuiModel.snapshot.State)
	}

	_, refreshCommand := tuiModel.Update(staleResult)
	if tuiModel.snapshot.State != model.StateOn {
		t.Fatalf("stale status overwrote action state: %s", tuiModel.snapshot.State)
	}
	if refreshCommand == nil {
		t.Fatal("superseded status did not schedule a fresh confirmation")
	}
	if !tuiModel.statusPending {
		t.Fatal("fresh confirmation was not marked pending")
	}
}

func TestStaleActionResultCannotOverwriteNewerWebSocketState(t *testing.T) {
	tuiModel, _ := newTestModel(t)

	_, actionCommand := tuiModel.startAction(ActionOn)
	if actionCommand == nil {
		t.Fatal("on action was not started")
	}
	delayedResult := actionCommand()
	tuiModel.acceptEvent(model.Event{
		Kind: model.EventSnapshot,
		Snapshot: model.Snapshot{
			State:           model.StateOff,
			HTTPReachable:   true,
			WebSocketOnline: true,
			LastSource:      "websocket",
		},
	})

	_, _ = tuiModel.Update(delayedResult)
	if tuiModel.busy {
		t.Fatal("stale action result did not clear busy state")
	}
	if tuiModel.snapshot.State != model.StateOff {
		t.Fatalf("stale action result overwrote newer event: %s", tuiModel.snapshot.State)
	}
}

func TestControllerFailureOverridesCachedGreenConnectivity(t *testing.T) {
	tuiModel, transport := newTestModel(t)
	transport.err = errors.New("named pipe disappeared")
	statusCommand := tuiModel.beginStatus("poll")
	if statusCommand == nil {
		t.Fatal("status request was not started")
	}
	_, _ = tuiModel.Update(statusCommand())

	state, _ := tuiModel.connectionState()
	if state != "Controller offline" {
		t.Fatalf("connection state = %q, want controller offline", state)
	}
	if !strings.Contains(tuiModel.renderLampStatus(), "LAST KNOWN") {
		t.Fatal("cached lamp state was not marked last known")
	}
}

func TestCompactMouseButtonsNeverFallThroughToLampToggle(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{
		{Width: 40, Height: 24},
		{Width: 70, Height: 30},
	} {
		t.Run(
			time.Duration(size.Width).String()+"x"+time.Duration(size.Height).String(),
			func(t *testing.T) {
				tuiModel, transport := newTestModel(t)
				_, _ = tuiModel.Update(size)
				_ = tuiModel.View()

				var target *actionRegion
				for index := range tuiModel.buttonRegions {
					if tuiModel.buttonRegions[index].action == ActionOn {
						target = &tuiModel.buttonRegions[index]
						break
					}
				}
				if target == nil {
					t.Fatal("compact layout omitted the On button")
				}
				if regionsOverlap(target.region, tuiModel.lampRegion) {
					t.Fatalf("On button overlaps lamp hitbox: %+v / %+v", target, tuiModel.lampRegion)
				}

				_, command := tuiModel.Update(tea.MouseClickMsg{
					X:      target.x,
					Y:      target.y,
					Button: tea.MouseLeft,
				})
				if command == nil {
					t.Fatal("On click did not start a command")
				}
				_, _ = tuiModel.Update(command())

				transport.mutex.Lock()
				actions := append([]Action(nil), transport.actions...)
				transport.mutex.Unlock()
				if len(actions) != 1 || actions[0] != ActionOn {
					t.Fatalf("compact click actions = %v, want [on]", actions)
				}
			},
		)
	}
}

func TestMicroViewsFitTerminalsBelowTwentyFourByEight(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{
		{Width: 1, Height: 1},
		{Width: 10, Height: 4},
		{Width: 23, Height: 7},
	} {
		t.Run(
			time.Duration(size.Width).String()+"x"+time.Duration(size.Height).String(),
			func(t *testing.T) {
				tuiModel, _ := newTestModel(t)
				_, _ = tuiModel.Update(size)
				view := tuiModel.View()
				if got := lipgloss.Width(view.Content); got > size.Width {
					t.Fatalf("view width = %d, terminal width = %d", got, size.Width)
				}
				if got := lipgloss.Height(view.Content); got > size.Height {
					t.Fatalf("view height = %d, terminal height = %d", got, size.Height)
				}
				if !strings.HasPrefix(view.Content, "R") {
					t.Fatalf("micro view lost product identity: %q", view.Content)
				}
			},
		)
	}
}

func TestPollCommandStopsWithModelContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transport := &fakeTransport{}
	tuiModel, err := NewModel(ctx, transport, ModelOptions{
		Poll:    time.Minute,
		Timeout: time.Second,
		Theme:   ThemeDark,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := tuiModel.pollAfter()
	cancel()

	done := make(chan tea.Msg, 1)
	go func() {
		done <- command()
	}()
	select {
	case message := <-done:
		if message != nil {
			t.Fatalf("canceled poll returned %T, want nil", message)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("poll command remained blocked after context cancellation")
	}
}

func TestTerminalTextIsSanitizedAndTruncatedByCellWidth(t *testing.T) {
	tuiModel, _ := newTestModel(t)
	hostile := "\x1b]52;c;clipboard\a message\r\n\u202ereversed"
	tuiModel.appendActivity(
		activityEvent("remote\x1b[31m"),
		activityLevelInfo,
		hostile,
	)
	tuiModel.setNotice(hostile, false)

	for _, value := range []string{
		string(tuiModel.activity[len(tuiModel.activity)-1].event),
		tuiModel.activity[len(tuiModel.activity)-1].detail,
		tuiModel.notice,
	} {
		if strings.ContainsAny(value, "\x1b\a\r\n\u202e") {
			t.Fatalf("terminal control survived sanitization: %q", value)
		}
	}

	fitted := fitText("🙂🙂", 3)
	if width := ansi.StringWidth(fitted); width > 3 {
		t.Fatalf("grapheme-aware fit width = %d for %q", width, fitted)
	}
	if !strings.Contains(fitted, "…") {
		t.Fatalf("wide text was not visibly truncated: %q", fitted)
	}

	hostileTransport := &fakeTransport{label: "\x1b]52;c;secret\a pipe"}
	labelModel, err := NewModel(context.Background(), hostileTransport, ModelOptions{
		Poll:    time.Second,
		Timeout: time.Second,
		Theme:   ThemeDark,
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail := labelModel.activity[0].detail; strings.ContainsAny(detail, "\x1b\a") {
		t.Fatalf("initial transport label was not sanitized: %q", detail)
	}
}

func regionsOverlap(a, b region) bool {
	return a.x < b.x+b.width &&
		a.x+a.width > b.x &&
		a.y < b.y+b.height &&
		a.y+a.height > b.y
}

package eventeffects

import (
	"errors"
	"sync"
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
)

type automationRecorder struct {
	mu      sync.Mutex
	configs []config.AutomationConfig
	events  []model.Event
	handled chan struct{}
	closes  int
}

func newAutomationRecorder() *automationRecorder {
	return &automationRecorder{handled: make(chan struct{}, 64)}
}

func (r *automationRecorder) ApplyConfig(cfg config.AutomationConfig) {
	r.mu.Lock()
	r.configs = append(r.configs, cfg)
	r.mu.Unlock()
}

func (r *automationRecorder) Handle(event model.Event) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	r.handled <- struct{}{}
}

func (r *automationRecorder) Close() {
	r.mu.Lock()
	r.closes++
	r.mu.Unlock()
}

func (r *automationRecorder) snapshot() (int, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.configs), len(r.events), r.closes
}

type cueRecorder struct {
	mu    sync.Mutex
	cues  []Cue
	heard chan Cue
	err   error
}

func newCueRecorder() *cueRecorder {
	return &cueRecorder{heard: make(chan Cue, 64)}
}

func (r *cueRecorder) play(cue Cue) error {
	r.mu.Lock()
	r.cues = append(r.cues, cue)
	err := r.err
	r.mu.Unlock()
	r.heard <- cue
	return err
}

func TestServiceMatchesGUISoundAndAutomationPolicy(t *testing.T) {
	t.Parallel()

	events := make(chan model.Event, 16)
	automation := newAutomationRecorder()
	player := newCueRecorder()
	cfg := config.Config{
		Notifications: config.NotificationsConfig{
			Sound:       true,
			InitialSync: false,
		},
	}
	service, err := newService(Options{
		Config:     cfg,
		Events:     events,
		Automation: automation,
		PlayCue:    player.play,
	}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	input := []model.Event{
		{
			Kind:          model.EventSnapshot,
			InitialSync:   true,
			PreviousState: model.StateUnknown,
			Snapshot:      model.Snapshot{State: model.StateOff},
		},
		{
			Kind:          model.EventSnapshot,
			PreviousState: model.StateOff,
			Snapshot:      model.Snapshot{State: model.StateOn},
		},
		{
			Kind:          model.EventSnapshot,
			PreviousState: model.StateOn,
			Snapshot:      model.Snapshot{State: model.StateOn},
		},
		{Kind: model.EventMessage, Message: "hello"},
		{Kind: model.EventBeep, BeepCount: 3},
	}
	for _, event := range input {
		events <- event
	}

	for range input {
		waitSignal(t, automation.handled, "automation event")
	}
	want := []Cue{CueOn, CueMessage, CueMessage, CueMessage, CueMessage}
	for _, cue := range want {
		if got := waitCue(t, player.heard); got != cue {
			t.Fatalf("cue = %q, want %q", got, cue)
		}
	}
	select {
	case unexpected := <-player.heard:
		t.Fatalf("unexpected extra cue %q", unexpected)
	case <-time.After(20 * time.Millisecond):
	}

	_, handled, _ := automation.snapshot()
	if handled != len(input) {
		t.Fatalf("automation handled %d events, want %d", handled, len(input))
	}
}

func TestServiceAppliesConfigurationImmediately(t *testing.T) {
	t.Parallel()

	events := make(chan model.Event, 8)
	automation := newAutomationRecorder()
	player := newCueRecorder()
	service, err := newService(Options{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Sound: false},
		},
		Events:     events,
		Automation: automation,
		PlayCue:    player.play,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	events <- model.Event{Kind: model.EventMessage, Message: "silent"}
	waitSignal(t, automation.handled, "initial event")
	assertNoCue(t, player.heard)

	cfg := config.Config{
		Notifications: config.NotificationsConfig{
			Sound:       true,
			InitialSync: true,
		},
		Automation: config.AutomationConfig{Enabled: true, OnScript: "on.cmd"},
	}
	service.ApplyConfig(cfg)
	events <- model.Event{
		Kind:          model.EventSnapshot,
		InitialSync:   true,
		PreviousState: model.StateUnknown,
		Snapshot:      model.Snapshot{State: model.StateOn},
	}
	events <- model.Event{Kind: model.EventMessage, Message: "audible"}
	if got := waitCue(t, player.heard); got != CueOn {
		t.Fatalf("first configured cue = %q, want %q", got, CueOn)
	}
	if got := waitCue(t, player.heard); got != CueMessage {
		t.Fatalf("second configured cue = %q, want %q", got, CueMessage)
	}

	cfg.Notifications.Sound = false
	service.ApplyConfig(cfg)
	events <- model.Event{Kind: model.EventBeep, BeepCount: 0}
	if got := waitCue(t, player.heard); got != CueMessage {
		t.Fatalf("explicit beep with sound disabled = %q, want %q", got, CueMessage)
	}

	configs, _, _ := automation.snapshot()
	if configs != 2 {
		t.Fatalf("automation received %d config updates, want 2", configs)
	}
}

func TestServiceCloseIsConcurrentIdempotentAndStopsIntake(t *testing.T) {
	t.Parallel()

	events := make(chan model.Event, 8)
	automation := newAutomationRecorder()
	player := newCueRecorder()
	service, err := newService(Options{
		Events:     events,
		Automation: automation,
		PlayCue:    player.play,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			service.Close()
		}()
	}
	callers.Wait()
	service.ApplyConfig(config.Config{
		Notifications: config.NotificationsConfig{Sound: true},
	})
	events <- model.Event{Kind: model.EventMessage, Message: "after close"}
	time.Sleep(10 * time.Millisecond)

	configs, handled, closes := automation.snapshot()
	if configs != 0 || handled != 0 || closes != 1 {
		t.Fatalf(
			"after concurrent close: configs=%d handled=%d closes=%d",
			configs,
			handled,
			closes,
		)
	}
	assertNoCue(t, player.heard)
}

func TestServiceLogsCueFailure(t *testing.T) {
	t.Parallel()

	events := make(chan model.Event, 1)
	automation := newAutomationRecorder()
	player := newCueRecorder()
	player.err = errors.New("speaker unavailable")
	logged := make(chan string, 1)
	service, err := newService(Options{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Sound: true},
		},
		Events:     events,
		Automation: automation,
		PlayCue:    player.play,
		Log: func(message string) {
			logged <- message
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	events <- model.Event{Kind: model.EventMessage, Message: "hello"}
	select {
	case message := <-logged:
		if message != "play message cue: speaker unavailable" {
			t.Fatalf("log = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cue failure log")
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	events := make(chan model.Event)
	automation := newAutomationRecorder()
	player := newCueRecorder()
	tests := []struct {
		name    string
		options Options
	}{
		{name: "events", options: Options{Automation: automation, PlayCue: player.play}},
		{name: "automation", options: Options{Events: events, PlayCue: player.play}},
		{name: "player", options: Options{Events: events, Automation: automation}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if service, err := New(test.options); err == nil || service != nil {
				t.Fatalf("New() = (%v, %v), want nil error result", service, err)
			}
		})
	}
}

func waitSignal(t *testing.T, channel <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitCue(t *testing.T, channel <-chan Cue) Cue {
	t.Helper()
	select {
	case cue := <-channel:
		return cue
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cue")
		return ""
	}
}

func assertNoCue(t *testing.T, channel <-chan Cue) {
	t.Helper()
	select {
	case cue := <-channel:
		t.Fatalf("unexpected cue %q", cue)
	case <-time.After(20 * time.Millisecond):
	}
}

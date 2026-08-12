package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func TestIdleSpinnerTickDoesNotScheduleRenderLoop(t *testing.T) {
	model := RichPreviewModel(false)
	_, command := model.Update(spinner.TickMsg{})
	if command != nil {
		t.Fatal("idle spinner tick scheduled another full-screen render")
	}
}

func TestRemoteSnapshotPollingIsBackstopNotRenderLoop(t *testing.T) {
	snapshot := RichPreviewSnapshot()
	snapshot.Status.DoorOpen = true
	model := Model{
		remote:         &RemoteBackend{},
		remoteSnapshot: snapshot,
		page:           PageDashboard,
		prefs:          Preferences{PollInterval: 250 * time.Millisecond},
	}
	if interval := model.statusInterval(); interval != time.Second {
		t.Fatalf("remote snapshot interval=%s, want 1s push-event backstop", interval)
	}

	model.remote = nil
	model.preview = &snapshot
	if interval := model.statusInterval(); interval != 125*time.Millisecond {
		t.Fatalf("local door-open interval=%s, want 125ms", interval)
	}
}

func TestRemoteLiveRateFollowsActiveAndIdlePages(t *testing.T) {
	rates := make([]time.Duration, 0, 2)
	model := Model{remote: &RemoteBackend{SetLiveInterval: func(value time.Duration) {
		rates = append(rates, value)
	}}, page: PageDashboard}
	model.switchPage(PageConsole)
	model.switchPage(PageDashboard)
	if len(rates) != 2 || rates[0] != remoteIdleLiveInterval || rates[1] != remoteActiveLiveInterval {
		t.Fatalf("live rates=%v", rates)
	}
}

func TestRemoteSnapshotUsesLiveRevisionWithoutFreezingConvergence(t *testing.T) {
	base := time.Date(2026, 8, 12, 3, 4, 5, 0, time.UTC)
	initial := RichPreviewSnapshot()
	initial.StatusUpdated = base
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: initial}
	live := RemoteLiveUpdate{
		Status: initial.Status, HaveStatus: true,
		StatusUpdated: base.Add(time.Second), StatusReceivedAt: base.Add(1100 * time.Millisecond),
	}
	live.Status.SupplyMV = 13_371
	updated, _ := model.Update(remoteLiveUpdateMsg(live))
	model = updated.(Model)
	stale := initial
	stale.Status.SupplyMV = 9_999
	updated, _ = model.Update(remoteSnapshotResultMsg{
		snapshot: stale, receivedAt: base.Add(4 * time.Second), statusSequence: 0,
	})
	model = updated.(Model)
	if model.remoteSnapshot.Status.SupplyMV != 13_371 || model.remoteStatusSequence != 1 {
		t.Fatalf("live status rolled back: %#v sequence=%d", model.remoteSnapshot.Status, model.remoteStatusSequence)
	}
	if got := model.statusFreshnessLabel(model.remoteSnapshot, base.Add(4*time.Second)); got == "live" {
		t.Fatalf("rejected stale snapshot falsely renewed freshness: %q", got)
	}

	converged := initial
	converged.Status.SupplyMV = 14_004
	converged.StatusUpdated = base.Add(4 * time.Second)
	updated, _ = model.Update(remoteSnapshotResultMsg{
		snapshot: converged, receivedAt: base.Add(4100 * time.Millisecond),
		statusSequence: model.remoteStatusSequence,
	})
	model = updated.(Model)
	if model.remoteSnapshot.Status.SupplyMV != 14_004 ||
		model.statusFreshnessLabel(model.remoteSnapshot, base.Add(4200*time.Millisecond)) != "live" {
		t.Fatalf("snapshot convergence stayed frozen: %#v freshness=%q",
			model.remoteSnapshot.Status,
			model.statusFreshnessLabel(model.remoteSnapshot, base.Add(4200*time.Millisecond)))
	}
}

func TestRemoteLEDRevisionAdvancesForIdenticalStateAndPreservesMissingState(t *testing.T) {
	base := time.Date(2026, 8, 12, 3, 4, 5, 0, time.UTC)
	blue := native.StatusLEDState{Blue: 120, Brightness: 120, Condition: 255}
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	model.remoteSnapshot.StatusLED = blue
	model.remoteSnapshot.HaveStatusLED = true
	model.remoteSnapshot.StatusLEDUpdated = base
	model.remoteSnapshot.StatusLEDEpoch = 1
	model.remoteSnapshot.StatusLEDRevision = 5

	replay := RemoteLiveUpdate{
		StatusLED: blue, HaveStatusLED: true,
		// Source clocks may step backward; only revision orders this update.
		StatusLEDUpdated: base.Add(-time.Hour), StatusLEDReceivedAt: base.Add(time.Second),
		StatusLEDEpoch: 1, StatusLEDRevision: 6,
	}
	updated, _ := model.Update(remoteLiveUpdateMsg(replay))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLEDRevision != 6 ||
		!model.remoteSnapshot.StatusLEDUpdated.Equal(replay.StatusLEDUpdated) {
		t.Fatalf("identical newer frame did not advance ordering watermark: %#v", model.remoteSnapshot)
	}
	delayed := replay
	delayed.StatusLED.Blue = 18
	delayed.StatusLEDRevision = 5
	updated, _ = model.Update(remoteLiveUpdateMsg(delayed))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != blue || model.remoteSnapshot.StatusLEDRevision != 6 {
		t.Fatalf("older different frame passed an identical newer watermark: %#v", model.remoteSnapshot)
	}
	unknown := RichPreviewSnapshot()
	unknown.HaveStatusLED = false
	unknown.StatusLED = native.StatusLEDState{}
	unknown.StatusLEDUpdated = time.Time{}
	updated, _ = model.Update(remoteSnapshotResultMsg{
		snapshot: unknown, receivedAt: base.Add(1500 * time.Millisecond),
		ledSequence: model.remoteLEDSequence,
	})
	model = updated.(Model)
	if !model.remoteSnapshot.HaveStatusLED || model.remoteSnapshot.StatusLED != blue {
		t.Fatalf("unknown snapshot synthesized a blue-to-off jump: %#v", model.remoteSnapshot)
	}

	off := replay
	off.StatusLED = native.StatusLEDState{Condition: 255}
	off.StatusLEDUpdated = base.Add(2 * time.Second)
	off.StatusLEDRevision = 7
	updated, _ = model.Update(remoteLiveUpdateMsg(off))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != off.StatusLED ||
		model.remoteSnapshot.StatusLEDRevision != 7 {
		t.Fatalf("intentional off frame was hidden: %#v", model.remoteSnapshot)
	}
	stale := RichPreviewSnapshot()
	stale.StatusLED = blue
	stale.HaveStatusLED = true
	stale.StatusLEDUpdated = base.Add(time.Second)
	stale.StatusLEDEpoch = 1
	stale.StatusLEDRevision = 6
	updated, _ = model.Update(remoteSnapshotResultMsg{
		snapshot: stale, receivedAt: base.Add(3 * time.Second),
		ledSequence: model.remoteLEDSequence,
	})
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != off.StatusLED {
		t.Fatalf("pre-event snapshot synthesized an off-to-blue jump: %#v", model.remoteSnapshot.StatusLED)
	}
}

func TestRemoteLEDSnapshotAheadDoesNotReplayDelayedRise(t *testing.T) {
	base := time.Date(2026, 8, 12, 3, 4, 5, 0, time.UTC)
	peak := native.StatusLEDState{
		Blue: 145, Brightness: 145,
		Effect: native.StatusEffectBreathe, Condition: native.StatusConditionBluetoothWaiting,
	}
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	model.remoteSnapshot.StatusLED = native.StatusLEDState{
		Blue: 8, Brightness: 145,
		Effect: native.StatusEffectBreathe, Condition: native.StatusConditionBluetoothWaiting,
	}
	model.remoteSnapshot.HaveStatusLED = true
	model.remoteSnapshot.StatusLEDUpdated = base
	model.remoteSnapshot.StatusLEDEpoch = 1
	model.remoteSnapshot.StatusLEDRevision = 8

	newerSnapshot := model.remoteSnapshot
	newerSnapshot.StatusLED = peak
	newerSnapshot.StatusLEDUpdated = base.Add(5 * time.Second)
	newerSnapshot.StatusLEDRevision = 10
	updated, _ := model.Update(remoteSnapshotResultMsg{
		snapshot: newerSnapshot, receivedAt: base.Add(6 * time.Second),
		statusSequence: model.remoteStatusSequence, ledSequence: model.remoteLEDSequence,
	})
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != peak || model.remoteSnapshot.StatusLEDRevision != 10 {
		t.Fatalf("newer snapshot did not overtake prior live state: %#v", model.remoteSnapshot)
	}

	// The snapshot HTTP response can overtake an already-sent WebSocket frame.
	// Replaying that older rising frame after the peak makes the terminal show
	// peak -> sudden minimum -> rise again, even though the MCU never jumped.
	delayedRise := RemoteLiveUpdate{
		StatusLED: native.StatusLEDState{
			Blue: 18, Brightness: 145,
			Effect: native.StatusEffectBreathe, Condition: native.StatusConditionBluetoothWaiting,
		},
		HaveStatusLED: true, StatusLEDUpdated: base.Add(time.Second),
		StatusLEDReceivedAt: base.Add(6 * time.Second),
		StatusLEDEpoch:      1, StatusLEDRevision: 9,
	}
	updated, _ = model.Update(remoteLiveUpdateMsg(delayedRise))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != peak ||
		model.remoteSnapshot.StatusLEDRevision != 10 {
		t.Fatalf("delayed rise rolled newer peak backward: state=%#v updated=%s",
			model.remoteSnapshot.StatusLED, model.remoteSnapshot.StatusLEDUpdated)
	}

	fall := delayedRise
	fall.StatusLED.Blue = 100
	fall.StatusLEDUpdated = base.Add(-time.Hour)
	fall.StatusLEDRevision = 11
	updated, _ = model.Update(remoteLiveUpdateMsg(fall))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != fall.StatusLED ||
		model.remoteSnapshot.StatusLEDRevision != 11 ||
		!model.remoteSnapshot.StatusLEDUpdated.Equal(fall.StatusLEDUpdated) {
		t.Fatalf("newer falling frame was rejected: %#v", model.remoteSnapshot)
	}
}

func TestRemoteLEDPrimaryEpochResetAcceptsLowerRevision(t *testing.T) {
	peak := native.StatusLEDState{Blue: 145, Brightness: 145, Effect: native.StatusEffectBreathe}
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	model.remoteSnapshot.StatusLED = peak
	model.remoteSnapshot.HaveStatusLED = true
	model.remoteSnapshot.StatusLEDEpoch = 1
	model.remoteSnapshot.StatusLEDRevision = 100

	restarted := RemoteLiveUpdate{
		StatusLED: native.StatusLEDState{Condition: 255}, HaveStatusLED: true,
		StatusLEDEpoch: 2, StatusLEDRevision: 1,
	}
	updated, _ := model.Update(remoteLiveUpdateMsg(restarted))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != restarted.StatusLED ||
		model.remoteSnapshot.StatusLEDEpoch != 2 || model.remoteSnapshot.StatusLEDRevision != 1 {
		t.Fatalf("new primary's lower revision was frozen by old epoch: %#v", model.remoteSnapshot)
	}

	delayedOldEpoch := restarted
	delayedOldEpoch.StatusLED = peak
	delayedOldEpoch.StatusLEDEpoch = 1
	delayedOldEpoch.StatusLEDRevision = 101
	updated, _ = model.Update(remoteLiveUpdateMsg(delayedOldEpoch))
	model = updated.(Model)
	if model.remoteSnapshot.StatusLED != restarted.StatusLED || model.remoteSnapshot.StatusLEDEpoch != 2 {
		t.Fatalf("delayed old-primary frame crossed the epoch boundary: %#v", model.remoteSnapshot)
	}
}

func TestRemoteLEDMissingNewEpochRejectsDelayedOldPrimaryThenAcceptsOff(t *testing.T) {
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	retained := native.StatusLEDState{Blue: 145, Brightness: 145}
	model.remoteSnapshot.HaveStatusLED = true
	model.remoteSnapshot.StatusLED = retained
	model.remoteSnapshot.StatusLEDEpoch = 1
	model.remoteSnapshot.StatusLEDRevision = 100
	missing := RichPreviewSnapshot()
	missing.HaveStatusLED = false
	missing.StatusLED = native.StatusLEDState{}
	missing.StatusLEDEpoch = 2
	missing.StatusLEDRevision = 0
	merged := mergeRemoteSnapshot(model.remoteSnapshot, missing, true, true)
	if !merged.HaveStatusLED || merged.StatusLED != retained ||
		merged.StatusLEDEpoch != 2 || merged.StatusLEDRevision != 0 {
		t.Fatalf("new-primary missing snapshot did not retain value and advance watermark: %#v", merged)
	}
	// A live A frame completing while the B snapshot RPC was in flight changes
	// the sequence gate. The provably newer B epoch must still win.
	model.remoteLEDSequence = 1
	updated, _ := model.Update(remoteSnapshotResultMsg{
		snapshot: missing, ledSequence: 0,
	})
	model = updated.(Model)
	if !model.remoteSnapshot.HaveStatusLED || model.remoteSnapshot.StatusLED != retained ||
		model.remoteSnapshot.StatusLEDEpoch != 2 || model.remoteSnapshot.StatusLEDRevision != 0 {
		t.Fatalf("new-primary missing live baseline did not retain value and advance watermark: %#v",
			model.remoteSnapshot)
	}
	delayedOld := RemoteLiveUpdate{
		StatusLED: native.StatusLEDState{Blue: 145, Brightness: 145}, HaveStatusLED: true,
		StatusLEDEpoch: 1, StatusLEDRevision: 101,
	}
	updated, _ = model.Update(remoteLiveUpdateMsg(delayedOld))
	model = updated.(Model)
	if !model.remoteSnapshot.HaveStatusLED || model.remoteSnapshot.StatusLED != retained ||
		model.remoteSnapshot.StatusLEDEpoch != 2 {
		t.Fatalf("old primary filled a missing new-primary baseline: %#v", model.remoteSnapshot)
	}
	off := RemoteLiveUpdate{
		StatusLED: native.StatusLEDState{Condition: 255}, HaveStatusLED: true,
		StatusLEDEpoch: 2, StatusLEDRevision: 1,
	}
	updated, _ = model.Update(remoteLiveUpdateMsg(off))
	model = updated.(Model)
	if !model.remoteSnapshot.HaveStatusLED || model.remoteSnapshot.StatusLED != off.StatusLED ||
		model.remoteSnapshot.StatusLEDRevision != 1 {
		t.Fatalf("same-new-epoch explicit off was not accepted: %#v", model.remoteSnapshot)
	}
}

func TestRemoteLEDMissingWithoutRetainedVisualRejectsOlderSnapshot(t *testing.T) {
	current := RichPreviewSnapshot()
	current.HaveStatusLED = false
	current.StatusLED = native.StatusLEDState{}
	current.StatusLEDEpoch = 2
	current.StatusLEDRevision = 0
	oldPrimary := RichPreviewSnapshot()
	oldPrimary.HaveStatusLED = true
	oldPrimary.StatusLED = native.StatusLEDState{Blue: 18, Brightness: 145}
	oldPrimary.StatusLEDEpoch = 1
	oldPrimary.StatusLEDRevision = 101
	merged := mergeRemoteSnapshot(current, oldPrimary, true, true)
	if merged.HaveStatusLED || merged.StatusLED != (native.StatusLEDState{}) ||
		merged.StatusLEDEpoch != 2 || merged.StatusLEDRevision != 0 {
		t.Fatalf("older snapshot filled a missing new-primary baseline: %#v", merged)
	}
}

func TestRemoteBackendKeepsFullModelAndRelaysPortOpen(t *testing.T) {
	localRuntime := control.New(control.Options{})
	defer localRuntime.Close()

	remoteSnapshot := RichPreviewSnapshot()
	remoteSnapshot.Port.Name = "REMOTE-COM4"
	remoteSnapshot.ConnectionState = "connected through IPC"
	called := make(chan []string, 1)
	engine := shell.New(10)
	if err := engine.Register(shell.Command{
		Name: "port", Usage: "port open|close", Summary: "manage the primary-owned serial port",
		Run: func(_ context.Context, args []string) (string, error) {
			called <- append([]string(nil), args...)
			return "remote port request accepted", nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	model := NewApplicationWithOptions(localRuntime, engine, Options{
		DisableWelcome: true,
		Remote: &RemoteBackend{
			Endpoint: "cafe-pc.local:8787", InitialSnapshot: remoteSnapshot,
		},
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 132, Height: 42})
	model = updated.(Model)
	if rendered := model.View(); !strings.Contains(rendered, "REMOTE-COM4") ||
		!strings.Contains(rendered, "PCController") {
		t.Fatalf("remote full TUI did not render the normal dashboard: %q", rendered)
	}

	_, command, handled := model.openPort()
	if !handled || command == nil {
		t.Fatal("remote port open was not routed through the full TUI command path")
	}
	message := command()
	result, ok := message.(commandResultMsg)
	if !ok || result.err != nil || result.output != "remote port request accepted" {
		t.Fatalf("remote command result=%#v", message)
	}
	if args := <-called; len(args) != 1 || args[0] != "open" {
		t.Fatalf("remote port args=%q", args)
	}
	if localRuntime.Snapshot().Connected {
		t.Fatal("remote TUI opened the local serial runtime")
	}
}

func TestRemoteSnapshotFailureIsVisibleAndRecovers(t *testing.T) {
	model := Model{remote: &RemoteBackend{}, remoteSnapshot: RichPreviewSnapshot()}
	updated, _ := model.Update(remoteSnapshotResultMsg{err: context.DeadlineExceeded})
	model = updated.(Model)
	if model.snapshot().Connected || model.snapshot().ConnectionState != "remote IPC unavailable" {
		t.Fatalf("failed remote snapshot=%#v", model.snapshot())
	}

	recovered := RichPreviewSnapshot()
	recovered.Port.Name = "RECOVERED-COM4"
	updated, _ = model.Update(remoteSnapshotResultMsg{snapshot: recovered})
	model = updated.(Model)
	if !model.snapshot().Connected || model.snapshot().Port.Name != "RECOVERED-COM4" ||
		model.remoteSnapshotError != "" {
		t.Fatalf("recovered remote snapshot=%#v error=%q", model.snapshot(), model.remoteSnapshotError)
	}
}

func TestRemoteFreshnessUsesLocalReceiptAcrossClockSkew(t *testing.T) {
	receivedAt := time.Date(2026, 8, 12, 2, 3, 4, 0, time.UTC)
	tests := []struct {
		name        string
		offset      time.Duration
		wantWarning string
	}{
		{
			name:        "remote clock behind",
			offset:      -9 * time.Second,
			wantWarning: "Clock skew · remote ≈9.0 s behind · check time sync",
		},
		{
			name:        "remote clock ahead",
			offset:      7 * time.Second,
			wantWarning: "Clock skew · remote ≈7.0 s ahead · check time sync",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := RichPreviewSnapshot()
			snapshot.StatusUpdated = receivedAt.Add(test.offset)
			model := Model{remote: &RemoteBackend{}}

			updated, _ := model.Update(remoteSnapshotResultMsg{
				snapshot: snapshot, receivedAt: receivedAt,
			})
			model = updated.(Model)
			if !model.snapshot().StatusUpdated.Equal(snapshot.StatusUpdated) {
				t.Fatalf("source timestamp changed: got %s want %s", model.snapshot().StatusUpdated, snapshot.StatusUpdated)
			}
			if got := model.statusFreshnessLabel(model.snapshot(), receivedAt.Add(100*time.Millisecond)); got != "live" {
				t.Fatalf("fresh local receipt=%q", got)
			}
			if got := model.statusFreshnessLabel(model.snapshot(), receivedAt.Add(2*time.Second)); got != "live" {
				t.Fatalf("receipt within remote poll jitter window=%q", got)
			}
			if got := model.statusFreshnessLabel(model.snapshot(), receivedAt.Add(freshnessLiveThreshold)); got != "2.5 s ago" {
				t.Fatalf("stale local receipt=%q", got)
			}
			if got := model.remoteClockWarning(); got != test.wantWarning {
				t.Fatalf("clock warning=%q want %q", got, test.wantWarning)
			}
		})
	}
}

func TestRemoteRepeatedTimestampDoesNotResetLocalFreshness(t *testing.T) {
	receivedAt := time.Date(2026, 8, 12, 2, 3, 4, 0, time.UTC)
	snapshot := RichPreviewSnapshot()
	snapshot.StatusUpdated = receivedAt.Add(-9 * time.Second)
	model := Model{remote: &RemoteBackend{}}
	updated, _ := model.Update(remoteSnapshotResultMsg{snapshot: snapshot, receivedAt: receivedAt})
	model = updated.(Model)
	updated, _ = model.Update(remoteSnapshotResultMsg{snapshot: snapshot, receivedAt: receivedAt.Add(5 * time.Second)})
	model = updated.(Model)
	if got := model.statusFreshnessLabel(model.snapshot(), receivedAt.Add(5*time.Second)); got != "5.0 s ago" {
		t.Fatalf("unchanged source timestamp freshness=%q; repeated polling must not make stale data live", got)
	}
}

func TestClosedRemoteEventStreamIsTerminalAndDoesNotResubscribe(t *testing.T) {
	events := make(chan control.Event)
	close(events)
	model := Model{
		remote:         &RemoteBackend{Events: events},
		remoteSnapshot: RichPreviewSnapshot(),
	}

	message := waitControlEvent(events)()
	if _, ok := message.(controlEventClosedMsg); !ok {
		t.Fatalf("closed stream message=%T; want controlEventClosedMsg", message)
	}
	updated, command := model.Update(message)
	model = updated.(Model)
	if !model.remoteEventsClosed {
		t.Fatal("closed remote event stream was not recorded as terminal")
	}
	if command != nil {
		t.Fatal("closed stream scheduled another command")
	}
	if len(model.logs) != 1 || !strings.Contains(model.logs[0], "event stream closed") {
		t.Fatalf("closed stream logs=%q", model.logs)
	}

	// A duplicate terminal notification remains inert and does not grow the
	// transcript or restart the closed-channel receive loop.
	updated, command = model.Update(controlEventClosedMsg{})
	model = updated.(Model)
	if command != nil || len(model.logs) != 1 {
		t.Fatalf("duplicate close command=%v logs=%q", command != nil, model.logs)
	}
}

func TestRemoteModelHasNoFillerReadyLogOrHostMutationHooks(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	model := NewApplicationWithOptions(runtime, shell.New(10), Options{
		DisableWelcome: true,
		Remote: &RemoteBackend{
			Endpoint: "cafe-pc.local:8787", InitialSnapshot: RichPreviewSnapshot(),
		},
	})
	if strings.Contains(strings.Join(model.logs, "\n"), "console ready") {
		t.Fatalf("remote transcript contains filler ready copy: %q", model.logs)
	}
	if model.saveHostIntegrations != nil || model.saveRF != nil {
		t.Fatal("remote model must not silently save remote-owned settings to local config")
	}

	model.page = PageAppSettings
	for _, row := range model.appSettingRows() {
		if (strings.HasPrefix(row.Key, "led.") || strings.HasPrefix(row.Key, peripheralNameSettingPrefix)) && row.Editable {
			t.Fatalf("remote-owned setting %q is misleadingly editable", row.Key)
		}
	}
	before := model.rfValue.DisplayRadix
	model.toggleRFRadix()
	if model.rfValue.DisplayRadix != before || !strings.Contains(model.notice, "unavailable") {
		t.Fatalf("remote RF mutation radix=%q notice=%q", model.rfValue.DisplayRadix, model.notice)
	}
}

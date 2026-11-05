package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestRuntimeStartsAndStopsSpinnerAroundAsynchronousWork(t *testing.T) {
	model, _ := newWorkspaceModel(t)
	runtime := newRuntimeModel(model)

	_, command := runtime.Update(keyMessage('s', "s"))
	if command == nil || !model.spinnerRunning {
		t.Fatal("opening settings did not start the runtime spinner")
	}
	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("settings runtime command = %T, want work plus spinner batch", message)
	}

	loaded := batch[0]()
	_, _ = runtime.Update(loaded)
	if model.settings.mode != settingsMenu {
		t.Fatalf("settings mode = %v after load, want menu", model.settings.mode)
	}

	tick := batch[1]()
	_, _ = runtime.Update(tick)
	if model.spinnerRunning {
		t.Fatal("spinner remained active after settings became ready")
	}
}

func TestRuntimeExpiresIdleNoticeWithoutContinuousSpinner(t *testing.T) {
	model, _ := newWorkspaceModel(t)
	runtime := newRuntimeModel(model)
	model.setNotice("saved", false)
	model.noticeUntil = time.Now().Add(5 * time.Millisecond)

	command := model.runtimeCommands(nil)
	if command == nil {
		t.Fatal("notice did not schedule an expiration")
	}
	message := command()
	_, _ = runtime.Update(message)
	if model.notice != "" || !model.noticeUntil.IsZero() {
		t.Fatalf("notice did not expire: %q at %v", model.notice, model.noticeUntil)
	}
	if model.spinnerRunning {
		t.Fatal("idle notice expiry started the animation spinner")
	}
}

func TestRuntimeReschedulesFractionallyEarlyNoticeTimer(t *testing.T) {
	model, _ := newWorkspaceModel(t)
	runtime := newRuntimeModel(model)
	model.setNotice("saved", false)
	model.noticeUntil = time.Now().Add(time.Second)
	model.noticeScheduled = model.noticeRevision

	_, command := runtime.Update(noticeExpiredMsg{
		revision: model.noticeRevision,
		at:       model.noticeUntil.Add(-time.Millisecond),
	})
	if command == nil {
		t.Fatal("early notice timer was not rescheduled")
	}
	if model.notice == "" {
		t.Fatal("early notice timer cleared the notice")
	}
}

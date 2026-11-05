//go:build windows

package ui

import (
	"strings"
	"unicode"

	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/sfx"
	"pccontroller.local/desktop/presentation"
)

type controllerNotification struct {
	message string
	audible bool
}

func (s *Shell) queueControllerNotification(message string, audible bool) {
	select {
	case s.controllerNotifications <- controllerNotification{message, audible}:
	default:
		s.logf("desktop notification queue full; skipped delivery")
	}
}

func (s *Shell) runControllerNotifications() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case pending := <-s.controllerNotifications:
			if err := s.ShowNotification("PCController", pending.message, pending.audible); err != nil {
				s.logf("desktop notification: %v", err)
			}
		}
	}
}

func (s *Shell) handleControllerMessage(message presentation.Message) {
	if s.closed.Load() || s.controllerState.Preview {
		return
	}
	text := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, message.Text))
	runes := []rune(text)
	if len(runes) > 4096 {
		text = string(runes[:4096])
	}
	if text == "" {
		return
	}
	if s.automation != nil {
		s.automation.Handle(model.Event{Kind: model.EventMessage, Snapshot: controllerAutomationSnapshot(s.controllerState), Message: text})
	}
	if s.Config().Notifications.CustomMessage {
		s.queueControllerNotification(text, true)
	}
}

func controllerAutomationSnapshot(state presentation.Snapshot) model.Snapshot {
	mode := model.StateUnknown
	if !state.Preview && state.Connected && state.HaveStatus {
		switch strings.ToLower(state.Mode) {
		case "running":
			mode = model.StateOn
		case "idle":
			mode = model.StateOff
		}
	}
	return model.Snapshot{State: mode, DeviceURL: state.Endpoint, LastSource: "pccontroller", LastChanged: state.ObservedAt}
}

func (s *Shell) handleControllerEvents(previous, next presentation.Snapshot) {
	before, after := controllerAutomationSnapshot(previous), controllerAutomationSnapshot(next)
	initial := !previous.Connected || previous.Generation != next.Generation || before.State == model.StateUnknown
	if after.State == model.StateUnknown || (!initial && before.State == after.State) {
		return
	}
	event := model.Event{Kind: model.EventSnapshot, Snapshot: after, PreviousState: before.State, InitialSync: initial}
	if s.automation != nil {
		s.automation.Handle(event)
	}
	cfg := s.Config().Notifications
	if initial && !cfg.InitialSync {
		return
	}
	if cfg.Sound {
		cue := sfx.CueOff
		if after.State == model.StateOn {
			cue = sfx.CueOn
		}
		if err := s.QueueAudio(string(cue), 1); err != nil {
			s.logf("program sound: %v", err)
		}
	}
	if cfg.Enabled && cfg.StateChanges {
		message := "Program mode: Idle"
		if after.State == model.StateOn {
			message = "Program mode: Running"
		}
		s.queueControllerNotification(message, false)
	}
}

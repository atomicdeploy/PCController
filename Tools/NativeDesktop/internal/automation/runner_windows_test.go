//go:build windows

package automation

import (
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/model"
)

func TestControllerAndStandaloneAutomationEnvironmentsStayDistinct(t *testing.T) {
	pending := job{event: "mode", state: model.StateOn, previous: model.StateOff, device: "pccontroller://localhost:8787"}
	controller := strings.Join((&Runner{prefix: "PCCONTROLLER"}).eventEnvironment(pending), "\n")
	for _, want := range []string{"PCCONTROLLER_EVENT=mode", "PCCONTROLLER_STATE=running", "PCCONTROLLER_PREVIOUS_STATE=idle", "PCCONTROLLER_DEVICE_URL=pccontroller://"} {
		if !strings.Contains(controller, want) {
			t.Fatalf("missing %q: %s", want, controller)
		}
	}
	donor := strings.Join((&Runner{}).eventEnvironment(pending), "\n")
	if !strings.Contains(donor, "PCCONTROLLER_DESKTOP_STATE=on") || strings.Contains(donor, "PCCONTROLLER_STATE=") {
		t.Fatal("standalone environment changed")
	}
}

func TestSystemEventScriptMapping(t *testing.T) {
	cfg := config.AutomationConfig{
		LockScript:          `C:\Automation\lock.ps1`,
		UnlockScript:        `C:\Automation\unlock.ps1`,
		SuspendScript:       `C:\Automation\suspend.ps1`,
		ResumeScript:        `C:\Automation\resume.ps1`,
		NetworkChangeScript: `C:\Automation\network.ps1`,
	}
	tests := []struct {
		event SystemEvent
		want  string
	}{
		{SystemEventLock, cfg.LockScript},
		{SystemEventUnlock, cfg.UnlockScript},
		{SystemEventSuspend, cfg.SuspendScript},
		{SystemEventResume, cfg.ResumeScript},
		{SystemEventNetworkChange, cfg.NetworkChangeScript},
	}
	for _, test := range tests {
		t.Run(string(test.event), func(t *testing.T) {
			got, ok := systemEventScript(cfg, test.event)
			if !ok || got != test.want {
				t.Fatalf("systemEventScript(%q) = (%q, %v), want (%q, true)",
					test.event, got, ok, test.want)
			}
		})
	}

	if got, ok := systemEventScript(cfg, SystemEvent("future")); ok || got != "" {
		t.Fatalf("unknown event = (%q, %v), want empty,false", got, ok)
	}
}

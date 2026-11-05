//go:build windows

package automation

import (
	"testing"

	"rayanlamp.local/app/internal/config"
)

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

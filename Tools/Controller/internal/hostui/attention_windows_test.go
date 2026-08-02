//go:build windows

package hostui

import "testing"

func TestWindowsAudioCueMapping(t *testing.T) {
	tests := []struct {
		cue     AudioCue
		sound   uintptr
		audible bool
	}{
		{AudioCueFocus, 0, false},
		{AudioCueNavigation, 0, false},
		{AudioCueSelect, 0x00, true},
		{AudioCueSuccess, 0x40, true},
		{AudioCueWarning, 0x30, true},
		{AudioCueError, 0x10, true},
		{AudioCueConnect, 0x40, true},
		{AudioCueDisconnect, 0x30, true},
	}
	for _, test := range tests {
		t.Run(string(test.cue), func(t *testing.T) {
			sound, audible := windowsAudioCueSound(test.cue)
			if sound != test.sound || audible != test.audible {
				t.Fatalf("windowsAudioCueSound(%q) = (0x%X, %t), want (0x%X, %t)", test.cue, sound, audible, test.sound, test.audible)
			}
		})
	}
}

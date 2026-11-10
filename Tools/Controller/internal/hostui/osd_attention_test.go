package hostui

import (
	"errors"
	"testing"
)

func TestOSDAttentionIsExplicitIndependentAndSuppressedByPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		request                OSDRequest
		sound, flash           int
		wantSound, wantTaskbar string
	}{
		{"default silent", OSDRequest{}, 0, 0, "", ""},
		{"explicit none", OSDRequest{Sound: "none"}, 0, 0, "", ""},
		{"sound", OSDRequest{Sound: "success"}, 1, 0, "requested", ""},
		{"flash", OSDRequest{FlashTaskbar: true}, 0, 1, "", "requested"},
		{"both", OSDRequest{Sound: "warning", FlashTaskbar: true}, 1, 1, "requested", "requested"},
		{"muted still flashes", OSDRequest{Sound: "error", SoundSuppressed: true, FlashTaskbar: true}, 0, 1, "suppressed", "requested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sounds, flashes := 0, 0
			result := performOSDAttention(tc.request, func(c AudioCue) error {
				sounds++
				if string(c) != tc.request.Sound {
					t.Fatal(c)
				}
				return nil
			}, func() (string, error) { flashes++; return "requested", nil })
			if sounds != tc.sound || flashes != tc.flash || result.Sound != tc.wantSound || result.Taskbar != tc.wantTaskbar || result.Error != "" {
				t.Fatal(result, sounds, flashes)
			}
		})
	}
	flashed := false
	result := performOSDAttention(OSDRequest{Sound: "warning", FlashTaskbar: true}, func(AudioCue) error { return errors.New("audio unavailable") }, func() (string, error) { flashed = true; return "no-window", nil })
	if !flashed || result.Sound != "failed" || result.Taskbar != "no-window" || result.Error != "audio unavailable" {
		t.Fatal(result)
	}
}

func TestOSDTaskbarNeverBorrowsForegroundOrActivatesWindows(t *testing.T) {
	for _, tc := range []struct {
		target, foreground uintptr
		want               string
	}{
		{0, 99, "no-window"}, {7, 7, "foreground"}, {7, 99, "requested"},
	} {
		calls := 0
		got := taskbarAttention(tc.target, tc.foreground, func(hwnd uintptr) {
			calls++
			if hwnd != tc.target {
				t.Fatal(hwnd)
			}
		})
		if got != tc.want || (calls == 1) != (tc.want == "requested") {
			t.Fatal(got, calls)
		}
	}
}

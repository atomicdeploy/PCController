package hostui

import (
	"errors"
	"pccontroller.local/desktop/osd"
)

func osdAttention(r OSDRequest) osd.AttentionStatus {
	return performOSDAttention(r, PlayAudioCue, flashOSDTaskbar)
}

// Audio and taskbar are independent best-effort effects. Never retry Show after
// an attention failure: the overlay is already visible and a retry duplicates it.
func performOSDAttention(r OSDRequest, sound func(AudioCue) error, flash func() (string, error)) osd.AttentionStatus {
	var result osd.AttentionStatus
	var failures []error
	if r.Sound != "" && r.Sound != "none" {
		if r.SoundSuppressed {
			result.Sound = "suppressed"
		} else if err := sound(AudioCue(r.Sound)); err != nil {
			result.Sound = "failed"
			failures = append(failures, err)
		} else {
			result.Sound = "requested"
		}
	}
	if r.FlashTaskbar {
		var err error
		result.Taskbar, err = flash()
		if err != nil {
			failures = append(failures, err)
		}
	}
	if err := errors.Join(failures...); err != nil {
		result.Error = err.Error()
	}
	return result
}

func taskbarAttention(target, foreground uintptr, flash func(uintptr)) string {
	if target == 0 {
		return "no-window"
	}
	if target == foreground {
		return "foreground"
	}
	flash(target)
	return "requested"
}

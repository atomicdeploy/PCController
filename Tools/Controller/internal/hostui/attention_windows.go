//go:build windows

package hostui

import (
	"fmt"

	"golang.org/x/sys/windows"
)

var messageBeep = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBeep")

func playSystemAudioCue(cue AudioCue) error {
	sound, audible := windowsAudioCueSound(cue)
	if !audible {
		return nil
	}
	result, _, callErr := messageBeep.Call(sound)
	if result == 0 {
		return fmt.Errorf("Windows UI audio cue %q failed: %v", cue, callErr)
	}
	return nil
}

func windowsAudioCueSound(cue AudioCue) (uintptr, bool) {
	const (
		messageOK          = uintptr(0x00000000)
		messageError       = uintptr(0x00000010)
		messageWarning     = uintptr(0x00000030)
		messageInformation = uintptr(0x00000040)
	)
	switch cue {
	case AudioCueSelect:
		return messageOK, true
	case AudioCueSuccess, AudioCueConnect:
		return messageInformation, true
	case AudioCueWarning, AudioCueDisconnect:
		return messageWarning, true
	case AudioCueError:
		return messageError, true
	case AudioCueFocus, AudioCueNavigation:
		// Repeated native focus/navigation sounds are disruptive. Web feedback
		// remains subtle and spatial, while native navigation stays silent.
		return 0, false
	default:
		return 0, false
	}
}

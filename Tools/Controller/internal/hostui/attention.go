package hostui

import "fmt"

// AudioCue is the stable intent vocabulary shared by native host feedback and
// the Web UI. Platform implementations deliberately decide how much feedback
// is appropriate; for example, native focus movement remains silent while a
// warning uses the user's configured operating-system sound.
type AudioCue string

const (
	AudioCueFocus      AudioCue = "focus"
	AudioCueSelect     AudioCue = "select"
	AudioCueNavigation AudioCue = "navigation"
	AudioCueSuccess    AudioCue = "success"
	AudioCueWarning    AudioCue = "warning"
	AudioCueError      AudioCue = "error"
	AudioCueConnect    AudioCue = "connect"
	AudioCueDisconnect AudioCue = "disconnect"
)

var audioCueNames = [...]AudioCue{
	AudioCueFocus,
	AudioCueSelect,
	AudioCueNavigation,
	AudioCueSuccess,
	AudioCueWarning,
	AudioCueError,
	AudioCueConnect,
	AudioCueDisconnect,
}

// AudioCueNames returns a copy so callers can expose capabilities without
// being able to mutate the package's validation set.
func AudioCueNames() []AudioCue {
	result := make([]AudioCue, len(audioCueNames))
	copy(result, audioCueNames[:])
	return result
}

func (cue AudioCue) Valid() bool {
	for _, candidate := range audioCueNames {
		if cue == candidate {
			return true
		}
	}
	return false
}

// PlayAudioCue uses short operating-system feedback and never owns the board
// buzzer. Callers remain responsible for applying their mute/notification
// preference before calling this function.
func PlayAudioCue(cue AudioCue) error {
	if !cue.Valid() {
		return fmt.Errorf("unsupported UI audio cue %q", cue)
	}
	return playSystemAudioCue(cue)
}

// WarningBeep is retained for the configured door-running notification path.
func WarningBeep() error { return PlayAudioCue(AudioCueWarning) }

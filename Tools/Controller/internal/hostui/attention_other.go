//go:build !windows

package hostui

// Desktop sounds are a quiet no-op on platforms where the host does not yet
// have a native, user-theme-aware cue backend.
func playSystemAudioCue(AudioCue) error { return nil }

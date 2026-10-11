//go:build !linux

package pcspeaker

// Linux beep uses a userspace KIOCSOUND cleanup path. Other supported helpers
// do not expose the same console-speaker emergency-stop command.
func stopExternalBeep(string) error {
	return nil
}

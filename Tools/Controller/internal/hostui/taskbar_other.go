//go:build !windows

package hostui

func SetTaskbarProgress(progress TerminalProgress) error {
	_, err := progress.OSCPayload()
	return err
}

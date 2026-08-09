//go:build !windows

package hostui

import "context"

type unsupportedNotifier struct{}

func newPlatformNotifier(NotifierOptions) Notifier                     { return unsupportedNotifier{} }
func (unsupportedNotifier) Notify(context.Context, Notification) error { return ErrUnsupported }
func (unsupportedNotifier) Status() NotificationStatus {
	return NotificationStatus{Supported: false, Available: false, LastError: ErrUnsupported.Error()}
}

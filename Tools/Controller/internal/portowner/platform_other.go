//go:build !windows && !linux

package portowner

import (
	"context"
	"errors"
)

type unsupportedEnumerator struct{}

func (unsupportedEnumerator) FindOwner(context.Context, string) (Owner, bool, error) {
	return Owner{}, false, errors.New("serial-owner discovery is available on Windows only")
}

type unsupportedActions struct{}

func (unsupportedActions) BringToForeground(context.Context, Owner) error {
	return errors.New("owner-window control is available on Windows only")
}
func (unsupportedActions) RequestGracefulClose(context.Context, Owner) error {
	return errors.New("owner-window control is available on Windows only")
}
func (unsupportedActions) Terminate(context.Context, Owner, string) error {
	return errors.New("owner-process control is available on Windows only")
}
func (unsupportedActions) TerminateConfirmation(owner Owner) string {
	return terminationConfirmation(owner)
}

func systemEnumerator() Enumerator          { return unsupportedEnumerator{} }
func DefaultActions() Actions               { return unsupportedActions{} }
func isAccessDenied(cause error) bool       { return looksAccessDenied(cause) }
func isLocalSerialTarget(value string) bool { return isCOMPort(value) }

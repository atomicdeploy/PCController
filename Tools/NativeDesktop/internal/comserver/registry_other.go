//go:build !windows

package comserver

func Register(string) error {
	return ErrUnsupported
}

func Unregister() error {
	return ErrUnsupported
}

func UnregisterExecutable(string) error {
	return ErrUnsupported
}

func HasOwnedRegistration(string) (bool, error) {
	return false, ErrUnsupported
}

func HasOwnedServerRegistration(string) (bool, error) {
	return false, ErrUnsupported
}

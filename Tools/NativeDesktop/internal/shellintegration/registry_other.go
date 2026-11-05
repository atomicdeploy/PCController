//go:build !windows

package shellintegration

func Register(string) error {
	return ErrUnsupported
}

func RegisterCurrentExecutable() error {
	return ErrUnsupported
}

func Unregister(string) error {
	return ErrUnsupported
}

func UnregisterCurrentExecutable() error {
	return ErrUnsupported
}

func IsRegistered(string) (bool, error) {
	return false, ErrUnsupported
}

func IsCurrentExecutableRegistered() (bool, error) {
	return false, ErrUnsupported
}

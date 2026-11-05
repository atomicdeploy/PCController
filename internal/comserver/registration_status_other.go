//go:build !windows

package comserver

func IsRegistered(string) (bool, error) {
	return false, ErrUnsupported
}

func IsCurrentExecutableRegistered() (bool, error) {
	return false, ErrUnsupported
}

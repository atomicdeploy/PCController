//go:build !windows

package shellintegration

func RegisterShell(string) error {
	return ErrUnsupported
}

func RegisterCurrentShell() error {
	return ErrUnsupported
}

func UnregisterShell(string) error {
	return ErrUnsupported
}

func UnregisterCurrentShell() error {
	return ErrUnsupported
}

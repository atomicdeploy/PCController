//go:build !windows

package shellintegration

func SetAppUserModelID() error {
	return ErrUnsupported
}

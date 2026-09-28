//go:build !windows

package link

func purgePendingSerialIO(sessionPort) error {
	return nil
}

//go:build windows

package link

import (
	"errors"
	"fmt"
)

type serialBufferResetter interface {
	ResetInputBuffer() error
	ResetOutputBuffer() error
}

// purgePendingSerialIO aborts in-flight overlapped reads and writes before the
// Windows handle is closed. go.bug.st/serial implements these methods with
// PurgeComm and the PURGE_RXABORT/PURGE_TXABORT flags.
func purgePendingSerialIO(port sessionPort) error {
	resetter, ok := port.(serialBufferResetter)
	if !ok {
		return nil
	}
	inputErr := resetter.ResetInputBuffer()
	outputErr := resetter.ResetOutputBuffer()
	return errors.Join(
		wrapPurgeError("input", inputErr),
		wrapPurgeError("output", outputErr),
	)
}

func wrapPurgeError(direction string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("purge pending serial %s: %w", direction, err)
}

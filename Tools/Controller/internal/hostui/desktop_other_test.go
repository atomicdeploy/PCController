//go:build !windows

package hostui

import (
	"errors"
	"testing"
)

func TestRemoveDesktopIntegrationReportsUnsupported(t *testing.T) {
	status, err := RemoveDesktopIntegration(DesktopIntegrationOptions{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("RemoveDesktopIntegration error=%v; want ErrUnsupported", err)
	}
	if status.Supported || status.LastError != ErrUnsupported.Error() {
		t.Fatalf("RemoveDesktopIntegration status=%+v", status)
	}
}

//go:build windows

package hostui

import (
	"strings"
	"testing"
)

type recordingRegistry map[string]string

func (registry recordingRegistry) Set(path, name, value string) error {
	registry[path+"|"+name] = value
	return nil
}

func TestDesktopRegistryUsesCurrentExecutableAndQuotedURIArgument(t *testing.T) {
	record := recordingRegistry{}
	executable := `C:\Program Files\PCController\controller.exe`
	if err := ensureProtocolRegistry(record, executable, "Test.PCController", "Test Controller"); err != nil {
		t.Fatal(err)
	}
	command := record[`Software\Classes\pccontroller\shell\open\command|`]
	if !strings.Contains(command, `"C:\Program Files\PCController\controller.exe"`) ||
		!strings.Contains(command, `uri "%1"`) {
		t.Fatalf("protocol command=%q", command)
	}
	if strings.Contains(command, "David") {
		t.Fatalf("protocol command unexpectedly hardcoded a user path: %q", command)
	}
}

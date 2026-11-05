//go:build windows

package comserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationStatusRequiresExactOwnershipInEveryView(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "RayanLamp.exe")
	expected, err := RegistrationCommand(executable)
	if err != nil {
		t.Fatal(err)
	}
	views := registryViewsForProcess(true)
	exists := func(string, uint32) (bool, error) { return true, nil }
	local := func(_ string, _ uint32) (string, bool, error) {
		return expected, true, nil
	}
	sharedValues := map[string]string{
		strings.ToLower(classesRoot + `\` + ProgID + `\CLSID`):          CLSID,
		strings.ToLower(classesRoot + `\` + ProgID + `\CurVer`):         VersionedProgID,
		strings.ToLower(classesRoot + `\` + VersionedProgID + `\CLSID`): CLSID,
	}
	shared := func(path, _ string, _ uint32) (string, bool, error) {
		value, ok := sharedValues[strings.ToLower(path)]
		return value, ok, nil
	}

	registered, err := isRegisteredWithReaders(
		executable,
		views,
		exists,
		local,
		shared,
	)
	if err != nil || !registered {
		t.Fatalf("exact registration = (%t, %v), want (true, nil)", registered, err)
	}

	foreignView := views[len(views)-1].access
	registered, err = isRegisteredWithReaders(
		executable,
		views,
		exists,
		func(root string, view uint32) (string, bool, error) {
			if view == foreignView {
				return `"C:\Other\RayanLamp.exe" /com-server`, true, nil
			}
			return local(root, view)
		},
		shared,
	)
	if err != nil || registered {
		t.Fatalf("foreign view registration = (%t, %v), want (false, nil)", registered, err)
	}
}

func TestRegistrationStatusIsReadOnlyAndFailsClosed(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "RayanLamp.exe")
	views := registryViewsForProcess(false)
	readFailure := errors.New("injected registry read failure")
	registered, err := isRegisteredWithReaders(
		executable,
		views,
		func(string, uint32) (bool, error) { return false, readFailure },
		func(string, uint32) (string, bool, error) {
			t.Fatal("LocalServer32 was read after the key probe failed")
			return "", false, nil
		},
		func(string, string, uint32) (string, bool, error) {
			t.Fatal("shared registration was read after the key probe failed")
			return "", false, nil
		},
	)
	if registered || !errors.Is(err, readFailure) {
		t.Fatalf("failed status = (%t, %v)", registered, err)
	}
}

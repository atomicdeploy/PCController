//go:build windows

package artifacts

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateHiddenAttributeRestoresInstalledVisibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.new")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(path)
	for _, hidden := range []bool{true, false} {
		if err := setUpdateHidden(path, hidden); err != nil {
			t.Fatal(err)
		}
		attributes, err := windows.GetFileAttributes(name)
		if err != nil || (attributes&windows.FILE_ATTRIBUTE_HIDDEN != 0) != hidden {
			t.Fatal("incorrect update visibility", err)
		}
	}
}

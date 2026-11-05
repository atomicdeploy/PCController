//go:build windows

package walk

import "testing"

func TestDialogCanPreservePreparedFirstShowBounds(t *testing.T) {
	dialog := &Dialog{centerInOwnerWhenRun: true}
	dialog.SetCenterInOwnerWhenRun(false)
	if dialog.centerInOwnerWhenRun {
		t.Fatal("prepared dialog bounds would be overwritten during Show")
	}
}

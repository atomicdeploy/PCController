//go:build windows

package pcspeaker

import (
	"reflect"
	"testing"
)

func TestExternalBeepArgumentsWindows(t *testing.T) {
	want := []string{"-f", "880", "-d", "250"}
	if got := externalBeepArguments(880, 250); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments=%q want %q", got, want)
	}
}

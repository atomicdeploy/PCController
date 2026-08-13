//go:build linux

package pcspeaker

import (
	"reflect"
	"testing"
)

func TestExternalBeepArgumentsLinux(t *testing.T) {
	want := []string{"-f", "880", "-l", "250"}
	if got := externalBeepArguments(880, 250); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments=%q want %q", got, want)
	}
}

package tui

import (
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/shell"
)

func TestUnifiedEffectCompletion(t *testing.T) {
	engine := shell.New(10)
	tests := []struct {
		name string
		line string
		want []string
	}{
		{name: "effect actions", line: "effect ", want: []string{"effect cancel", "effect category", "effect create", "effect delete", "effect export", "effect import", "effect inspect", "effect list", "effect play", "effect program", "effect record", "effect rename", "effect status", "effect stop", "effect update"}},
		{name: "effect names are live data", line: "effect play ", want: nil},
		{name: "split strip effect command is absent", line: "strip e", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := completionCandidates(engine, test.line); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("completionCandidates(%q)=%#v want %#v", test.line, got, test.want)
			}
		})
	}
}

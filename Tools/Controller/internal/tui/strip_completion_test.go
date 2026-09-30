package tui

import (
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/shell"
)

func TestStripEffectCompletion(t *testing.T) {
	engine := shell.New(10)
	tests := []struct {
		name string
		line string
		want []string
	}{
		{name: "effect command", line: "strip e", want: []string{"strip effect"}},
		{name: "effect actions", line: "strip effect ", want: []string{"strip effect list", "strip effect play"}},
		{name: "effect name prefix", line: "strip effect play w", want: []string{"strip effect play white-thunder"}},
		{
			name: "canonical effect names",
			line: "strip effect play ",
			want: []string{
				"strip effect play converging-red",
				"strip effect play police",
				"strip effect play white-thunder",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := completionCandidates(engine, test.line); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("completionCandidates(%q)=%#v want %#v", test.line, got, test.want)
			}
		})
	}
}

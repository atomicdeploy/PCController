package messagefabric

import "testing"

func TestTargetsSurfaceUsesArrayLegacyTargetAndAll(t *testing.T) {
	for name, test := range map[string]struct {
		target  string
		targets []string
		surface string
		want    bool
	}{
		"array":  {targets: []string{"native", "web"}, surface: "web", want: true},
		"legacy": {target: "native, TUI", surface: "tui", want: true},
		"all":    {targets: []string{"all"}, surface: "web", want: true},
		"other":  {targets: []string{"native"}, surface: "web", want: false},
		"empty":  {target: "all", surface: "", want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := TargetsSurface(test.target, test.targets, test.surface); got != test.want {
				t.Fatalf("TargetsSurface(%q, %#v, %q)=%t, want %t", test.target, test.targets, test.surface, got, test.want)
			}
		})
	}
}

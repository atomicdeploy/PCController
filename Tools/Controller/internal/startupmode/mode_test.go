package startupmode

import "testing"

func TestTUIModeExcludesMachineReadableAndBackgroundCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"tui"}, {"--config", "custom.json", "tui"}, {"--app-name=Desk", "tui", "--ipc-addr", "127.0.0.1:8787"}} {
		if !TUI(args) {
			t.Fatalf("TUI rejected %q", args)
		}
	}
	for _, args := range [][]string{{"version"}, {"web"}, {"__self-update-helper", "journal.json"}, {"ipc", "serve"}, {"--config", "x.json", "exec", "status"}, {"--config"}, {"--config="}, {"--app-name="}} {
		if TUI(args) {
			t.Fatalf("splash would contaminate %q", args)
		}
	}
}

func TestBackgroundWebRequiresExplicitNoTUI(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"web", "--no-open", "--no-tray"}, false},
		{[]string{"--config", "custom.json", "web", "--no-tui=true"}, true},
		{[]string{"web", "--no-tui"}, true},
		{[]string{"web", "--no-tui", "--no-tui=false"}, false},
		{[]string{"web", "--no-open"}, false},
		{[]string{"web", "--no-open", "--no-tray", "--no-tray=false"}, false},
		{[]string{"web", "--no-open", "--no-tray", "--help"}, false},
		{[]string{"tui", "--no-open", "--no-tray"}, false},
		{[]string{"--config", "--no-open", "web", "--no-tray"}, false},
	} {
		if got := BackgroundWeb(test.args); got != test.want {
			t.Fatalf("args=%q background=%v want %v", test.args, got, test.want)
		}
	}
}

package consolewindow

import (
	"strings"
	"testing"
)

func TestValidateBoundsAndFont(t *testing.T) {
	valid := Settings{Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Settings){
		"columns":   func(value *Settings) { value.Columns = 55 },
		"rows":      func(value *Settings) { value.Rows = 121 },
		"font face": func(value *Settings) { value.FontFace = strings.Repeat("x", 32) },
		"font size": func(value *Settings) { value.FontSize = 73 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := Validate(candidate); err == nil {
				t.Fatal("invalid console settings were accepted")
			}
		})
	}
}

func TestRemoteSessionReason(t *testing.T) {
	values := map[string]string{"SSH_CONNECTION": "client 1 server 2", "SESSIONNAME": "Console"}
	if got := remoteSessionReason(func(name string) string { return values[name] }); !strings.Contains(got, "SSH") {
		t.Fatalf("SSH reason=%q", got)
	}
	values = map[string]string{"SESSIONNAME": "RDP-Tcp#4"}
	if got := remoteSessionReason(func(name string) string { return values[name] }); !strings.Contains(got, "remote desktop") {
		t.Fatalf("RDP reason=%q", got)
	}
	if got := remoteSessionReason(func(string) string { return "" }); got != "" {
		t.Fatalf("local session reason=%q", got)
	}
}

func TestDisabledManagementIsHarmless(t *testing.T) {
	result, err := Apply(Settings{})
	if err != nil || result.Applied || !strings.Contains(result.Reason, "disabled") {
		t.Fatalf("disabled result=%#v err=%v", result, err)
	}
}

func TestApplyNormalizesFontFaceBeforePlatformDispatch(t *testing.T) {
	settings := normalizeSettings(Settings{Enabled: true, Columns: 132, Rows: 40, FontFace: strings.Repeat(" ", 40) + "Consolas", FontSize: 18})
	if err := Validate(settings); err != nil {
		t.Fatalf("trimmed font face should be valid: %v", err)
	}
	if settings.FontFace != "Consolas" {
		t.Fatalf("normalized font face=%q", settings.FontFace)
	}
}

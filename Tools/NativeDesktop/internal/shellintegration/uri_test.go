package shellintegration

import (
	"errors"
	"strings"
	"testing"
)

func TestParseURIMapsAllowedActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw    string
		method string
	}{
		{"rayan-lamp://on", "lamp.on"},
		{"RAYAN-LAMP://OFF", "lamp.off"},
		{"rayan-lamp://toggle/", "lamp.toggle"},
		{"rayan-lamp:/show", "window.show"},
		{"rayan-lamp:/hide", "window.hide"},
		{"rayan-lamp://reconnect", "reconnect"},
		{"rayan-lamp://settings", "settings.show"},
		{"rayan-lamp://diagnostics", "diagnostics.show"},
		{"rayan-lamp://web", "web.show"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			method, err := ParseURI(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if method != test.method {
				t.Fatalf("method = %q, want %q", method, test.method)
			}
		})
	}
}

func TestParseURIRejectsNonCanonicalOrUnsafeInput(t *testing.T) {
	t.Parallel()

	tooLong := "rayan-lamp://" + strings.Repeat("a", maxURIBytes)
	tests := []string{
		"",
		" ",
		" rayan-lamp://on",
		"rayan-lamp://on ",
		"http://on",
		"rayan-lamp:on",
		"rayan-lamp://",
		"rayan-lamp:/",
		"rayan-lamp:///on",
		"rayan-lamp://on/off",
		"rayan-lamp:/on/off",
		"rayan-lamp://user@on",
		"rayan-lamp://on:80",
		"rayan-lamp://on?source=browser",
		"rayan-lamp://on?",
		"rayan-lamp://on#fragment",
		"rayan-lamp://%6fn",
		"rayan-lamp:/%6fn",
		`rayan-lamp:\on`,
		"rayan-lamp://on\n",
		"rayan-lamp://ön",
		"rayan-lamp://on-off",
		"rayan-lamp://status",
		"rayan-lamp://quit",
		tooLong,
	}
	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if method, err := ParseURI(raw); err == nil {
				t.Fatalf("ParseURI(%q) = %q, want error", raw, method)
			} else if !errors.Is(err, ErrInvalidURI) {
				t.Fatalf("ParseURI(%q) error = %v, want ErrInvalidURI", raw, err)
			}
		})
	}
}

func TestURIActionAllowlistHasOnlyCanonicalDispatcherMethods(t *testing.T) {
	t.Parallel()

	if len(uriActions) != 9 {
		t.Fatalf("URI action count = %d, want 9", len(uriActions))
	}
	for action, method := range uriActions {
		if action == "" || strings.ContainsAny(action, "./\\") {
			t.Errorf("unsafe action key %q", action)
		}
		if method == "" || strings.ContainsAny(method, " \t\r\n") {
			t.Errorf("unsafe dispatcher method %q for %q", method, action)
		}
	}
}

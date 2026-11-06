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
		{"pccontroller-desktop://on", "device.on"},
		{"PCCONTROLLER-DESKTOP://OFF", "device.off"},
		{"pccontroller-desktop://toggle/", "device.toggle"},
		{"pccontroller-desktop:/show", "window.show"},
		{"pccontroller-desktop:/hide", "window.hide"},
		{"pccontroller-desktop://reconnect", "reconnect"},
		{"pccontroller-desktop://settings", "settings.show"},
		{"pccontroller-desktop://diagnostics", "diagnostics.show"},
		{"pccontroller-desktop://web", "web.show"},
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

	tooLong := "pccontroller-desktop://" + strings.Repeat("a", maxURIBytes)
	tests := []string{
		"",
		" ",
		" pccontroller-desktop://on",
		"pccontroller-desktop://on ",
		"http://on",
		"pccontroller-desktop:on",
		"pccontroller-desktop://",
		"pccontroller-desktop:/",
		"pccontroller-desktop:///on",
		"pccontroller-desktop://on/off",
		"pccontroller-desktop:/on/off",
		"pccontroller-desktop://user@on",
		"pccontroller-desktop://on:80",
		"pccontroller-desktop://on?source=browser",
		"pccontroller-desktop://on?",
		"pccontroller-desktop://on#fragment",
		"pccontroller-desktop://%6fn",
		"pccontroller-desktop:/%6fn",
		`pccontroller-desktop:\on`,
		"pccontroller-desktop://on\n",
		"pccontroller-desktop://ön",
		"pccontroller-desktop://on-off",
		"pccontroller-desktop://status",
		"pccontroller-desktop://quit",
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

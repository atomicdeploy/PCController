package hostui

import "testing"

func TestConfigurationConsoleUserMatches(t *testing.T) {
	for _, test := range []struct {
		owner uint32
		user  int
		want  bool
	}{{0, 0, false}, {0, 501, false}, {501, 0, false}, {501, 502, false}, {501, 501, true}} {
		if got := configurationConsoleUserMatches(test.owner, test.user); got != test.want {
			t.Fatalf("owner=%d user=%d available=%v", test.owner, test.user, got)
		}
	}
}

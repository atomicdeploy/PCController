package ports

import (
	"context"
	"testing"
	"time"

	"go.bug.st/serial/enumerator"
)

func TestDetailedPortSourceRetainsEnumeratedPorts(t *testing.T) {
	result := detailedPortsToInfo([]*enumerator.PortDetails{
		{Name: "COM1"},
		{Name: "COM18", IsUSB: true, VID: "1a86", PID: "7523", Product: "USB-SERIAL CH340"},
		{Name: "COM19", IsUSB: true, VID: "1A86", PID: "7523", Product: "USB-SERIAL CH340"},
	})
	if len(result) != 3 || result[1].Name != "COM18" || result[2].Name != "COM19" ||
		result[1].VID != "1A86" || result[1].PID != "7523" {
		t.Fatalf("SetupAPI-derived details were lost: %#v", result)
	}
}

func TestCandidatesFilterAndOrder(t *testing.T) {
	all := []Info{
		{Name: "COM19", IsUSB: true, VID: "1A86", PID: "7523", Product: "USB Serial"},
		{Name: "COM3"},
		{Name: "COM18", IsUSB: true, VID: "1A86", PID: "7523", Product: "CH340"},
	}

	got := Candidates(all, Filter{VID: "0x1a86", PID: "07523"})
	if len(got) != 2 || got[0].Name != "COM18" || got[1].Name != "COM19" {
		t.Fatalf("unexpected candidates: %#v", got)
	}

	got = Candidates(all, Filter{Name: "ch340"})
	if len(got) != 1 || got[0].Name != "COM18" {
		t.Fatalf("name filter got %#v", got)
	}
}

func TestExplicitPortWins(t *testing.T) {
	all := []Info{
		{Name: "COM9", IsUSB: true},
		{Name: "COM18", IsUSB: true},
	}
	got := Candidates(all, Filter{Port: "com18"})
	if len(got) != 1 || got[0].Name != "COM18" {
		t.Fatalf("explicit selection got %#v", got)
	}
}

func TestParseHumanAndStableSelectors(t *testing.T) {
	tests := []struct {
		value string
		want  Filter
	}{
		{"COM18", Filter{Port: "COM18"}},
		{"1A86:7523", Filter{VID: "1A86", PID: "7523"}},
		{
			`USB\VID_1A86&PID_7523\INSTANCE`,
			Filter{VID: "1A86", PID: "7523"},
		},
		{"serial:ABC123", Filter{SerialNumber: "ABC123"}},
		{"instance:USB\\DEVICE", Filter{InstanceID: "USB\\DEVICE"}},
		{"USB-SERIAL CH340", Filter{Name: "USB-SERIAL CH340"}},
	}
	for _, test := range tests {
		got, err := ParseSelector(test.value)
		if err != nil {
			t.Fatalf("%q: %v", test.value, err)
		}
		if got.Port != test.want.Port || got.VID != test.want.VID ||
			got.PID != test.want.PID || got.Name != test.want.Name ||
			got.SerialNumber != test.want.SerialNumber ||
			got.InstanceID != test.want.InstanceID {
			t.Fatalf("%q => %#v, want %#v", test.value, got, test.want)
		}
	}
}

func TestPreferredIdentityResolvesOnlyStrongUniqueMatch(t *testing.T) {
	candidates := []Info{
		{
			Name: "COM4", VID: "1A86", PID: "7523",
			InstanceID: "USB\\A",
		},
		{
			Name: "COM18", VID: "1A86", PID: "7523",
			InstanceID: "USB\\B",
		},
	}
	got, ok := PreferredCandidate(
		candidates,
		Identity{Port: "COM4", InstanceID: "USB\\B"},
	)
	if !ok || got.Name != "COM18" {
		t.Fatalf("strong preferred identity = %#v, %t", got, ok)
	}
	if _, ok := PreferredCandidate(
		candidates,
		Identity{Port: "COM18", VID: "1A86", PID: "7523"},
	); ok {
		t.Fatal("remembered COM number alone suppressed ambiguity")
	}
}

func TestDeviceChangeWatcherCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	changes, err := WatchChanges(ctx)
	if err != nil {
		t.Skipf("platform device notifications unavailable: %v", err)
	}
	cancel()
	select {
	case _, ok := <-changes:
		if ok {
			// A queued real device event is allowed; the next receive must
			// still close promptly after cancellation.
			select {
			case <-changes:
			case <-time.After(time.Second):
				t.Fatal("device watcher did not close after cancellation")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("device watcher did not stop after cancellation")
	}
}

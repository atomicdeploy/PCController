//go:build windows

package ports

import "testing"

func TestRegistrySelectionPrefersOnlyPresentDuplicate(t *testing.T) {
	got := selectRegistryDevice([]registryDevice{
		{
			FriendlyName: "USB-SERIAL CH340",
			InstanceID:   `USB\VID_1A86&PID_7523\5&25B7E96&0&11`,
			Present:      false,
		},
		{
			FriendlyName: "USB-SERIAL CH340",
			InstanceID:   `USB\VID_1A86&PID_7523\6&2CC1445A&0&3`,
			Present:      true,
		},
	})
	if !got.Present ||
		got.InstanceID != `USB\VID_1A86&PID_7523\6&2CC1445A&0&3` {
		t.Fatalf("selected stale or missing device: %#v", got)
	}
}

func TestRegistrySelectionNeverPersistsAmbiguousInstance(t *testing.T) {
	for name, devices := range map[string][]registryDevice{
		"two present": {
			{
				FriendlyName: "USB-SERIAL CH340",
				InstanceID:   `USB\A`,
				Present:      true,
			},
			{
				FriendlyName: "USB-SERIAL CH340",
				InstanceID:   `USB\B`,
				Present:      true,
			},
		},
		"only historical": {
			{
				FriendlyName: "USB-SERIAL CH340",
				InstanceID:   `USB\OLD`,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := selectRegistryDevice(devices)
			if got.Present || got.InstanceID != "" {
				t.Fatalf("ambiguous identity was retained: %#v", got)
			}
			if got.FriendlyName != "USB-SERIAL CH340" {
				t.Fatalf("common descriptive name was lost: %#v", got)
			}
		})
	}
}

func TestRegistrySelectionDropsConflictingFriendlyNames(t *testing.T) {
	got := selectRegistryDevice([]registryDevice{
		{FriendlyName: "Adapter A", InstanceID: `USB\OLD-A`},
		{FriendlyName: "Adapter B", InstanceID: `USB\OLD-B`},
	})
	if got.FriendlyName != "" || got.InstanceID != "" {
		t.Fatalf("conflicting phantom registry data was retained: %#v", got)
	}
}

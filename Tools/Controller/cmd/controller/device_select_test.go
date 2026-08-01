package main

import (
	"bytes"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/ports"
)

func TestInteractiveDeviceSelectionForAmbiguousFriendlyName(t *testing.T) {
	options := &connectionFlags{
		device: "USB-SERIAL CH340",
		overrides: map[string]bool{
			"device": true,
		},
	}
	all := []ports.Info{
		{
			Name: "COM4", VID: "1A86", PID: "7523",
			FriendlyName: "USB-SERIAL CH340",
		},
		{
			Name: "COM18", VID: "1A86", PID: "7523",
			FriendlyName: "USB-SERIAL CH340",
		},
	}
	var output bytes.Buffer
	if err := selectInteractiveDeviceFrom(
		options,
		all,
		strings.NewReader("2\n"),
		&output,
	); err != nil {
		t.Fatal(err)
	}
	if options.filter().Port != "COM18" {
		t.Fatalf("selected filter = %#v", options.filter())
	}
	if !strings.Contains(output.String(), "1)") ||
		!strings.Contains(output.String(), "2)") {
		t.Fatalf("selection menu missing choices: %q", output.String())
	}
}

func TestStrongRememberedIdentityAvoidsInteractivePrompt(t *testing.T) {
	options := &connectionFlags{
		vid: "1A86", pid: "7523",
		preferred: ports.Identity{
			InstanceID: "USB\\B",
		},
		overrides: map[string]bool{},
	}
	all := []ports.Info{
		{Name: "COM4", VID: "1A86", PID: "7523", InstanceID: "USB\\A"},
		{Name: "COM18", VID: "1A86", PID: "7523", InstanceID: "USB\\B"},
	}
	var output bytes.Buffer
	if err := selectInteractiveDeviceFrom(
		options,
		all,
		strings.NewReader(""),
		&output,
	); err != nil {
		t.Fatal(err)
	}
	if options.filter().Port != "" ||
		options.filter().Preferred.InstanceID != "USB\\B" {
		t.Fatalf("preferred identity was not preserved: %#v", options.filter())
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected prompt for strong identity: %q", output.String())
	}
}

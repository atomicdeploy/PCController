package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/native"
)

func TestStripHostMenuStagesThenUsesSharedCommands(t *testing.T) {
	config := appconfig.Defaults()
	config.StripEffects = appconfig.DefaultStripEffects()
	live := control.Snapshot{Connected: true, Hello: native.Hello{Capabilities: native.CapabilityAddressableLED}}
	var calls []string
	var commandError error
	a := &hostStripMenuActions{
		config: func() appconfig.Config { return config }, snapshot: func() control.Snapshot { return live },
		state: func() control.OutputStreamState { return control.OutputStreamState{} },
		execute: func(_ context.Context, command string) (string, error) {
			calls = append(calls, command)
			return "ACK", commandError
		},
		color: "ffffff", brightness: 100, pixels: 100,
	}
	manager := hostmenu.New(config.HostMenus, hostmenu.Callbacks{
		Read: func(_ context.Context, action string) (string, error) {
			output, _, err := a.Read(action)
			return output, err
		},
		Write: func(_ context.Context, action, value string) (string, error) {
			output, _, err := a.Write(action, value)
			return output, err
		},
		Execute: func(ctx context.Context, action string) (string, error) {
			output, _, err := a.Execute(ctx, action)
			return output, err
		},
	})
	ctx := context.Background()
	if err := manager.UpdateSelectOptions("strip.effects", stripHostMenuOptions(config.StripEffects)); err != nil {
		t.Fatal(err)
	}
	if err := manager.Open("strip-controls"); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := manager.Refresh(ctx); err != nil || snapshot.Value != "Stream idle" {
		t.Fatalf("status=%+v err=%v", snapshot, err)
	}
	// K2 selects color; K4 selects Red without touching outputs.
	for _, key := range []int{2, 4} {
		if _, err := manager.HandleKey(ctx, key, "press"); err != nil {
			t.Fatal(err)
		}
	}
	if a.color != "ff0000" || len(calls) != 0 {
		t.Fatalf("selection color=%s calls=%v", a.color, calls)
	}
	for action, value := range map[string]string{"host.strip.brightness": "50", "host.strip.pixels": "2"} {
		if _, _, err := a.Write(action, value); err != nil {
			t.Fatal(err)
		}
	}
	// Navigate past staged brightness/count to Apply, then use the physical action.
	for _, key := range []int{2, 2, 2, 4} {
		if _, err := manager.HandleKey(ctx, key, "press"); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"strip frame 800000800000"}) {
		t.Fatalf("apply commands=%v", calls)
	}
	for _, action := range []string{"host.strip.play", "host.strip.stop", "host.strip.off"} {
		if _, handled, err := a.Execute(ctx, action); !handled || err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if !strings.Contains(calls[1], "effect:"+config.StripEffects[0].ID) || calls[2] != "strip stop" || calls[3] != "strip clear" {
		t.Fatalf("commands=%v", calls)
	}
	commandError = errors.New("board busy")
	if snapshot, err := manager.HandleKey(ctx, 4, "press"); !errors.Is(err, commandError) || snapshot.Value != "ERROR" {
		t.Fatalf("failed apply=%+v err=%v", snapshot, err)
	}
	before := len(calls)
	live.Connected = false
	if _, _, err := a.Execute(ctx, "host.strip.apply"); err == nil || len(calls) != before {
		t.Fatal("offline action dispatched")
	}
	live.Connected = true
	live.Hello.Capabilities = 0
	if _, _, err := a.Execute(ctx, "host.strip.play"); err == nil || len(calls) != before {
		t.Fatal("unsupported action dispatched")
	}
	for action, value := range map[string]string{"host.strip.pixels": "101", "host.strip.brightness": "-1", "host.strip.color": "bad", "host.strip.effect": "removed"} {
		if _, _, err := a.Write(action, value); err == nil {
			t.Fatalf("accepted %s=%s", action, value)
		}
	}
}

func TestStripHostMenuCatalogRefreshAndLocalizedNames(t *testing.T) {
	config := appconfig.Defaults()
	config.StripEffects = appconfig.DefaultStripEffects()
	a := &hostStripMenuActions{config: func() appconfig.Config { return config }}
	selected, _, _ := a.Read("host.strip.effect")
	if selected != config.StripEffects[0].ID {
		t.Fatal(selected)
	}
	config.StripEffects = []appconfig.StripEffect{{ID: "new-effect", Name: "لامپ new long effect label"}}
	selected, _, _ = a.Read("host.strip.effect")
	if selected != "new-effect" {
		t.Fatal(selected)
	}
	options := stripHostMenuOptions(config.StripEffects)
	manager := hostmenu.New(config.HostMenus, hostmenu.Callbacks{})
	if len(options[0].Label) > 16 {
		t.Fatal(options)
	}
	if err := manager.UpdateSelectOptions("strip.effects", options); err != nil {
		t.Fatal(err)
	}
	config.StripEffects = nil
	selected, _, _ = a.Read("host.strip.effect")
	if selected != "" {
		t.Fatalf("stale selection=%s", selected)
	}
}

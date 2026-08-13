package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

type fakeVisibleSurfacePlatform struct {
	start func(visibleSurfaceSpec) visibleSurfaceStart
	focus func(visibleSurfaceSpec, hostui.AppInstance) visibleSurfaceStart
}

func (platform fakeVisibleSurfacePlatform) Start(
	_ context.Context,
	spec visibleSurfaceSpec,
) visibleSurfaceStart {
	if platform.start == nil {
		return visibleSurfaceStart{Reason: "unexpected start"}
	}
	return platform.start(spec)
}

func (platform fakeVisibleSurfacePlatform) Focus(
	_ context.Context,
	spec visibleSurfaceSpec,
	instance hostui.AppInstance,
) visibleSurfaceStart {
	if platform.focus == nil {
		return visibleSurfaceStart{Reason: "focus unsupported"}
	}
	return platform.focus(spec, instance)
}

func TestNamedSurfaceEnsureReusesAndTargetsLiveInstance(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	instances := hostui.NewInstanceRegistry()
	if _, err := instances.Upsert(hostui.AppInstance{
		ID: "webui:live", Surface: hostui.SurfaceWebUI, Page: "dashboard",
	}); err != nil {
		t.Fatal(err)
	}
	var action hostui.AppAction
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, listen: "0.0.0.0:8787",
		action:   func(value hostui.AppAction) error { action = value; return nil },
		platform: fakeVisibleSurfacePlatform{}, registrationTimeout: 0, now: time.Now,
	}
	result, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
		Surface: hostui.SurfaceWebUI, Mode: hostui.SurfaceLaunchEnsure,
		Target: "webui:live", Page: "settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || !result.Confirmed || result.Effective != "existing" || result.InstanceID != "webui:live" {
		t.Fatalf("result=%#v", result)
	}
	if action.Kind != "app.page" || action.Value != "settings" || action.Target != "webui:live" || action.Source != "surface-launcher" {
		t.Fatalf("action=%#v", action)
	}
}

func TestNamedSurfaceLaunchUsesOnlyProductOwnedArgumentsAndConfirmsRegistry(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	instances := hostui.NewInstanceRegistry()
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, listen: "0.0.0.0:8787",
		action:              func(hostui.AppAction) error { return nil },
		registrationTimeout: 500 * time.Millisecond, now: time.Now,
	}
	launcher.platform = fakeVisibleSurfacePlatform{start: func(spec visibleSurfaceSpec) visibleSurfaceStart {
		if spec.Surface != hostui.SurfaceTUI {
			t.Fatalf("surface=%q", spec.Surface)
		}
		joined := strings.Join(spec.Arguments, "\x00")
		for _, required := range []string{"--config", store.Path(), "tui", "--ipc-addr", "127.0.0.1:8787"} {
			if !strings.Contains(joined, required) {
				t.Fatalf("arguments=%#v missing %q", spec.Arguments, required)
			}
		}
		go func() {
			_, _ = instances.Upsert(hostui.AppInstance{
				ID: "tui:new", Surface: hostui.SurfaceTUI, Page: "dashboard",
			})
		}()
		return visibleSurfaceStart{Backend: "fake-terminal", LauncherProcessID: 321, Accepted: true}
	}}
	result, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
		Surface: hostui.SurfaceTUI, Mode: hostui.SurfaceLaunchLaunch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || !result.Confirmed || result.InstanceID != "tui:new" || result.Backend != "fake-terminal" || result.LauncherProcessID != 321 {
		t.Fatalf("result=%#v", result)
	}
}

func TestNamedSurfaceRejectsUnknownOrCrossSurfacePage(t *testing.T) {
	for _, test := range []struct{ surface, page string }{
		{surface: "tui", page: "workbench"},
		{surface: "webui", page: "console"},
		{surface: "webui", page: "made-up"},
	} {
		if err := validateSurfaceLaunchPage(test.surface, test.page); err == nil {
			t.Fatalf("surface=%q page=%q was accepted", test.surface, test.page)
		}
	}
	if err := validateSurfaceLaunchPage("tui", "updates"); err != nil {
		t.Fatal(err)
	}
}

func TestNamedSurfaceFocusReportsUnsupportedWithoutLaunchingDuplicate(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	instances := hostui.NewInstanceRegistry()
	if _, err := instances.Upsert(hostui.AppInstance{ID: "tui:live", Surface: "tui"}); err != nil {
		t.Fatal(err)
	}
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, listen: "127.0.0.1:8787",
		platform: fakeVisibleSurfacePlatform{}, registrationTimeout: 0, now: time.Now,
	}
	result, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
		Surface: "tui", Mode: "focus", Target: "tui:live",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted || !result.Confirmed || result.InstanceID != "tui:live" ||
		result.Effective != "unavailable" || !strings.Contains(result.Reason, "focus unsupported") {
		t.Fatalf("result=%#v", result)
	}
}

func TestSurfaceChildEnvironmentPreservesGUIAndDropsCredentials(t *testing.T) {
	const token = "configured-auth-token"
	filtered := surfaceChildEnvironment([]string{
		"DISPLAY=:0", "WAYLAND_DISPLAY=wayland-0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
		"XAUTHORITY=/run/user/1000/xauth", "GH_TOKEN=secret", "APP_PASSWORD=secret",
		"WRAPPED=prefix-" + token + "-suffix", "LANG=en_US.UTF-8",
	}, token)
	joined := strings.Join(filtered, "\n")
	for _, expected := range []string{"DISPLAY=:0", "WAYLAND_DISPLAY=wayland-0", "DBUS_SESSION_BUS_ADDRESS=", "XAUTHORITY=", "LANG="} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("environment=%q missing %q", joined, expected)
		}
	}
	for _, rejected := range []string{"GH_TOKEN", "APP_PASSWORD", token} {
		if strings.Contains(joined, rejected) {
			t.Fatalf("environment=%q leaked %q", joined, rejected)
		}
	}
}

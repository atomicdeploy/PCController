package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
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

func TestNamedSurfaceLaunchWithPageDoesNotNavigateExistingInstance(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	instances := hostui.NewInstanceRegistry()
	if _, err := instances.Upsert(hostui.AppInstance{ID: "tui:existing", Surface: hostui.SurfaceTUI}); err != nil {
		t.Fatal(err)
	}
	var actions []hostui.AppAction
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, listen: "127.0.0.1:8787",
		action: func(action hostui.AppAction) error {
			actions = append(actions, action)
			return nil
		},
		registrationTimeout: 500 * time.Millisecond, now: time.Now,
	}
	launcher.platform = fakeVisibleSurfacePlatform{start: func(visibleSurfaceSpec) visibleSurfaceStart {
		if _, err := instances.Upsert(hostui.AppInstance{ID: "tui:new", Surface: hostui.SurfaceTUI}); err != nil {
			t.Fatal(err)
		}
		return visibleSurfaceStart{Backend: "fake-terminal", Accepted: true}
	}}
	result, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
		Surface: hostui.SurfaceTUI, Mode: hostui.SurfaceLaunchLaunch, Page: "settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Confirmed || result.InstanceID != "tui:new" {
		t.Fatalf("result=%#v", result)
	}
	if len(actions) != 1 || actions[0].Target != "tui:new" || actions[0].Value != "settings" {
		t.Fatalf("actions=%#v", actions)
	}
}

func TestNamedSurfaceLaunchSerializesSameSurfaceRegistration(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	instances := hostui.NewInstanceRegistry()
	firstStarted := make(chan struct{})
	allowFirstRegistration := make(chan struct{})
	secondStarted := make(chan struct{})
	var releaseFirst sync.Once
	defer releaseFirst.Do(func() { close(allowFirstRegistration) })
	var startsMu sync.Mutex
	starts := 0
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, listen: "127.0.0.1:8787",
		action:              func(hostui.AppAction) error { return nil },
		registrationTimeout: time.Second, now: time.Now,
	}
	launcher.platform = fakeVisibleSurfacePlatform{start: func(visibleSurfaceSpec) visibleSurfaceStart {
		startsMu.Lock()
		starts++
		start := starts
		startsMu.Unlock()
		switch start {
		case 1:
			close(firstStarted)
			go func() {
				<-allowFirstRegistration
				_, _ = instances.Upsert(hostui.AppInstance{ID: "tui:first", Surface: hostui.SurfaceTUI})
			}()
		case 2:
			close(secondStarted)
			_, _ = instances.Upsert(hostui.AppInstance{ID: "tui:second", Surface: hostui.SurfaceTUI})
		default:
			t.Errorf("unexpected starts=%d", start)
		}
		return visibleSurfaceStart{Backend: "fake-terminal", Accepted: true}
	}}

	results := make(chan hostui.SurfaceLaunchResult, 2)
	errors := make(chan error, 2)
	launch := func() {
		result, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
			Surface: hostui.SurfaceTUI, Mode: hostui.SurfaceLaunchLaunch,
		})
		results <- result
		errors <- err
	}
	go launch()
	<-firstStarted
	go launch()
	select {
	case <-secondStarted:
		t.Fatal("second launch started before the first registration completed")
	case <-time.After(100 * time.Millisecond):
	}
	releaseFirst.Do(func() { close(allowFirstRegistration) })

	first := <-results
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second launch did not begin after the first registration")
	}
	second := <-results
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if !first.Confirmed || !second.Confirmed || first.InstanceID == second.InstanceID {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if first.InstanceID != "tui:first" || second.InstanceID != "tui:second" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestNamedSurfaceLaunchRateLimitsNewWindowsPerSurface(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	starts := 0
	launcher := &namedSurfaceLauncher{
		store: store, instances: hostui.NewInstanceRegistry(), listen: "127.0.0.1:8787",
		action:              func(hostui.AppAction) error { return nil },
		registrationTimeout: 0, now: func() time.Time { return now },
	}
	launcher.platform = fakeVisibleSurfacePlatform{start: func(visibleSurfaceSpec) visibleSurfaceStart {
		starts++
		return visibleSurfaceStart{Backend: "fake-terminal", Accepted: true}
	}}
	request := hostui.SurfaceLaunchRequest{Surface: hostui.SurfaceTUI, Mode: hostui.SurfaceLaunchLaunch}
	for attempt := 0; attempt < maximumVisibleSurfaceStarts; attempt++ {
		result, err := launcher.launch(context.Background(), request)
		if err != nil || !result.Accepted {
			t.Fatalf("attempt=%d result=%#v err=%v", attempt, result, err)
		}
	}
	blocked, err := launcher.launch(context.Background(), request)
	if err != nil || blocked.Accepted || blocked.Effective != "unavailable" ||
		!strings.Contains(blocked.Reason, "rate limit") {
		t.Fatalf("blocked=%#v err=%v", blocked, err)
	}
	if starts != maximumVisibleSurfaceStarts {
		t.Fatalf("starts=%d", starts)
	}
	otherSurface, err := launcher.launch(context.Background(), hostui.SurfaceLaunchRequest{
		Surface: hostui.SurfaceWebUI, Mode: hostui.SurfaceLaunchLaunch,
	})
	if err != nil || !otherSurface.Accepted || starts != maximumVisibleSurfaceStarts+1 {
		t.Fatalf("otherSurface=%#v starts=%d err=%v", otherSurface, starts, err)
	}
	now = now.Add(visibleSurfaceStartWindow)
	afterWindow, err := launcher.launch(context.Background(), request)
	if err != nil || !afterWindow.Accepted || starts != maximumVisibleSurfaceStarts+2 {
		t.Fatalf("afterWindow=%#v starts=%d err=%v", afterWindow, starts, err)
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

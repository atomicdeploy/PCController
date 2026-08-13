package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	surfaceRegistrationWait     = 1500 * time.Millisecond
	visibleSurfaceStartWindow   = 10 * time.Second
	maximumVisibleSurfaceStarts = 3
)

type surfaceLaunchGate struct {
	available    chan struct{}
	recentStarts []time.Time
}

type visibleSurfaceSpec struct {
	Surface     string
	Executable  string
	Arguments   []string
	URL         string
	Title       string
	Directory   string
	Environment []string
}

type visibleSurfaceStart struct {
	Backend           string
	LauncherProcessID int
	Accepted          bool
	Reason            string
}

type visibleSurfacePlatform interface {
	Start(context.Context, visibleSurfaceSpec) visibleSurfaceStart
	Focus(context.Context, visibleSurfaceSpec, hostui.AppInstance) visibleSurfaceStart
}

type namedSurfaceLauncher struct {
	store               *appconfig.Store
	instances           *hostui.InstanceRegistry
	action              func(hostui.AppAction) error
	listen              string
	platform            visibleSurfacePlatform
	registrationTimeout time.Duration
	now                 func() time.Time
	gatesMu             sync.Mutex
	gates               map[string]*surfaceLaunchGate
}

func newNamedSurfaceLaunchCoordinator(
	store *appconfig.Store,
	instances *hostui.InstanceRegistry,
	action func(hostui.AppAction) error,
	listen string,
) *hostui.SurfaceLaunchCoordinator {
	launcher := &namedSurfaceLauncher{
		store: store, instances: instances, action: action,
		listen: listen, platform: newVisibleSurfacePlatform(),
		registrationTimeout: surfaceRegistrationWait, now: time.Now,
	}
	return hostui.NewSurfaceLaunchCoordinator(launcher.launch)
}

func (launcher *namedSurfaceLauncher) launch(
	ctx context.Context,
	request hostui.SurfaceLaunchRequest,
) (hostui.SurfaceLaunchResult, error) {
	if launcher == nil || launcher.store == nil || launcher.instances == nil || launcher.platform == nil {
		return hostui.SurfaceLaunchResult{}, errors.New("application surface launcher is unavailable")
	}
	spec, err := launcher.spec(request)
	if err != nil {
		return hostui.SurfaceLaunchResult{}, err
	}
	gate := launcher.surfaceGate(request.Surface)
	select {
	case <-ctx.Done():
		return hostui.SurfaceLaunchResult{}, ctx.Err()
	case <-gate.available:
	}
	defer func() { gate.available <- struct{}{} }()

	before := launcher.instances.List()
	existing, haveExisting := selectSurfaceInstance(before, request.Surface, request.Target)
	if request.Target != "" && !haveExisting {
		return unavailableSurfaceResult("target instance is not registered"), nil
	}
	if haveExisting && request.Page != "" && request.Mode != hostui.SurfaceLaunchLaunch {
		if err := launcher.navigate(request.Page, existing.ID); err != nil {
			return hostui.SurfaceLaunchResult{}, err
		}
	}

	switch request.Mode {
	case hostui.SurfaceLaunchEnsure:
		if haveExisting {
			return hostui.SurfaceLaunchResult{
				Effective: "existing", InstanceID: existing.ID,
				Accepted: true, Confirmed: true,
			}, nil
		}
	case hostui.SurfaceLaunchFocus:
		if !haveExisting {
			return unavailableSurfaceResult("no live instance is available to focus"), nil
		}
		focused := launcher.platform.Focus(ctx, spec, existing)
		return surfaceProcessResult("focus-requested", focused, existing.ID, true), nil
	}

	if !gate.allowStart(launcher.now().UTC()) {
		return unavailableSurfaceResult("surface launch rate limit reached; try again shortly"), nil
	}
	started := launcher.platform.Start(ctx, spec)
	if !started.Accepted {
		return surfaceProcessResult("unavailable", started, "", false), nil
	}
	instance := launcher.waitForNewInstance(ctx, before, request.Surface)
	confirmed := instance.ID != ""
	if confirmed && request.Page != "" && request.Surface == hostui.SurfaceTUI {
		if err := launcher.navigate(request.Page, instance.ID); err != nil {
			return hostui.SurfaceLaunchResult{}, err
		}
	}
	result := surfaceProcessResult("started", started, instance.ID, confirmed)
	if !confirmed && result.Reason == "" {
		result.Reason = "operating system accepted the launch; instance registration is pending"
	}
	return result, nil
}

func (launcher *namedSurfaceLauncher) surfaceGate(surface string) *surfaceLaunchGate {
	launcher.gatesMu.Lock()
	defer launcher.gatesMu.Unlock()
	if launcher.gates == nil {
		launcher.gates = make(map[string]*surfaceLaunchGate)
	}
	if gate := launcher.gates[surface]; gate != nil {
		return gate
	}
	gate := &surfaceLaunchGate{available: make(chan struct{}, 1)}
	gate.available <- struct{}{}
	launcher.gates[surface] = gate
	return gate
}

func (gate *surfaceLaunchGate) allowStart(now time.Time) bool {
	cutoff := now.Add(-visibleSurfaceStartWindow)
	starts := gate.recentStarts[:0]
	for _, started := range gate.recentStarts {
		if started.After(cutoff) {
			starts = append(starts, started)
		}
	}
	gate.recentStarts = starts
	if len(gate.recentStarts) >= maximumVisibleSurfaceStarts {
		return false
	}
	gate.recentStarts = append(gate.recentStarts, now)
	return true
}

func (launcher *namedSurfaceLauncher) spec(
	request hostui.SurfaceLaunchRequest,
) (visibleSurfaceSpec, error) {
	if err := validateSurfaceLaunchPage(request.Surface, request.Page); err != nil {
		return visibleSurfaceSpec{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return visibleSurfaceSpec{}, fmt.Errorf("resolve controller executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return visibleSurfaceSpec{}, fmt.Errorf("resolve absolute controller executable: %w", err)
	}
	directory, err := os.Getwd()
	if err != nil {
		return visibleSurfaceSpec{}, fmt.Errorf("resolve controller working directory: %w", err)
	}
	address, err := localHostDialAddress(launcher.listen)
	if err != nil {
		return visibleSurfaceSpec{}, err
	}
	runtimeConfig := launcher.store.CurrentRuntime()
	spec := visibleSurfaceSpec{
		Surface: request.Surface, Executable: executable,
		Title:     productidentity.Title(launcher.store.Current().UI.AppTitle),
		Directory: directory,
		Environment: surfaceChildEnvironment(
			os.Environ(), runtimeConfig.IPC.AuthToken,
		),
	}
	switch request.Surface {
	case hostui.SurfaceTUI:
		spec.Arguments = []string{
			"--config", launcher.store.Path(), "tui", "--ipc-addr", address,
		}
	case hostui.SurfaceWebUI:
		spec.URL, err = browserURL(launcher.listen)
		if err == nil && request.Page != "" {
			spec.URL, err = webURLForAppAction(spec.URL, hostui.AppAction{
				Kind: "app.page", Value: request.Page,
			})
		}
		if err != nil {
			return visibleSurfaceSpec{}, err
		}
	default:
		return visibleSurfaceSpec{}, errors.New("surface must be tui or webui")
	}
	return spec, nil
}

func validateSurfaceLaunchPage(surface, page string) error {
	page = strings.ToLower(strings.TrimSpace(page))
	if page == "" {
		return nil
	}
	var allowed map[string]struct{}
	switch surface {
	case hostui.SurfaceTUI:
		allowed = map[string]struct{}{
			"dashboard": {}, "controls": {}, "menus": {}, "board": {},
			"settings": {}, "rf": {}, "updates": {}, "automations": {},
			"events": {}, "console": {},
		}
	case hostui.SurfaceWebUI:
		allowed = map[string]struct{}{
			"dashboard": {}, "controls": {}, "workbench": {}, "device": {},
			"data": {}, "updates": {}, "events": {}, "settings": {},
		}
	default:
		return errors.New("surface must be tui or webui")
	}
	if _, ok := allowed[page]; !ok {
		return fmt.Errorf("page %q is not available on the %s surface", page, surface)
	}
	return nil
}

func (launcher *namedSurfaceLauncher) navigate(page, target string) error {
	if launcher.action == nil {
		return errors.New("application navigation routing is unavailable")
	}
	return launcher.action(hostui.AppAction{
		Kind: "app.page", Value: page, Target: target, Source: "surface-launcher",
	})
}

func (launcher *namedSurfaceLauncher) waitForNewInstance(
	ctx context.Context,
	before []hostui.AppInstance,
	surface string,
) hostui.AppInstance {
	if launcher.registrationTimeout <= 0 {
		return hostui.AppInstance{}
	}
	known := make(map[string]struct{}, len(before))
	for _, instance := range before {
		known[instance.ID] = struct{}{}
	}
	deadline := launcher.now().Add(launcher.registrationTimeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, instance := range launcher.instances.List() {
			if !strings.EqualFold(instance.Surface, surface) {
				continue
			}
			if _, existed := known[instance.ID]; !existed {
				return instance
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return hostui.AppInstance{}
		}
		select {
		case <-ctx.Done():
			return hostui.AppInstance{}
		case <-ticker.C:
		}
	}
}

func selectSurfaceInstance(
	instances []hostui.AppInstance,
	surface, target string,
) (hostui.AppInstance, bool) {
	for _, instance := range instances {
		if target != "" && instance.ID != target {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(instance.Surface), surface) {
			return instance, true
		}
	}
	return hostui.AppInstance{}, false
}

func unavailableSurfaceResult(reason string) hostui.SurfaceLaunchResult {
	return hostui.SurfaceLaunchResult{
		Effective: "unavailable", Reason: reason, Accepted: false, Confirmed: false,
	}
}

func surfaceProcessResult(
	effective string,
	process visibleSurfaceStart,
	instanceID string,
	confirmed bool,
) hostui.SurfaceLaunchResult {
	if !process.Accepted {
		effective = "unavailable"
	}
	return hostui.SurfaceLaunchResult{
		Effective: effective, InstanceID: instanceID, Backend: process.Backend,
		Reason: process.Reason, LauncherProcessID: process.LauncherProcessID,
		Accepted: process.Accepted, Confirmed: confirmed,
	}
}

func surfaceChildEnvironment(environment []string, secret string) []string {
	secret = strings.TrimSpace(secret)
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "-", "_"))
		credentialLike := strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "secret") ||
			strings.Contains(normalized, "password") ||
			strings.Contains(normalized, "passwd") ||
			strings.Contains(normalized, "credential") ||
			strings.Contains(normalized, "cookie") ||
			strings.Contains(normalized, "api_key") ||
			strings.Contains(normalized, "apikey")
		containsConfiguredSecret := secret != "" && strings.Contains(value, secret)
		if credentialLike || containsConfiguredSecret {
			continue
		}
		result = append(result, entry)
	}
	return result
}

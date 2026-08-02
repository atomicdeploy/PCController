package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/programmer"
)

func TestCompileOnlyCommandDoesNotLoadOrMutateRuntimeConfig(t *testing.T) {
	for _, test := range []struct {
		name string
		args func(string, string) []string
	}{
		{
			name: "program compile",
			args: func(project, output string) []string {
				return []string{
					"program", "--method", "compile", "--sketch", project,
					"--output-dir", output, "--dry-run",
				}
			},
		},
		{
			name: "toolchain compile alias",
			args: func(project, output string) []string {
				return []string{
					"toolchain", "compile", project,
					"--output-dir", output, "--dry-run",
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			invalid := []byte(`{"schema":1,"host_menus":{"request_gesture":"status-hold-k4"}}`)
			if err := os.WriteFile(path, invalid, 0o600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--config", path}, test.args(findProjectRoot(), t.TempDir())...)
			var stdout, stderr bytes.Buffer
			err := run(args, &stdout, &stderr)
			if err != nil {
				t.Fatalf("compile-only command depended on runtime config: %v\nstderr: %s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "compile") {
				t.Fatalf("compile plan missing from output: %q", stdout.String())
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, invalid) {
				t.Fatalf("compile-only command mutated runtime config:\n%s", after)
			}
		})
	}
}

func TestRuntimeCommandStillValidatesRuntimeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(
		path,
		[]byte(`{"schema":1,"host_menus":{"request_gesture":"status-hold-k4"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "runtime port discovery", args: []string{"ports"}},
		{
			name: "device programming",
			args: []string{
				"program", "--method", "urclock", "--operation", "probe", "--dry-run",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", path}, test.args...)
			err := run(args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "request_gesture") {
				t.Fatalf("runtime config validation error=%v", err)
			}
		})
	}
}

func TestBrowserURLUsesReachableLoopbackAddress(t *testing.T) {
	for input, expected := range map[string]string{
		"127.0.0.1:8787": "http://127.0.0.1:8787/",
		"localhost:9000": "http://localhost:9000/",
		"[::1]:8787":     "http://[::1]:8787/",
		"0.0.0.0:8787":   "http://127.0.0.1:8787/",
		"[::]:8787":      "http://127.0.0.1:8787/",
	} {
		got, err := browserURL(input)
		if err != nil || got != expected {
			t.Errorf("browserURL(%q)=%q, %v; want %q", input, got, err, expected)
		}
	}
	if _, err := browserURL("missing-port"); err == nil {
		t.Fatal("invalid listen address should fail")
	}
}

func TestWebBrowserAutoOpenRequiresConnectedController(t *testing.T) {
	tests := []struct {
		name      string
		noOpen    bool
		connected bool
		want      bool
	}{
		{name: "connected default", connected: true, want: true},
		{name: "disconnected default", connected: false, want: false},
		{name: "connected explicit no-open", noOpen: true, connected: true, want: false},
		{name: "disconnected explicit no-open", noOpen: true, connected: false, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := webBrowserAutoOpenAllowed(test.noOpen, test.connected); got != test.want {
				t.Fatalf("webBrowserAutoOpenAllowed(%v, %v)=%v; want %v", test.noOpen, test.connected, got, test.want)
			}
		})
	}
}

type recordingHostPanelBridge struct {
	pushes   chan hostmenu.Snapshot
	releases chan struct{}
}

func newRecordingHostPanelBridge() *recordingHostPanelBridge {
	return &recordingHostPanelBridge{
		pushes:   make(chan hostmenu.Snapshot, 16),
		releases: make(chan struct{}, 16),
	}
}

func (bridge *recordingHostPanelBridge) Push(snapshot hostmenu.Snapshot) error {
	bridge.pushes <- snapshot
	return nil
}

func (bridge *recordingHostPanelBridge) Release() error {
	bridge.releases <- struct{}{}
	return nil
}

func waitHostMenuDefinitionChange(
	t *testing.T,
	changes <-chan hostmenu.DefinitionChange,
	menuID string,
	predicate func(hostmenu.DefinitionChange) bool,
) hostmenu.DefinitionChange {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case change := <-changes:
			if change.MenuID == menuID && (predicate == nil || predicate(change)) {
				return change
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for host-menu change %q", menuID)
		}
	}
}

func findHostMenuDefinition(
	manager *hostmenu.Manager,
	menuID string,
) (appconfig.HostMenu, bool) {
	for _, menu := range manager.Config().Menus {
		if menu.ID == menuID {
			return menu, true
		}
	}
	return appconfig.HostMenu{}, false
}

func waitHostPanelPush(t *testing.T, pushes <-chan hostmenu.Snapshot) hostmenu.Snapshot {
	t.Helper()
	select {
	case snapshot := <-pushes:
		return snapshot
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for host front-panel push")
		return hostmenu.Snapshot{}
	}
}

func waitHostPanelRelease(t *testing.T, releases <-chan struct{}) {
	t.Helper()
	select {
	case <-releases:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for host front-panel release")
	}
}

func assertNoHostPanelAction(t *testing.T, bridge *recordingHostPanelBridge) {
	t.Helper()
	select {
	case snapshot := <-bridge.pushes:
		t.Fatalf("inactive edit unexpectedly pushed front panel: %+v", snapshot)
	case <-bridge.releases:
		t.Fatal("inactive edit unexpectedly released front panel")
	case <-time.After(120 * time.Millisecond):
	}
}

func removeHostMenu(config *appconfig.Config, menuID string) {
	menus := config.HostMenus.Menus[:0]
	for _, menu := range config.HostMenus.Menus {
		if menu.ID != menuID {
			menus = append(menus, menu)
		}
	}
	config.HostMenus.Menus = menus
	for menuIndex := range config.HostMenus.Menus {
		items := config.HostMenus.Menus[menuIndex].Items[:0]
		for _, item := range config.HostMenus.Menus[menuIndex].Items {
			if item.Type != "submenu" || item.Submenu != menuID {
				items = append(items, item)
			}
		}
		config.HostMenus.Menus[menuIndex].Items = items
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"help"}, "controller ws serve"},
		{[]string{"version"}, productidentity.Version + " source-hash=unknown built=unknown"},
	} {
		var stdout, stderr bytes.Buffer
		if err := run(test.args, &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v", test.args, err)
		}
		plainOutput := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(stdout.String(), "")
		if !strings.Contains(plainOutput, test.want) {
			t.Fatalf("%v output %q missing %q", test.args, stdout.String(), test.want)
		}
	}
}

func TestPersistedProductTitleAppearsInHelpAndVersion(t *testing.T) {
	t.Setenv("APP_TITLE", "")
	path := filepath.Join(t.TempDir(), "config.json")
	value := appconfig.Defaults()
	value.UI.AppTitle = "Workshop Controller"
	if err := appconfig.Write(path, value); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"help", "version"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{"--config", path, command}, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if !strings.Contains(stdout.String(), "Workshop Controller") {
			t.Fatalf("%s did not use persisted product title: %q", command, stdout.String())
		}
	}
}

func TestUsageVT100StylingPreservesPlainContent(t *testing.T) {
	const plain = "◆ Workshop Controller\n\nInteractive control:\n  controller tui [connection flags]"
	styled := decorateUsage(plain, true)
	for _, want := range []string{"\x1b[1;36m", "\x1b[1;33m", "\x1b[1;32m", "\x1b[0m"} {
		if !strings.Contains(styled, want) {
			t.Fatalf("styled usage missing %q: %q", want, styled)
		}
	}
	stripANSI := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if got := stripANSI.ReplaceAllString(styled, ""); got != plain {
		t.Fatalf("styled usage changed content: got %q want %q", got, plain)
	}
	if got := decorateUsage(plain, false); got != plain {
		t.Fatalf("plain fallback changed content: got %q want %q", got, plain)
	}
}

func TestDeniedHostMenuInteractionSelectsShortErrorCue(t *testing.T) {
	opcode, payload, ok := hostMenuInteractionCue(hostmenu.InteractionEvent{Kind: "menu.action.denied"})
	if !ok || opcode != native.OpBuzzer || len(payload) != 4 ||
		binary.LittleEndian.Uint16(payload[:2]) != 180 ||
		binary.LittleEndian.Uint16(payload[2:]) != 80 {
		t.Fatalf("denied cue opcode/payload=0x%02X % X ok=%t", opcode, payload, ok)
	}
	if _, _, ok := hostMenuInteractionCue(hostmenu.InteractionEvent{Kind: "menu.changed"}); ok {
		t.Fatal("non-denied interaction selected an error cue")
	}
}

func TestWatchedHostMenusAcrossFormatsRoutePreviewAndRelease(t *testing.T) {
	for _, extension := range []string{".json", ".yaml", ".toml"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "controller"+extension)
			store, err := appconfig.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			manager := hostmenu.New(store.Current().HostMenus, hostmenu.Callbacks{})
			if err := manager.Open("host"); err != nil {
				t.Fatal(err)
			}
			bridge := newRecordingHostPanelBridge()
			changes := make(chan hostmenu.DefinitionChange, 32)
			routeErrors := make(chan error, 4)
			manager.SetDefinitionChanged(func(change hostmenu.DefinitionChange) {
				changes <- change
				if routeErr := syncFallbackHostMenuOverlay(manager, bridge, &change); routeErr != nil {
					routeErrors <- routeErr
				}
			})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			updates := store.Subscribe(ctx)
			<-updates // The manager was constructed from this initial snapshot.
			go func() {
				for value := range updates {
					manager.UpdateConfig(value.HostMenus)
				}
			}()
			watchErrors := make(chan error, 4)
			go store.Watch(ctx, 10*time.Millisecond, nil, func(watchErr error) {
				watchErrors <- watchErr
			})

			// An external atomic file edit exercises fsnotify, format decoding,
			// Store.Reload, the subscription, Manager.UpdateConfig, and the same
			// production routing helper used by cap19 firmware.
			active := store.Current()
			active.HostMenus.Menus[0].Label = "LIVE"
			active.HostMenus.Menus[0].Title = "Watched Menu"
			active.HostMenus.Menus[0].Content = "Updated now"
			if err := appconfig.Write(path, active); err != nil {
				t.Fatal(err)
			}
			change := waitHostMenuDefinitionChange(t, changes, "host", func(change hostmenu.DefinitionChange) bool {
				return change.Active
			})
			if change.Kind != "menu.content.changed" {
				t.Fatalf("active change kind=%q", change.Kind)
			}
			preview := waitHostPanelPush(t, bridge.pushes)
			if preview.Panel.Segments != "LIVE" ||
				preview.Panel.LCDLine1 != "Watched Menu" ||
				preview.Panel.LCDLine2 != "Updated now" {
				t.Fatalf("active edit did not push matching TM1637/LCD preview: %+v", preview.Panel)
			}
			loaded, _, err := appconfig.Load(path)
			if err != nil || loaded.HostMenus.Menus[0].Label != "LIVE" {
				t.Fatalf("%s parse/write reload=%+v err=%v", extension, loaded.HostMenus.Menus[0], err)
			}

			// Store.Update uses the same atomic encoder/decoder for each format.
			// Editing a different node must emit an event without disturbing the
			// currently displayed host page.
			if _, err := store.Update(func(config *appconfig.Config) error {
				config.HostMenus.Menus[1].Content = "Inactive edit"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			change = waitHostMenuDefinitionChange(t, changes, "pc-settings", func(hostmenu.DefinitionChange) bool {
				menu, exists := findHostMenuDefinition(manager, "pc-settings")
				return exists && menu.Content == "Inactive edit"
			})
			if change.Active || change.Kind != "menu.content.changed" {
				t.Fatalf("inactive change=%+v", change)
			}
			assertNoHostPanelAction(t, bridge)

			// Hiding the active node closes the session and releases physical
			// capture; restoring it is persisted before testing deletion too.
			if err := manager.Open("pc-settings"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Update(func(config *appconfig.Config) error {
				config.HostMenus.Menus[1].Flags.Visible = false
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waitHostMenuDefinitionChange(t, changes, "pc-settings", func(hostmenu.DefinitionChange) bool {
				menu, exists := findHostMenuDefinition(manager, "pc-settings")
				return exists && !menu.Flags.Visible
			})
			waitHostPanelRelease(t, bridge.releases)
			if manager.Snapshot().Active {
				t.Fatal("hidden active host menu retained front-panel session")
			}

			if _, err := store.Update(func(config *appconfig.Config) error {
				config.HostMenus.Menus[1].Flags.Visible = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waitHostMenuDefinitionChange(t, changes, "pc-settings", func(hostmenu.DefinitionChange) bool {
				menu, exists := findHostMenuDefinition(manager, "pc-settings")
				return exists && menu.Flags.Visible
			})
			waitHostPanelRelease(t, bridge.releases)
			if err := manager.Open("pc-settings"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Update(func(config *appconfig.Config) error {
				removeHostMenu(config, "pc-settings")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waitHostMenuDefinitionChange(t, changes, "pc-settings", func(hostmenu.DefinitionChange) bool {
				_, exists := findHostMenuDefinition(manager, "pc-settings")
				return !exists
			})
			waitHostPanelRelease(t, bridge.releases)
			if manager.Snapshot().Active {
				t.Fatal("deleted active host menu retained front-panel session")
			}
			persisted, _, err := appconfig.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, menu := range persisted.HostMenus.Menus {
				if menu.ID == "pc-settings" {
					t.Fatal("deleted host menu remained in persisted configuration")
				}
			}

			select {
			case routeErr := <-routeErrors:
				t.Fatalf("host-menu preview routing failed: %v", routeErr)
			default:
			}
			select {
			case watchErr := <-watchErrors:
				t.Fatalf("configuration watcher failed: %v", watchErr)
			default:
			}
		})
	}
}

func TestToolchainSyncDryRunUsesControllerOwnedPlan(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runToolchainSync(
		[]string{"--dry-run", "--cli", "arduino-cli"},
		&stdout,
		&stderr,
		store,
	); err != nil {
		t.Fatalf("dry-run: %v\nstderr=%s", err, stderr.String())
	}
	for _, want := range []string{
		"core update-index",
		"lib install \"Adafruit PWM Servo Driver Library\"",
		"MiniCore:avr",
		"no changes made",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestBootAndToolchainCLIArguments(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		call  func([]string) ([]string, error)
		want  []string
	}{
		{
			name: "boot info", input: []string{"info", "--port", "COM18"},
			call: bootCLIArguments,
			want: []string{
				"--method", "urclock", "--operation", "metadata",
				"--port", "COM18",
			},
		},
		{
			name: "boot read", input: []string{"read", "backup.hex"},
			call: bootCLIArguments,
			want: []string{
				"--method", "urclock", "--operation", "read-flash",
				"--output", "backup.hex",
			},
		},
		{
			name: "boot backup", input: []string{"backup", "safe copies"},
			call: bootCLIArguments,
			want: []string{
				"--method", "urclock", "--operation", "backup",
				"--output", "safe copies",
			},
		},
		{
			name: "toolchain bootloader", input: []string{"install-bootloader", "--programmer", "usbasp"},
			call: toolchainCLIArguments,
			want: []string{
				"--method", "toolchain", "--operation", "install-bootloader",
				"--programmer", "usbasp",
			},
		},
	}
	for _, test := range tests {
		got, err := test.call(test.input)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got %v, want %v", test.name, got, test.want)
		}
	}
	if _, err := toolchainCLIArguments([]string{"upload", ".", "--port", "COM18"}); err == nil ||
		!strings.Contains(err.Error(), "usage") {
		t.Fatalf("unpublished direct upload command was exposed: %v", err)
	}
}

func TestNormalizeGuardedFlashCLIArguments(t *testing.T) {
	got, err := normalizeProgramCLIArgs([]string{
		"flash", "firmware image.hex", "COM18", "--allow-incomplete-backup",
		"--reinitialize-eeprom",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--operation", "write-flash", "--method", "urclock",
		"--hex", "firmware image.hex", "--allow-incomplete-backup",
		"--reinitialize-eeprom", "--port", "COM18",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized=%#v want=%#v", got, want)
	}
	usb, err := normalizeProgramCLIArgs([]string{
		"flash", "firmware.hex", "USB-SERIAL CH340", "--method", "usbasp",
	})
	if err != nil || !strings.Contains(strings.Join(usb, " "), "--method usbasp") ||
		!strings.Contains(strings.Join(usb, " "), "--app-device USB-SERIAL CH340") {
		t.Fatalf("USBasp normalized=%#v err=%v", usb, err)
	}
	prefixedUSBasp, err := normalizeProgramCLIArgs([]string{
		"--method", "usbasp", "flash", "firmware.hex", "USB-SERIAL CH340",
	})
	if err != nil || !reflect.DeepEqual(prefixedUSBasp, usb) {
		t.Fatalf("prefixed USBasp normalized=%#v want=%#v err=%v", prefixedUSBasp, usb, err)
	}
	before, err := normalizeProgramCLIArgs([]string{
		"--allow-incomplete-backup", "--app-reconnect=false", "flash",
		"firmware.hex", "--dry-run", "COM18",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBefore := []string{
		"--operation", "write-flash", "--method", "urclock", "--hex", "firmware.hex",
		"--allow-incomplete-backup", "--app-reconnect=false", "--dry-run", "--port", "COM18",
	}
	if !reflect.DeepEqual(before, wantBefore) {
		t.Fatalf("flags-before normalized=%#v want=%#v", before, wantBefore)
	}
	canonical := []string{"--hex", "flash", "--operation", "verify-flash"}
	gotCanonical, err := normalizeProgramCLIArgs(canonical)
	if err != nil || !reflect.DeepEqual(gotCanonical, canonical) {
		t.Fatalf("canonical flags were reinterpreted: got=%#v err=%v", gotCanonical, err)
	}
}

func TestProgramCLIRejectsEEPROMReinitializationWithoutCompleteBackup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runProgramWithConfig([]string{
		"flash", "candidate.hex", "COM18",
		"--reinitialize-eeprom", "--allow-incomplete-backup",
	}, &stdout, &stderr, appconfig.Defaults())
	if err == nil || !strings.Contains(err.Error(), "requires a complete verified raw flash") {
		t.Fatalf("unsafe development EEPROM reinitialization was accepted: %v", err)
	}
}

func TestProgramWithoutOperationShowsUsageWithoutOpeningHardware(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = runProgram(nil, &stdout, &stderr, store)
	if err == nil || !strings.Contains(err.Error(), "program flash HEX") {
		t.Fatalf("missing safe program usage: %v", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"usage-only program command produced output: stdout=%q stderr=%q",
			stdout.String(), stderr.String(),
		)
	}
}

func TestStandaloneUSBaspRequiresSeparateApplicationLifecycleSelector(t *testing.T) {
	t.Setenv("PCCONTROLLER_DEVICE", "")
	t.Setenv("PCCONTROLLER_PORT", "")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	base := []string{
		"--method", "usbasp", "--operation", "write-flash",
		"--hex", "firmware.hex", "--dry-run",
	}
	var stdout, stderr bytes.Buffer
	err = runProgram(base, &stdout, &stderr, store)
	if err == nil || !strings.Contains(err.Error(), "--app-device") {
		t.Fatalf("standalone USBasp did not fail closed without application selector: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	withApplication := append(append([]string(nil), base...), "--app-device", "COM18")
	if err := runProgram(withApplication, &stdout, &stderr, store); err != nil {
		t.Fatalf("USBasp application lifecycle dry-run: %v\nstderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "application lifecycle selector=COM18 (never passed to ISP)") {
		t.Fatalf("dry-run did not separate application selector from ISP:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	withOverride := append(append([]string(nil), base...), "--allow-incomplete-backup")
	if err := runProgram(withOverride, &stdout, &stderr, store); err != nil {
		t.Fatalf("explicit recovery override rejected: %v", err)
	}
	if !strings.Contains(stderr.String(), "application lifecycle skipped") {
		t.Fatalf("override warning missing: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestProgramShellWordsPreserveBackupAndEEPROMIntent(t *testing.T) {
	backup := programShellWords(programmer.Options{
		Method: programmer.MethodUrclock, Operation: programmer.OperationBackup,
		OutputPath: `C:\safe backups`,
	})
	wantBackup := []string{"program", "backup", "urclock", `C:\safe backups`}
	if !reflect.DeepEqual(backup, wantBackup) {
		t.Fatalf("backup words = %#v, want %#v", backup, wantBackup)
	}
	eeprom := programShellWords(programmer.Options{
		Method: programmer.MethodUSBasp, Operation: programmer.OperationWriteEEPROM,
		HexPath: "settings.hex", ConfirmEEPROMWrite: true,
	})
	wantEEPROM := []string{
		"program", "write-eeprom", "usbasp", "settings.hex", "CONFIRM",
	}
	if !reflect.DeepEqual(eeprom, wantEEPROM) {
		t.Fatalf("EEPROM words = %#v, want %#v", eeprom, wantEEPROM)
	}
}

func TestProgramShellWordsRouteFlashThroughGuard(t *testing.T) {
	got := programShellWords(programmer.Options{
		Method: programmer.MethodUrclock, Operation: programmer.OperationWriteFlash,
		HexPath: `C:\build output\firmware.hex`,
	})
	want := []string{"program", "flash", `C:\build output\firmware.hex`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guarded flash words=%#v want=%#v", got, want)
	}
	usbasp := programShellWords(programmer.Options{
		Method: programmer.MethodUSBasp, Operation: programmer.OperationWriteFlash,
		HexPath: `C:\build output\full.hex`,
	})
	wantUSBasp := []string{
		"program", "flash", `C:\build output\full.hex`, "--method", "usbasp",
	}
	if !reflect.DeepEqual(usbasp, wantUSBasp) {
		t.Fatalf("USBasp guarded flash words=%#v want=%#v", usbasp, wantUSBasp)
	}
}

func TestWSClientProgrammerMethodValidation(t *testing.T) {
	if method, err := validatedWSFlashMethod("usbasp"); err != nil || method != programmer.MethodUSBasp {
		t.Fatalf("canonical USBasp method rejected: method=%q err=%v", method, err)
	}
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runWS(
		[]string{"client", "--method", "avrdude"},
		&stdout,
		&stderr,
		store,
	); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("direct avrdude WS flashing was not rejected: %v", err)
	}
}

func TestSecondaryFirmwareDelegatesToPrimaryOperationAndFollowsProgress(t *testing.T) {
	firmware := filepath.Join(t.TempDir(), "candidate.hex")
	content := []byte(":020000000102FB\n:00000001FF\n")
	if err := os.WriteFile(firmware, content, 0o600); err != nil {
		t.Fatal(err)
	}
	document, err := programmer.LoadIntelHex(firmware)
	if err != nil {
		t.Fatal(err)
	}
	statusCalls := 0
	call := func(_ context.Context, method string, params, target any) error {
		switch method {
		case "controller.artifact.upload":
			request := params.(artifacts.UploadRequest)
			if request.Kind != artifacts.KindFirmware || request.SHA256 != document.SourceSHA256 ||
				!bytes.Equal(request.Data, content) {
				t.Fatalf("upload request=%+v", request)
			}
			*target.(*artifacts.OperationResult) = artifacts.OperationResult{
				Artifact: &artifacts.Descriptor{Kind: artifacts.KindFirmware, SHA256: document.SourceSHA256},
			}
		case "controller.snapshot":
			// Device identity is optional enrichment for the deterministic key.
		case "controller.update.firmware":
			request := params.(artifacts.UpdateRequest)
			if !request.Authorized || request.Method != "urclock" ||
				!request.AllowIncompleteBackup || !request.ReinitializeEEPROM ||
				request.IdempotencyKey == "" ||
				request.ArtifactSHA256 != document.SourceSHA256 {
				t.Fatalf("update request=%+v", request)
			}
			*target.(*artifacts.OperationResult) = artifacts.OperationResult{
				Operation: artifacts.UpdateStatus{ID: "op-primary", State: "queued"},
			}
		case "controller.update.status":
			statusCalls++
			status := artifacts.UpdateStatus{
				ID: "op-primary", State: "programming", ProgressPercent: 40,
				Detail: "guarded transaction", ProgrammingMethod: artifacts.ProgrammingMethodUrclock,
				BootloaderOutcome: artifacts.BootloaderNotAttempted,
			}
			if statusCalls > 1 {
				status.State, status.ProgressPercent, status.Detail = "completed", 100, "operation completed"
				status.ArtifactSHA256 = document.SourceSHA256
				status.BootloaderOutcome = artifacts.BootloaderSucceeded
			}
			*target.(*artifacts.UpdateStatus) = status
		default:
			t.Fatalf("unexpected primary method %q", method)
		}
		return nil
	}
	var output bytes.Buffer
	if err := delegatePrimaryFirmwareUpdate(
		context.Background(), firmware, "urclock", "", true, true, &output, call,
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"delegated firmware SHA-256", "primary operation op-primary",
		"programming 40%", "completed 100%", "bootloader=succeeded",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("progress output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestPrimaryFirmwareProgressReportsTypedISPFallback(t *testing.T) {
	call := func(_ context.Context, method string, _ any, target any) error {
		if method != "controller.update.status" {
			t.Fatalf("unexpected method %q", method)
		}
		*target.(*artifacts.UpdateStatus) = artifacts.UpdateStatus{
			ID: "failed-op", State: "failed", Detail: "bootloader did not answer",
			ErrorCode: "bootloader_timeout", ProgrammingMethod: artifacts.ProgrammingMethodUrclock,
			BootloaderOutcome: artifacts.BootloaderTimedOut, ISPFallbackSuggested: true,
		}
		return nil
	}
	err := monitorPrimaryFirmwareUpdate(
		context.Background(), "failed-op", io.Discard, call,
	)
	if err == nil || !strings.Contains(err.Error(), "bootloader=timed_out") ||
		!strings.Contains(err.Error(), "ISP fallback suggested") {
		t.Fatalf("typed failure=%v", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("expected unknown command error")
	}
}

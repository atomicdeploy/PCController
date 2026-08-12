package appconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

func TestLoadOrCreateAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Current().Connection.BaudRate != 115200 {
		t.Fatalf("unexpected defaults: %#v", store.Current())
	}
	value := store.Current()
	value.Connection.Port = "COM18"
	value.Connection.ResetOnReconnect = true
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	reloaded, changed, err := store.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || reloaded.Connection.Port != "COM18" ||
		!reloaded.Connection.ResetOnReconnect {
		t.Fatalf("reload got changed=%t config=%#v", changed, reloaded)
	}
}

func TestUIPeripheralNamesAreValidatedAndRemainFileBacked(t *testing.T) {
	value := Defaults()
	value.UI.SeparatePortButtons = true
	value.UI.PeripheralNames = map[string]string{
		"relay.5": "Workbench lamp",
		"pwm.0":   "Cabinet strip",
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded := store.Current().UI
	if !loaded.SeparatePortButtons || loaded.PeripheralNames["relay.5"] != "Workbench lamp" {
		t.Fatalf("UI naming/action-bar config did not round-trip: %#v", loaded)
	}

	value.UI.PeripheralNames["relay.6"] = strings.Repeat("x", 65)
	if err := value.Validate(); err == nil {
		t.Fatal("expected overlong peripheral name rejection")
	}
}

func TestPeripheralRegistryCoversEveryCoreRoleAndNamingCapacity(t *testing.T) {
	descriptors := PeripheralDescriptors()
	if len(descriptors) != 34 {
		t.Fatalf("core peripheral descriptor count=%d, want 34", len(descriptors))
	}
	seen := make(map[string]bool, len(descriptors))
	counts := make(map[string]int)
	for _, descriptor := range descriptors {
		if descriptor.Key == "" || descriptor.DefaultName == "" || descriptor.Control == "" {
			t.Fatalf("incomplete descriptor: %+v", descriptor)
		}
		if descriptor.DefaultDescription != "" {
			t.Fatalf("descriptor %q has a deployment-specific default description %q", descriptor.Key, descriptor.DefaultDescription)
		}
		if seen[descriptor.Key] {
			t.Fatalf("duplicate peripheral key %q", descriptor.Key)
		}
		seen[descriptor.Key] = true
		counts[descriptor.Kind]++
	}
	for kind, want := range map[string]int{
		"relay": 8, "motion": 2, "pwm": 16, "display": 2, "sensor": 6,
	} {
		if counts[kind] != want {
			t.Fatalf("%s descriptor count=%d, want %d", kind, counts[kind], want)
		}
	}
	if MaxPeripheralNames < len(descriptors) {
		t.Fatalf("peripheral naming capacity %d is below core catalog %d", MaxPeripheralNames, len(descriptors))
	}
	config := Defaults()
	config.UI.PeripheralNames = make(map[string]string, len(descriptors))
	for _, descriptor := range descriptors {
		config.UI.PeripheralNames[descriptor.Key] = "Named " + descriptor.DefaultName
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("complete 34-peripheral naming catalog was rejected: %v", err)
	}

	copyOfCatalog := PeripheralDescriptors()
	copyOfCatalog[0].DefaultName = "mutated"
	if name, _ := PeripheralDefaultName("relay.1"); name == "mutated" {
		t.Fatal("callers can mutate the canonical peripheral registry")
	}
	for key, want := range map[string]string{
		"relay.1": "Relay 1", "motion.a": "Motion A", "pwm.0": "PWM 0", "pwm.15": "PWM 15",
	} {
		if got, ok := PeripheralDefaultName(key); !ok || got != want {
			t.Fatalf("generic default name for %s=%q found=%t, want %q", key, got, ok, want)
		}
	}
}

func TestControlDescriptorsResolveOneOrderedCrossSurfaceContract(t *testing.T) {
	config := Defaults()
	config.UI.PeripheralNames = map[string]string{"relay.5": "Legacy bench lamp"}
	first, last := 0, MaxPresentedControls-1
	config.UI.PeripheralPresentation = map[string]PeripheralPresentation{
		"relay.5":  {Name: "Bench lamp", Description: "Overhead work light", Order: &last},
		"pwm.0":    {Name: "Left MOSFET", Description: "Left bank dimmer"},
		"motion.b": {Name: "Rear side", Description: "Rear lift", Order: &first},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	controls := ControlDescriptors(config.UI)
	if len(controls) != MaxPresentedControls {
		t.Fatalf("control count=%d, want %d", len(controls), MaxPresentedControls)
	}
	byKey := make(map[string]ControlDescriptor, len(controls))
	for _, control := range controls {
		if _, duplicate := byKey[control.Key]; duplicate {
			t.Fatalf("duplicate control key %q", control.Key)
		}
		byKey[control.Key] = control
	}
	for key, want := range map[string]struct {
		kind        string
		name        string
		description string
	}{
		"relay.5":  {"relay", "Bench lamp", "Overhead work light"},
		"pwm.0":    {"mosfet", "Left MOSFET", "Left bank dimmer"},
		"motion.b": {"side", "Rear side", "Rear lift"},
	} {
		got, ok := byKey[key]
		if !ok || got.Kind != want.kind || got.Name != want.name || got.Description != want.description {
			t.Fatalf("control %q=%+v, want kind=%q name=%q description=%q", key, got, want.kind, want.name, want.description)
		}
	}
	if controls[0].Key != "motion.b" || controls[len(controls)-1].Key != "relay.5" {
		t.Fatalf("custom order was not normalized: first=%s last=%s", controls[0].Key, controls[len(controls)-1].Key)
	}
	for order, control := range controls {
		if control.Order != order {
			t.Fatalf("control %s order=%d, want normalized %d", control.Key, control.Order, order)
		}
	}
	if control, found := byKey["pwm.11"]; !found || control.Control != "role-specific" {
		t.Fatalf("role-specific MOSFET/PWM descriptor is missing: %+v", control)
	}
}

func TestLegacyCopiedPeripheralDefaultsAreRemovedOnLoadAndWrite(t *testing.T) {
	order := 4
	value := Defaults()
	value.UI.PeripheralNames = map[string]string{
		"relay.5":      "User Relay 5",
		"relay.6":      "Relay 6",
		"relay.7":      "Bench extraction",
		"sensor.power": "Load power",
	}
	value.UI.PeripheralPresentation = map[string]PeripheralPresentation{
		"relay.5": {
			Name: "User Relay 5", Description: "Protected relay output R5 (user output)",
		},
		"relay.6": {Name: "Relay 6"},
		"pwm.11": {
			Name: "Enclosure light", Description: "12-bit PWM channel 11 (illumination)", Order: &order,
		},
		"pwm.0": {Name: "Cooling fan", Description: "Variable-speed cooling output"},
	}
	byKey := make(map[string]ControlDescriptor, MaxPresentedControls)
	for _, control := range ControlDescriptors(value.UI) {
		byKey[control.Key] = control
	}
	if relay := byKey["relay.5"]; relay.Name != "Relay 5" || relay.Description != "" {
		t.Fatalf("resolved contract exposed a copied legacy default: %#v", relay)
	}
	if roleSpecific := byKey["pwm.11"]; roleSpecific.Name != "PWM 11" || roleSpecific.Description != "" {
		t.Fatalf("resolved contract exposed a copied role-specific default: %#v", roleSpecific)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	if value.UI.PeripheralNames["relay.5"] != "User Relay 5" ||
		value.UI.PeripheralPresentation["pwm.11"].Name != "Enclosure light" {
		t.Fatal("Write mutated the caller's presentation maps")
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, copiedDefault := range []string{"User Relay 5", "Enclosure light", "Load power"} {
		if strings.Contains(string(written), copiedDefault) {
			t.Fatalf("copied registry default %q remained in persisted config:\n%s", copiedDefault, written)
		}
	}
	assertMigrated := func(label string, loaded Config) {
		t.Helper()
		if len(loaded.UI.PeripheralNames) != 1 || loaded.UI.PeripheralNames["relay.7"] != "Bench extraction" {
			t.Fatalf("%s names=%#v", label, loaded.UI.PeripheralNames)
		}
		if _, exists := loaded.UI.PeripheralPresentation["relay.5"]; exists {
			t.Fatalf("%s retained legacy relay presentation: %#v", label, loaded.UI.PeripheralPresentation)
		}
		if _, exists := loaded.UI.PeripheralPresentation["relay.6"]; exists {
			t.Fatalf("%s retained copied current default: %#v", label, loaded.UI.PeripheralPresentation)
		}
		roleSpecific, exists := loaded.UI.PeripheralPresentation["pwm.11"]
		if !exists || roleSpecific.Name != "" || roleSpecific.Description != "" ||
			roleSpecific.Order == nil || *roleSpecific.Order != order {
			t.Fatalf("%s did not preserve the independent order override: %#v", label, roleSpecific)
		}
		custom := loaded.UI.PeripheralPresentation["pwm.0"]
		if custom.Name != "Cooling fan" || custom.Description != "Variable-speed cooling output" {
			t.Fatalf("%s lost custom presentation: %#v", label, custom)
		}
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	assertMigrated("write", loaded)

	legacyPath := filepath.Join(t.TempDir(), "legacy.json")
	legacyDocument, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, legacyDocument, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err = Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	assertMigrated("load", loaded)
}

func TestInvalidReloadRetainsLastGoodValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Reload(); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if store.Current().Connection.BaudRate != 115200 {
		t.Fatal("invalid reload replaced last-known-good configuration")
	}
}

func TestWatcherAppliesValidChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan Config, 1)
	go store.Watch(ctx, 10*time.Millisecond, func(value Config) {
		changed <- value
	}, func(err error) {
		t.Errorf("watch error: %v", err)
	})
	value := store.Current()
	value.Connection.Port = "COM18"
	value.OSActions.Brightness.Enabled = true
	value.OSActions.Brightness.MinPercent = 15
	value.OSActions.Brightness.MaxPercent = 85
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-changed:
		if got.Connection.Port != "COM18" || !got.OSActions.Brightness.Enabled ||
			got.OSActions.Brightness.MinPercent != 15 || got.OSActions.Brightness.MaxPercent != 85 {
			t.Fatalf("watch got connection=%#v brightness=%#v", got.Connection, got.OSActions.Brightness)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not observe change")
	}
}

func TestFilesystemWatchRegistrationRetriesOnlyDisappearingEntries(t *testing.T) {
	transient := &os.PathError{
		Op:   "lstat",
		Path: filepath.Join(t.TempDir(), ".config-123.json"),
		Err:  os.ErrNotExist,
	}
	attempts := 0
	err := retryFilesystemWatchRegistration(context.Background(), 4, 0, func() error {
		attempts++
		if attempts < 3 {
			return transient
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("transient registration err=%v attempts=%d, want success on attempt 3", err, attempts)
	}

	permanent := errors.New("watch permission denied")
	attempts = 0
	err = retryFilesystemWatchRegistration(context.Background(), 4, 0, func() error {
		attempts++
		return permanent
	})
	if !errors.Is(err, permanent) || attempts != 1 {
		t.Fatalf("permanent registration err=%v attempts=%d, want immediate failure", err, attempts)
	}

	attempts = 0
	err = retryFilesystemWatchRegistration(context.Background(), 4, 0, func() error {
		attempts++
		return transient
	})
	if !errors.Is(err, os.ErrNotExist) || attempts != 4 {
		t.Fatalf("persistent disappearing entry err=%v attempts=%d, want bounded failure", err, attempts)
	}
}

func TestFilesystemWatchRegistrationStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := retryFilesystemWatchRegistration(ctx, 4, time.Hour, func() error {
		attempts++
		return os.ErrNotExist
	})
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Fatalf("canceled registration err=%v attempts=%d", err, attempts)
	}
}

func TestReloadErrorReportingSuppressesOnlyIdenticalConsecutiveFailures(t *testing.T) {
	var last string
	var reported []string
	report := func(err error) { reported = append(reported, err.Error()) }
	first := errors.New("invalid JSON")
	second := errors.New("invalid baud rate")

	reportDistinctReloadError(&last, first, report)
	reportDistinctReloadError(&last, first, report)
	reportDistinctReloadError(&last, second, report)
	reportDistinctReloadError(&last, nil, report)
	reportDistinctReloadError(&last, first, report)

	want := []string{"invalid JSON", "invalid baud rate", "invalid JSON"}
	if !reflect.DeepEqual(reported, want) {
		t.Fatalf("reported reload errors = %q, want %q", reported, want)
	}
}

func TestMacroValidation(t *testing.T) {
	value := Defaults()
	value.Macros = []Macro{{
		ID: 1, Name: "demo", Label: "dEMO",
		Steps: []MacroStep{{AtUS: 0, Kind: "relay", Target: 7, Value: 1}},
	}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Macros[0].Steps[0].Target = 8
	if err := value.Validate(); err == nil {
		t.Fatal("expected invalid relay target")
	}
}

func TestUIAppTitleCountsUnicodeCharacters(t *testing.T) {
	value := Defaults()
	value.UI.AppTitle = "مرکز کنترل رایانه"
	if err := value.Validate(); err != nil {
		t.Fatalf("Persian title should be valid: %v", err)
	}
	value.UI.AppTitle = strings.Repeat("ر", 65)
	if err := value.Validate(); err == nil {
		t.Fatal("expected a title longer than 64 Unicode characters to fail")
	}
}

func TestTUIConsoleValidation(t *testing.T) {
	value := Defaults()
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*TUIConsole){
		"columns":   func(console *TUIConsole) { console.Columns = 55 },
		"rows":      func(console *TUIConsole) { console.Rows = 121 },
		"font":      func(console *TUIConsole) { console.FontFace = strings.Repeat("x", 32) },
		"font size": func(console *TUIConsole) { console.FontSize = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := value
			mutate(&candidate.UI.TUIConsole)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid TUI console setting was accepted")
			}
		})
	}
}

func TestTUIConsoleFontFaceIsCanonicalizedOnDisk(t *testing.T) {
	value := Defaults()
	value.UI.TUIConsole.FontFace = "   Consolas   "
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.TUIConsole.FontFace != "Consolas" {
		t.Fatalf("font face=%q", loaded.UI.TUIConsole.FontFace)
	}
}

func TestAutomationValidationAndRecursiveRuleRejection(t *testing.T) {
	value := Defaults()
	code := uint32(0x123456)
	value.Automations = []Automation{{
		Name: "remote-up", Enabled: true,
		Match: AutomationMatch{
			Kind: "rf", Gesture: "down", RFCode: &code, RFProtocol: 1,
		},
		Actions: []AutomationAction{{
			Type: "board", Command: "relay side left up",
		}, {
			Type: "emit", Event: "remote-up",
		}},
	}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Automations[0].Match.Kind = "automation"
	if err := value.Validate(); err == nil {
		t.Fatal("expected recursive automation match rejection")
	}
}

func TestOSActionPolicyAndAutomationValidation(t *testing.T) {
	value := Defaults()
	if value.OSActions.VirtualKeys.Enabled || value.OSActions.Power.Enabled || value.OSActions.Brightness.Enabled {
		t.Fatal("OS actions are not disabled by default")
	}
	value.Automations = []Automation{{
		Name: "remote-f13", Enabled: true,
		Match:   AutomationMatch{Kind: "rf.receive"},
		Actions: []AutomationAction{{Type: "virtual-key", VirtualKey: "F13"}},
	}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Automations[0].Actions[0].VirtualKey = "0x10"
	if err := value.Validate(); err == nil {
		t.Fatal("reserved modifier virtual key was accepted")
	}
	value = Defaults()
	value.OSActions.Power.AllowAutomation = true
	value.Automations = []Automation{{
		Name: "lock-on-event", Match: AutomationMatch{Kind: "door"},
		Actions: []AutomationAction{{Type: "power", Power: "lock", Confirm: "CONFIRM"}},
	}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRememberDevicePersistsPCIdentityWithoutAliasing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := DeviceIdentity{
		Port: "COM18", VID: "1a86", PID: "7523",
		Name:       "USB-SERIAL CH340",
		InstanceID: `USB\VID_1A86&PID_7523\TEST`,
		LastSeen:   time.Now(),
	}
	changed, err := store.RememberDevice(identity)
	if err != nil || !changed {
		t.Fatalf("remember changed=%t err=%v", changed, err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Connection.LastDevice == nil ||
		reloaded.Connection.LastDevice.Port != "COM18" ||
		reloaded.Connection.LastDevice.VID != "1A86" {
		t.Fatalf("remembered identity=%#v", reloaded.Connection.LastDevice)
	}
	copyValue := store.Current()
	copyValue.Connection.LastDevice.Port = "COM99"
	if store.Current().Connection.LastDevice.Port != "COM18" {
		t.Fatal("last-device identity was shallow-copied")
	}
	changed, err = store.RememberDevice(identity)
	if err != nil || changed {
		t.Fatalf("same identity changed=%t err=%v", changed, err)
	}
}

func TestLoadMergesNewUIDefaultsWithoutOverridingExplicitFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := map[string]any{
		"schema": SchemaVersion,
		"connection": map[string]any{
			"baud_rate": 115200, "startup_wait_ms": 1200,
			"request_timeout_ms": 1200, "hello_attempts": 3,
		},
		"ui": map[string]any{
			"status_interval_ms": 250,
			"event_log_limit":    500,
			"show_power":         false,
		},
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	value, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if value.UI.AppTitle != productidentity.DefaultAppTitle() || value.UI.TableLayout != "compact" || value.UI.HistoryHours != 24 ||
		!value.UI.ShowCurrent || value.UI.ShowPower ||
		!value.UI.LCDServiceEnabled || value.UI.MirrorPromptToLCD {
		t.Fatalf("merged UI defaults=%#v", value.UI)
	}
	if value.Safety.MotionDoorPolicy != "always" ||
		value.IPC.WebSocketPath != "/ipc" {
		t.Fatalf("merged backend defaults=%#v %#v", value.Safety, value.IPC)
	}
}

func TestUpdateUIPersistsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ui := store.Current().UI
	ui.AppTitle = "Workshop Controller"
	ui.ShowBusVoltage = false
	updated, err := store.UpdateUI(ui)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UI.AppTitle != ui.AppTitle || updated.UI.ShowBusVoltage {
		t.Fatalf("updated UI=%#v", updated.UI)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UI.AppTitle != ui.AppTitle || reloaded.UI.ShowBusVoltage {
		t.Fatalf("persisted UI=%#v", reloaded.UI)
	}
}

func TestPresentationOverridesRemainRuntimeOnlyAndSurviveUIUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	value := Defaults()
	value.UI.AppTitle = "Configured Name"
	value.UI.Tagline = "Configured tagline"
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPresentationOverrides("Environment Name", "Flag tagline"); err != nil {
		t.Fatal(err)
	}
	effective := store.Current()
	if effective.UI.AppTitle != "Environment Name" || effective.UI.Tagline != "Flag tagline" {
		t.Fatalf("runtime presentation=%#v", effective.UI)
	}
	effective.UI.ShowGraphs = false
	if _, err := store.UpdateUI(effective.UI); err != nil {
		t.Fatal(err)
	}
	persisted, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.UI.AppTitle != "Configured Name" || persisted.UI.Tagline != "Configured tagline" || persisted.UI.ShowGraphs {
		t.Fatalf("persisted configuration absorbed runtime overrides: %#v", persisted.UI)
	}
	if current := store.Current(); current.UI.AppTitle != "Environment Name" || current.UI.Tagline != "Flag tagline" || current.UI.ShowGraphs {
		t.Fatalf("effective configuration lost override/update: %#v", current.UI)
	}
}

func TestDefaultsUseBuildPresentationVariables(t *testing.T) {
	oldTitle := productidentity.DefaultTitle
	oldTagline := productidentity.DefaultFirstRunTagline
	productidentity.DefaultTitle = "Build Controller"
	productidentity.DefaultFirstRunTagline = "Build-time first-run line"
	t.Cleanup(func() {
		productidentity.DefaultTitle = oldTitle
		productidentity.DefaultFirstRunTagline = oldTagline
	})

	defaults := Defaults()
	if defaults.UI.AppTitle != "Build Controller" || defaults.UI.Tagline != "Build-time first-run line" {
		t.Fatalf("build presentation defaults=%#v", defaults.UI)
	}
}

func TestSubscribePushesValidatedUpdatesWithoutPolling(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	updates := store.Subscribe(ctx)
	select {
	case initial := <-updates:
		if initial.UI.AppTitle == "" {
			t.Fatal("subscriber did not receive current configuration")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive initial configuration")
	}
	if _, err := store.Update(func(config *Config) error {
		config.UI.AppTitle = "Instant reload"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case updated := <-updates:
		if updated.UI.AppTitle != "Instant reload" {
			t.Fatalf("subscriber title=%q", updated.UI.AppTitle)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber waited for a polling interval")
	}
	cancel()
	select {
	case _, open := <-updates:
		if open {
			t.Fatal("subscriber channel remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not close after cancellation")
	}
}

func TestJSONYAMLAndTOMLRoundTrip(t *testing.T) {
	for _, extension := range []string{".json", ".yaml", ".yml", ".toml"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "controller"+extension)
			value := Defaults()
			value.UI.AppTitle = "Workshop Controller"
			value.UI.TUIConsole = TUIConsole{
				Enabled: true, Columns: 144, Rows: 44,
				FontFace: "Cascadia Mono", FontSize: 20,
			}
			value.Connection.Port = "COM18"
			value.Integrations.Hotkeys = []Hotkey{{
				Name: "stop", Enabled: true, Chord: "CTRL+ALT+S",
				Command: "relay all off",
			}}
			if err := Write(path, value); err != nil {
				t.Fatal(err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(first) < 2 || first[len(first)-1] != '\n' || first[len(first)-2] == '\n' || first[len(first)-2] == '\r' {
				t.Fatalf("configuration does not have exactly one final newline: %q", first[maxInt(0, len(first)-4):])
			}
			loaded, _, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.UI.AppTitle != value.UI.AppTitle ||
				loaded.UI.TUIConsole != value.UI.TUIConsole ||
				loaded.Connection.Port != "COM18" ||
				len(loaded.Integrations.Hotkeys) != 1 {
				t.Fatalf("round trip mismatch: %#v", loaded)
			}
			if err := Write(path, loaded); err != nil {
				t.Fatal(err)
			}
			second, err := os.ReadFile(path)
			if err != nil || string(first) != string(second) {
				t.Fatalf("serialization is not idempotent err=%v", err)
			}
		})
	}
}

func TestWritePersistsOnlyUserOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, Defaults()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var defaultsDocument map[string]any
	if err := json.Unmarshal(content, &defaultsDocument); err != nil {
		t.Fatal(err)
	}
	if len(defaultsDocument) != 1 || defaultsDocument["schema"] != float64(SchemaVersion) {
		t.Fatalf("default configuration was expanded on disk: %s", content)
	}

	value := Defaults()
	value.UI.AppTitle = "Workshop Controller"
	value.UI.ShowGraphs = false
	value.Integrations.Hotkeys = []Hotkey{}
	value.Melodies = []Melody{}
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	if _, expanded := document["connection"]; expanded {
		t.Fatalf("unchanged connection defaults leaked into sparse file: %s", content)
	}
	ui, ok := document["ui"].(map[string]any)
	if !ok || len(ui) != 2 || ui["app_title"] != value.UI.AppTitle || ui["show_graphs"] != false {
		t.Fatalf("explicit UI overrides were not preserved: %#v", document["ui"])
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.AppTitle != value.UI.AppTitle || loaded.UI.ShowGraphs ||
		len(loaded.Integrations.Hotkeys) != 0 || len(loaded.Melodies) != 0 {
		t.Fatalf("sparse configuration round trip lost explicit values: %#v", loaded)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func TestYAMLAndTOMLWatcherApplyChanges(t *testing.T) {
	for _, extension := range []string{".yaml", ".toml"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "controller"+extension)
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			changed := make(chan Config, 1)
			go store.Watch(ctx, 10*time.Millisecond, func(value Config) {
				changed <- value
			}, func(err error) {
				t.Errorf("watch error: %v", err)
			})
			value := store.Current()
			value.UI.AppTitle = "Live Reload " + extension
			value.OSActions.Brightness.Enabled = true
			value.OSActions.Brightness.MinPercent = 25
			value.OSActions.Brightness.MaxPercent = 75
			if err := Write(path, value); err != nil {
				t.Fatal(err)
			}
			select {
			case reloaded := <-changed:
				if reloaded.UI.AppTitle != value.UI.AppTitle ||
					!reloaded.OSActions.Brightness.Enabled ||
					reloaded.OSActions.Brightness.MinPercent != 25 ||
					reloaded.OSActions.Brightness.MaxPercent != 75 {
					t.Fatalf("watch title=%q brightness=%#v", reloaded.UI.AppTitle, reloaded.OSActions.Brightness)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("watcher did not observe non-JSON configuration change")
			}
		})
	}
}

func TestUnsupportedConfigExtensionIsRejected(t *testing.T) {
	value := Defaults()
	if err := Write(filepath.Join(t.TempDir(), "config.ini"), value); err == nil {
		t.Fatal("expected unsupported configuration extension error")
	}
}

func TestFutureConfigFieldsAreIgnoredButKnownTypesRemainStrict(t *testing.T) {
	tests := []struct {
		name       string
		extension  string
		compatible string
		badKnown   string
	}{
		{
			name: "JSON", extension: ".json",
			compatible: `{"schema":1,"future_root":{"enabled":true},"ipc":{"future_policy":{"mode":"observe"}},"programming":{"future_toolchain_cli":"next-cli"}}`,
			badKnown:   `{"schema":1,"connection":{"baud_rate":"fast"},"future_root":true}`,
		},
		{
			name: "YAML", extension: ".yaml",
			compatible: "schema: 1\nfuture_root:\n  enabled: true\nipc:\n  future_policy:\n    mode: observe\nprogramming:\n  future_toolchain_cli: next-cli\n",
			badKnown:   "schema: 1\nconnection:\n  baud_rate: fast\nfuture_root: true\n",
		},
		{
			name: "TOML", extension: ".toml",
			compatible: "schema = 1\n[future_root]\nenabled = true\n[ipc.future_policy]\nmode = 'observe'\n[programming]\nfuture_toolchain_cli = 'next-cli'\n",
			badKnown:   "schema = 1\nfuture_root = true\n[connection]\nbaud_rate = 'fast'\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config"+test.extension)
			if err := os.WriteFile(path, []byte(test.compatible), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := Load(path)
			if err != nil {
				t.Fatalf("future fields rejected: %v", err)
			}
			if loaded.Schema != SchemaVersion || loaded.Connection.BaudRate != Defaults().Connection.BaudRate {
				t.Fatalf("known/default fields changed: %#v", loaded.Connection)
			}
			if err := os.WriteFile(path, []byte(test.badKnown), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path); err == nil {
				t.Fatal("known field with wrong type was accepted")
			}
		})
	}
}

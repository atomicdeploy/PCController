package appconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestMacroValidation(t *testing.T) {
	value := Defaults()
	value.Macros = []Macro{{
		ID: 1, Name: "demo", Label: "dEMO",
		Steps: []MacroStep{{AtMS: 0, Kind: "relay", Target: 7, Value: 1}},
	}}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Macros[0].Steps[0].Target = 8
	if err := value.Validate(); err == nil {
		t.Fatal("expected invalid relay target")
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
	if value.UI.AppTitle != "PCController" || value.UI.HistoryHours != 24 ||
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

func TestUnsupportedConfigExtensionAndUnknownFieldsAreRejected(t *testing.T) {
	value := Defaults()
	if err := Write(filepath.Join(t.TempDir(), "config.ini"), value); err == nil {
		t.Fatal("expected unsupported configuration extension error")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("schema: 1\nunknown_field: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected strict unknown YAML field rejection")
	}
}

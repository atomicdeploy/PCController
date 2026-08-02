package control

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostfacts"
	"pccontroller.local/controller/internal/native"
)

type fixedHostFactsProvider struct {
	profile string
}

func (provider *fixedHostFactsProvider) Query(_ context.Context, profile string) (hostfacts.Result, error) {
	provider.profile = profile
	return hostfacts.Result{
		Profile: "system", Class: "Win32_OperatingSystem", Source: "wmi",
		Columns:     []string{"Caption", "BuildNumber"},
		Rows:        []map[string]any{{"Caption": "Windows", "BuildNumber": "26100"}},
		CollectedAt: time.Unix(1, 0).UTC(), DurationMS: 7,
	}, nil
}

func TestDecodeHexAndStatusFormatting(t *testing.T) {
	decoded, err := decodeHex("A5:01-00_ff")
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 4 || decoded[0] != 0xA5 || decoded[3] != 0xFF {
		t.Fatalf("decoded % X", decoded)
	}
	text := formatStatus(native.Status{
		SupplyMV: 5000, BusMV: 12345, CurrentMA: 120,
		TLEDCenti: 2512, TBTCenti: -550,
		PWMChannel: 4, PWMValue: 2048,
		ResetCause: 0x08, ResetCount: 17,
	})
	for _, expected := range []string{
		"supply=5.000V", "bus=12.345V", "tLED=25.12C",
		"channel=4", "value=2048", "reset_cause=0x08", "reset_count=17",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
}

func TestFormatHelloUsesCompactBuildIdentity(t *testing.T) {
	formatted := formatHello(native.Hello{
		BoardKind:      native.BoardKindPCController,
		Name:           "PCController",
		IdentitySchema: native.IdentitySchemaCompact,
		BuildHash:      0x2FD9F81C,
		BuildTimestamp: 0x35019D5D,
		BuildStamp:     "260801194258",
	})
	for _, expected := range []string{
		"build=2FD9F81C",
		"timestamp=260801194258",
		"packed=0x35019D5D",
	} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("missing %q in %q", expected, formatted)
		}
	}
	if strings.Contains(formatted, "legacy-firmware") {
		t.Fatalf("compact identity formatted as legacy: %q", formatted)
	}
}

func TestParseBool(t *testing.T) {
	for _, value := range []string{"on", "1", "true", "active"} {
		result, err := parseBool(value)
		if err != nil || !result {
			t.Fatalf("%q result=%v err=%v", value, result, err)
		}
	}
	if _, err := parseBool("maybe"); err == nil {
		t.Fatal("expected invalid state")
	}
}

func TestSettingsSetAcceptsOnlyCurrentCompleteForm(t *testing.T) {
	full, err := settingsFromSetArgs([]string{
		"set", "1", "2", "255", "0", "7", "0", "80", "2", "250",
		"9", "true", "3", "1", "0", "9", "37", "240",
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.DefaultPage != 9 || !full.SaveLastPage() ||
		full.DisplayBrightness != 7 || full.DisplayClosedBrightness != 0 ||
		full.StatusColor() != 3 || full.VoltageDecimals() != 1 ||
		full.CurrentDecimals() != 0 || full.MotionExitHoldSeconds != 9 ||
		full.MotionBreakMS() != 37 ||
		full.OutputPersistence != 2 || full.RelayRestoreMask != 0xF0 ||
		full.ExtendedFlags != 0x67 {
		t.Fatalf("full extended set = %#v", full)
	}
	if _, err := settingsFromSetArgs([]string{
		"set", "1", "2", "255", "0", "7", "0", "80", "2", "250",
		"9", "true", "3", "1", "3", "9", "37", "240",
	}); err == nil {
		t.Fatal("expected out-of-range current decimals error")
	}
	if _, err := settingsFromSetArgs([]string{
		"set", "1", "2", "255", "0", "7", "80", "2", "250",
	}); err == nil {
		t.Fatal("retired partial settings form was accepted")
	}
}

func TestFormatSettingsIncludesDecodedExtendedFields(t *testing.T) {
	settings := native.Settings{
		ExtendedFlags:         native.SettingsSaveLastPage,
		MotionExitHoldSeconds: 9,
		OutputPersistence:     native.OutputPersistUserPWM,
		RelayRestoreMask:      0xF0,
		MotionBreakMSValue:    37,
	}
	if err := settings.SetStatusColor(4); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetVoltageDecimals(0); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCurrentDecimals(1); err != nil {
		t.Fatal(err)
	}
	formatted := formatSettings(settings)
	for _, expected := range []string{
		"display_closed=0",
		"save_last=true",
		"status_color=4",
		"voltage_decimals=0",
		"current_decimals=1",
		"motion_exit_hold=9s",
		"motion_break=37ms",
		"programming_latch=false",
		"output_persistence=0x04",
		"relay_restore_mask=0xF0",
		"extended=0x99",
	} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("missing %q in %q", expected, formatted)
		}
	}
}

func TestLiveSettingsExportIsExplicitlyLiveAndComplete(t *testing.T) {
	settings := native.Settings{
		Flags: 1, LightMode: 2, OnBrightness: 180, DisplayBrightness: 5,
		MotionBreakMSValue: 37,
	}
	encoded, err := encodeLiveSettingsExport(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"format": "controller-mcu-settings/v1"`,
		`"source": "live-opcode"`,
		`"motion_break_ms": 37`,
	} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("live settings export missing %s: %s", expected, encoded)
		}
	}
}

func TestRFMapArgs(t *testing.T) {
	payload, description, err := rfMapArgs([]string{"7", "relay", "6", "toggle"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{7, native.RFActionRelay, 5, native.RFBehaviorToggle}
	if string(payload) != string(want) {
		t.Fatalf("payload = % X, want % X", payload, want)
	}
	if !strings.Contains(description, "relay:6/toggle") {
		t.Fatalf("description = %q", description)
	}

	payload, _, err = rfMapArgs([]string{"2", "side", "right", "down"})
	if err != nil {
		t.Fatal(err)
	}
	want = []byte{2, native.RFActionSide, 1, native.RFBehaviorDown}
	if string(payload) != string(want) {
		t.Fatalf("side payload = % X, want % X", payload, want)
	}
	if _, _, err := rfMapArgs(
		[]string{"3", "relay", "4", "toggle"},
	); err == nil || !strings.Contains(err.Error(), "R5..R8") {
		t.Fatalf("unsafe R1..R4 mapping error=%v", err)
	}
}

func TestBootProgramArguments(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{
			[]string{"info", "COM18"},
			[]string{"metadata", "urclock", "COM18"},
		},
		{
			[]string{"read", "backup.hex", "COM18"},
			[]string{"read-flash", "urclock", "backup.hex", "COM18"},
		},
		{
			[]string{"write", "firmware.hex"},
			[]string{"flash", "firmware.hex"},
		},
		{
			[]string{"verify", "firmware.hex"},
			[]string{"verify-flash", "urclock", "firmware.hex"},
		},
		{
			[]string{"probe"},
			[]string{"probe", "urclock"},
		},
		{
			[]string{"start"},
			[]string{"start", "urclock"},
		},
	}
	for _, test := range tests {
		got, err := bootProgramArguments(test.input)
		if err != nil {
			t.Fatalf("%v: %v", test.input, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%v => %v, want %v", test.input, got, test.want)
		}
	}
	for _, input := range [][]string{
		nil,
		{"unknown"},
		{"read"},
		{"probe", "COM18", "extra"},
	} {
		if _, err := bootProgramArguments(input); err == nil {
			t.Fatalf("%v: expected usage error", input)
		}
	}
}

func TestDevelopmentEEPROMReinitializationRequiresCompleteBackup(t *testing.T) {
	_, err := safeFlashCommand(
		context.Background(), nil, CommandOptions{},
		[]string{"candidate.hex", "--reinitialize-eeprom", "--allow-incomplete-backup"},
	)
	if err == nil || !strings.Contains(err.Error(), "requires a complete verified raw flash") {
		t.Fatalf("unsafe EEPROM reinitialization override was accepted: %v", err)
	}
}

func TestToolchainProgramArguments(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{
			[]string{"compile", "."},
			[]string{"compile", "."},
		},
		{
			[]string{"core-info"},
			[]string{"core-info", "toolchain"},
		},
		{
			[]string{"install-bootloader"},
			[]string{"install-bootloader", "toolchain"},
		},
	}
	for _, test := range tests {
		got, err := toolchainProgramArguments(test.input)
		if err != nil {
			t.Fatalf("%v: %v", test.input, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%v => %v, want %v", test.input, got, test.want)
		}
	}
	for _, input := range [][]string{{"upload"}, {"upload", ".", "COM18"}} {
		if _, err := toolchainProgramArguments(input); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("%v: expected unpublished-command error, got %v", input, err)
		}
	}
}

func TestConfiguredMelodyAndStatusEffectCommands(t *testing.T) {
	target := &recordingOutputTarget{}
	outputs := NewOutputScheduler(target)
	defer outputs.Close()
	config := appconfig.Defaults()
	config.Melodies = []appconfig.Melody{{
		Name: "custom",
		Notes: []appconfig.MelodyNote{{
			FrequencyHz: 440, DurationMS: 1,
		}},
	}}
	config.StatusEffects = []appconfig.StatusLEDEffect{{
		Name: "signal", Kind: "flash",
		Red: 1, Green: 2, Blue: 3, Brightness: 100,
		PeriodMS: 200, DurationMS: 210,
	}}
	provider := func() appconfig.Config { return config }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	list, err := melodyCommand(ctx, outputs, provider, []string{"list"})
	if err != nil || !strings.Contains(list, "custom notes=1") {
		t.Fatalf("melody list=%q err=%v", list, err)
	}
	result, err := melodyCommand(
		ctx,
		outputs,
		provider,
		[]string{"wait", "custom"},
	)
	if err != nil || !strings.Contains(result, "completed") {
		t.Fatalf("melody wait=%q err=%v", result, err)
	}

	list, err = rgbCommand(
		ctx,
		nil,
		outputs,
		provider,
		[]string{"effect", "list"},
	)
	if err != nil || !strings.Contains(list, "signal kind=flash") {
		t.Fatalf("effect list=%q err=%v", list, err)
	}
	result, err = rgbCommand(
		ctx,
		nil,
		outputs,
		provider,
		[]string{"effect", "play", "signal"},
	)
	if err != nil || !strings.Contains(result, "started") {
		t.Fatalf("effect play=%q err=%v", result, err)
	}
	if _, err := rgbCommand(
		ctx,
		nil,
		outputs,
		provider,
		[]string{"effect", "stop"},
	); err != nil {
		t.Fatal(err)
	}
}

func TestHostConfigCommandUpdatesApplicationTitle(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	engine := NewCommandEngine(runtime, CommandOptions{
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})
	output, err := engine.Execute(context.Background(), "config set ui.app_title Workshop Control Desk")
	if err != nil || config.UI.AppTitle != "Workshop Control Desk" || !strings.Contains(output, "hot-reload queued") {
		t.Fatalf("config set output=%q title=%q err=%v", output, config.UI.AppTitle, err)
	}
	output, err = engine.Execute(context.Background(), "config get ui.app_title")
	if err != nil || !strings.Contains(output, `"Workshop Control Desk"`) {
		t.Fatalf("config get output=%q err=%v", output, err)
	}
	if _, err := engine.Execute(context.Background(), "config set ui.unknown value"); err == nil {
		t.Fatal("unsupported config path was accepted")
	}
}

func TestHostConfigCommandUpdatesAppearanceWithoutNoOpWrites(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	writes := 0
	engine := NewCommandEngine(runtime, CommandOptions{
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			config = candidate
			writes++
			return nil
		},
	})

	for _, command := range []string{
		"config set ui.appearance.theme dark",
		"config set ui.appearance.locale fa",
		"config set ui.appearance.direction rtl",
		"config set ui.appearance.reduce_motion on",
		"config set ui.appearance.compact_numbers true",
		"config set ui.appearance.audio_muted yes",
		"config set ui.appearance.audio_volume 37%",
	} {
		if output, err := engine.Execute(context.Background(), command); err != nil || !strings.Contains(output, "hot-reload queued") {
			t.Fatalf("%q output=%q err=%v", command, output, err)
		}
	}
	appearance := config.UI.Appearance
	if appearance.Theme != "dark" || appearance.Locale != "fa" || appearance.Direction != "rtl" ||
		!appearance.ReduceMotion || !appearance.CompactNumbers || !appearance.AudioMuted || appearance.AudioVolume != 0.37 {
		t.Fatalf("appearance=%#v", appearance)
	}
	if output, err := engine.Execute(context.Background(), "config set ui.appearance.theme dark"); err != nil || output != "ui.appearance.theme unchanged" {
		t.Fatalf("no-op output=%q err=%v", output, err)
	}
	if writes != 7 {
		t.Fatalf("writes=%d, want 7", writes)
	}
	for _, command := range []string{
		"config set ui.appearance.theme ultraviolet",
		"config set ui.appearance.audio_volume 101",
		"config set ui.appearance.reduce_motion perhaps",
	} {
		if _, err := engine.Execute(context.Background(), command); err == nil {
			t.Fatalf("invalid command %q was accepted", command)
		}
	}
	if writes != 7 {
		t.Fatalf("invalid writes changed count to %d", writes)
	}
}

func TestHostConfigSetRequiresReadableConfiguration(t *testing.T) {
	engine := NewCommandEngine(New(Options{}), CommandOptions{
		UpdateHostConfig: func(func(*appconfig.Config) error) error { return nil },
	})
	if _, err := engine.Execute(context.Background(), "config set ui.appearance.theme dark"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing HostConfig err=%v", err)
	}
}

func TestOSCommandsExposeStatusPolicyAndDenyExecutionByDefault(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	engine := NewCommandEngine(runtime, CommandOptions{
		HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	})

	status, err := engine.Execute(context.Background(), "os status")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"host=", "os=", "Windows SetupAPI", "DIGCF_PRESENT"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("OS status missing %q: %s", expected, status)
		}
	}
	policy, err := engine.Execute(context.Background(), "os policy")
	if err != nil || !strings.Contains(policy, "virtual_keys enabled=false") ||
		!strings.Contains(policy, "power enabled=false") ||
		!strings.Contains(policy, "brightness enabled=false range=0..100") {
		t.Fatalf("policy=%q err=%v", policy, err)
	}

	before := runtime.LatestEventID()
	if _, err := engine.Execute(context.Background(), "os key F13"); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled virtual-key command err=%v", err)
	}
	event, err := runtime.WaitEvent(context.Background(), before, "os.virtual-key.audit")
	if err != nil || !strings.Contains(event.Text, "denied") {
		t.Fatalf("virtual-key audit=%#v err=%v", event, err)
	}

	before = runtime.LatestEventID()
	if _, err := engine.Execute(context.Background(), "os lock CONFIRM"); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled lock command err=%v", err)
	}
	event, err = runtime.WaitEvent(context.Background(), before, "os.power.audit")
	if err != nil || !strings.Contains(event.Text, "denied") {
		t.Fatalf("power audit=%#v err=%v", event, err)
	}

	before = runtime.LatestEventID()
	if _, err := engine.Execute(context.Background(), "os brightness set 55"); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled brightness command err=%v", err)
	}
	event, err = runtime.WaitEvent(context.Background(), before, "os.brightness")
	if err != nil || !strings.Contains(event.Text, "denied") {
		t.Fatalf("brightness audit=%#v err=%v", event, err)
	}

	updated, err := engine.Execute(context.Background(), "os virtual allow F14")
	if err != nil || !strings.Contains(updated, "F14") {
		t.Fatalf("virtual allow=%q err=%v", updated, err)
	}
	updated, err = engine.Execute(context.Background(), "os power-policy deny lock")
	if err != nil || strings.Contains(updated, "allowed=lock") {
		t.Fatalf("power deny=%q err=%v", updated, err)
	}
	updated, err = engine.Execute(context.Background(), "os power-policy allow lock")
	if err != nil || !strings.Contains(updated, "lock") {
		t.Fatalf("power allow=%q err=%v", updated, err)
	}
	updated, err = engine.Execute(context.Background(), "os brightness-policy range 20 80")
	if err != nil || !strings.Contains(updated, "range=20..80") {
		t.Fatalf("brightness range=%q err=%v", updated, err)
	}
	updated, err = engine.Execute(context.Background(), "os brightness-policy enable")
	if err != nil || !strings.Contains(updated, "brightness enabled=true") {
		t.Fatalf("brightness enable=%q err=%v", updated, err)
	}
}

func TestOSFactsUsesBoundedProviderAndCatalog(t *testing.T) {
	runtime := New(Options{})
	provider := &fixedHostFactsProvider{}
	engine := NewCommandEngine(runtime, CommandOptions{HostFacts: provider})
	output, err := engine.Execute(context.Background(), "os facts system")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"profile=system", "class=Win32_OperatingSystem", "source=wmi",
		`Caption="Windows"`, `BuildNumber="26100"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("host facts missing %q: %s", expected, output)
		}
	}
	if provider.profile != "system" {
		t.Fatalf("profile=%q", provider.profile)
	}
	catalog, err := engine.Execute(context.Background(), "os facts list")
	if err != nil || !strings.Contains(catalog, `"profile": "serial"`) || strings.Contains(catalog, "SELECT ") {
		t.Fatalf("catalog=%q err=%v", catalog, err)
	}
}

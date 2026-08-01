package control

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

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

func TestSettingsSetFormsPreserveAndReplaceExtendedFields(t *testing.T) {
	current := native.Settings{
		DefaultPage:   4,
		ExtendedFlags: 0xDB,
	}
	compact, err := settingsFromSetArgs(current, []string{
		"set", "1", "2", "255", "0", "7", "80", "2", "250",
	})
	if err != nil {
		t.Fatal(err)
	}
	if compact.DefaultPage != current.DefaultPage ||
		compact.ExtendedFlags != current.ExtendedFlags {
		t.Fatalf(
			"compact set erased extended fields: got %#v want page=%d flags=0x%02X",
			compact,
			current.DefaultPage,
			current.ExtendedFlags,
		)
	}

	previousExtended, err := settingsFromSetArgs(current, []string{
		"set", "1", "2", "255", "0", "7", "80", "2", "250",
		"9", "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if previousExtended.DefaultPage != 9 ||
		previousExtended.ExtendedFlags != current.ExtendedFlags&^1 {
		t.Fatalf(
			"11-argument set did not preserve bits 1..7: %#v",
			previousExtended,
		)
	}

	full, err := settingsFromSetArgs(current, []string{
		"set", "1", "2", "255", "0", "7", "80", "2", "250",
		"9", "true", "3", "1", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.DefaultPage != 9 || !full.SaveLastPage() ||
		full.StatusColor() != 3 || full.VoltageDecimals() != 1 ||
		full.CurrentDecimals() != 0 || full.ExtendedFlags != 0x67 {
		t.Fatalf("full extended set = %#v", full)
	}
	if _, err := settingsFromSetArgs(current, []string{
		"set", "1", "2", "255", "0", "7", "80", "2", "250",
		"9", "true", "3", "1", "3",
	}); err == nil {
		t.Fatal("expected out-of-range current decimals error")
	}
}

func TestFormatSettingsIncludesDecodedExtendedFields(t *testing.T) {
	settings := native.Settings{ExtendedFlags: native.SettingsSaveLastPage}
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
		"save_last=true",
		"status_color=4",
		"voltage_decimals=0",
		"current_decimals=1",
		"extended=0x99",
	} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("missing %q in %q", expected, formatted)
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

func TestArduinoProgramArguments(t *testing.T) {
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
			[]string{"core-info", "arduino"},
		},
		{
			[]string{"burn-bootloader"},
			[]string{"burn-bootloader", "arduino"},
		},
	}
	for _, test := range tests {
		got, err := arduinoProgramArguments(test.input)
		if err != nil {
			t.Fatalf("%v: %v", test.input, err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%v => %v, want %v", test.input, got, test.want)
		}
	}
	for _, input := range [][]string{{"upload"}, {"upload", ".", "COM18"}} {
		if _, err := arduinoProgramArguments(input); err == nil || !strings.Contains(err.Error(), "program flash") {
			t.Fatalf("%v: expected guarded-flash error, got %v", input, err)
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

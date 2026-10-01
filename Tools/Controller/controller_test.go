package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

func TestSharedFacadeSnapshotUsesRuntimeEffectCatalog(t *testing.T) {
	runtime := control.New(control.Options{})
	config := appconfig.Defaults()
	engine := control.NewCommandEngine(runtime, control.CommandOptions{
		HostConfig: func() appconfig.Config { return config },
	})
	client := AttachSharedRuntime(runtime, engine)
	defer client.Shutdown()

	snapshot := client.Snapshot()
	if len(snapshot.Effects) != len(config.StripEffects) {
		t.Fatalf("snapshot effects=%d, want runtime catalog=%d", len(snapshot.Effects), len(config.StripEffects))
	}
	if len(snapshot.Effects) == 0 || snapshot.Effects[0].Reference != "strip:police" {
		t.Fatalf("snapshot effects did not preserve runtime catalog: %#v", snapshot.Effects)
	}
}

func TestShutdownRetainsCompletionUntilRuntimeCloseSucceeds(t *testing.T) {
	client := New(Options{})
	closeErr := errors.New("cancel serial I/O")
	closeCalls := 0
	client.runtimeClose = func() error {
		closeCalls++
		if closeCalls == 1 {
			return closeErr
		}
		return client.runtime.Close()
	}

	if err := client.Shutdown(); !errors.Is(err, closeErr) {
		t.Fatalf("first Shutdown error = %v, want %v", err, closeErr)
	}
	select {
	case <-client.done:
		t.Fatal("failed Shutdown signaled terminal completion")
	default:
	}
	if err := client.Shutdown(); err != nil {
		t.Fatalf("retry Shutdown: %v", err)
	}
	select {
	case <-client.done:
	default:
		t.Fatal("successful retry did not signal terminal completion")
	}
	if closeCalls != 2 {
		t.Fatalf("runtime close calls = %d, want 2", closeCalls)
	}
}

func TestPublicOptionsExposeCanonicalFirmwareFeatureStatus(t *testing.T) {
	client := New(Options{FirmwareFeatures: []string{
		"EEPROM-MENU-LABELS",
		"eeprom-boot-opcodes",
		"eeprom-menu-labels",
	}})
	defer client.Shutdown()
	status, err := client.Execute(context.Background(), "toolchain features")
	if err != nil || status != "firmware features: eeprom-boot-opcodes, eeprom-menu-labels" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	client.ApplyHostOptions(Options{FirmwareFeatures: []string{"unknown"}})
	if _, err := client.Execute(context.Background(), "toolchain features"); err == nil ||
		!strings.Contains(err.Error(), "unsupported firmware feature") {
		t.Fatalf("invalid public feature error=%v", err)
	}
}

func TestToolchainConfigurationSurvivesPublicFacadeUpdates(t *testing.T) {
	client := New(Options{ToolchainCLI: "cli", ToolchainConfig: "managed.yaml", Avrdude: "avr", AvrdudeConf: "avr.conf"})
	defer client.Shutdown()
	options := client.currentCommandOptions()
	if options.ArduinoConfig != "managed.yaml" || options.ArduinoCLI != "cli" || options.Avrdude != "avr" || options.AvrdudeConf != "avr.conf" {
		t.Fatalf("initial programming paths lost: %#v", options)
	}
	client.ApplyHostOptions(Options{ToolchainCLI: "updated-cli", ToolchainConfig: "updated.yaml", Avrdude: "updated-avr", AvrdudeConf: "updated.conf"})
	options = client.currentCommandOptions()
	if options.ArduinoConfig != "updated.yaml" || options.ArduinoCLI != "updated-cli" || options.Avrdude != "updated-avr" || options.AvrdudeConf != "updated.conf" {
		t.Fatalf("updated programming paths lost: %#v", options)
	}
}

func TestPublicRFValidationWithoutDevice(t *testing.T) {
	client := New(Options{})
	defer client.Shutdown()
	if err := client.BeginRFLearn(context.Background(), 0); err == nil {
		t.Fatal("expected zero learn timeout to fail validation")
	}
	err := client.MapLearnedRF(context.Background(), 1, RFMapping{
		Action: RFActionRelay, Value: 8, Behavior: RFBehaviorToggle,
	})
	if err == nil {
		t.Fatal("expected invalid relay mapping to fail validation")
	}
	err = client.MapLearnedRF(context.Background(), 1, RFMapping{
		Action: RFActionRelay, Value: 0, Behavior: RFBehaviorToggle,
	})
	if err == nil {
		t.Fatal("expected unsafe R1 mapping to fail host validation")
	}
	if err := client.BeginRFLearn(context.Background(), 121*time.Second); err == nil {
		t.Fatal("expected oversized learn timeout to fail validation")
	}
}

func TestPublicPeripheralValidationWithoutDevice(t *testing.T) {
	client := New(Options{})
	defer client.Shutdown()
	ctx := context.Background()
	if err := client.SetRelay(ctx, 0, true); err == nil {
		t.Fatal("expected R0 to fail validation")
	}
	if err := client.SetRelay(ctx, 9, true); err == nil {
		t.Fatal("expected R9 to fail validation")
	}
	if err := client.SetPWMChannel(ctx, 16, 1); err == nil {
		t.Fatal("expected PWM channel 16 to fail validation")
	}
	if err := client.SetPWMChannel(ctx, 0, 4096); err == nil {
		t.Fatal("expected PWM value 4096 to fail validation")
	}
	if err := client.PlayTone(ctx, 440, 0); err == nil {
		t.Fatal("expected zero-duration tone to fail validation")
	}
	if err := client.TransmitRF(ctx, 0, 24, 1, 350, 1); err == nil {
		t.Fatal("expected zero RF code to fail validation")
	}
	if _, err := client.StartMelody(ctx, Melody{
		Name: "bad",
		Notes: []MelodyNote{{
			FrequencyHz: 1,
			DurationMS:  1,
		}},
	}, 1); err == nil {
		t.Fatal("expected invalid melody to fail validation")
	}
	if _, err := client.StartStatusLEDEffect(ctx, StatusLEDEffect{
		Name: "bad", Kind: "breathe", PeriodMS: 100,
	}); err == nil {
		t.Fatal("expected invalid status LED effect to fail validation")
	}
}

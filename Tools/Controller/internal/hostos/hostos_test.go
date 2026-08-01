package hostos

import (
	"context"
	"errors"
	"testing"
)

type fakeBrightnessBackend struct {
	status    BrightnessStatus
	readErr   error
	setErr    error
	setValues []int
}

func (backend *fakeBrightnessBackend) Brightness(context.Context) (BrightnessStatus, error) {
	return backend.status, backend.readErr
}

func (backend *fakeBrightnessBackend) SetBrightness(_ context.Context, percent int) (BrightnessStatus, error) {
	backend.setValues = append(backend.setValues, percent)
	if backend.setErr != nil {
		return BrightnessStatus{}, backend.setErr
	}
	result := backend.status
	result.Percent = percent
	result.RawCurrent = uint32(percent)
	return result, nil
}

func TestVirtualKeyResolutionAndPolicyDefaults(t *testing.T) {
	for input, want := range map[string]uint16{
		"up": 0x26, "RIGHT": 0x27, "F13": 0x7C,
		"0x25": 0x25, "37": 0x25, "VK_0x7C": 0x7C,
	} {
		resolved, err := ResolveVirtualKey(input)
		if err != nil || resolved.Code != want {
			t.Fatalf("ResolveVirtualKey(%q)=%#v err=%v want=0x%02X", input, resolved, err, want)
		}
	}
	for _, denied := range []string{"SHIFT", "0x10", "VK_0x5B", "no-such-key"} {
		if _, err := ResolveVirtualKey(denied); err == nil {
			t.Fatalf("reserved/unknown key %q accepted", denied)
		}
	}
	policy := DefaultPolicy()
	if policy.VirtualKeys.Enabled || policy.Power.Enabled || policy.Brightness.Enabled {
		t.Fatal("OS actions must be disabled by default")
	}
	if err := ValidatePolicy(policy); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Executor{}).PressVirtualKey(
		context.Background(), policy.VirtualKeys, VirtualKeyRequest{Key: "F13"},
	); err == nil {
		t.Fatal("disabled virtual-key policy executed")
	}
}

func TestBrightnessUsesInjectableBackendAndPolicyGate(t *testing.T) {
	backend := &fakeBrightnessBackend{status: BrightnessStatus{
		Percent: 42, RawMinimum: 0, RawCurrent: 42, RawMaximum: 100,
		Display: "Test DDC monitor",
	}}
	executor := NewExecutor(backend)
	read, err := executor.MonitorBrightness(context.Background())
	if err != nil || read.Changed || read.Status.Percent != 42 || read.Status.Display != "Test DDC monitor" {
		t.Fatalf("brightness read=%+v err=%v", read, err)
	}
	policy := DefaultPolicy().Brightness
	if _, err := executor.SetMonitorBrightness(context.Background(), policy, 55); err == nil {
		t.Fatal("disabled brightness policy wrote to backend")
	}
	if len(backend.setValues) != 0 {
		t.Fatalf("disabled write reached backend: %v", backend.setValues)
	}
	policy.Enabled, policy.MinPercent, policy.MaxPercent = true, 20, 80
	if _, err := executor.SetMonitorBrightness(context.Background(), policy, 10); err == nil {
		t.Fatal("out-of-policy brightness reached backend")
	}
	changed, err := executor.SetMonitorBrightness(context.Background(), policy, 55)
	if err != nil || !changed.Changed || changed.Status.Percent != 55 ||
		len(backend.setValues) != 1 || backend.setValues[0] != 55 {
		t.Fatalf("brightness change=%+v values=%v err=%v", changed, backend.setValues, err)
	}
	backend.readErr = errors.New("DDC unsupported")
	if _, err := executor.MonitorBrightness(context.Background()); !errors.Is(err, backend.readErr) {
		t.Fatalf("unsupported error=%v", err)
	}
}

func TestBrightnessPolicyRangeValidation(t *testing.T) {
	for _, policy := range []BrightnessPolicy{
		{MinPercent: -1, MaxPercent: 100},
		{MinPercent: 0, MaxPercent: 101},
		{MinPercent: 80, MaxPercent: 20},
	} {
		value := DefaultPolicy()
		value.Brightness = policy
		if err := ValidatePolicy(value); err == nil {
			t.Fatalf("invalid brightness policy accepted: %+v", policy)
		}
	}
}

func TestPowerPolicyIsAllowlistedConfirmedAndDisabledByDefault(t *testing.T) {
	policy := DefaultPolicy().Power
	for input, want := range map[string]string{
		"suspend": "sleep", "power-off": "shutdown", "reboot": "restart",
		"lock-workstation": "lock",
	} {
		actual, err := NormalizePowerAction(input)
		if err != nil || actual != want {
			t.Fatalf("NormalizePowerAction(%q)=%q err=%v", input, actual, err)
		}
	}
	if _, err := DefaultExecutor.Power(
		context.Background(), policy,
		PowerRequest{Action: "sleep", Confirmation: "CONFIRM"},
	); err == nil {
		t.Fatal("disabled power policy executed")
	}
}

func TestReleaseAllWithoutHeldKeysIsSafe(t *testing.T) {
	// This verifies shutdown cleanup without calling SendInput: a fresh executor
	// has no held keys, so release-all must be an idempotent no-op.
	executor := &Executor{}
	if err := executor.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if err := executor.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
}

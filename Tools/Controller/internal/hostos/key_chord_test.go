package hostos

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestKeyStrokeExactConsentAndCanonicalization(t *testing.T) {
	key, err := ResolveKeyStroke(" shift + control + s ")
	if err != nil || key.Name != "CTRL+SHIFT+S" || !reflect.DeepEqual(key.Codes, []uint16{0x11, 0x10, 'S'}) {
		t.Fatalf("%+v %v", key, err)
	}
	for _, invalid := range []string{"CTRL", "CTRL+", "CTRL+CTRL+S", "S+CTRL", "CTRL+SHIFT", "CTRL+NO_SUCH_KEY"} {
		if _, err := ResolveKeyStroke(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	policy := DefaultPolicy().VirtualKeys
	policy.Allowed = []string{"S"}
	if KeyStrokeAllowed(policy, key.Name) {
		t.Fatal("single key authorized a chord")
	}
	policy.Allowed = []string{"shift+ctrl+s"}
	if !KeyStrokeAllowed(policy, key.Name) || KeyStrokeAllowed(policy, "S") {
		t.Fatal("exact chord consent not respected")
	}
}

func TestKeyStrokeNativeOrderReleaseCancellationAndFailure(t *testing.T) {
	for _, mode := range []string{"complete", "cancel", "failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events []int
			executor := &Executor{
				keyDown: func(code uint16) error {
					events = append(events, int(code))
					if code == 'S' && mode == "failure" {
						return errors.New("native failure")
					}
					if code == 'S' && mode == "cancel" {
						cancel()
					}
					return nil
				},
				keyUp: func(code uint16) error { events = append(events, -int(code)); return nil },
			}
			policy := DefaultPolicy().VirtualKeys
			policy.Enabled = true
			policy.Allowed = []string{"CTRL+S"}
			_, err := executor.PressVirtualKey(ctx, policy, VirtualKeyRequest{Key: "CTRL+S", HoldMS: 10})
			if (mode == "complete") != (err == nil) {
				t.Fatalf("mode %s: %v", mode, err)
			}
			want := []int{0x11, 'S', -'S', -0x11}
			if mode == "failure" {
				want = []int{0x11, 'S', -0x11}
			}
			if !reflect.DeepEqual(events, want) || len(executor.pressed) != 0 {
				t.Fatalf("events=%v pressed=%v", events, executor.pressed)
			}
		})
	}
}

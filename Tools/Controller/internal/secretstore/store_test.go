package secretstore

import (
	"errors"
	"os"
	"testing"
)

type memoryBackend struct {
	values map[string]string
}

func (backend *memoryBackend) Status() Status {
	return Status{Provider: "memory", Available: true, Scope: "test"}
}
func (backend *memoryBackend) Get(name string) (string, error) {
	value, ok := backend.values[name]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}
func (backend *memoryBackend) Set(name, value string) error {
	backend.values[name] = value
	return nil
}
func (backend *memoryBackend) Delete(name string) error {
	if _, ok := backend.values[name]; !ok {
		return ErrNotFound
	}
	delete(backend.values, name)
	return nil
}

func TestResolverSeparatesDurableAndEnvironmentReferences(t *testing.T) {
	backend := &memoryBackend{values: make(map[string]string)}
	resolver := NewWithBackend(backend)
	if err := resolver.Set("os:ipc.remote", "0123456789abcdefghijklmn"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolver.Resolve("os:ipc.remote"); err != nil || got != "0123456789abcdefghijklmn" {
		t.Fatalf("durable resolve=%q err=%v", got, err)
	}
	t.Setenv("PC_CONTROLLER_TEST_SECRET", "transient-secret-value")
	if got, err := resolver.Resolve("env:PC_CONTROLLER_TEST_SECRET"); err != nil || got != "transient-secret-value" {
		t.Fatalf("environment resolve=%q err=%v", got, err)
	}
	if err := resolver.Set("env:PC_CONTROLLER_TEST_SECRET", "replacement"); err == nil {
		t.Fatal("environment reference was unexpectedly mutable")
	}
}

func TestResolverReportsMissingAndUnavailableWithoutFallback(t *testing.T) {
	resolver := NewWithBackend(&memoryBackend{values: make(map[string]string)})
	if _, err := resolver.Resolve("os:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing durable secret error=%v", err)
	}
	os.Unsetenv("PC_CONTROLLER_MISSING_SECRET")
	if _, err := resolver.Resolve("env:PC_CONTROLLER_MISSING_SECRET"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing environment secret error=%v", err)
	}
	unavailable := NewWithBackend(nil)
	if _, err := unavailable.Resolve("os:missing"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable error=%v", err)
	}
}

func TestReferenceAndValueValidation(t *testing.T) {
	for _, valid := range []string{"os:ipc.remote", "os:webhooks/main", "env:HOST_TOKEN_1"} {
		if err := ValidateReference(valid); err != nil {
			t.Errorf("valid reference %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "plain", "file:secret", "os:../secret", "env:A/B", "os:has space"} {
		if err := ValidateReference(invalid); err == nil {
			t.Errorf("invalid reference %q accepted", invalid)
		}
	}
	if err := ValidateValue("line\nbreak"); err == nil {
		t.Fatal("control character accepted")
	}
	if err := ValidateValue(string(make([]byte, MaxSecretBytes+1))); err == nil {
		t.Fatal("oversized value accepted")
	}
}

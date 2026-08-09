package programmer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompileLockSerializesContendersAndReleasesCleanly(t *testing.T) {
	identity := compileLockTestIdentity(t)
	first, err := acquireCompileExecutionLock(context.Background(), identity, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })

	canceled, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var diagnostics bytes.Buffer
	second, err := acquireCompileExecutionLock(canceled, identity, &diagnostics)
	if second != nil {
		_ = second.Release()
		t.Fatal("a concurrent compile acquired an already-held kernel lock")
	}
	if !errors.Is(err, canceled.Err()) {
		t.Fatalf("contention error=%v, want %v", err, canceled.Err())
	}
	message := diagnostics.String()
	if !strings.Contains(message, "Another firmware compile is active") ||
		!strings.Contains(message, "PID ") ||
		!strings.Contains(message, "source "+identityHash(identity)) {
		t.Fatalf("contention diagnostics are not actionable: %q", message)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	replacement, err := acquireCompileExecutionLock(context.Background(), identity, io.Discard)
	if err != nil {
		t.Fatalf("released compile lock was not reusable: %v", err)
	}
	if err := replacement.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCompileLockHonorsPreCanceledContextWithoutTouchingCache(t *testing.T) {
	identity := compileLockTestIdentity(t)
	cacheRoot := filepath.Dir(filepath.Dir(identity.SketchPath))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	lock, err := acquireCompileExecutionLock(canceled, identity, io.Discard)
	if lock != nil {
		_ = lock.Release()
		t.Fatal("pre-canceled compile acquired a lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled error=%v, want context cancellation", err)
	}
	if _, statErr := os.Stat(cacheRoot); !os.IsNotExist(statErr) {
		t.Fatalf("pre-canceled compile mutated cache root: %v", statErr)
	}
}

func TestCompileLockRecoversAbandonedDiagnosticRecord(t *testing.T) {
	identity := compileLockTestIdentity(t)
	lockPath := filepath.Join(filepath.Dir(filepath.Dir(identity.SketchPath)), ".compile.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	abandoned := `{"pid":4242,"hostname":"old-host","acquired_at":"2026-08-01T00:00:00Z","source_hash":"DEADBEEF"}` + "\n"
	if err := os.WriteFile(lockPath, []byte(abandoned), 0o600); err != nil {
		t.Fatal(err)
	}

	var diagnostics bytes.Buffer
	lock, err := acquireCompileExecutionLock(context.Background(), identity, &diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if !strings.Contains(diagnostics.String(), "Recovered abandoned firmware compile lock") ||
		!strings.Contains(diagnostics.String(), "PID 4242 on old-host") {
		t.Fatalf("abandoned-lock recovery was not explained: %q", diagnostics.String())
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCompileExecutionOwnsLockThroughCompilerRunner(t *testing.T) {
	root := firmwareCompileFixture(t)
	cache := filepath.Join(t.TempDir(), "compile-cache")
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", cache)
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	options := Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: "arduino-cli",
	}
	planned, identity, err := PlanCompile(options)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(filepath.Dir(identity.SketchPath)), ".compile.lock")
	runnerFailure := errors.New("stop after lock assertion")
	runner := CommandRunnerFunc(func(context.Context, Command, io.Writer) error {
		probe, openErr := os.OpenFile(lockPath, os.O_RDWR, 0o600)
		if openErr != nil {
			t.Fatalf("open compile lock probe: %v", openErr)
		}
		defer probe.Close()
		locked, lockErr := tryLockCompileFile(probe)
		if lockErr != nil {
			t.Fatalf("probe compile lock: %v", lockErr)
		}
		if locked {
			_ = unlockCompileFile(probe)
			t.Fatal("compile lock was not held while the compiler runner executed")
		}
		return runnerFailure
	})
	if err := ExecuteWithRunner(context.Background(), planned, io.Discard, runner); !errors.Is(err, runnerFailure) {
		t.Fatalf("ExecuteWithRunner error=%v, want runner sentinel", err)
	}

	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	locked, err := tryLockCompileFile(probe)
	if err != nil {
		t.Fatal(err)
	}
	if !locked {
		t.Fatal("compile execution returned without releasing its kernel lock")
	}
	if err := unlockCompileFile(probe); err != nil {
		t.Fatal(err)
	}
}

func TestCompilePlanningLeavesLockAndCacheUntouched(t *testing.T) {
	root := firmwareCompileFixture(t)
	cache := filepath.Join(t.TempDir(), "missing-cache")
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", cache)
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	options := Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: "arduino-cli",
	}
	planned, _, err := PlanCompile(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(planned); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("compile planning/dry-run mutated managed cache: %v", err)
	}
}

func compileLockTestIdentity(t *testing.T) CompileIdentity {
	t.Helper()
	root := firmwareCompileFixture(t)
	t.Setenv("PCCONTROLLER_ARDUINO_CACHE", filepath.Join(t.TempDir(), "compile-cache"))
	t.Setenv("PCCONTROLLER_BUILD_TIMESTAMP", "35019D5D")
	_, identity, err := PlanCompile(Options{
		Method: MethodCompile, SketchPath: root, ArduinoCLI: "arduino-cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func identityHash(identity CompileIdentity) string {
	const digits = "0123456789ABCDEF"
	encoded := make([]byte, 8)
	value := identity.SourceHash
	for index := len(encoded) - 1; index >= 0; index-- {
		encoded[index] = digits[value&0x0F]
		value >>= 4
	}
	return string(encoded)
}

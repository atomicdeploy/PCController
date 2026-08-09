//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/aptmirror"
)

func TestMirrorInstallIsConfigIndependentAndReadOnlyByDefault(t *testing.T) {
	originalReadFile := linuxAPTMirrorReadFile
	originalInstall := linuxAPTMirrorInstall
	originalExecutable := linuxHostProvisionExecutable
	originalArchitectures := linuxAPTMirrorArchitectures
	originalAdoptionLock := linuxAPTMirrorAdoptionLock
	t.Cleanup(func() {
		linuxAPTMirrorReadFile = originalReadFile
		linuxAPTMirrorInstall = originalInstall
		linuxHostProvisionExecutable = originalExecutable
		linuxAPTMirrorArchitectures = originalArchitectures
		linuxAPTMirrorAdoptionLock = originalAdoptionLock
	})
	linuxAPTMirrorAdoptionLock = func() (func(), error) {
		t.Fatal("dry-run acquired the mutating APT mirror adoption lock")
		return nil, nil
	}
	linuxAPTMirrorArchitectures = func() ([]string, error) { return []string{"amd64", "i386"}, nil }
	linuxAPTMirrorReadFile = func(path string) ([]byte, error) {
		if path != "/etc/os-release" {
			return nil, os.ErrNotExist
		}
		return []byte("ID=ubuntu\nVERSION_CODENAME=resolute\n"), nil
	}
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	linuxHostProvisionExecutable = func() (string, error) { return executable, nil }
	var captured aptmirror.InstallOptions
	linuxAPTMirrorInstall = func(_ context.Context, options aptmirror.InstallOptions) (aptmirror.InstallReport, error) {
		captured = options
		return aptmirror.InstallReport{ExecutableTarget: options.Config.Paths.StableExecutable}, nil
	}
	invalidConfig := filepath.Join(t.TempDir(), "invalid.json")
	invalid := []byte(`{"schema":1,"host_menus":{"request_gesture":"invalid"}}`)
	if err := os.WriteFile(invalidConfig, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--config", invalidConfig, "toolchain", "mirror-install", "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("mirror install loaded runtime config: %v\nstderr: %s", err, stderr.String())
	}
	if captured.Apply || captured.Config.Paths.StableExecutable != "/opt/pccontroller/bin/controller" ||
		!reflect.DeepEqual(captured.Config.Architectures, []string{"amd64", "i386"}) {
		t.Fatalf("dry-run options=%+v", captured)
	}
	after, _ := os.ReadFile(invalidConfig)
	if !bytes.Equal(after, invalid) {
		t.Fatal("mirror dry-run mutated runtime config")
	}
}

func TestMirrorAdoptionLockCoversCLIAndProvisionTransactions(t *testing.T) {
	originalReadFile := linuxAPTMirrorReadFile
	originalInstall := linuxAPTMirrorInstall
	originalExecutable := linuxHostProvisionExecutable
	originalArchitectures := linuxAPTMirrorArchitectures
	originalAdoptionLock := linuxAPTMirrorAdoptionLock
	originalCurrentEUID := linuxHostProvisionCurrentEUID
	originalLookPath := linuxHostProvisionLookPath
	originalRun := linuxHostProvisionRun
	t.Cleanup(func() {
		linuxAPTMirrorReadFile = originalReadFile
		linuxAPTMirrorInstall = originalInstall
		linuxHostProvisionExecutable = originalExecutable
		linuxAPTMirrorArchitectures = originalArchitectures
		linuxAPTMirrorAdoptionLock = originalAdoptionLock
		linuxHostProvisionCurrentEUID = originalCurrentEUID
		linuxHostProvisionLookPath = originalLookPath
		linuxHostProvisionRun = originalRun
	})

	linuxAPTMirrorReadFile = func(path string) ([]byte, error) {
		if path != "/etc/os-release" {
			return nil, os.ErrNotExist
		}
		return []byte("ID=ubuntu\nVERSION_CODENAME=resolute\n"), nil
	}
	linuxAPTMirrorArchitectures = func() ([]string, error) { return []string{"amd64"}, nil }
	linuxHostProvisionCurrentEUID = func() int { return 0 }
	executable := filepath.Join(t.TempDir(), "controller")
	if err := os.WriteFile(executable, []byte("controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	linuxHostProvisionExecutable = func() (string, error) { return executable, nil }
	linuxHostProvisionLookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/usr/bin/systemctl", nil
		}
		return "", errors.New("not found")
	}

	lockHeld := false
	acquisitions := 0
	releases := 0
	contentionChecks := 0
	var events []string
	linuxAPTMirrorAdoptionLock = func() (func(), error) {
		if lockHeld {
			contentionChecks++
			return nil, errors.New("APT mirror adoption is already running")
		}
		lockHeld = true
		acquisitions++
		events = append(events, "lock")
		return func() {
			if !lockHeld {
				t.Error("APT mirror adoption lock released twice")
				return
			}
			lockHeld = false
			releases++
			events = append(events, "unlock")
		}, nil
	}
	linuxHostProvisionRun = func(_ context.Context, command linuxHostProvisionCommand, _ []string, output io.Writer) error {
		if !lockHeld {
			t.Errorf("systemd command ran outside adoption lock: %+v", command)
		}
		events = append(events, strings.Join(command.Args, " "))
		if len(command.Args) > 0 && command.Args[0] == "show" {
			_, _ = io.WriteString(output, "not-found\ninactive\n")
			return nil
		}
		if len(command.Args) > 0 && strings.HasPrefix(command.Args[0], "is-") {
			return errors.New("inactive")
		}
		return nil
	}
	linuxAPTMirrorInstall = func(_ context.Context, options aptmirror.InstallOptions) (aptmirror.InstallReport, error) {
		if !options.Apply || !lockHeld {
			t.Errorf("mutating install ran without adoption lock: apply=%v held=%v", options.Apply, lockHeld)
		}
		events = append(events, "install")
		if release, err := linuxAPTMirrorAdoptionLock(); err == nil {
			release()
			t.Error("concurrent adoption unexpectedly acquired the lock")
		}
		return aptmirror.InstallReport{Applied: true}, nil
	}

	if err := runToolchainMirrorInstall([]string{"--apply"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("mirror-install apply: %v", err)
	}
	var provisionReport linuxHostProvisionReport
	if _, err := provisionLinuxUbuntuMirrors(context.Background(), linuxHostProvisionOptions{
		Apply: true,
	}, executable, io.Discard, &provisionReport); err != nil {
		t.Fatalf("provision-host mirror adoption: %v", err)
	}
	if lockHeld || acquisitions != 2 || releases != 2 || contentionChecks != 2 {
		t.Fatalf("lock lifecycle held=%v acquisitions=%d releases=%d contention=%d events=%q",
			lockHeld, acquisitions, releases, contentionChecks, events)
	}
	for index := 0; index < len(events); {
		if events[index] != "lock" {
			t.Fatalf("transaction did not begin with lock: %q", events[index:])
		}
		end := index
		for end < len(events) && events[end] != "unlock" {
			end++
		}
		if end == len(events) || !containsArgument(events[index:end], "install") ||
			!containsArgument(events[index:end], "enable --now pccontroller-apt-mirror-health.timer") {
			t.Fatalf("lock did not cover install through activation: %q", events[index:])
		}
		index = end + 1
	}
}

func TestLinuxDebianArchitecturesPreservesForeignArchitectures(t *testing.T) {
	originalLookPath := linuxHostProvisionLookPath
	originalRun := linuxHostProvisionRun
	t.Cleanup(func() {
		linuxHostProvisionLookPath = originalLookPath
		linuxHostProvisionRun = originalRun
	})
	linuxHostProvisionLookPath = func(name string) (string, error) {
		if name != "dpkg" {
			t.Fatalf("unexpected executable lookup %q", name)
		}
		return "/usr/bin/dpkg", nil
	}
	linuxHostProvisionRun = func(_ context.Context, command linuxHostProvisionCommand, environment []string, output io.Writer) error {
		if command.Name != "/usr/bin/dpkg" || len(command.Args) != 1 || !containsArgument(environment, "LC_ALL=C") {
			t.Fatalf("architecture command=%+v environment=%q", command, environment)
		}
		switch command.Args[0] {
		case "--print-architecture":
			_, _ = io.WriteString(output, "amd64\n")
		case "--print-foreign-architectures":
			_, _ = io.WriteString(output, "i386\namd64\n")
		default:
			t.Fatalf("unexpected dpkg argument %q", command.Args[0])
		}
		return nil
	}
	architectures, err := linuxDebianArchitectures()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(architectures, []string{"amd64", "i386"}) {
		t.Fatalf("architectures=%q", architectures)
	}
}

func TestLegacyMirrorTimerIsQuiescedAndRestoredExactly(t *testing.T) {
	originalLookPath := linuxHostProvisionLookPath
	originalRun := linuxHostProvisionRun
	t.Cleanup(func() {
		linuxHostProvisionLookPath = originalLookPath
		linuxHostProvisionRun = originalRun
	})
	linuxHostProvisionLookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/usr/bin/systemctl", nil
		}
		return "", errors.New("not found")
	}
	var commands [][]string
	linuxHostProvisionRun = func(_ context.Context, command linuxHostProvisionCommand, _ []string, output io.Writer) error {
		commands = append(commands, append([]string(nil), command.Args...))
		joined := strings.Join(command.Args, " ")
		if strings.HasPrefix(joined, "show ") {
			if strings.Contains(joined, "apt-mirror-health.service") && !strings.Contains(joined, "pccontroller-") {
				_, _ = io.WriteString(output, "loaded\nactivating\n")
			} else {
				_, _ = io.WriteString(output, "not-found\ninactive\n")
			}
			return nil
		}
		if strings.Contains(joined, "is-enabled") && strings.Contains(joined, "apt-mirror-health.timer") &&
			!strings.Contains(joined, "pccontroller-") {
			return nil
		}
		if strings.Contains(joined, "is-active") && strings.Contains(joined, "apt-mirror-health.timer") &&
			!strings.Contains(joined, "pccontroller-") {
			return nil
		}
		if strings.Contains(joined, "is-") {
			return errors.New("inactive")
		}
		return nil
	}
	state, err := inspectMirrorSystemd(context.Background(), nil)
	if err != nil || !state.LegacyEnabled || !state.LegacyActive || !state.LegacyServiceLoaded || !state.LegacyServiceWasRunning {
		t.Fatalf("legacy state=%+v err=%v", state, err)
	}
	if err := quiesceMirrorSystemd(context.Background(), nil, state); err != nil {
		t.Fatal(err)
	}
	if err := restoreMirrorTimerState(context.Background(), nil, state); err != nil {
		t.Fatal(err)
	}
	joined := make([]string, len(commands))
	for index := range commands {
		joined[index] = strings.Join(commands[index], " ")
	}
	text := strings.Join(joined, "\n")
	for _, wanted := range []string{
		"disable --now apt-mirror-health.timer",
		"stop apt-mirror-health.service",
		"enable apt-mirror-health.timer",
		"start apt-mirror-health.timer",
		"start --no-block apt-mirror-health.service",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("rollback omitted %q:\n%s", wanted, text)
		}
	}
}

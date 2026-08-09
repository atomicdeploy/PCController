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
	t.Cleanup(func() {
		linuxAPTMirrorReadFile = originalReadFile
		linuxAPTMirrorInstall = originalInstall
		linuxHostProvisionExecutable = originalExecutable
	})
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
	if captured.Apply || captured.Config.Paths.StableExecutable != "/opt/pccontroller/bin/controller" {
		t.Fatalf("dry-run options=%+v", captured)
	}
	after, _ := os.ReadFile(invalidConfig)
	if !bytes.Equal(after, invalid) {
		t.Fatal("mirror dry-run mutated runtime config")
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
	linuxHostProvisionRun = func(_ context.Context, command linuxHostProvisionCommand, _ []string, _ io.Writer) error {
		commands = append(commands, append([]string(nil), command.Args...))
		joined := strings.Join(command.Args, " ")
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
	if err != nil || !state.LegacyEnabled || !state.LegacyActive {
		t.Fatalf("legacy state=%+v err=%v", state, err)
	}
	if err := quiesceLegacyMirrorTimer(context.Background(), nil, state); err != nil {
		t.Fatal(err)
	}
	if got := commands[len(commands)-1]; !reflect.DeepEqual(got, []string{"disable", "--now", "apt-mirror-health.timer"}) {
		t.Fatalf("legacy quiesce=%q", got)
	}
	if err := restoreMirrorTimerState(context.Background(), nil, state); err != nil {
		t.Fatal(err)
	}
	joined := make([]string, len(commands))
	for index := range commands {
		joined[index] = strings.Join(commands[index], " ")
	}
	text := strings.Join(joined, "\n")
	for _, wanted := range []string{"enable apt-mirror-health.timer", "start apt-mirror-health.timer"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("rollback omitted %q:\n%s", wanted, text)
		}
	}
}

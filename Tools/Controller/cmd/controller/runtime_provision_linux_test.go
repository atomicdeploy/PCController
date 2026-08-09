//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestRuntimeCommandsAreConfigIndependent(t *testing.T) {
	for _, action := range []string{"runtime-stage", "runtime-install", "runtime-status", "runtime-rollback", "runtime-uninstall"} {
		if !configIndependentToolchainRuntime([]string{"toolchain", action}) {
			t.Fatalf("%s was not config-independent", action)
		}
	}
	if configIndependentToolchainRuntime([]string{"toolchain", "bootstrap"}) {
		t.Fatal("ordinary target-user toolchain bootstrap unexpectedly became config-independent")
	}
}

func TestRuntimeWebListenIsLoopbackOnly(t *testing.T) {
	for input, want := range map[string]string{
		"127.0.0.1:8787": "127.0.0.1:8787",
		"[::1]:8787":     "[::1]:8787",
		"localhost:8787": "localhost:8787",
	} {
		if got, err := loopbackWebListen(input); err != nil || got != want {
			t.Fatalf("loopbackWebListen(%q)=%q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"0.0.0.0:8787", "192.168.1.2:8787", "127.0.0.1:0", "127.0.0.1:http"} {
		if _, err := loopbackWebListen(input); err == nil {
			t.Fatalf("unsafe transient listen address accepted: %s", input)
		}
	}
}

func TestTransientRuntimeListenPreservesResolvedSecretReference(t *testing.T) {
	t.Setenv("PCC_RUNTIME_TRANSIENT_TOKEN", "abcdefghijklmnopqrstuvwxyz012345")
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.IPC.AuthToken = ""
		config.IPC.AuthTokenRef = "env:PCC_RUNTIME_TRANSIENT_TOKEN"
		config.IPC.Listen = "127.0.0.1:9999"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err := transientWebRuntimeConfig(store, "127.0.0.1:8787")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IPC.AuthToken != "abcdefghijklmnopqrstuvwxyz012345" {
		t.Fatalf("transient runtime did not resolve the configured token: token=%q", resolved.IPC.AuthToken)
	}
	persisted := store.Current()
	if persisted.IPC.AuthToken != "" || persisted.IPC.AuthTokenRef != "env:PCC_RUNTIME_TRANSIENT_TOKEN" {
		t.Fatalf("transient listen mutated the saved secret/reference: token=%q ref=%q", persisted.IPC.AuthToken, persisted.IPC.AuthTokenRef)
	}
	if resolved.IPC.Listen != "127.0.0.1:8787" {
		t.Fatalf("transient listen=%q", resolved.IPC.Listen)
	}
	readiness := runtimeWindowReadinessConfig(store)
	if readiness.IPC.Listen != "127.0.0.1:8787" || readiness.IPC.AuthToken != "abcdefghijklmnopqrstuvwxyz012345" {
		t.Fatalf("readiness endpoint/token=%q/%q", readiness.IPC.Listen, readiness.IPC.AuthToken)
	}
	if store.Current().IPC.Listen != "127.0.0.1:9999" {
		t.Fatalf("readiness override mutated saved listen: %q", store.Current().IPC.Listen)
	}
}

func TestRuntimeInstallDryRunValidatesCanonicalInputsWithoutOpeningConfig(t *testing.T) {
	account, err := user.Current()
	if err != nil || account.Uid == "0" {
		t.Skip("test requires a discoverable non-root Linux account")
	}
	packageDirectory := t.TempDir()
	controller := filepath.Join(packageDirectory, "controller")
	virtualBoard := filepath.Join(t.TempDir(), "virtual_board")
	writeRuntimeTestExecutable(t, controller, "#!/bin/sh\necho 'PCController 1.2.3 source-hash="+strings.Repeat("a", 64)+"'\n")
	writeRuntimeTestExecutable(t, virtualBoard, "#!/bin/sh\necho 'PCController Virtual Board --port PORT'\n")
	content, err := os.ReadFile(controller)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	manifest := map[string]any{
		"format": "pccontroller-host-package-manifest/v1",
		"target": map[string]any{"platform": runtime.GOOS, "architecture": runtime.GOARCH},
		"identity": map[string]any{
			"version": "1.2.3", "sourceSHA256": strings.Repeat("a", 64), "buildTime": "2026-08-09T00:00:00Z",
		},
		"validation": map[string]any{"tests": "passed", "vet": "passed"},
		"artifacts": []map[string]any{{
			"path": "controller", "bytes": len(content), "sha256": hex.EncodeToString(digest[:]),
		}},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDirectory, "host-manifest.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidConfig := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalidConfig, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = run([]string{
		"--config", invalidConfig,
		"toolchain", "runtime-install", "--target-user", account.Username,
		"--package", packageDirectory, "--virtual-board", virtualBoard,
		"--browser", "/bin/sh", "--dry-run", "--json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("dry-run: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `"package_validated": true`) || !strings.Contains(stdout.String(), `"applied": false`) {
		t.Fatalf("unexpected report: %s", stdout.String())
	}
}

func writeRuntimeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

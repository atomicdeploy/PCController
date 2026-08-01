package programmer

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveToolchainPolicySelectsLatestStableAndReportsCanaries(t *testing.T) {
	const (
		stableCommit = "1111111111111111111111111111111111111111"
		stableTree   = "2222222222222222222222222222222222222222"
		canaryCommit = "3333333333333333333333333333333333333333"
		canaryTree   = "4444444444444444444444444444444444444444"
	)
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	writeJSON := func(writer http.ResponseWriter, value any) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			t.Errorf("encode fixture: %v", err)
		}
	}
	mux.HandleFunc("/cli", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, []map[string]any{
			{"tag_name": "v2.0.0-rc.1", "prerelease": true, "assets": []any{}},
			{"tag_name": "v1.5.1", "assets": []any{}},
			{"tag_name": "v1.6.0", "assets": []map[string]string{{
				"name":                 "firmware-cli_1.6.0_Windows_64bit.zip",
				"browser_download_url": server.URL + "/asset.zip",
				"digest":               "sha256:" + strings.Repeat("a", 64),
			}}},
		})
	})
	mux.HandleFunc("/core", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"packages": []map[string]any{{
			"name": "MiniCore", "platforms": []map[string]string{
				{"architecture": "avr", "version": "3.1.2", "url": server.URL + "/core-3.1.2.tar.bz2", "checksum": "SHA-256:" + strings.Repeat("b", 64)},
				{"architecture": "avr", "version": "3.2.0", "url": server.URL + "/core-3.2.0.tar.bz2", "checksum": "SHA-256:" + strings.Repeat("c", 64)},
			},
		}}})
	})
	mux.HandleFunc("/libraries", func(writer http.ResponseWriter, _ *http.Request) {
		var archive bytes.Buffer
		compressed := gzip.NewWriter(&archive)
		_ = json.NewEncoder(compressed).Encode(map[string]any{"libraries": []map[string]string{
			{"name": "Example Library", "version": "1.0.0", "url": server.URL + "/library-1.0.0.zip", "checksum": "SHA-256:" + strings.Repeat("d", 64)},
			{"name": "Example Library", "version": "1.1.0", "url": server.URL + "/library-1.1.0.zip", "checksum": "SHA-256:" + strings.Repeat("e", 64)},
		}})
		_ = compressed.Close()
		_, _ = writer.Write(archive.Bytes())
	})
	mux.HandleFunc("/tags", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, []map[string]any{
			{"name": "v99.0", "commit": map[string]string{"sha": strings.Repeat("f", 40)}},
			{"name": "u8.0", "commit": map[string]string{"sha": strings.Repeat("0", 40)}},
			{"name": "u8.0.1", "commit": map[string]string{"sha": stableCommit}},
		})
	})
	mux.HandleFunc("/commits/u8.0.1", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"sha": stableCommit, "commit": map[string]any{"tree": map[string]string{"sha": stableTree}}})
	})
	mux.HandleFunc("/commits/main", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"sha": canaryCommit, "commit": map[string]any{"tree": map[string]string{"sha": canaryTree}}})
	})
	mux.HandleFunc("/go", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("go1.26.5\n2026-07-01T21:24:27Z\n"))
	})

	moduleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.test/tool\n\ngo 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.sum"), []byte("exact-module-lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := ToolchainPolicy{
		Format: ToolchainPolicyFormat, Name: "test", FQBN: "MiniCore:avr:test",
		LibraryIndex: server.URL + "/libraries",
		CLI: ToolchainCLIPolicy{
			Dependency: "firmware-cli", Repository: "example/cli", ReleaseAPI: server.URL + "/cli",
			Constraint: ToolchainConstraint{Channel: "stable", MinimumVersion: "1.0.0"},
			Assets:     []ToolchainCLIAssetPolicy{{GOOS: "windows", GOARCH: "amd64", Suffix: "_Windows_64bit.zip", Archive: "zip"}},
		},
		Core:      ToolchainCorePolicy{ID: "MiniCore:avr", IndexURL: server.URL + "/core", Constraint: ToolchainConstraint{Channel: "stable", MinimumVersion: "3.0.0"}},
		Libraries: []ToolchainLibraryPolicy{{Name: "Example Library", Constraint: ToolchainConstraint{Channel: "stable", MinimumVersion: "1.0.0"}}},
		Bootloader: ToolchainBootloaderPolicy{
			Repository: "example/urboot", TagPrefix: "u", TagsAPI: server.URL + "/tags",
			CommitsAPI: server.URL + "/commits", CanaryRef: "main",
			Constraint: ToolchainConstraint{Channel: "stable", MinimumVersion: "8.0"},
		},
		Go: ToolchainGoPolicy{VersionURL: server.URL + "/go", Constraint: ToolchainConstraint{Channel: "stable", MinimumVersion: "1.25.0"}},
	}
	resolvedAt := time.Date(2026, 8, 2, 12, 34, 56, 0, time.UTC)
	resolution, err := ResolveToolchainPolicy(context.Background(), policy, ToolchainResolveOptions{
		HTTPClient: server.Client(), IncludeCanary: true, ModuleDir: moduleDir,
		Now: func() time.Time { return resolvedAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	lock := resolution.Lock
	if lock.Firmware.CLI.Version != "1.6.0" || lock.Firmware.CoreVersion != "3.2.0" || lock.Firmware.Libraries[0].Version != "1.1.0" {
		t.Fatalf("latest stable releases were not selected: %+v", lock.Firmware)
	}
	if lock.Bootloader.Tag != "u8.0.1" || lock.Bootloader.SourceCommit != stableCommit || lock.Bootloader.TreeHash != stableTree {
		t.Fatalf("stable bootloader identity is wrong: %+v", lock.Bootloader)
	}
	if lock.Go.Version != "1.26.5" || lock.Go.GoModSHA256 == "" || lock.Go.GoSumSHA256 == "" {
		t.Fatalf("Go resolution is incomplete: %+v", lock.Go)
	}
	if resolution.Canary.CLIRelease != "v2.0.0-rc.1" || resolution.Canary.BootloaderCommit != canaryCommit {
		t.Fatalf("canary observations are wrong: %+v", resolution.Canary)
	}
	if lock.ResolvedAt != resolvedAt.Format(time.RFC3339) {
		t.Fatalf("resolved timestamp=%q", lock.ResolvedAt)
	}
}

func TestCompareToolchainLocksIgnoresResolutionTimestamp(t *testing.T) {
	current := ToolchainLock{
		Firmware:   ToolchainProfile{CLI: ToolchainCLI{Dependency: "cli", Version: "1.0"}, CoreID: "core", CoreVersion: "1.0", Libraries: []ToolchainLibrary{{Name: "lib", Version: "1.0"}}},
		Bootloader: ResolvedBootloader{Repository: "boot", Tag: "u1", SourceCommit: "aaa"},
		Go:         ResolvedGo{Version: "1.25.0", GoModSHA256: "same", GoSumSHA256: "old"},
	}
	resolved := current
	resolved.ResolvedAt = "different-but-ignored"
	resolved.Firmware.Libraries = []ToolchainLibrary{{Name: "lib", Version: "1.1"}}
	resolved.Go.GoSumSHA256 = "new"
	changes := CompareToolchainLocks(current, resolved)
	if len(changes) != 2 || changes[0].Name != "go.sum" || changes[1].Name != "lib" {
		t.Fatalf("unexpected deterministic changes: %+v", changes)
	}
}

func TestCompareToolchainLocksDetectsEveryArtifactIdentity(t *testing.T) {
	baseline := ToolchainLock{
		Firmware: ToolchainProfile{
			CLI: ToolchainCLI{Dependency: "cli", Version: "1.0.0", Assets: []ToolchainAsset{{
				GOOS: "windows", GOARCH: "amd64", Archive: "zip",
				URL: "https://example.test/cli.zip", SHA256: strings.Repeat("a", 64),
			}}},
			CoreID: "core:avr", CoreVersion: "1.0.0",
			Libraries: []ToolchainLibrary{{Name: "library", Version: "1.0.0"}},
		},
		CoreSource: ResolvedSource{URL: "https://example.test/core.tar", SHA256: strings.Repeat("b", 64)},
		Libraries: []ResolvedToolchainLibrary{{
			Name: "library", Version: "1.0.0",
			ResolvedSource: ResolvedSource{URL: "https://example.test/library.zip", SHA256: strings.Repeat("c", 64)},
		}},
		Bootloader: ResolvedBootloader{
			Repository: "example/boot", Tag: "u1.0.0",
			SourceCommit: strings.Repeat("d", 40), TreeHash: strings.Repeat("e", 40),
		},
		Go: ResolvedGo{Version: "1.26.5", GoModSHA256: strings.Repeat("f", 64), GoSumSHA256: strings.Repeat("0", 64)},
	}
	clone := func() ToolchainLock {
		encoded, err := json.Marshal(baseline)
		if err != nil {
			t.Fatal(err)
		}
		var result ToolchainLock
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	tests := []struct {
		name   string
		mutate func(*ToolchainLock)
	}{
		{"CLI asset URL", func(lock *ToolchainLock) { lock.Firmware.CLI.Assets[0].URL += ".new" }},
		{"CLI asset SHA-256", func(lock *ToolchainLock) { lock.Firmware.CLI.Assets[0].SHA256 = strings.Repeat("1", 64) }},
		{"core URL", func(lock *ToolchainLock) { lock.CoreSource.URL += ".new" }},
		{"core SHA-256", func(lock *ToolchainLock) { lock.CoreSource.SHA256 = strings.Repeat("2", 64) }},
		{"library URL", func(lock *ToolchainLock) { lock.Libraries[0].URL += ".new" }},
		{"library SHA-256", func(lock *ToolchainLock) { lock.Libraries[0].SHA256 = strings.Repeat("3", 64) }},
		{"bootloader source tree", func(lock *ToolchainLock) { lock.Bootloader.TreeHash = strings.Repeat("4", 40) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved := clone()
			test.mutate(&resolved)
			if changes := CompareToolchainLocks(baseline, resolved); len(changes) == 0 {
				t.Fatalf("%s drift was not reported", test.name)
			}
		})
	}
}

func TestStableVersionComparisonRejectsPrereleases(t *testing.T) {
	if _, ok := parseStableVersion("1.5.2-rc.1"); ok {
		t.Fatal("prerelease was treated as stable")
	}
	if compareStableVersions("1.10.0", "1.9.9") <= 0 || compareStableVersions("8.0", "8.0.0") != 0 {
		t.Fatal("semantic comparison is incorrect")
	}
}

func TestUpdateToolchainLockPreservesBytesWhenOnlyTimestampChanges(t *testing.T) {
	current, err := LoadToolchainLock(filepath.Join("..", "..", "toolchain-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "toolchain-lock.json")
	original, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	original = append(original, '\n')
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := current
	resolved.ResolvedAt = "2099-01-01T00:00:00Z"
	written, err := UpdateToolchainLock(path, current, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("timestamp-only resolution rewrote the lock")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("timestamp-only resolution changed lock bytes")
	}
}

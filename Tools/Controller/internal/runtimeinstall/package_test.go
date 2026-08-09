//go:build !windows

package runtimeinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePackageChecksManifestHashesTargetAndBothSmokes(t *testing.T) {
	root := t.TempDir()
	controller := filepath.Join(root, "controller")
	board := filepath.Join(t.TempDir(), "virtual_board")
	writeExecutable(t, controller, "controller")
	writeExecutable(t, board, "board")
	manifest := testHostManifest(t, controller, "linux", "amd64")
	writeJSON(t, filepath.Join(root, "host-manifest.json"), manifest)
	var smokes []string
	validated, err := validatePackageFor(
		context.Background(), root, board, "linux", "amd64",
		func(_ context.Context, executable string, arguments ...string) (string, error) {
			smokes = append(smokes, filepath.Base(executable)+" "+strings.Join(arguments, " "))
			if executable == controller {
				return "PCController 1.2.3 source-hash=" + strings.Repeat("a", 64), nil
			}
			return "PCController Virtual Board\n--port PORT", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Controller.Path != controller || validated.VirtualBoard.Path != board || len(smokes) != 2 {
		t.Fatalf("validated=%+v smokes=%v", validated, smokes)
	}
}

func TestValidatePackageRejectsWrongTargetAndArtifactMutation(t *testing.T) {
	root := t.TempDir()
	controller := filepath.Join(root, "controller")
	board := filepath.Join(t.TempDir(), "virtual_board")
	writeExecutable(t, controller, "controller")
	writeExecutable(t, board, "board")
	manifest := testHostManifest(t, controller, "linux", "arm64")
	writeJSON(t, filepath.Join(root, "host-manifest.json"), manifest)
	if _, err := validatePackageFor(context.Background(), root, board, "linux", "amd64", nil); err == nil ||
		!strings.Contains(err.Error(), "requires linux/amd64") {
		t.Fatalf("wrong target error=%v", err)
	}
	manifest.Target.Architecture = "amd64"
	writeJSON(t, filepath.Join(root, "host-manifest.json"), manifest)
	if err := os.WriteFile(controller, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := validatePackageFor(context.Background(), root, board, "linux", "amd64", nil); err == nil ||
		!strings.Contains(err.Error(), "artifact size") {
		t.Fatalf("mutation error=%v", err)
	}
}

func TestValidatePackageRejectsSymlinkedVirtualBoard(t *testing.T) {
	root := t.TempDir()
	controller := filepath.Join(root, "controller")
	realBoard := filepath.Join(t.TempDir(), "virtual_board.real")
	board := filepath.Join(t.TempDir(), "virtual_board")
	writeExecutable(t, controller, "controller")
	writeExecutable(t, realBoard, "board")
	if err := os.Symlink(realBoard, board); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(root, "host-manifest.json"), testHostManifest(t, controller, "linux", "amd64"))
	if _, err := validatePackageFor(context.Background(), root, board, "linux", "amd64", nil); err == nil ||
		!strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlink error=%v", err)
	}
}

func testHostManifest(t *testing.T, controller, platform, architecture string) HostManifest {
	t.Helper()
	content, err := os.ReadFile(controller)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	manifest := HostManifest{Format: HostManifestFormat}
	manifest.Target.Platform = platform
	manifest.Target.Architecture = architecture
	manifest.Identity.Version = "1.2.3"
	manifest.Identity.SourceSHA256 = strings.Repeat("a", 64)
	manifest.Identity.BuildTime = "2026-08-09T00:00:00Z"
	manifest.Artifacts = []HostArtifact{{
		Path: "controller", Bytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
	}}
	return manifest
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

//go:build linux

package runtimeinstall

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestStageCanonicalOutputsWithoutExecutionAndNormalizesModes(t *testing.T) {
	packageDirectory, board, executionMarker := canonicalStageSource(t)
	stageRoot := filepath.Join(t.TempDir(), "runtime-input")
	oldMask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldMask) })
	report, err := Stage(context.Background(), StageOptions{
		Root: stageRoot, SourcePackage: packageDirectory, SourceVirtualBoard: board, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(executionMarker); !os.IsNotExist(err) {
		t.Fatalf("source artifact executed during staging: %v", err)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{
		{stageRoot, 0o755},
		{filepath.Dir(report.PackageDirectory), 0o755},
		{report.PackageDirectory, 0o755},
		{filepath.Join(report.PackageDirectory, "controller"), 0o755},
		{filepath.Join(report.PackageDirectory, "host-manifest.json"), 0o644},
		{report.VirtualBoard, 0o755},
		{filepath.Join(filepath.Dir(report.PackageDirectory), "stage-manifest.json"), 0o644},
	} {
		info, err := os.Lstat(item.path)
		if err != nil {
			t.Fatalf("inspect %s: %v", item.path, err)
		}
		if info.Mode().Perm() != item.mode {
			t.Fatalf("%s mode=%04o want=%04o", item.path, info.Mode().Perm(), item.mode)
		}
	}
	if report.ControllerSHA256 == "" || report.VirtualBoardSHA256 == "" || report.PackageDirectory == packageDirectory || report.VirtualBoard == board {
		t.Fatalf("stage report=%+v", report)
	}
	second, err := Stage(context.Background(), StageOptions{
		Root: stageRoot, SourcePackage: packageDirectory, SourceVirtualBoard: board, Apply: true,
	})
	if err != nil || second.StageID != report.StageID {
		t.Fatalf("idempotent stage=%+v err=%v", second, err)
	}
}

func TestStageRejectsNoncanonicalAndSymlinkedSources(t *testing.T) {
	packageDirectory, board, _ := canonicalStageSource(t)
	if _, err := Stage(context.Background(), StageOptions{
		SourcePackage: filepath.Dir(packageDirectory), SourceVirtualBoard: board,
	}); err == nil || !strings.Contains(err.Error(), "exact Controller/bin") {
		t.Fatalf("noncanonical package error=%v", err)
	}
	realBoard := board + ".real"
	if err := os.Rename(board, realBoard); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBoard, board); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(context.Background(), StageOptions{
		SourcePackage: packageDirectory, SourceVirtualBoard: board,
	}); err == nil {
		t.Fatal("symlinked canonical VirtualBoard was accepted")
	}
}

func canonicalStageSource(t *testing.T) (string, string, string) {
	t.Helper()
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	packageDirectory := filepath.Join(repository, "Tools", "Controller", "bin")
	boardDirectory := filepath.Join(repository, "Tools", "VirtualBoard", ".build", "release", "bin")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(boardDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	executionMarker := filepath.Join(t.TempDir(), "executed")
	controller := filepath.Join(packageDirectory, "controller")
	controllerContent := "#!/bin/sh\ntouch '" + executionMarker + "'\necho 'PCController 1.2.3 source-hash=" + strings.Repeat("a", 64) + "'\n"
	if err := os.WriteFile(controller, []byte(controllerContent), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := testHostManifest(t, controller, "linux", runtime.GOARCH)
	writeJSON(t, filepath.Join(packageDirectory, "host-manifest.json"), manifest)
	board := filepath.Join(boardDirectory, "virtual_board")
	if err := os.WriteFile(board, []byte("#!/bin/sh\ntouch '"+executionMarker+"'\necho 'PCController Virtual Board --port PORT'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return packageDirectory, board, executionMarker
}

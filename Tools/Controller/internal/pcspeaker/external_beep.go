package pcspeaker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var externalBeepLookPath = exec.LookPath
var externalBeepCommand = exec.CommandContext

func playExternalBeep(ctx context.Context, driverDirectory string, frequencyHz, durationMS int) error {
	path, err := findExternalBeep(driverDirectory)
	if err != nil {
		return err
	}
	command := externalBeepCommand(ctx, path, externalBeepArguments(frequencyHz, durationMS)...)
	if output, err := command.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("external beep %s failed: %s", filepath.Base(path), detail)
	}
	return nil
}

func findExternalBeep(driverDirectory string) (string, error) {
	names := []string{"beep"}
	if runtime.GOOS == "windows" {
		names = []string{"beep.exe", "pc-beep.exe", "beep"}
		if directory := strings.TrimSpace(driverDirectory); directory != "" {
			for _, name := range names[:2] {
				candidate := filepath.Join(directory, name)
				if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
					return candidate, nil
				}
			}
		}
	}
	for _, name := range names {
		if path, err := externalBeepLookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("native PC speaker failed and no external beep command was found")
}

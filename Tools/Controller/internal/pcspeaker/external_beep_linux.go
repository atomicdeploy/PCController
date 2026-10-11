//go:build linux

package pcspeaker

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const externalBeepStopTimeout = time.Second

func externalBeepArguments(frequencyHz, durationMS int) []string {
	return []string{"-f", strconv.Itoa(frequencyHz), "-l", strconv.Itoa(durationMS)}
}

// stopExternalBeep clears a Linux console speaker after an interrupted helper.
// The beep utility drives KIOCSOUND in userspace, so CommandContext's SIGKILL
// can prevent its normal cleanup and leave the kernel tone running forever.
func stopExternalBeep(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), externalBeepStopTimeout)
	defer cancel()
	command := externalBeepCommand(ctx, path, "-f", "0", "-l", "0")
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("external beep %s emergency stop failed: %s", filepath.Base(path), detail)
}

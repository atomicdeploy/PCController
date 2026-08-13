//go:build windows

package pcspeaker

import "strconv"

func externalBeepArguments(frequencyHz, durationMS int) []string {
	return []string{"-f", strconv.Itoa(frequencyHz), "-d", strconv.Itoa(durationMS)}
}

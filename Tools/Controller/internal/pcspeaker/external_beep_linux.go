//go:build linux

package pcspeaker

import "strconv"

func externalBeepArguments(frequencyHz, durationMS int) []string {
	return []string{"-f", strconv.Itoa(frequencyHz), "-l", strconv.Itoa(durationMS)}
}

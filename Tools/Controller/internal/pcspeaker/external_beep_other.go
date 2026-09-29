//go:build !windows && !linux

package pcspeaker

import "strconv"

func externalBeepArguments(frequencyHz, durationMS int) []string {
	return []string{strconv.Itoa(frequencyHz), strconv.Itoa(durationMS)}
}

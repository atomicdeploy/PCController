//go:build windows

package winfx

import "testing"

func TestPlayWaveMemoryRejectsNonWaveDataBeforeCallingWinMM(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{
		nil,
		[]byte("short"),
		[]byte("RIFF\x04\x00\x00\x00NOPE"),
	} {
		if err := PlayWaveMemory(data); err == nil {
			t.Fatalf("PlayWaveMemory(%q) unexpectedly succeeded", data)
		}
	}
}

//go:build windows

package winfx

import (
	"bytes"
	"errors"
	"runtime"
	"unsafe"
)

// PlayWaveMemory synchronously plays a complete RIFF/WAVE stream through
// WinMM. Synchronous SND_MEMORY playback guarantees that wave remains alive
// until Windows has finished consuming the caller's exact bytes.
//
// Call this from a worker goroutine: playback intentionally blocks for the
// duration of the sound.
func PlayWaveMemory(wave []byte) error {
	if len(wave) < 12 ||
		!bytes.Equal(wave[0:4], []byte("RIFF")) ||
		!bytes.Equal(wave[8:12], []byte("WAVE")) {
		return errors.New("winfx: memory sound is not a RIFF/WAVE stream")
	}
	if err := cuePlaySoundW.Find(); err != nil {
		return err
	}

	cueMu.Lock()
	defer cueMu.Unlock()
	ok, _, callErr := cuePlaySoundW.Call(
		uintptr(unsafe.Pointer(&wave[0])),
		0,
		sndMemory|sndNoDefault|sndSystem,
	)
	runtime.KeepAlive(wave)
	if ok == 0 {
		return boolCallError("PlaySoundW(SND_MEMORY)", callErr)
	}
	return nil
}

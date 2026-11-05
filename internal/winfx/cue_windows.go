//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Cue string

const (
	CueOn      Cue = "on"
	CueOff     Cue = "off"
	CueMessage Cue = "message"
	CueError   Cue = "error"

	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
	sndAlias     = 0x00010000
	sndSystem    = 0x00200000

	mbIconHand        = 0x00000010
	mbIconExclamation = 0x00000030
	mbIconAsterisk    = 0x00000040
)

var (
	cueWinMM       = windows.NewLazySystemDLL("winmm.dll")
	cuePlaySoundW  = cueWinMM.NewProc("PlaySoundW")
	cueUser32      = windows.NewLazySystemDLL("user32.dll")
	cueMessageBeep = cueUser32.NewProc("MessageBeep")
	cueMu          sync.Mutex
)

type cueSpec struct {
	alias string
	beep  uintptr
}

// PlayCue starts a configured Windows system sound asynchronously. Concurrent
// calls are serialized around PlaySoundW, and an unavailable sound alias falls
// back to MessageBeep.
func PlayCue(cue Cue) error {
	var spec cueSpec
	switch Cue(strings.ToLower(strings.TrimSpace(string(cue)))) {
	case CueOn:
		spec = cueSpec{alias: "SystemAsterisk", beep: mbIconAsterisk}
	case CueOff:
		spec = cueSpec{alias: "SystemDefault", beep: 0}
	case CueMessage:
		spec = cueSpec{alias: "SystemNotification", beep: mbIconAsterisk}
	case CueError:
		spec = cueSpec{alias: "SystemHand", beep: mbIconHand}
	default:
		return fmt.Errorf("winfx: unknown audible cue %q", cue)
	}

	alias, err := windows.UTF16PtrFromString(spec.alias)
	if err != nil {
		return fmt.Errorf("encode sound alias: %w", err)
	}

	cueMu.Lock()
	defer cueMu.Unlock()

	var playErr error
	if err := cuePlaySoundW.Find(); err == nil {
		ok, _, callErr := cuePlaySoundW.Call(
			uintptr(unsafePointer(alias)),
			0,
			sndAlias|sndAsync|sndNoDefault|sndSystem,
		)
		if ok != 0 {
			return nil
		}
		playErr = boolCallError("PlaySoundW", callErr)
	} else {
		playErr = err
	}

	if err := cueMessageBeep.Find(); err != nil {
		return errors.Join(playErr, fmt.Errorf("resolve MessageBeep: %w", err))
	}
	ok, _, callErr := cueMessageBeep.Call(spec.beep)
	if ok == 0 {
		return errors.Join(playErr, boolCallError("MessageBeep", callErr))
	}
	return nil
}

// unsafePointer is kept tiny so cue.go does not spread unsafe conversions
// through the control flow.
func unsafePointer[T any](value *T) uintptr {
	return uintptr(unsafe.Pointer(value))
}

package appconfig

import (
	"runtime"
	"testing"
)

func TestDefaultBuzzerMirrorIsOptInWithoutNativeDependency(t *testing.T) {
	value := Defaults().Integrations.BuzzerMirror
	if value.Enabled || value.NativeEnabled || !value.WebAudioEnabled || value.DriverDirectory != "" {
		t.Fatalf("defaults=%+v", value)
	}
	if err := validateBuzzerMirror(value); err != nil {
		t.Fatal(err)
	}
}

func TestBuzzerMirrorRejectsInvalidDriverDirectory(t *testing.T) {
	value := DefaultBuzzerMirror()
	value.DriverDirectory = "driver\nnext"
	if err := validateBuzzerMirror(value); err == nil {
		t.Fatal("multiline driver directory was accepted")
	}
}

func TestBuzzerMirrorRequiresDriverDirectoryOnlyForNativePlayback(t *testing.T) {
	value := DefaultBuzzerMirror()
	value.NativeEnabled = true
	if err := validateBuzzerMirror(value); runtime.GOOS == "windows" && err == nil {
		t.Fatal("native Windows playback accepted an empty driver directory")
	} else if runtime.GOOS == "linux" && err != nil {
		t.Fatalf("native %s playback incorrectly requires WinRing0: %v", runtime.GOOS, err)
	} else if runtime.GOOS != "windows" && runtime.GOOS != "linux" && err == nil {
		t.Fatalf("native %s playback was accepted without a backend", runtime.GOOS)
	}
	value.DriverDirectory = `C:\optional\winring0`
	if err := validateBuzzerMirror(value); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBuzzerPlaybackSupportedOSes(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		if !nativeBuzzerPlaybackSupported(goos) {
			t.Fatalf("native backend for %s was hidden", goos)
		}
	}
	for _, goos := range []string{"aix", "darwin", "freebsd", "plan9", "js"} {
		if nativeBuzzerPlaybackSupported(goos) {
			t.Fatalf("native playback unexpectedly reported for %s", goos)
		}
	}
}

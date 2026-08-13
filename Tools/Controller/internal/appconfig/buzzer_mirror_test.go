package appconfig

import "testing"

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

func TestBuzzerMirrorAllowsPlatformNativeOrExternalPlaybackWithoutDriver(t *testing.T) {
	value := DefaultBuzzerMirror()
	value.NativeEnabled = true
	if err := validateBuzzerMirror(value); err != nil {
		t.Fatal(err)
	}
	value.DriverDirectory = `C:\optional\winring0`
	if err := validateBuzzerMirror(value); err != nil {
		t.Fatal(err)
	}
}

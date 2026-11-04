package appconfig

import "testing"

func TestStripFPSConfigurationBounds(t *testing.T) {
	for _, fps := range []int{1, 30, 60, 120, 0, -1, 121} {
		config := Defaults()
		config.StripEffects[0].DefaultFPS = fps
		err := config.Validate()
		wantValid := fps >= 1 && fps <= 120
		if (err == nil) != wantValid {
			t.Fatalf("FPS %d: validation error = %v, want valid %v", fps, err, wantValid)
		}
	}
}

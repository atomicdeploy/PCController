package appconfig

import "fmt"

// ShouldShowIntro is shared by the early terminal frame and the interactive
// welcome. Empty is the backwards-compatible spelling of auto.
func (ui UI) ShouldShowIntro() bool {
	switch ui.IntroMode {
	case "always":
		return true
	case "skip":
		return false
	default:
		return !ui.SetupComplete
	}
}

func ValidateIntroMode(mode string) error {
	switch mode {
	case "", "auto", "always", "skip":
		return nil
	default:
		return fmt.Errorf("intro mode must be auto, always, or skip")
	}
}

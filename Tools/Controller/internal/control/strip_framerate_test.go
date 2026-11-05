package control

import (
	"context"
	"strconv"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestStripCommandsAdmitHighFPSAndRejectOutsideBoundsBeforeSending(t *testing.T) {
	for _, fps := range []int{1, 30, 60, 120, 0, -1, 121} {
		for _, kind := range []string{"rainbow", "effect"} {
			t.Run(kind+"/"+strconv.Itoa(fps), func(t *testing.T) {
				target := &recordingOutputTarget{}
				outputs := NewOutputScheduler(target)
				defer outputs.Close()
				var err error
				if kind == "rainbow" {
					_, err = stripStreamCommand(context.Background(), outputs, []string{"rainbow", "1", strconv.Itoa(fps)})
				} else {
					_, err = playStripProgramCommand(context.Background(), outputs, "police", []string{"1", strconv.Itoa(fps)}, appconfig.DefaultStripEffects())
				}
				wantValid := fps >= 1 && fps <= 120
				if (err == nil) != wantValid {
					t.Fatalf("FPS %d: error = %v, want valid %v", fps, err, wantValid)
				}
				if !wantValid && len(target.snapshotCommands()) != 0 {
					t.Fatal("invalid FPS reached output transport")
				}
			})
		}
	}
}

func TestStripTimelineAdmitsHighFPS(t *testing.T) {
	for _, fps := range []int{60, 120, 0, 121} {
		config := appconfig.Defaults()
		config.StripEffects[0].DefaultFPS = fps
		runner := NewMacroRunner(nil, nil, nil)
		runner.hostConfig = func() appconfig.Config { return config }
		steps, err := compileMediaTimeline(MediaTimelinePlan{ClientID: "fps-test", Revision: 1,
			Cues: []MediaTimelineCue{{ID: "strip", Reference: "effect:police", DurationMS: 100}}}, runner)
		if wantValid := fps >= 1 && fps <= 120; (err == nil) != wantValid {
			t.Fatalf("timeline FPS %d: error = %v, steps = %d", fps, err, len(steps))
		}
	}
}

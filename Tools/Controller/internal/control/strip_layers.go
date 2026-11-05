package control

import (
	"math"
	"pccontroller.local/controller/internal/appconfig"
	"time"
)

func stripEase(t float64, kind string) float64 {
	switch kind {
	case "ease-in":
		return t * t
	case "ease-out":
		return 1 - (1-t)*(1-t)
	case "ease-in-out":
		return t * t * (3 - 2*t)
	}
	return t
}
func renderStripLayers(p appconfig.StripProgram, count int, elapsed time.Duration) []byte {
	frame := make([]byte, count*3)
	// Runtime overrides must not silently clip an authored segment.
	if appconfig.ValidateStripLayers(p, count) != nil {
		return frame
	}
	for _, l := range p.Layers {
		if !l.Enabled {
			continue
		}
		var pixels []int
		for _, s := range p.Segments {
			if s.ID == l.SegmentID {
				pixels = s.Pixels
				break
			}
		}
		phase := (elapsed.Milliseconds() + int64(l.PhaseMS)) % int64(l.PeriodMS)
		t := float64(phase) / float64(l.PeriodMS-1)
		for i, pixel := range pixels {
			mix := 0.0
			switch l.Primitive {
			case "gradient":
				if len(pixels) > 1 {
					mix = float64(i) / float64(len(pixels)-1)
				}
			case "ramp":
				mix = t
			case "blink":
				if phase*100 >= int64(l.PeriodMS*l.DutyPercent) {
					mix = 1
				}
			case "breathe":
				mix = (1 - math.Cos(t*2*math.Pi)) / 2
			}
			if l.Reverse {
				mix = 1 - mix
			}
			mix = stripEase(mix, l.Easing)
			interpolate := func(a, b byte) byte { return byte(math.Round(float64(a) + (float64(b)-float64(a))*mix)) }
			frame[pixel*3] = interpolate(l.Primary.Red, l.Secondary.Red)
			frame[pixel*3+1] = interpolate(l.Primary.Green, l.Secondary.Green)
			frame[pixel*3+2] = interpolate(l.Primary.Blue, l.Secondary.Blue)
		}
	}
	return frame
}

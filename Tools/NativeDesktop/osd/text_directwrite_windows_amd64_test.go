//go:build windows && amd64

package osd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func directWriteForTest(t *testing.T) *directWriteRasterizer {
	t.Helper()
	text, err := newDirectWriteRasterizer()
	if err != nil {
		t.Fatal("DirectWrite experiment unavailable", err)
	}
	return text.(*directWriteRasterizer)
}

func TestDirectWriteActualWeightsFractionalSizeAndUnicode(t *testing.T) {
	if unsafe.Sizeof(textTargetProperties{}) != 28 || unsafe.Sizeof(textMetrics{}) != 36 || unsafe.Sizeof(textOverhang{}) != 16 {
		t.Fatal("Windows SDK ABI structure size mismatch")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := directWriteForTest(t)
	defer r.close()
	for _, wanted := range []uint32{400, 600} {
		actual, err := r.fontWeight(wanted)
		if err != nil || actual != wanted {
			t.Fatal("actual primary-family font weight", wanted, actual, err)
		}
	}
	for _, text := range []string{"PCController 100% · 0123456789", "کنترل سینما — آماده", "Latin / فارسی ۱۲۳", "日本語 中文", "× ✓ →", "🎬 Cinema"} {
		for _, dpi := range []int{96, 110, 120, 144, 168, 192, 240, 288, 384} {
			im, err := r.image(text, float32(14*dpi)/96, false, color.RGBA{255, 255, 255, 255})
			if err != nil {
				t.Fatal(text, dpi, err)
			}
			ink := glyphInkBounds(im)
			if ink.Empty() || ink.Min.X < 1 || ink.Min.Y < 1 || ink.Max.X >= im.Bounds().Max.X || ink.Max.Y >= im.Bounds().Max.Y {
				t.Fatal("missing/clipped glyph coverage", text, dpi, ink, im.Bounds())
			}
			partial := 0
			for i := 0; i < len(im.Pix); i += 4 {
				a := im.Pix[i+3]
				if im.Pix[i] != a || im.Pix[i+1] != a || im.Pix[i+2] != a {
					t.Fatal("subpixel color fringes in white alpha coverage")
				}
				if a > 0 && a < 255 {
					partial++
				}
			}
			if partial == 0 {
				t.Fatal("antialiasing missing", text, dpi)
			}
		}
	}
	a, err := r.image("Fractional metrics", 18.2, false, color.RGBA{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.image("Fractional metrics", 18.4, false, color.RGBA{255, 255, 255, 255})
	if err != nil || bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("fractional font sizes were rounded to GDI integer metrics", err)
	}
}

type unavailableText struct{}

func (unavailableText) name() string { return "unavailable" }
func (unavailableText) close()       {}
func (unavailableText) image(string, float32, bool, color.RGBA) (*image.RGBA, error) {
	return nil, errors.New("simulated Direct2D target loss")
}

func TestDirectWriteFullCardFallbackPreservesGDI(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	request := Request{Title: "PCController", Text: "Target-loss fallback", Actions: []Action{{ID: "ok", Label: "Continue"}}}
	a, err := preparePreferredText(request, 144, 1920, nil, true, unavailableText{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	b, err := prepareWithText(request, 144, 1920, nil, true, gdiRasterizer{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	if a.textBackend != "gdi" || !bytes.Equal(a.paint(0, true, nil).Pix, b.paint(0, true, nil).Pix) {
		t.Fatal("fallback mixed text metrics or changed legacy card")
	}
	c, err := prepare(request, 144, 1920, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if c.textBackend != "directwrite" {
		t.Fatal("normal preparation silently used GDI")
	}
	request.Text = strings.Repeat("W", 2048)
	long, err := prepare(request, 144, 1920, nil, true)
	if err != nil {
		t.Fatal("long text must retain GDI marquee fallback", err)
	}
	defer long.close()
	if !long.scrolls() {
		t.Fatal("long-text fallback lost marquee")
	}
}

type countingText struct {
	textRasterizer
	calls int
}

func (c *countingText) image(text string, size float32, semibold bool, ink color.RGBA) (*image.RGBA, error) {
	c.calls++
	return c.textRasterizer.image(text, size, semibold, ink)
}

func TestDirectWriteAnimationNeverRerasterizesText(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	backend := directWriteForTest(t)
	defer backend.close()
	count := &countingText{textRasterizer: backend}
	r, err := prepareWithText(Request{Title: strings.Repeat("Long heading ", 8), Text: "Cached live text", Actions: []Action{{ID: "ok", Label: "Continue"}}}, 144, 1920, nil, true, count)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	preparedCalls := count.calls
	for i := 0; i < 120; i++ {
		r.paint(time.Duration(i)*16*time.Millisecond, false, []float64{float64(i%60) / 60})
	}
	if count.calls != preparedCalls {
		t.Fatal("animation rerasterized text", preparedCalls, count.calls)
	}
}

func TestDirectWriteFormatCacheIsBounded(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := directWriteForTest(t)
	defer r.close()
	for i := 0; i < 70; i++ {
		if _, err := r.image("DPI cache", 12+float32(i)/10, i%2 == 0, color.RGBA{255, 255, 255, 255}); err != nil {
			t.Fatal(err)
		}
		if len(r.formats) > 32 {
			t.Fatal("unbounded format cache", len(r.formats))
		}
	}
}

func TestDirectWriteCardSlotsDoNotClipLatinOrPersianInk(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	backend := directWriteForTest(t)
	defer backend.close()
	for _, dpi := range []int{96, 110, 120, 144, 168, 192, 240, 288, 384} {
		for _, title := range []string{"ÁgjQy · PCController", "آماده‌سازی گچپژ"} {
			r, err := prepareWithText(Request{Title: title, Text: "ÁgjQy · آماده‌سازی", Actions: []Action{{ID: "ok", Label: "آماده‌سازی"}}}, dpi, 1920*dpi/96, nil, true, backend)
			if err != nil {
				t.Fatal(err)
			}
			if glyphInkBounds(r.title).Max.Y > r.titleRect.Dy() || glyphInkBounds(r.bodies[0]).Max.Y > r.bodyRects[0].Dy() || r.buttons[0].Bounds().Dy() > r.hits[0].Dy() {
				r.close()
				t.Fatal("slot clips native glyphs", dpi, title)
			}
			r.close()
		}
	}
}

func TestOSDLiveDirectWritePresenterRetainsAndClosesItsTarget(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("opt-in source-owned presenter lifecycle")
	}
	p := newWindowPresenter(Options{})
	defer p.Close()
	first, err := p.Show(context.Background(), Request{ID: "text-first", Title: "PCController", Text: "Retained native text target", DurationMS: 1500, ReduceMotion: true})
	if err != nil || first.TextRenderer != "directwrite" {
		t.Fatal("first presentation did not use DirectWrite", first, err)
	}
	target := p.text
	second, err := p.Show(context.Background(), Request{ID: "text-second", Title: "PCController", Text: "Same target; new cached glyphs", DurationMS: 1500, ReduceMotion: true})
	if err != nil || second.TextRenderer != "directwrite" || p.text != target {
		t.Fatal("presenter recreated target for every notification", second, err)
	}
	previous, ok := p.Result("text-first")
	if !ok || previous.Active || previous.TextRenderer != first.TextRenderer {
		t.Fatal("renderer missing from replacement receipt", previous)
	}
	p.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		t.Fatal("presenter did not close")
	}
	if p.text != nil {
		t.Fatal("native text target outlived its owner thread")
	}
}

func TestDirectWriteTintAlphaAndResourceLifetime(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	text := directWriteForTest(t)
	white, err := text.image("Alpha × متن", 21, false, color.RGBA{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	tint := color.RGBA{139, 85, 220, 127}
	colored, err := text.image("Alpha × متن", 21, false, tint)
	if err != nil {
		t.Fatal(err)
	}
	if white.Bounds() != colored.Bounds() {
		t.Fatal("color changed text layout")
	}
	for i := 0; i < len(white.Pix); i += 4 {
		a := uint16(white.Pix[i+3]) * uint16(tint.A) / 255
		if colored.Pix[i+3] != byte(a) || colored.Pix[i] != byte(uint16(tint.R)*a/255) || colored.Pix[i+1] != byte(uint16(tint.G)*a/255) || colored.Pix[i+2] != byte(uint16(tint.B)*a/255) {
			t.Fatal("incorrect premultiplied tint/coverage")
		}
	}
	text.close()
	// Warm exactly the sizes/weights used below before measuring handle growth.
	warm := directWriteForTest(t)
	for j := 0; j < 4; j++ {
		if _, err := warm.image("Resource lifetime", float32(18+j)/1.1, j%2 == 0, color.RGBA{255, 255, 255, 255}); err != nil {
			warm.close()
			t.Fatal(err)
		}
	}
	warm.close()
	resources := windows.NewLazySystemDLL("user32.dll").NewProc("GetGuiResources")
	process := windows.CurrentProcess()
	before, _, _ := resources.Call(uintptr(process), 0)
	for i := 0; i < 40; i++ {
		r := directWriteForTest(t)
		for j := 0; j < 4; j++ {
			if _, err := r.image("Resource lifetime", float32(18+j)/1.1, j%2 == 0, color.RGBA{255, 255, 255, 255}); err != nil {
				r.close()
				t.Fatal(err)
			}
		}
		r.close()
	}
	after, _, _ := resources.Call(uintptr(process), 0)
	if after > before+2 {
		t.Fatal("GDI resource growth across 40 DirectWrite sessions", before, after)
	}
}

func TestDirectWriteComparisonExperiment(t *testing.T) {
	output := os.Getenv("PCCONTROLLER_OSD_TEXT_EXPERIMENT")
	if output == "" {
		t.Skip("opt-in native raster comparisons and timings")
	}
	if !filepath.IsAbs(output) {
		t.Fatal("absolute experiment directory required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initialization := time.Now()
	raster := directWriteForTest(t)
	initializationMS := float64(time.Since(initialization).Microseconds()) / 1000
	defer raster.close()
	write := func(name string, im image.Image) {
		t.Helper()
		file, err := os.Create(filepath.Join(output, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, im); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{Title: "PCController · Cinema", Text: "Ready · 100% · voltage 12.48 V\nکنترل سینما — آماده", Actions: []Action{{ID: "ok", Label: "Continue"}, {ID: "cancel", Label: "Cancel"}}}
	for _, dpi := range []int{96, 110, 120, 144, 168, 192, 240, 288, 384} {
		for _, dark := range []bool{true, false} {
			for _, backend := range []textRasterizer{gdiRasterizer{}, raster} {
				r, err := prepareWithText(request, dpi, 1920*dpi/96, nil, dark, backend)
				if err != nil {
					t.Fatal(backend.name(), dpi, err)
				}
				write(fmt.Sprintf("%s-%d-dark-%t", backend.name(), dpi, dark), r.paint(0, true, nil))
				r.close()
			}
		}
	}
	// Compare raw GDI, native DirectWrite, and a 2x/downsampled DirectWrite
	// candidate at the same physical output size; no AI/redrawn approximations.
	for _, dark := range []bool{true, false} {
		bg, fg := color.RGBA{24, 22, 30, 255}, color.RGBA{240, 235, 248, 255}
		if !dark {
			bg, fg = color.RGBA{249, 248, 252, 255}, color.RGBA{36, 33, 44, 255}
		}
		grid := image.NewRGBA(image.Rect(0, 0, 1050, 320))
		draw.Draw(grid, grid.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
		for col, caption := range []string{"GDI baseline", "DirectWrite / Direct2D", "DirectWrite 2× downsampled"} {
			label, err := raster.image(caption, 14, false, fg)
			if err != nil {
				t.Fatal(err)
			}
			draw.Draw(grid, label.Bounds().Add(image.Pt(col*350+12, 4)), label, image.Point{}, draw.Over)
		}
		for row, line := range []string{"PCController · Semibold 600", "Regular 400 · 100% · 12.48 V", "کنترل سینما — آماده ۱۲۳"} {
			for col := 0; col < 3; col++ {
				var im *image.RGBA
				var err error
				if col == 0 {
					im, err = gdiRasterizer{}.image(line, 21, row == 0, fg)
				} else {
					factor := float32(col)
					im, err = raster.image(line, 21*factor, row == 0, fg)
					if err == nil && col == 2 {
						im, err = downsampleText(im)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				at := image.Pt(col*350+12, row*100+35)
				draw.Draw(grid, im.Bounds().Add(at).Intersect(image.Rect(col*350, 0, (col+1)*350, 320)), im, image.Point{}, draw.Over)
			}
		}
		write(fmt.Sprintf("comparison-dark-%t", dark), grid)
	}
	timing := map[string]any{"dpi": 144, "iterations": 80, "actual_weights": raster.weights, "first_session_init_ms": initializationMS}
	for _, backend := range []textRasterizer{gdiRasterizer{}, raster} {
		var preparation, frames []float64
		for i := 0; i < 80; i++ {
			started := time.Now()
			r, err := prepareWithText(request, 144, 1920, nil, true, backend)
			if err != nil {
				t.Fatal(err)
			}
			preparation = append(preparation, float64(time.Since(started).Microseconds())/1000)
			started = time.Now()
			for j := 0; j < 8; j++ {
				r.paint(time.Duration(j)*time.Second, false, []float64{float64(j) / 8})
			}
			frames = append(frames, float64(time.Since(started).Microseconds())/8000)
			r.close()
		}
		slices.Sort(preparation)
		slices.Sort(frames)
		timing[backend.name()] = map[string]float64{"preparation_median_ms": preparation[40], "preparation_p95_ms": preparation[75], "cached_frame_median_ms": frames[40], "cached_frame_p95_ms": frames[75]}
	}
	var automatic []float64
	for i := 0; i < 80; i++ {
		started := time.Now()
		r, err := prepare(request, 144, 1920, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if r.textBackend != "directwrite" {
			r.close()
			t.Fatal("automatic path did not use DirectWrite")
		}
		automatic = append(automatic, float64(time.Since(started).Microseconds())/1000)
		r.close()
	}
	slices.Sort(automatic)
	timing["isolated_preview_with_session_initialization"] = map[string]float64{"preparation_median_ms": automatic[40], "preparation_p95_ms": automatic[75]}
	encoded, _ := json.MarshalIndent(timing, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "timings.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
}

func downsampleText(source *image.RGBA) (*image.RGBA, error) {
	result := image.NewRGBA(image.Rect(0, 0, int(math.Ceil(float64(source.Bounds().Dx())/2)), int(math.Ceil(float64(source.Bounds().Dy())/2))))
	for y := 0; y < result.Bounds().Dy(); y++ {
		for x := 0; x < result.Bounds().Dx(); x++ {
			for channel := 0; channel < 4; channel++ {
				sum, count := 0, 0
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						if x*2+dx < source.Bounds().Dx() && y*2+dy < source.Bounds().Dy() {
							sum += int(source.Pix[source.PixOffset(x*2+dx, y*2+dy)+channel])
							count++
						}
					}
				}
				result.Pix[result.PixOffset(x, y)+channel] = byte(sum / max(1, count))
			}
		}
	}
	return result, nil
}

//go:build windows

package traymenu

import (
	"crypto/sha256"
	"testing"
	"unsafe"

	"pccontroller.local/desktop/assets"
	"pccontroller.local/desktop/internal/winfx"
)

func TestPersianPopupUsesPhysicalYekanFaceAtEveryDPI(t *testing.T) {
	if err := winfx.EnsurePersianFont(); err != nil {
		t.Fatal(err)
	}
	for _, dpi := range []int32{96, 120, 144, 192} {
		family := winfx.PersianFontFamily(false)
		font, err := newMenuFontForFamily(dpi, family)
		if err != nil {
			t.Fatal(err)
		}
		face, err := winfx.FontFaceName(uintptr(font.handle))
		width := font.measure("تنظیمات PCController ۱۲۳۴۵۶۷۸۹۰")
		font.close()
		if err != nil || face != family || width <= 0 {
			t.Fatalf("DPI %d: face=%q width=%d error=%v", dpi, face, width, err)
		}
	}
}

func TestEmbeddedDeviceIconsDecode(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"icons/app.png",
		"icons/device-on.png",
		"icons/device-off.png",
		"icons/device-unknown.png",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := assets.Files.ReadFile(name)
			if err != nil {
				t.Fatalf("read embedded %s: %v", name, err)
			}
			icons, err := prepareIcons(
				[]Item{{ID: "icon", Label: "Icon", IconPNG: data}},
				22,
			)
			if err != nil {
				t.Fatalf("prepareIcons(%s): %v", name, err)
			}
			if len(icons) != 1 || icons[0].image == nil {
				t.Fatalf("prepareIcons(%s) = %#v", name, icons)
			}
			if size := icons[0].image.Bounds().Size(); size.X != 22 || size.Y != 22 {
				t.Fatalf("prepared icon size = %v, want 22x22", size)
			}
		})
	}
}

func TestPrepareIconsReusesCachedHighQualityRaster(t *testing.T) {
	data, err := assets.Files.ReadFile("icons/app.png")
	if err != nil {
		t.Fatalf("read embedded app icon: %v", err)
	}
	const targetSize = int32(23)
	key := preparedIconCacheKey{
		digest:     sha256.Sum256(data),
		targetSize: targetSize,
	}
	preparedIconCache.Lock()
	delete(preparedIconCache.entries, key)
	preparedIconCache.Unlock()

	items := []Item{{ID: "app", Label: "App", IconPNG: data}}
	first, err := prepareIcons(items, targetSize)
	if err != nil {
		t.Fatalf("first prepareIcons: %v", err)
	}
	second, err := prepareIcons(items, targetSize)
	if err != nil {
		t.Fatalf("second prepareIcons: %v", err)
	}
	if first[0].image == nil || second[0].image == nil {
		t.Fatal("prepared icon cache returned a nil raster")
	}
	if first[0].image != second[0].image {
		t.Fatal("identical PNG and target size were decoded and rescaled again")
	}
}

func TestBuildIconPrewarmJobsDeduplicatesAndKeepsDPIOrder(t *testing.T) {
	first := []byte("first")
	second := []byte("second")
	jobs := buildIconPrewarmJobs(
		[][]byte{first, nil, second, first},
		[]int32{33, 0, 22, 33},
	)
	if len(jobs) != 4 {
		t.Fatalf("prewarm jobs = %d, want 4 unique requests", len(jobs))
	}
	got := []struct {
		digest [sha256.Size]byte
		size   int32
	}{
		{sha256.Sum256(jobs[0].data), jobs[0].targetSize},
		{sha256.Sum256(jobs[1].data), jobs[1].targetSize},
		{sha256.Sum256(jobs[2].data), jobs[2].targetSize},
		{sha256.Sum256(jobs[3].data), jobs[3].targetSize},
	}
	want := []struct {
		digest [sha256.Size]byte
		size   int32
	}{
		{sha256.Sum256(first), 33},
		{sha256.Sum256(second), 33},
		{sha256.Sum256(first), 22},
		{sha256.Sum256(second), 22},
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("prewarm job %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestIconPrewarmWorkerCountIsBounded(t *testing.T) {
	tests := []struct {
		jobs int
		want int
	}{
		{jobs: -1, want: 0},
		{jobs: 0, want: 0},
		{jobs: 1, want: 1},
		{jobs: 3, want: 3},
		{jobs: 4, want: 4},
		{jobs: 80, want: 4},
	}
	for _, test := range tests {
		if got := iconPrewarmWorkerCount(test.jobs); got != test.want {
			t.Fatalf(
				"iconPrewarmWorkerCount(%d) = %d, want %d",
				test.jobs,
				got,
				test.want,
			)
		}
	}
}

func TestPrewarmIconPNGsPopulatesEveryTarget(t *testing.T) {
	data, err := assets.Files.ReadFile("icons/menu-settings.png")
	if err != nil {
		t.Fatalf("read embedded settings icon: %v", err)
	}
	targetSizes := []int32{17, 19}
	keys := make([]preparedIconCacheKey, 0, len(targetSizes))
	preparedIconCache.Lock()
	for _, targetSize := range targetSizes {
		key := preparedIconCacheKey{
			digest:     sha256.Sum256(data),
			targetSize: targetSize,
		}
		keys = append(keys, key)
		delete(preparedIconCache.entries, key)
	}
	preparedIconCache.Unlock()

	PrewarmIconPNGs([][]byte{data}, targetSizes)

	preparedIconCache.Lock()
	defer preparedIconCache.Unlock()
	for index, key := range keys {
		entry := preparedIconCache.entries[key]
		if entry == nil || entry.err != nil || entry.image == nil {
			t.Fatalf("prewarm target %d was not cached: %#v", targetSizes[index], entry)
		}
		select {
		case <-entry.ready:
		default:
			t.Fatalf("prewarm target %d returned before raster completion", targetSizes[index])
		}
		size := entry.image.Bounds().Size()
		if size.X != int(targetSizes[index]) ||
			size.Y != int(targetSizes[index]) {
			t.Fatalf(
				"prewarm raster %d size = %v, want %dx%d",
				index,
				size,
				targetSizes[index],
				targetSizes[index],
			)
		}
	}
}

func TestPrepareIconsRejectsInvalidPNG(t *testing.T) {
	t.Parallel()
	_, err := prepareIcons(
		[]Item{{ID: "bad", Label: "Bad", IconPNG: []byte("not png")}},
		22,
	)
	if err == nil {
		t.Fatal("invalid PNG accepted")
	}
}

func TestNativeInteropLayouts(t *testing.T) {
	t.Parallel()
	if got := unsafe.Sizeof(accentPolicy{}); got != 16 {
		t.Fatalf("accentPolicy size = %d, want 16", got)
	}
	dataSize := unsafe.Sizeof(compositionAttributeData{})
	if dataSize != 12 && dataSize != 24 {
		t.Fatalf("compositionAttributeData size = %d, want 12 or 24", dataSize)
	}
	if got := unsafe.Sizeof(dwmMargins{}); got != 16 {
		t.Fatalf("dwmMargins size = %d, want 16", got)
	}
}

func TestPixelAlphaBlend(t *testing.T) {
	t.Parallel()
	pixels := []byte{10, 20, 30, 255}
	blendPixel(pixels, 1, 1, 0, 0, 128, 0, 0, 128)
	if pixels[2] <= 30 || pixels[0] >= 10 || pixels[3] != 255 {
		t.Fatalf("blend result BGRA = %v", pixels)
	}

	transparent := []byte{0, 0, 0, 0}
	blendPixel(transparent, 1, 1, 0, 0, 64, 32, 16, 128)
	if want := []byte{16, 32, 64, 128}; !equalBytes(transparent, want) {
		t.Fatalf("transparent blend BGRA = %v, want %v", transparent, want)
	}
}

func TestMaterialForegroundKeepsCornersAndHoverTranslucent(t *testing.T) {
	t.Parallel()

	dimensions := dimensions{
		dpi:          baseDPI,
		width:        100,
		height:       60,
		outerPadding: 6,
		itemHeight:   38,
		cornerRadius: 10,
		rows: []row{
			{index: 0, top: 6, bottom: 44},
		},
	}
	session := popupSession{
		options:    Options{Items: []Item{{ID: "one", Label: "One"}}},
		dimensions: dimensions,
		dark:       true,
		palette:    paletteFor(true),
		icons:      make([]preparedIcon, 1),
		hover:      newHoverAnimator(1),
		result:     Result{BackdropUsed: BackdropAcrylic},
	}
	session.hover.levels[0] = hoverLevelMaximum

	pixels := make([]byte, int(dimensions.width*dimensions.height*4))
	session.drawPixels(pixels)
	if alphaAt(pixels, dimensions.width, 0, 0) != 0 {
		t.Fatalf("top-left corner alpha = %d, want transparent", alphaAt(pixels, dimensions.width, 0, 0))
	}
	centerAlpha := alphaAt(pixels, dimensions.width, 50, 24)
	if centerAlpha <= materialSurfaceAlpha(BackdropAcrylic, true) || centerAlpha >= 255 {
		t.Fatalf("hover alpha = %d, want a translucent highlight over material", centerAlpha)
	}
}

func TestForegroundCacheReusesPresentationOnlyFrames(t *testing.T) {
	t.Parallel()

	session := &popupSession{
		hover:        newHoverAnimator(2),
		presentation: 96,
	}
	session.hover.levels[1] = 80
	buffer := &foregroundBuffer{
		frameReady:  true,
		hoverLevels: []uint8{0, 80},
	}
	if foregroundFrameNeedsRebuild(buffer, session) {
		t.Fatal("prepared foreground unexpectedly required an initial rebuild")
	}

	// Entrance/exit changes only SourceConstantAlpha and must not rerasterize
	// labels, icons, or the rounded material plane.
	session.presentation = 224
	if foregroundFrameNeedsRebuild(buffer, session) {
		t.Fatal("presentation-only frame invalidated cached foreground pixels")
	}

	session.hover.levels[1] = 144
	if !foregroundFrameNeedsRebuild(buffer, session) {
		t.Fatal("hover-frame change did not invalidate cached foreground pixels")
	}
}

func TestSolidForegroundIsOpaqueInsideRoundedSurface(t *testing.T) {
	t.Parallel()

	session := popupSession{
		dimensions: dimensions{
			dpi:          baseDPI,
			width:        40,
			height:       30,
			cornerRadius: 8,
		},
		palette: paletteFor(false),
		result:  Result{BackdropUsed: BackdropSolid},
	}
	pixels := make([]byte, 40*30*4)
	session.drawPixels(pixels)
	if got := alphaAt(pixels, 40, 20, 15); got != 255 {
		t.Fatalf("solid center alpha = %d, want 255", got)
	}
	if got := alphaAt(pixels, 40, 0, 0); got != 0 {
		t.Fatalf("solid rounded corner alpha = %d, want 0", got)
	}
}

func TestPresentationEasingEndpointsAndMonotonicity(t *testing.T) {
	t.Parallel()

	if easeByte(0) != 0 || easeByte(255) != 255 {
		t.Fatalf("easing endpoints = %d, %d", easeByte(0), easeByte(255))
	}
	previous := uint8(0)
	for value := 1; value <= 255; value++ {
		current := easeByte(uint8(value))
		if current < previous {
			t.Fatalf("easeByte decreased at %d: %d -> %d", value, previous, current)
		}
		previous = current
	}
}

func alphaAt(pixels []byte, width, x, y int32) byte {
	return pixels[(int(y)*int(width)+int(x))*4+3]
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

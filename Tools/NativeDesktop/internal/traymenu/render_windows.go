//go:build windows

package traymenu

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/graphics"
)

const (
	maxDecodedIconPixels        = 4 << 20
	maxPreparedIconCacheEntries = 256
	maxIconPrewarmWorkers       = 4
)

type preparedIcon struct {
	image  image.Image
	handle win.HICON
}

type preparedIconCacheKey struct {
	digest     [sha256.Size]byte
	targetSize int32
}

type preparedIconCacheEntry struct {
	ready chan struct{}
	image image.Image
	err   error
}

type iconPrewarmJob struct {
	data       []byte
	targetSize int32
}

var preparedIconCache = struct {
	sync.Mutex
	entries map[preparedIconCacheKey]*preparedIconCacheEntry
}{
	entries: make(map[preparedIconCacheKey]*preparedIconCacheEntry),
}

type menuFont struct {
	handle win.HFONT
	bold   win.HFONT
}

// foregroundBuffer owns the popup's per-session GDI surfaces. The old paint
// path created and destroyed two DIB sections and two DCs, then rerasterized
// every glyph and icon on every 16 ms tick. Keeping one output DIB and caching
// immutable base/content planes makes presentation-only frames a single
// UpdateLayeredWindow call; hover frames only composite the changing row
// highlight between the cached planes.
type foregroundBuffer struct {
	width, height int32

	bitmap      win.HBITMAP
	memory      win.HDC
	oldBitmap   win.HGDIOBJ
	pixels      []byte
	maskBitmap  win.HBITMAP
	maskMemory  win.HDC
	oldMask     win.HGDIOBJ
	oldMaskFont win.HGDIOBJ
	mask        []byte
	base        []byte
	content     []byte
	hoverLevels []uint8
	frameReady  bool
}

func newMenuFont(dpi int32) (menuFont, error) {
	return newMenuFontForFamily(dpi, "")
}

func newMenuFontForFamily(dpi int32, family string) (menuFont, error) {
	var spec win.LOGFONT
	spec.LfHeight = -scale(15, dpi)
	spec.LfWeight = win.FW_NORMAL
	spec.LfCharSet = win.DEFAULT_CHARSET
	spec.LfOutPrecision = win.OUT_DEFAULT_PRECIS
	spec.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
	// Grayscale antialiasing can be converted into a correct alpha mask for the
	// layered foreground. ClearType's independent RGB subpixels cannot.
	spec.LfQuality = win.ANTIALIASED_QUALITY
	spec.LfPitchAndFamily = win.DEFAULT_PITCH | win.FF_DONTCARE
	family = strings.TrimSpace(family)
	if family == "" {
		family = "Segoe UI"
	}
	face, err := windows.UTF16FromString(family)
	if err != nil {
		return menuFont{}, err
	}
	copy(spec.LfFaceName[:], face)
	handle := win.CreateFontIndirect(&spec)
	if handle == 0 {
		return menuFont{}, errors.New("traymenu: CreateFontIndirect failed")
	}
	spec.LfWeight = win.FW_BOLD
	bold := win.CreateFontIndirect(&spec)
	if bold == 0 {
		win.DeleteObject(win.HGDIOBJ(handle))
		return menuFont{}, errors.New("traymenu: create default-item font")
	}
	return menuFont{handle: handle, bold: bold}, nil
}

func (f menuFont) close() {
	if f.bold != 0 {
		win.DeleteObject(win.HGDIOBJ(f.bold))
	}
	if f.handle != 0 {
		win.DeleteObject(win.HGDIOBJ(f.handle))
	}
}

func (f menuFont) measure(value string) int32 {
	return measureMenuText(f.handle, value)
}

func (f menuFont) measureBold(value string) int32 { return measureMenuText(f.bold, value) }

func measureMenuText(font win.HFONT, value string) int32 {
	if value == "" || font == 0 {
		return 0
	}
	text, err := windows.UTF16FromString(value)
	if err != nil {
		return 0
	}
	dc := win.GetDC(0)
	if dc == 0 {
		return int32(len([]rune(value))) * 8
	}
	defer win.ReleaseDC(0, dc)
	old := win.SelectObject(dc, win.HGDIOBJ(font))
	defer win.SelectObject(dc, old)
	var size win.SIZE
	if !win.GetTextExtentPoint32(dc, &text[0], int32(len(text)-1), &size) {
		return int32(len([]rune(value))) * 8
	}
	runtime.KeepAlive(text)
	return size.CX
}

func prepareIcons(items []Item, targetSize int32) ([]preparedIcon, error) {
	if targetSize < 1 {
		return nil, fmt.Errorf("traymenu: invalid target icon size %d", targetSize)
	}
	icons := make([]preparedIcon, len(items))
	for index, item := range items {
		if item.Separator {
			continue
		}
		if len(item.IconPNG) == 0 {
			icons[index].handle = win.HICON(item.IconHandle)
			continue
		}
		prepared, err := cachedPreparedIcon(item.IconPNG, targetSize)
		if err != nil {
			return nil, fmt.Errorf("traymenu: prepare item %d PNG: %w", index, err)
		}
		icons[index].image = prepared
	}
	return icons, nil
}

// PrewarmIconPNGs prepares immutable menu artwork before the first popup
// gesture. It is safe to call from a background goroutine: decoding and
// scaling are pure Go, and no HWND or GDI object is touched.
func PrewarmIconPNGs(iconPNGs [][]byte, targetSizes []int32) {
	jobs := buildIconPrewarmJobs(iconPNGs, targetSizes)
	workerCount := iconPrewarmWorkerCount(len(jobs))
	if workerCount == 0 {
		return
	}

	queue := make(chan iconPrewarmJob, len(jobs))
	for _, job := range jobs {
		queue <- job
	}
	close(queue)

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for job := range queue {
				_, _ = cachedPreparedIcon(job.data, job.targetSize)
			}
		}()
	}
	workers.Wait()
}

func buildIconPrewarmJobs(
	iconPNGs [][]byte,
	targetSizes []int32,
) []iconPrewarmJob {
	if len(iconPNGs) == 0 || len(targetSizes) == 0 {
		return nil
	}
	jobs := make(
		[]iconPrewarmJob,
		0,
		len(iconPNGs)*len(targetSizes),
	)
	seen := make(map[preparedIconCacheKey]struct{}, cap(jobs))
	for _, targetSize := range targetSizes {
		if targetSize < 1 {
			continue
		}
		for _, data := range iconPNGs {
			if len(data) == 0 {
				continue
			}
			key := preparedIconCacheKey{
				digest:     sha256.Sum256(data),
				targetSize: targetSize,
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			jobs = append(jobs, iconPrewarmJob{
				data:       data,
				targetSize: targetSize,
			})
		}
	}
	return jobs
}

func iconPrewarmWorkerCount(jobCount int) int {
	if jobCount < 1 {
		return 0
	}
	if jobCount < maxIconPrewarmWorkers {
		return jobCount
	}
	return maxIconPrewarmWorkers
}

func cachedPreparedIcon(data []byte, targetSize int32) (image.Image, error) {
	if targetSize < 1 {
		return nil, fmt.Errorf("invalid target icon size %d", targetSize)
	}
	key := preparedIconCacheKey{
		digest:     sha256.Sum256(data),
		targetSize: targetSize,
	}

	preparedIconCache.Lock()
	if entry := preparedIconCache.entries[key]; entry != nil {
		preparedIconCache.Unlock()
		<-entry.ready
		return entry.image, entry.err
	}
	if len(preparedIconCache.entries) >= maxPreparedIconCacheEntries {
		// Menu artwork is tiny and normally stable. A bounded wholesale
		// rotation keeps arbitrary API callers from turning it into an
		// unbounded process-lifetime image cache.
		preparedIconCache.entries =
			make(map[preparedIconCacheKey]*preparedIconCacheEntry)
	}
	entry := &preparedIconCacheEntry{ready: make(chan struct{})}
	preparedIconCache.entries[key] = entry
	preparedIconCache.Unlock()

	entry.image, entry.err = decodeAndScaleMenuIcon(data, targetSize)

	preparedIconCache.Lock()
	if entry.err != nil && preparedIconCache.entries[key] == entry {
		delete(preparedIconCache.entries, key)
	}
	close(entry.ready)
	preparedIconCache.Unlock()
	return entry.image, entry.err
}

func decodeAndScaleMenuIcon(data []byte, targetSize int32) (image.Image, error) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 || width > 4096 || height > 4096 ||
		int64(width)*int64(height) > maxDecodedIconPixels {
		return nil, errors.New("PNG dimensions are unsafe")
	}
	scaleFactor := math.Min(
		float64(targetSize)/float64(width),
		float64(targetSize)/float64(height),
	)
	targetWidth := max32(1, int32(math.Round(float64(width)*scaleFactor)))
	targetHeight := max32(1, int32(math.Round(float64(height)*scaleFactor)))
	scaled, err := graphics.Resize(
		decoded,
		int(targetWidth),
		int(targetHeight),
	)
	if err != nil {
		return nil, fmt.Errorf("scale PNG: %w", err)
	}
	return scaled, nil
}

func (s *popupSession) paint() {
	var paint win.PAINTSTRUCT
	destination := win.BeginPaint(s.hwnd, &paint)
	if destination == 0 {
		return
	}
	defer win.EndPaint(s.hwnd, &paint)

	if err := s.paintToDC(destination); err != nil {
		s.failRender()
	}
}

func (s *popupSession) paintToDC(destination win.HDC) error {
	// Black pixels in a fully extended DWM frame are the transparent native
	// material host. The visible menu content is composed independently by the
	// layered child below. Solid mode instead uses the exact surface color so
	// its entrance fade can never expose a black or white intermediate frame.
	width, height := s.dimensions.width, s.dimensions.height
	var err error
	if s.result.BackdropUsed == BackdropSolid {
		err = fillColorClient(
			destination,
			width,
			height,
			s.palette.background,
		)
	} else {
		err = fillBlackClient(destination, width, height)
	}
	if err != nil {
		return err
	}
	if s.overlay == 0 {
		return nil
	}
	return s.renderForeground(destination)
}

func (s *popupSession) renderForeground(destination win.HDC) error {
	if s == nil {
		return errors.New("traymenu: foreground session is unavailable")
	}
	if s.foreground == nil {
		buffer, err := newForegroundBuffer(s, destination)
		if err != nil {
			return err
		}
		s.foreground = buffer
	}
	return s.foreground.present(s)
}

func (s *popupSession) presentForeground() error {
	if s == nil || s.foreground == nil {
		return errors.New("traymenu: foreground frame was not prepared")
	}
	return s.foreground.present(s)
}

func (s *popupSession) releaseForeground() {
	if s == nil || s.foreground == nil {
		return
	}
	s.foreground.close()
	s.foreground = nil
}

func newForegroundBuffer(
	session *popupSession,
	destination win.HDC,
) (*foregroundBuffer, error) {
	if session == nil || destination == 0 {
		return nil, errors.New("traymenu: invalid foreground buffer target")
	}
	width, height := session.dimensions.width, session.dimensions.height
	header := win.BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})),
		BiWidth:       width,
		BiHeight:      -height,
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: win.BI_RGB,
	}
	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(
		destination,
		&header,
		win.DIB_RGB_COLORS,
		&bits,
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return nil, errors.New("traymenu: CreateDIBSection for foreground failed")
	}

	memory := win.CreateCompatibleDC(destination)
	if memory == 0 {
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: CreateCompatibleDC for foreground failed")
	}
	oldBitmap := win.SelectObject(memory, win.HGDIOBJ(bitmap))
	if oldBitmap == 0 || oldBitmap == win.HGDIOBJ(^uintptr(0)) {
		win.DeleteDC(memory)
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: select foreground DIB failed")
	}

	byteCount := int64(width) * int64(height) * 4
	if byteCount < 1 || byteCount > int64(^uint(0)>>1) {
		win.SelectObject(memory, oldBitmap)
		win.DeleteDC(memory)
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: foreground dimensions overflow")
	}
	pixels := unsafe.Slice((*byte)(bits), int(byteCount))

	var maskBits unsafe.Pointer
	maskBitmap := win.CreateDIBSection(
		memory,
		&header,
		win.DIB_RGB_COLORS,
		&maskBits,
		0,
		0,
	)
	if maskBitmap == 0 || maskBits == nil {
		win.SelectObject(memory, oldBitmap)
		win.DeleteDC(memory)
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: CreateDIBSection for text mask failed")
	}
	maskMemory := win.CreateCompatibleDC(memory)
	if maskMemory == 0 {
		win.DeleteObject(win.HGDIOBJ(maskBitmap))
		win.SelectObject(memory, oldBitmap)
		win.DeleteDC(memory)
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: CreateCompatibleDC for text mask failed")
	}
	oldMask := win.SelectObject(maskMemory, win.HGDIOBJ(maskBitmap))
	if oldMask == 0 || oldMask == win.HGDIOBJ(^uintptr(0)) {
		win.DeleteDC(maskMemory)
		win.DeleteObject(win.HGDIOBJ(maskBitmap))
		win.SelectObject(memory, oldBitmap)
		win.DeleteDC(memory)
		win.DeleteObject(win.HGDIOBJ(bitmap))
		return nil, errors.New("traymenu: select text-mask DIB failed")
	}
	oldMaskFont := win.SelectObject(
		maskMemory,
		win.HGDIOBJ(session.font.handle),
	)
	win.SetBkMode(maskMemory, win.TRANSPARENT)
	win.SetTextColor(maskMemory, colorRef(rgb{r: 255, g: 255, b: 255}))

	buffer := &foregroundBuffer{
		width:       width,
		height:      height,
		bitmap:      bitmap,
		memory:      memory,
		oldBitmap:   oldBitmap,
		pixels:      pixels,
		maskBitmap:  maskBitmap,
		maskMemory:  maskMemory,
		oldMask:     oldMask,
		oldMaskFont: oldMaskFont,
		mask:        unsafe.Slice((*byte)(maskBits), int(byteCount)),
		base:        make([]byte, int(byteCount)),
		content:     make([]byte, int(byteCount)),
		hoverLevels: make([]uint8, len(session.hover.levels)),
	}
	session.drawBasePixels(buffer.base)
	session.drawStaticPixels(buffer.content)
	if err := session.drawAlphaContent(
		buffer.maskMemory,
		buffer.mask,
		buffer.content,
	); err != nil {
		buffer.close()
		return nil, err
	}
	return buffer, nil
}

func (buffer *foregroundBuffer) close() {
	if buffer == nil {
		return
	}
	if buffer.maskMemory != 0 {
		if buffer.oldMaskFont != 0 &&
			buffer.oldMaskFont != win.HGDIOBJ(^uintptr(0)) {
			win.SelectObject(buffer.maskMemory, buffer.oldMaskFont)
		}
		if buffer.oldMask != 0 &&
			buffer.oldMask != win.HGDIOBJ(^uintptr(0)) {
			win.SelectObject(buffer.maskMemory, buffer.oldMask)
		}
		win.DeleteDC(buffer.maskMemory)
	}
	if buffer.maskBitmap != 0 {
		win.DeleteObject(win.HGDIOBJ(buffer.maskBitmap))
	}
	if buffer.memory != 0 {
		if buffer.oldBitmap != 0 &&
			buffer.oldBitmap != win.HGDIOBJ(^uintptr(0)) {
			win.SelectObject(buffer.memory, buffer.oldBitmap)
		}
		win.DeleteDC(buffer.memory)
	}
	if buffer.bitmap != 0 {
		win.DeleteObject(win.HGDIOBJ(buffer.bitmap))
	}
	buffer.bitmap = 0
	buffer.memory = 0
	buffer.maskBitmap = 0
	buffer.maskMemory = 0
	buffer.pixels = nil
	buffer.mask = nil
	buffer.base = nil
	buffer.content = nil
	buffer.hoverLevels = nil
	buffer.frameReady = false
}

func (buffer *foregroundBuffer) present(session *popupSession) error {
	if buffer == nil || session == nil || buffer.memory == 0 {
		return errors.New("traymenu: foreground buffer is unavailable")
	}
	if foregroundFrameNeedsRebuild(buffer, session) {
		copy(buffer.pixels, buffer.base)
		session.drawHoverPixels(buffer.pixels)
		compositePremultipliedSurface(buffer.pixels, buffer.content)
		copy(buffer.hoverLevels, session.hover.levels)
		buffer.frameReady = true
	}
	if err := updateLayeredChild(
		session.overlay,
		buffer.memory,
		buffer.width,
		buffer.height,
		easeByte(session.presentation),
	); err != nil {
		return err
	}
	runtime.KeepAlive(buffer.pixels)
	runtime.KeepAlive(buffer.mask)
	return nil
}

func foregroundFrameNeedsRebuild(
	buffer *foregroundBuffer,
	session *popupSession,
) bool {
	return buffer == nil || session == nil || !buffer.frameReady ||
		!equalHoverLevels(buffer.hoverLevels, session.hover.levels)
}

func equalHoverLevels(first, second []uint8) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func compositePremultipliedSurface(destination, source []byte) {
	limit := min(len(destination), len(source))
	limit -= limit % 4
	for index := 0; index < limit; index += 4 {
		alpha := source[index+3]
		if alpha == 0 {
			continue
		}
		inverse := uint32(255 - alpha)
		destination[index] = byte(min(
			255,
			int(source[index])+int(uint32(destination[index])*inverse/255),
		))
		destination[index+1] = byte(min(
			255,
			int(source[index+1])+int(uint32(destination[index+1])*inverse/255),
		))
		destination[index+2] = byte(min(
			255,
			int(source[index+2])+int(uint32(destination[index+2])*inverse/255),
		))
		destination[index+3] = byte(min(
			255,
			int(alpha)+int(uint32(destination[index+3])*inverse/255),
		))
	}
}

func (s *popupSession) drawBasePixels(pixels []byte) {
	d := s.dimensions
	clear(pixels)
	if s.result.BackdropUsed == BackdropSolid {
		fillRoundedRectAlpha(
			pixels,
			d.width,
			d.height,
			0,
			0,
			d.width,
			d.height,
			d.cornerRadius,
			s.palette.background,
			255,
		)
	} else {
		fillRoundedRectAlpha(
			pixels,
			d.width,
			d.height,
			0,
			0,
			d.width,
			d.height,
			d.cornerRadius,
			s.palette.background,
			materialSurfaceAlpha(s.result.BackdropUsed, s.dark),
		)
	}

	for _, row := range d.rows {
		item := s.options.Items[row.index]
		if item.Separator {
			y := (row.top + row.bottom) / 2
			drawHorizontalLineAlpha(
				pixels,
				d.width,
				d.height,
				d.outerPadding+scale(10, d.dpi),
				d.width-d.outerPadding-scale(10, d.dpi),
				y,
				s.palette.separator,
				132,
			)
		}
	}
}

func (s *popupSession) drawHoverPixels(pixels []byte) {
	d := s.dimensions
	for _, row := range d.rows {
		item := s.options.Items[row.index]
		if item.Separator {
			continue
		}
		if alpha := s.hover.alpha(
			row.index,
			hoverMaximumAlpha(s.dark),
		); alpha != 0 {
			fillRoundedRectAlpha(
				pixels,
				d.width,
				d.height,
				d.outerPadding+scale(2, d.dpi),
				row.top+scale(1, d.dpi),
				d.width-d.outerPadding-scale(2, d.dpi),
				row.bottom-scale(1, d.dpi),
				scale(7, d.dpi),
				s.palette.hover,
				alpha,
			)
		}
	}
}

func (s *popupSession) drawStaticPixels(pixels []byte) {
	d := s.dimensions
	clear(pixels)
	for _, row := range d.rows {
		item := s.options.Items[row.index]
		if item.Separator {
			continue
		}
		opacity := byte(255)
		if item.Disabled {
			opacity = 105
		}
		if item.Checked {
			checkY := (row.top + row.bottom) / 2
			drawCheckMark(
				pixels,
				d.width,
				d.height,
				d.checkX,
				checkY,
				scale(2, d.dpi),
				d.dpi,
				s.palette.check,
			)
		}
		if icon := s.icons[row.index].image; icon != nil {
			y := row.top + (d.itemHeight-d.iconSize)/2
			drawScaledIcon(
				pixels,
				d.width,
				d.height,
				d.iconX,
				y,
				d.iconSize,
				d.iconSize,
				icon,
				opacity,
			)
		}
	}
	strokeRoundedRectAlpha(
		pixels,
		d.width,
		d.height,
		0,
		0,
		d.width,
		d.height,
		d.cornerRadius,
		max32(1, scale(1, d.dpi)),
		s.palette.border,
		176,
	)
}

// drawPixels retains the complete non-text foreground composition used by
// deterministic raster tests and diagnostic callers. Animation frames use
// the split cached planes above so immutable pixels are not regenerated.
func (s *popupSession) drawPixels(pixels []byte) {
	if s == nil {
		return
	}
	s.drawBasePixels(pixels)
	static := make([]byte, len(pixels))
	s.drawStaticPixels(static)
	s.drawHoverPixels(pixels)
	compositePremultipliedSurface(pixels, static)
}

func materialSurfaceAlpha(backdrop Backdrop, dark bool) byte {
	switch backdrop {
	case BackdropBlur:
		if dark {
			return 56
		}
		return 82
	case BackdropAcrylic, BackdropAuto:
		if dark {
			return 24
		}
		return 30
	default:
		return 255
	}
}

func hoverMaximumAlpha(dark bool) byte {
	if dark {
		return 94
	}
	return 82
}

func (s *popupSession) drawAlphaContent(
	dc win.HDC,
	mask, destination []byte,
) error {
	d := s.dimensions
	byteCount := int64(d.width) * int64(d.height) * 4
	if byteCount < 1 ||
		byteCount > int64(len(mask)) ||
		byteCount > int64(len(destination)) {
		return errors.New("traymenu: text-mask dimensions overflow")
	}
	mask = mask[:int(byteCount)]

	for _, row := range d.rows {
		item := s.options.Items[row.index]
		if item.Separator {
			continue
		}

		if icon := s.icons[row.index]; icon.image == nil && icon.handle != 0 {
			clear(mask)
			y := row.top + (d.itemHeight-d.iconSize)/2
			win.DrawIconEx(
				dc,
				d.iconX,
				y,
				icon.handle,
				d.iconSize,
				d.iconSize,
				0,
				0,
				win.DI_NORMAL,
			)
			compositeIconMask(destination, mask, d.width, d.height)
		}

		textColor := s.palette.text
		shortcutColor := s.palette.secondary
		if item.Disabled {
			textColor = s.palette.disabled
			shortcutColor = s.palette.disabled
		}
		labelRect := win.RECT{
			Left:   d.textX,
			Top:    row.top,
			Right:  d.textRight,
			Bottom: row.bottom,
		}
		clear(mask)
		labelAlignment := uint32(win.DT_LEFT)
		if d.rightToLeft {
			labelAlignment = win.DT_RIGHT | win.DT_RTLREADING
		}
		labelFont := s.font.handle
		if item.Default {
			labelFont = s.font.bold
		}
		previousFont := win.SelectObject(dc, win.HGDIOBJ(labelFont))
		drawText(dc, item.Label, &labelRect, labelAlignment)
		win.SelectObject(dc, previousFont)
		compositeTextMask(
			destination,
			mask,
			d.width,
			d.height,
			labelRect,
			textColor,
		)

		if item.Shortcut != "" {
			shortcutRect := win.RECT{
				Left:   d.shortcutX,
				Top:    row.top,
				Right:  d.shortcutRight,
				Bottom: row.bottom,
			}
			clear(mask)
			shortcutAlignment := uint32(win.DT_RIGHT)
			if d.rightToLeft {
				shortcutAlignment = win.DT_LEFT
			}
			drawText(dc, item.Shortcut, &shortcutRect, shortcutAlignment)
			compositeTextMask(
				destination,
				mask,
				d.width,
				d.height,
				shortcutRect,
				shortcutColor,
			)
		}
	}
	runtime.KeepAlive(mask)
	return nil
}

func compositeTextMask(
	destination, mask []byte,
	width, height int32,
	rect win.RECT,
	color rgb,
) {
	left := clamp32(rect.Left, 0, width)
	top := clamp32(rect.Top, 0, height)
	right := clamp32(rect.Right, left, width)
	bottom := clamp32(rect.Bottom, top, height)
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			index := (int(y)*int(width) + int(x)) * 4
			coverage := maxByte(mask[index], maxByte(mask[index+1], mask[index+2]))
			if coverage == 0 {
				continue
			}
			blendPixel(
				destination,
				width,
				height,
				x,
				y,
				byte(uint16(color.r)*uint16(coverage)/255),
				byte(uint16(color.g)*uint16(coverage)/255),
				byte(uint16(color.b)*uint16(coverage)/255),
				coverage,
			)
		}
	}
}

func compositeIconMask(destination, source []byte, width, height int32) {
	hasAlpha := false
	for index := 3; index < len(source); index += 4 {
		if source[index] != 0 {
			hasAlpha = true
			break
		}
	}
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			index := (int(y)*int(width) + int(x)) * 4
			alpha := source[index+3]
			if !hasAlpha {
				alpha = maxByte(
					source[index],
					maxByte(source[index+1], source[index+2]),
				)
			}
			if alpha == 0 {
				continue
			}
			blue, green, red := source[index], source[index+1], source[index+2]
			if blue > alpha {
				blue = byte(uint16(blue) * uint16(alpha) / 255)
			}
			if green > alpha {
				green = byte(uint16(green) * uint16(alpha) / 255)
			}
			if red > alpha {
				red = byte(uint16(red) * uint16(alpha) / 255)
			}
			blendPixel(destination, width, height, x, y, red, green, blue, alpha)
		}
	}
}

func drawText(dc win.HDC, value string, rect *win.RECT, alignment uint32) {
	if value == "" {
		return
	}
	text, err := windows.UTF16FromString(value)
	if err != nil {
		return
	}
	flags := alignment | win.DT_VCENTER | win.DT_SINGLELINE | win.DT_NOPREFIX | win.DT_END_ELLIPSIS
	win.DrawTextEx(dc, &text[0], int32(len(text)-1), rect, flags, nil)
	runtime.KeepAlive(text)
}

func fillPixels(pixels []byte, width, height int32, color rgb) {
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			setPixel(pixels, width, height, x, y, color)
		}
	}
}

func setPixel(pixels []byte, width, height, x, y int32, color rgb) {
	if x < 0 || y < 0 || x >= width || y >= height {
		return
	}
	index := (int(y)*int(width) + int(x)) * 4
	pixels[index] = color.b
	pixels[index+1] = color.g
	pixels[index+2] = color.r
	pixels[index+3] = 255
}

func fillRoundedRect(pixels []byte, width, height, left, top, right, bottom, radius int32, color rgb) {
	fillRoundedRectAlpha(
		pixels,
		width,
		height,
		left,
		top,
		right,
		bottom,
		radius,
		color,
		255,
	)
}

func fillRoundedRectAlpha(
	pixels []byte,
	width, height, left, top, right, bottom, radius int32,
	color rgb,
	alpha byte,
) {
	if right <= left || bottom <= top {
		return
	}
	if radius < 0 {
		radius = 0
	}
	maxRadius := min32((right-left)/2, (bottom-top)/2)
	if radius > maxRadius {
		radius = maxRadius
	}
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			if insideRoundedRect(x, y, left, top, right, bottom, radius) {
				blendColor(pixels, width, height, x, y, color, alpha)
			}
		}
	}
}

func strokeRoundedRect(pixels []byte, width, height, left, top, right, bottom, radius, thickness int32, color rgb) {
	strokeRoundedRectAlpha(
		pixels,
		width,
		height,
		left,
		top,
		right,
		bottom,
		radius,
		thickness,
		color,
		255,
	)
}

func strokeRoundedRectAlpha(
	pixels []byte,
	width, height, left, top, right, bottom, radius, thickness int32,
	color rgb,
	alpha byte,
) {
	if thickness < 1 {
		thickness = 1
	}
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			outer := insideRoundedRect(x, y, left, top, right, bottom, radius)
			inner := insideRoundedRect(
				x,
				y,
				left+thickness,
				top+thickness,
				right-thickness,
				bottom-thickness,
				max32(0, radius-thickness),
			)
			if outer && !inner {
				blendColor(pixels, width, height, x, y, color, alpha)
			}
		}
	}
}

func insideRoundedRect(x, y, left, top, right, bottom, radius int32) bool {
	if x < left || x >= right || y < top || y >= bottom {
		return false
	}
	if radius <= 0 {
		return true
	}
	var centerX, centerY int32
	switch {
	case x < left+radius && y < top+radius:
		centerX, centerY = left+radius, top+radius
	case x >= right-radius && y < top+radius:
		centerX, centerY = right-radius-1, top+radius
	case x < left+radius && y >= bottom-radius:
		centerX, centerY = left+radius, bottom-radius-1
	case x >= right-radius && y >= bottom-radius:
		centerX, centerY = right-radius-1, bottom-radius-1
	default:
		return true
	}
	dx, dy := int64(x-centerX), int64(y-centerY)
	return dx*dx+dy*dy <= int64(radius)*int64(radius)
}

func drawHorizontalLine(pixels []byte, width, height, left, right, y int32, color rgb) {
	drawHorizontalLineAlpha(pixels, width, height, left, right, y, color, 255)
}

func drawHorizontalLineAlpha(
	pixels []byte,
	width, height, left, right, y int32,
	color rgb,
	alpha byte,
) {
	for x := left; x < right; x++ {
		blendColor(pixels, width, height, x, y, color, alpha)
	}
}

func drawCheckMark(pixels []byte, width, height, x, y, thickness, dpi int32, color rgb) {
	drawLine(pixels, width, height, x, y, x+scale(4, dpi), y+scale(4, dpi), thickness, color)
	drawLine(pixels, width, height, x+scale(4, dpi), y+scale(4, dpi), x+scale(11, dpi), y-scale(5, dpi), thickness, color)
}

func drawLine(pixels []byte, width, height, x0, y0, x1, y1, thickness int32, color rgb) {
	dx := abs32(x1 - x0)
	sx := int32(-1)
	if x0 < x1 {
		sx = 1
	}
	dy := -abs32(y1 - y0)
	sy := int32(-1)
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		radius := max32(0, thickness/2)
		for yy := -radius; yy <= radius; yy++ {
			for xx := -radius; xx <= radius; xx++ {
				setPixel(pixels, width, height, x0+xx, y0+yy, color)
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		twice := 2 * err
		if twice >= dy {
			err += dy
			x0 += sx
		}
		if twice <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawScaledIcon(pixels []byte, width, height, left, top, boxWidth, boxHeight int32, source image.Image, opacity byte) {
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth < 1 || sourceHeight < 1 || boxWidth < 1 || boxHeight < 1 {
		return
	}
	drawWidth := int32(sourceWidth)
	drawHeight := int32(sourceHeight)
	startX := left + (boxWidth-drawWidth)/2
	startY := top + (boxHeight-drawHeight)/2

	for y := int32(0); y < drawHeight; y++ {
		sourceY := bounds.Min.Y + int(y)
		for x := int32(0); x < drawWidth; x++ {
			sourceX := bounds.Min.X + int(x)
			r16, g16, b16, a16 := source.At(sourceX, sourceY).RGBA()
			alpha := uint32(a16>>8) * uint32(opacity) / 255
			if alpha == 0 {
				continue
			}
			r := uint32(r16>>8) * uint32(opacity) / 255
			g := uint32(g16>>8) * uint32(opacity) / 255
			b := uint32(b16>>8) * uint32(opacity) / 255
			blendPixel(
				pixels,
				width,
				height,
				startX+x,
				startY+y,
				byte(r),
				byte(g),
				byte(b),
				byte(alpha),
			)
		}
	}
}

func blendPixel(pixels []byte, width, height, x, y int32, sourceR, sourceG, sourceB, sourceA byte) {
	if x < 0 || y < 0 || x >= width || y >= height {
		return
	}
	index := (int(y)*int(width) + int(x)) * 4
	inverse := uint32(255 - sourceA)
	pixels[index] = byte(min32(255, int32(uint32(sourceB)+uint32(pixels[index])*inverse/255)))
	pixels[index+1] = byte(min32(255, int32(uint32(sourceG)+uint32(pixels[index+1])*inverse/255)))
	pixels[index+2] = byte(min32(255, int32(uint32(sourceR)+uint32(pixels[index+2])*inverse/255)))
	pixels[index+3] = byte(min32(
		255,
		int32(uint32(sourceA)+uint32(pixels[index+3])*inverse/255),
	))
}

func blendColor(
	pixels []byte,
	width, height, x, y int32,
	color rgb,
	alpha byte,
) {
	blendPixel(
		pixels,
		width,
		height,
		x,
		y,
		byte(uint16(color.r)*uint16(alpha)/255),
		byte(uint16(color.g)*uint16(alpha)/255),
		byte(uint16(color.b)*uint16(alpha)/255),
		alpha,
	)
}

func colorRef(color rgb) win.COLORREF {
	return win.COLORREF(uint32(color.r) | uint32(color.g)<<8 | uint32(color.b)<<16)
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func abs32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func clamp32(value, minimum, maximum int32) int32 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxByte(a, b byte) byte {
	if a > b {
		return a
	}
	return b
}

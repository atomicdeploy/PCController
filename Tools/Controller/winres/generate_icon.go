//go:build ignore

// generate_icon creates the canonical product-mark PNG and multi-size Windows
// ICO. The ICO is embedded into the executable and copied byte-for-byte to the
// browser favicon, keeping every operating-system surface on one asset.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/envfile"
)

var (
	backgroundA = color.RGBA{21, 18, 29, 255}
	backgroundB = color.RGBA{36, 24, 39, 255}
	surface     = color.RGBA{14, 12, 19, 255}
	white       = color.RGBA{248, 244, 255, 255}
	softWhite   = color.RGBA{216, 204, 255, 255}
	violet      = color.RGBA{139, 92, 246, 255}
	coral       = color.RGBA{236, 72, 153, 255}
	amber       = color.RGBA{245, 158, 11, 255}
	green       = color.RGBA{52, 211, 153, 255}
	neutral     = color.RGBA{148, 163, 184, 255}
)

func blend(a, b color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R)*(1-amount) + float64(b.R)*amount),
		G: uint8(float64(a.G)*(1-amount) + float64(b.G)*amount),
		B: uint8(float64(a.B)*(1-amount) + float64(b.B)*amount),
		A: uint8(float64(a.A)*(1-amount) + float64(b.A)*amount),
	}
}

func brandColor(amount float64) color.RGBA {
	if amount < 0.58 {
		return blend(violet, coral, amount/0.58)
	}
	return blend(coral, amber, (amount-0.58)/0.42)
}

func insideRoundedRect(x, y int, bounds image.Rectangle, radius int) bool {
	if !image.Pt(x, y).In(bounds) {
		return false
	}
	if x >= bounds.Min.X+radius && x < bounds.Max.X-radius {
		return true
	}
	if y >= bounds.Min.Y+radius && y < bounds.Max.Y-radius {
		return true
	}
	cx := bounds.Min.X + radius
	if x >= bounds.Max.X-radius {
		cx = bounds.Max.X - radius - 1
	}
	cy := bounds.Min.Y + radius
	if y >= bounds.Max.Y-radius {
		cy = bounds.Max.Y - radius - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func fillRoundedRect(canvas *image.RGBA, bounds image.Rectangle, radius int, paint func(x, y int) color.RGBA) {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if insideRoundedRect(x, y, bounds, radius) {
				canvas.SetRGBA(x, y, paint(x, y))
			}
		}
	}
}

func fillRect(canvas *image.RGBA, bounds image.Rectangle, value color.RGBA) {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			canvas.SetRGBA(x, y, value)
		}
	}
}

func cloneRGBA(source *image.RGBA) *image.RGBA {
	target := image.NewRGBA(source.Bounds())
	copy(target.Pix, source.Pix)
	return target
}

func fillCircle(canvas *image.RGBA, center image.Point, radius int, value color.RGBA) {
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			dx, dy := x-center.X, y-center.Y
			if dx*dx+dy*dy <= radius*radius && image.Pt(x, y).In(canvas.Bounds()) {
				canvas.SetRGBA(x, y, value)
			}
		}
	}
}

func intAbs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func drawLine(canvas *image.RGBA, from, to image.Point, thickness int, value color.RGBA) {
	dx, dy := intAbs(to.X-from.X), intAbs(to.Y-from.Y)
	stepX, stepY := 1, 1
	if from.X > to.X {
		stepX = -1
	}
	if from.Y > to.Y {
		stepY = -1
	}
	err := dx - dy
	for {
		fillCircle(canvas, from, thickness/2, value)
		if from == to {
			return
		}
		double := 2 * err
		if double > -dy {
			err -= dy
			from.X += stepX
		}
		if double < dx {
			err += dx
			from.Y += stepY
		}
	}
}

func stateIcon(source *image.RGBA, state string) *image.RGBA {
	target := cloneRGBA(source)
	badge := surface
	switch state {
	case "connected":
		badge = green
	case "reconnecting":
		badge = violet
	case "paused":
		badge = amber
	case "offline":
		badge = neutral
	}
	fillRoundedRect(target, image.Rect(153, 153, 241, 241), 25, func(_, _ int) color.RGBA { return white })
	fillRoundedRect(target, image.Rect(159, 159, 235, 235), 20, func(_, _ int) color.RGBA { return badge })
	switch state {
	case "connected":
		drawLine(target, image.Pt(177, 198), image.Pt(192, 213), 8, surface)
		drawLine(target, image.Pt(192, 213), image.Pt(220, 180), 8, surface)
	case "reconnecting":
		drawLine(target, image.Pt(176, 190), image.Pt(192, 176), 7, white)
		drawLine(target, image.Pt(192, 176), image.Pt(207, 190), 7, white)
		drawLine(target, image.Pt(218, 207), image.Pt(202, 221), 7, white)
		drawLine(target, image.Pt(202, 221), image.Pt(187, 207), 7, white)
	case "paused":
		fillRoundedRect(target, image.Rect(180, 177, 192, 218), 4, func(_, _ int) color.RGBA { return surface })
		fillRoundedRect(target, image.Rect(202, 177, 214, 218), 4, func(_, _ int) color.RGBA { return surface })
	case "offline":
		fillRoundedRect(target, image.Rect(176, 191, 218, 205), 5, func(_, _ int) color.RGBA { return surface })
	}
	return target
}

func writeStateICOs(path string, source *image.RGBA) error {
	extension := filepath.Ext(path)
	stem := strings.TrimSuffix(path, extension)
	for _, state := range []string{"connected", "reconnecting", "paused", "offline"} {
		if err := writeICO(stem+"-"+state+extension, stateIcon(source, state)); err != nil {
			return fmt.Errorf("write %s state icon: %w", state, err)
		}
	}
	return nil
}

func resizeRGBA(source *image.RGBA, size int) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, size, size))
	scaleX := float64(source.Bounds().Dx()) / float64(size)
	scaleY := float64(source.Bounds().Dy()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sourceX := (float64(x)+0.5)*scaleX - 0.5
			sourceY := (float64(y)+0.5)*scaleY - 0.5
			x0 := max(0, min(source.Bounds().Dx()-1, int(math.Floor(sourceX))))
			y0 := max(0, min(source.Bounds().Dy()-1, int(math.Floor(sourceY))))
			x1 := min(source.Bounds().Dx()-1, x0+1)
			y1 := min(source.Bounds().Dy()-1, y0+1)
			fx := math.Max(0, math.Min(1, sourceX-float64(x0)))
			fy := math.Max(0, math.Min(1, sourceY-float64(y0)))
			c00 := source.RGBAAt(x0, y0)
			c10 := source.RGBAAt(x1, y0)
			c01 := source.RGBAAt(x0, y1)
			c11 := source.RGBAAt(x1, y1)
			channel := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bottom := float64(c)*(1-fx) + float64(d)*fx
				return uint8(math.Round(top*(1-fy) + bottom*fy))
			}
			target.SetRGBA(x, y, color.RGBA{
				R: channel(c00.R, c10.R, c01.R, c11.R),
				G: channel(c00.G, c10.G, c01.G, c11.G),
				B: channel(c00.B, c10.B, c01.B, c11.B),
				A: channel(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return target
}

func writeICO(path string, source *image.RGBA) error {
	sizes := []int{256, 128, 64, 48, 32, 24, 16}
	images := make([][]byte, len(sizes))
	for index, size := range sizes {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, resizeRGBA(source, size)); err != nil {
			return fmt.Errorf("encode %d px icon: %w", size, err)
		}
		images[index] = encoded.Bytes()
	}
	var output bytes.Buffer
	_ = binary.Write(&output, binary.LittleEndian, uint16(0))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(len(images)))
	offset := 6 + 16*len(images)
	for index, data := range images {
		sizeByte := byte(sizes[index])
		if sizes[index] == 256 {
			sizeByte = 0
		}
		_ = output.WriteByte(sizeByte)
		_ = output.WriteByte(sizeByte)
		_ = output.WriteByte(0)
		_ = output.WriteByte(0)
		_ = binary.Write(&output, binary.LittleEndian, uint16(1))
		_ = binary.Write(&output, binary.LittleEndian, uint16(32))
		_ = binary.Write(&output, binary.LittleEndian, uint32(len(data)))
		_ = binary.Write(&output, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range images {
		_, _ = output.Write(data)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write ICO: %w", err)
	}
	return nil
}

func main() {
	if _, err := envfile.LoadProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "environment:", err)
		os.Exit(1)
	}
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: go run generate_icon.go OUTPUT.png [OUTPUT.ico]")
		os.Exit(2)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 256, 256))
	fillRoundedRect(canvas, image.Rect(4, 4, 252, 252), 48, func(x, y int) color.RGBA {
		return blend(backgroundA, backgroundB, float64(x+y-8)/488)
	})
	fillRoundedRect(canvas, image.Rect(48, 49, 208, 207), 30, func(x, y int) color.RGBA {
		return brandColor(float64(x+y-97) / 316)
	})
	fillRoundedRect(canvas, image.Rect(64, 65, 192, 191), 20, func(_, _ int) color.RGBA { return surface })
	fillRoundedRect(canvas, image.Rect(86, 87, 170, 169), 18, func(x, y int) color.RGBA {
		return brandColor(float64(x+y-173) / 164)
	})
	fillRoundedRect(canvas, image.Rect(99, 100, 157, 156), 11, func(_, _ int) color.RGBA { return white })
	for index := 0; index < 6; index++ {
		y := 69 + index*23
		fillRect(canvas, image.Rect(31, y, 53, y+8), softWhite)
		fillRect(canvas, image.Rect(203, y, 225, y+8), softWhite)
		x := 68 + index*23
		fillRect(canvas, image.Rect(x, 32, x+8, 54), softWhite)
		fillRect(canvas, image.Rect(x, 202, x+8, 224), softWhite)
	}
	file, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := png.Encode(file, canvas); err != nil {
		panic(err)
	}
	if len(os.Args) == 3 {
		if err := writeICO(os.Args[2], canvas); err != nil {
			panic(err)
		}
		if err := writeStateICOs(os.Args[2], canvas); err != nil {
			panic(err)
		}
	}
}

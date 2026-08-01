//go:build ignore

// generate_icon creates the source PNG consumed by go-winres. It uses only the
// Go standard library so the Windows packaging path stays reproducible.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run generate_icon.go OUTPUT.png")
		os.Exit(2)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			dx, dy := x-128, y-128
			distance := dx*dx + dy*dy
			if distance > 124*124 {
				canvas.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
				continue
			}
			canvas.SetRGBA(
				x,
				y,
				color.RGBA{
					R: uint8(13 + y/16),
					G: uint8(42 + x/7),
					B: uint8(74 + (255-y)/5),
					A: 255,
				},
			)
		}
	}
	white := color.RGBA{235, 249, 255, 255}
	dark := color.RGBA{9, 31, 48, 255}
	accent := color.RGBA{75, 226, 196, 255}
	draw.Draw(canvas, image.Rect(55, 61, 201, 195), &image.Uniform{dark}, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(67, 73, 189, 183), &image.Uniform{white}, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(83, 90, 173, 166), &image.Uniform{dark}, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(96, 104, 160, 152), &image.Uniform{accent}, image.Point{}, draw.Src)
	for index := 0; index < 6; index++ {
		y := 73 + index*22
		draw.Draw(canvas, image.Rect(39, y, 55, y+8), &image.Uniform{white}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(201, y, 217, y+8), &image.Uniform{white}, image.Point{}, draw.Src)
		x := 67 + index*22
		draw.Draw(canvas, image.Rect(x, 45, x+8, 61), &image.Uniform{white}, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(x, 195, x+8, 211), &image.Uniform{white}, image.Point{}, draw.Src)
	}
	file, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := png.Encode(file, canvas); err != nil {
		panic(err)
	}
}

package graphics

import (
	"image"
	"testing"
)

func BenchmarkResizeAnimationFrame(b *testing.B) {
	source := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Resize(source, 192, 192); err != nil {
			b.Fatal(err)
		}
	}
}

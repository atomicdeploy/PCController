package native

import (
	"bytes"
	"testing"
)

func TestStripFrameChunksExactRGBAndSingleCommit(t *testing.T) {
	rgb := make([]byte, 300)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	frames, err := StripFramePayloads(rgb)
	if err != nil {
		t.Fatal(err)
	}
	var reconstructed []byte
	for _, chunk := range frames[:len(frames)-1] {
		if len(chunk) > MaxPayload || chunk[0] != 0xFD || int(chunk[1])*3 != len(reconstructed) {
			t.Fatalf("invalid chunk %v", chunk)
		}
		reconstructed = append(reconstructed, chunk[2:]...)
	}
	if !bytes.Equal(reconstructed, rgb) || !bytes.Equal(frames[len(frames)-1], []byte{0xFC}) {
		t.Fatal("frame changed or missing commit")
	}
	for _, length := range []int{0, 1, 301, 303} {
		if _, err := StripFramePayloads(make([]byte, length)); err == nil {
			t.Fatalf("accepted invalid size %d", length)
		}
	}
}

func TestStripFullBrightnessPreservesExactColor(t *testing.T) {
	payload, err := AddressableLEDPayload(99, 255, 128, 1, 255)
	if err != nil || !bytes.Equal(payload, []byte{99, 255, 128, 1, 255}) {
		t.Fatalf("full brightness changed RGB: %v %v", payload, err)
	}
	for _, count := range []int{0, 101, -1} {
		if _, err := StripConfigurePayload(count); err == nil {
			t.Fatalf("accepted count %d", count)
		}
	}
}

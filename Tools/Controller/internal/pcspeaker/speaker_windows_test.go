//go:build windows

package pcspeaker

import (
	"encoding/binary"
	"testing"
)

func TestWinRing0IOCTLValuesMatchOpenLibSys(t *testing.T) {
	if ioctlGetRefCount != 0x9C402004 || ioctlReadIOPortByte != 0x9C4060CC || ioctlWriteIOPortByte != 0x9C40A0D8 {
		t.Fatalf("unexpected IOCTLs ref=%#x read=%#x write=%#x", ioctlGetRefCount, ioctlReadIOPortByte, ioctlWriteIOPortByte)
	}
}

func TestWinRing0WritePortByteUsesFullAlignedInputStructure(t *testing.T) {
	input := winRing0WritePortByteInput(0x61, 0x03)
	if len(input) != 8 {
		t.Fatalf("input bytes=%d want 8", len(input))
	}
	if port := binary.LittleEndian.Uint32(input[:4]); port != 0x61 || input[4] != 0x03 {
		t.Fatalf("input port=%#x value=%#x", port, input[4])
	}
	for index, value := range input[5:] {
		if value != 0 {
			t.Fatalf("padding byte %d=%#x", index+5, value)
		}
	}
}

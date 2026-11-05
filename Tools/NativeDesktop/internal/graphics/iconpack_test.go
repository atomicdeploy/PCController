package graphics

import (
	"encoding/binary"
	"testing"
)

func TestDecodeIconPackAndSelectExactFrame(t *testing.T) {
	data := testIconPack(
		testIconFrame{size: 32, payload: []byte("thirty-two")},
		testIconFrame{size: 16, payload: []byte("sixteen")},
		testIconFrame{size: 64, payload: []byte("sixty-four")},
	)
	pack, err := DecodeIconPack(data)
	if err != nil {
		t.Fatalf("DecodeIconPack returned an error: %v", err)
	}
	frames := pack.Frames()
	if len(frames) != 3 || frames[0].Width != 16 ||
		frames[1].Width != 32 || frames[2].Width != 64 {
		t.Fatalf("Frames() = %#v, want frames ordered 16, 32, 64", frames)
	}
	frame, ok := pack.Select(32, 32)
	if !ok || string(frame.Data) != "thirty-two" {
		t.Fatalf("Select(32, 32) = %#v, %v", frame, ok)
	}
}

func TestIconPackSelectPrefersNearbyLargerFrame(t *testing.T) {
	pack, err := DecodeIconPack(testIconPack(
		testIconFrame{size: 16, payload: []byte("small")},
		testIconFrame{size: 24, payload: []byte("large")},
	))
	if err != nil {
		t.Fatal(err)
	}
	frame, ok := pack.Select(20, 20)
	if !ok || frame.Width != 24 {
		t.Fatalf("Select(20, 20) = %#v, %v; want 24px frame", frame, ok)
	}
}

func TestDecodeIconPackRejectsMalformedContainers(t *testing.T) {
	valid := testIconPack(testIconFrame{size: 16, payload: []byte("payload")})
	tests := []struct {
		name string
		data []byte
	}{
		{name: "truncated header", data: []byte{0, 0, 1}},
		{name: "cursor type", data: append([]byte(nil), valid...)},
		{name: "truncated directory", data: valid[:12]},
		{name: "duplicate dimensions", data: testIconPack(
			testIconFrame{size: 16, payload: []byte("one")},
			testIconFrame{size: 16, payload: []byte("two")},
		)},
		{name: "payload inside directory", data: append([]byte(nil), valid...)},
	}
	tests[1].data[2] = 2
	binary.LittleEndian.PutUint32(tests[4].data[18:22], 1)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeIconPack(test.data); err == nil {
				t.Fatal("DecodeIconPack unexpectedly accepted malformed input")
			}
		})
	}
}

type testIconFrame struct {
	size    byte
	payload []byte
}

func testIconPack(frames ...testIconFrame) []byte {
	headerSize := iconDirectoryHeaderSize + len(frames)*iconDirectoryEntrySize
	totalSize := headerSize
	for _, frame := range frames {
		totalSize += len(frame.payload)
	}
	data := make([]byte, totalSize)
	binary.LittleEndian.PutUint16(data[2:4], 1)
	binary.LittleEndian.PutUint16(data[4:6], uint16(len(frames)))
	payloadOffset := headerSize
	for index, frame := range frames {
		entry := iconDirectoryHeaderSize + index*iconDirectoryEntrySize
		data[entry] = frame.size
		data[entry+1] = frame.size
		binary.LittleEndian.PutUint16(data[entry+4:entry+6], 1)
		binary.LittleEndian.PutUint16(data[entry+6:entry+8], 32)
		binary.LittleEndian.PutUint32(data[entry+8:entry+12], uint32(len(frame.payload)))
		binary.LittleEndian.PutUint32(data[entry+12:entry+16], uint32(payloadOffset))
		copy(data[payloadOffset:], frame.payload)
		payloadOffset += len(frame.payload)
	}
	return data
}

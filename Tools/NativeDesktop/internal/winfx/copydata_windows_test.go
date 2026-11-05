//go:build windows

package winfx

import (
	"errors"
	"testing"
	"unsafe"
)

func TestParseCopyDataCopiesPayload(t *testing.T) {
	source := []byte(`{"method":"show"}`)
	packet := CopyDataPacket{
		Tag:  42,
		Size: uint32(len(source)),
		Data: unsafe.Pointer(&source[0]),
	}
	tag, got, err := ParseCopyData(&packet, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if tag != 42 || string(got) != string(source) {
		t.Fatalf("ParseCopyData = (%d, %q), want (42, %q)", tag, got, source)
	}
	source[0] = '!'
	if got[0] == '!' {
		t.Fatal("ParseCopyData returned receiver-owned memory instead of a copy")
	}
}

func TestParseCopyDataBounds(t *testing.T) {
	source := []byte("too large")
	packet := CopyDataPacket{
		Size: uint32(len(source)),
		Data: unsafe.Pointer(&source[0]),
	}
	_, _, err := ParseCopyData(&packet, 3)
	if !errors.Is(err, ErrCopyDataTooLarge) {
		t.Fatalf("ParseCopyData error = %v", err)
	}

	_, _, err = ParseCopyData(&CopyDataPacket{Size: 1}, 10)
	if !errors.Is(err, ErrInvalidCopyData) {
		t.Fatalf("nil data error = %v", err)
	}
}

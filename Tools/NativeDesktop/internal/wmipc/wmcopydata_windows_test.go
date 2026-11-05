//go:build windows

package wmipc

import (
	"encoding/json"
	"testing"
	"unsafe"

	"rayanlamp.local/app/internal/ipc"
)

func TestDecode(t *testing.T) {
	request, err := ipc.NewRequest("1", "toggle", map[string]bool{"force": true})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(request)
	data := copyDataStruct{
		Data:   copyDataMagic,
		Length: uint32(len(payload)),
		Bytes:  unsafe.Pointer(&payload[0]),
	}
	decoded, err := Decode(uintptr(unsafe.Pointer(&data)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "toggle" || decoded.ID != "1" {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeRejectsWrongMagicAndOversize(t *testing.T) {
	data := copyDataStruct{Data: 1, Length: 1, Bytes: unsafe.Pointer(&[]byte{1}[0])}
	if _, err := Decode(uintptr(unsafe.Pointer(&data))); err == nil {
		t.Fatal("expected wrong magic error")
	}
	data.Data = copyDataMagic
	data.Length = maxPayload + 1
	if _, err := Decode(uintptr(unsafe.Pointer(&data))); err == nil {
		t.Fatal("expected size error")
	}
}

func TestDecodeRejectsTrailingJSONData(t *testing.T) {
	request, err := ipc.NewRequest("1", "toggle", nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, suffix := range map[string][]byte{
		"concatenated value": payload,
		"trailing data":      []byte("not-json"),
	} {
		t.Run(name, func(t *testing.T) {
			input := append(append([]byte(nil), payload...), suffix...)
			data := copyDataStruct{
				Data:   copyDataMagic,
				Length: uint32(len(input)),
				Bytes:  unsafe.Pointer(&input[0]),
			}
			if _, err := Decode(uintptr(unsafe.Pointer(&data))); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}

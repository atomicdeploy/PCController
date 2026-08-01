package wsrelay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareMessageRoundTripAndTamper(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "firmware.hex")
	if err := os.WriteFile(path, []byte(":0100000001FE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, err := Load(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "firmware.hex" || string(decoded.Data) != string(message.Data) {
		t.Fatalf("unexpected decoded message: %#v", decoded)
	}

	tampered := strings.Replace(string(encoded), message.SHA256, strings.Repeat("0", 64), 1)
	if _, err := Decode([]byte(tampered), 1024); err == nil {
		t.Fatal("expected SHA-256 mismatch")
	}
}

func TestDecodeRejectsPathAndOversize(t *testing.T) {
	message := FirmwareMessage{
		Version: MessageVersion, Type: MessageType,
		Name: "../firmware.hex", Data: []byte{1},
	}
	message.SHA256 = "4bf5122f344554c53bde2ebb8cd2b7e3d1600ad631c385a5d7c3e0c5e8f0a5d7"
	if _, err := Encode(message); err == nil {
		t.Fatal("expected unsafe name rejection")
	}
}

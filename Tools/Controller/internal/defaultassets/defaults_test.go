package defaultassets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"testing/fstest"
)

func TestEmbeddedDefaultsAreUnavailableWhenBuildDidNotStageThem(t *testing.T) {
	bundle, err := loadFS(fstest.MapFS{"assets/.gitkeep": {Data: nil}})
	if err != nil || bundle.Enabled {
		t.Fatalf("unstaged defaults = %#v, %v", bundle, err)
	}
}

func TestEmbeddedDefaultsRequireCompleteVerifiedIntelHex(t *testing.T) {
	firmware := testIntelHex([]byte{1, 2, 3, 4})
	eeprom := testIntelHex(make([]byte, 1024))
	metadata := Metadata{
		Format:   "controller-embedded-defaults/v1",
		Firmware: testArtifact("firmware", "default-firmware.hex", firmware),
		EEPROM:   testArtifact("eeprom", "default-eeprom.hex", eeprom),
	}
	raw, _ := json.Marshal(metadata)
	bundle, err := loadFS(fstest.MapFS{
		metadataPath:                  {Data: raw},
		"assets/default-firmware.hex": {Data: firmware},
		"assets/default-eeprom.hex":   {Data: eeprom},
	})
	if err != nil || !bundle.Enabled {
		t.Fatalf("verified defaults = %#v, %v", bundle, err)
	}
	if !bytes.Equal(bundle.EEPROM.Data, eeprom) {
		t.Fatal("EEPROM bytes differ from verified embedded bytes")
	}

	metadata.EEPROM.Bytes--
	raw, _ = json.Marshal(metadata)
	if _, err := loadFS(fstest.MapFS{
		metadataPath:                  {Data: raw},
		"assets/default-firmware.hex": {Data: firmware},
		"assets/default-eeprom.hex":   {Data: eeprom},
	}); err == nil {
		t.Fatal("size-damaged EEPROM bundle was accepted")
	}
}

func testArtifact(kind, name string, content []byte) Artifact {
	digest := sha256.Sum256(content)
	return Artifact{Kind: kind, Name: name, File: name, SHA256: hex.EncodeToString(digest[:]), Bytes: len(content)}
}

func testIntelHex(data []byte) []byte {
	var result bytes.Buffer
	for address := 0; address < len(data); address += 16 {
		end := address + 16
		if end > len(data) {
			end = len(data)
		}
		record := []byte{byte(end - address), byte(address >> 8), byte(address), 0}
		record = append(record, data[address:end]...)
		var sum byte
		for _, value := range record {
			sum += value
		}
		record = append(record, byte(-sum))
		result.WriteByte(':')
		result.WriteString(hex.EncodeToString(record))
		result.WriteByte('\n')
	}
	result.WriteString(":00000001ff\n")
	return result.Bytes()
}

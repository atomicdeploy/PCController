package programmer

import (
	"bytes"
	"testing"
)

func TestGenerateDefaultEEPROMIntelHexCreatesSafeCurrentSettings(t *testing.T) {
	content, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		t.Fatal(err)
	}
	image, err := ParseIntelHex(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := image.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DataBytes != PCControllerEEPROMBytes || inspection.MinimumAddress != 0 ||
		inspection.MaximumAddress+1 != PCControllerEEPROMBytes {
		t.Fatalf("default EEPROM coverage = %#v", inspection)
	}
	record, err := image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	if record[EEPROMSettingsValueBytes] != avrCRC8(record[:EEPROMSettingsValueBytes]) {
		t.Fatal("default settings CRC does not match")
	}
	wantOrder := []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC}
	if !bytes.Equal(record[21:28], wantOrder) || record[28] != 0 {
		t.Fatalf("dense menu order / closed brightness = % X / %d", record[21:28], record[28])
	}
	if record[1] != 0 || record[6] != 0 {
		t.Fatalf("deployment defaults must keep illumination/PWM off: mode=%d pwm=%d", record[1], record[6])
	}
	if record[30] != 1 {
		t.Fatalf("default motion break=%d ms, want 1", record[30])
	}
	decoded := decodeOfflineSettings(image)
	if !decoded.Valid || decoded.Values.DefaultMenuPage != 0 || decoded.Values.VisibleMenuMask != DefaultVisibleMenuMask {
		t.Fatalf("generated default settings = %#v", decoded)
	}
	remotes := decodeOfflineRemotes(image)
	if !remotes.Valid || remotes.ValidCount != 0 || remotes.InvalidCount != 0 {
		t.Fatalf("generated default RF store = %#v", remotes)
	}
}

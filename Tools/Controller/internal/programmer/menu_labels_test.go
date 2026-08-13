package programmer

import "testing"

func filledEEPROM() []byte {
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	return data
}

func TestMenuLabelsLayoutUsesOnlyDeclaredFreeRegions(t *testing.T) {
	if EEPROMSettingsAddress+EEPROMSettingsRecordBytes != EEPROMMenuLabelsHeaderAddress ||
		EEPROMMenuLabelsHeaderEnd > EEPROMRemoteHeaderAddress {
		t.Fatalf("menu-label header 0x%04X..0x%04X collides with settings/RF layout",
			EEPROMMenuLabelsHeaderAddress, EEPROMMenuLabelsHeaderEnd-1)
	}
	if EEPROMMenuLabelsAddress != EEPROMStatusProfileAddress+uint32(EEPROMStatusProfileCount)*EEPROMStatusProfileRecordBytes ||
		EEPROMMenuLabelsCommitAddress != PCControllerEEPROMBytes-1 ||
		EEPROMMenuLabelsEnd != PCControllerEEPROMBytes {
		t.Fatalf("menu-label payload/commit boundaries = 0x%04X..0x%04X end=0x%04X",
			EEPROMMenuLabelsAddress, EEPROMMenuLabelsCommitAddress, EEPROMMenuLabelsEnd)
	}
}

func TestMenuLabelsVersionedCRCWritePlanIsAtomic(t *testing.T) {
	labels := []byte(defaultEEPROMMenuLabels)
	if got := menuLabelsCRC(labels); got != 0x8B {
		t.Fatalf("factory menu-label CRC = 0x%02X, want cross-engine vector 0x8B", got)
	}

	data := filledEEPROM()
	if err := applyMenuLabelsWritePlan(data, labels); err != nil {
		t.Fatal(err)
	}
	if !validMenuLabelsRecord(data) {
		t.Fatal("fully committed factory label record was rejected")
	}

	replacement := append([]byte(nil), labels...)
	replacement[0] = 'D'
	plan, err := menuLabelsWritePlan(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan), int(EEPROMMenuLabelBytes)+3; got != want {
		t.Fatalf("write plan length=%d, want invalid + payload + header + commit = %d", got, want)
	}
	if plan[0] != (EEPROMByteWrite{Address: EEPROMMenuLabelsCommitAddress, Value: eepromMenuLabelsInvalidCommit}) ||
		plan[len(plan)-1] != (EEPROMByteWrite{Address: EEPROMMenuLabelsCommitAddress, Value: EEPROMMenuLabelsFormatMarker}) {
		t.Fatalf("write plan does not invalidate first and commit last: first=%#v last=%#v", plan[0], plan[len(plan)-1])
	}

	// Start from a valid old record and simulate power loss after every write.
	// Each partial update must be unavailable; only the final commit may pass.
	for prefix := 1; prefix < len(plan); prefix++ {
		torn := append([]byte(nil), data...)
		for _, write := range plan[:prefix] {
			torn[write.Address] = write.Value
		}
		if validMenuLabelsRecord(torn) {
			t.Fatalf("torn write prefix %d/%d became valid", prefix, len(plan))
		}
	}
	for _, write := range plan {
		data[write.Address] = write.Value
	}
	if !validMenuLabelsRecord(data) || data[EEPROMMenuLabelsAddress] != 'D' {
		t.Fatalf("committed replacement rejected or wrong: valid=%t first=%q", validMenuLabelsRecord(data), data[EEPROMMenuLabelsAddress])
	}

	for _, address := range []uint32{
		EEPROMMenuLabelsCRCAddress,
		EEPROMMenuLabelsCommitAddress,
		EEPROMMenuLabelsAddress + EEPROMMenuLabelBytes - 1,
	} {
		corrupt := append([]byte(nil), data...)
		corrupt[address] ^= 0x01
		if validMenuLabelsRecord(corrupt) {
			t.Fatalf("single-bit corruption at 0x%04X became valid", address)
		}
	}

	// These are the two printable corruption classes that the former XOR byte
	// could not detect: a transposition, and matching XOR deltas in two cells.
	transposed := append([]byte(nil), data...)
	transposed[EEPROMMenuLabelsAddress], transposed[EEPROMMenuLabelsAddress+1] =
		transposed[EEPROMMenuLabelsAddress+1], transposed[EEPROMMenuLabelsAddress]
	if validMenuLabelsRecord(transposed) {
		t.Fatal("printable label transposition became valid")
	}
	equalDelta := append([]byte(nil), data...)
	equalDelta[EEPROMMenuLabelsAddress] ^= 0x01
	equalDelta[EEPROMMenuLabelsAddress+1] ^= 0x01
	if validMenuLabelsRecord(equalDelta) {
		t.Fatal("two-cell equal-delta corruption became valid")
	}
}

func TestMenuLabelsWritePlanRejectsInvalidPayload(t *testing.T) {
	if _, err := menuLabelsWritePlan(make([]byte, EEPROMMenuLabelBytes-1)); err == nil {
		t.Fatal("short label payload unexpectedly accepted")
	}
	labels := []byte(defaultEEPROMMenuLabels)
	labels[17] = '\n'
	if _, err := menuLabelsWritePlan(labels); err == nil {
		t.Fatal("non-printable label payload unexpectedly accepted")
	}
}

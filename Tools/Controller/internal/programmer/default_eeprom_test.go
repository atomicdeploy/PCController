package programmer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/native"
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
	if record[19]&0x07 != 0 {
		t.Fatalf("closed brightness = %d", record[19]&0x07)
	}
	if record[1] != 0 || record[6] != 0 {
		t.Fatalf("deployment defaults must keep illumination/PWM off: mode=%d pwm=%d", record[1], record[6])
	}
	if record[21] != 1 {
		t.Fatalf("default motion break=%d ms, want 1", record[21])
	}
	decoded := decodeOfflineSettings(image)
	if !decoded.Valid || decoded.Schema != EEPROMSettingsRecordSchema || decoded.Values.DefaultMenuPage != 0 {
		t.Fatalf("generated default settings = %#v", decoded)
	}
	remotes := decodeOfflineRemotes(image)
	if !remotes.Valid || remotes.ValidCount != 0 || remotes.InvalidCount != 0 {
		t.Fatalf("generated default RF store = %#v", remotes)
	}
	profiles := native.DefaultStatusProfiles(native.FactoryStatusBrightness)
	for condition, want := range profiles {
		record, err := image.BytesAt(
			EEPROMStatusProfileAddress+uint32(condition)*EEPROMStatusProfileRecordBytes,
			EEPROMStatusProfileRecordBytes,
		)
		if err != nil {
			t.Fatal(err)
		}
		if record[EEPROMStatusProfileBytes] != avrCRC8(record[:EEPROMStatusProfileBytes]) {
			t.Fatalf("status profile %d CRC does not match", condition)
		}
		encoded, err := native.StatusProfileSetPayload(byte(condition), want)
		if err != nil || !bytes.Equal(record[:EEPROMStatusProfileBytes], encoded[1:]) {
			t.Fatalf("status profile %d = % X, want % X (err=%v)", condition, record[:EEPROMStatusProfileBytes], encoded[1:], err)
		}
	}
}

func TestGenerateProgrammingEEPROMIntelHexArmsQuietVisibleLatch(t *testing.T) {
	content, err := GenerateProgrammingEEPROMIntelHex()
	if err != nil {
		t.Fatal(err)
	}
	image, err := ParseIntelHex(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	record, err := image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	if record[EEPROMSettingsValueBytes] != avrCRC8(record[:EEPROMSettingsValueBytes]) {
		t.Fatal("programming settings CRC does not match")
	}
	wantFlags := native.SettingsSilent | native.SettingsProgrammingMode
	if record[0]&wantFlags != wantFlags || record[1] != 0 || record[2] != 0 ||
		record[3] != 0 || record[5] != 0 || record[6] != 0 || record[20] != 0 {
		t.Fatalf("programming image is not latched/off: % X", record)
	}
	if record[4] == 0 || record[19]&0x07 != record[4] {
		t.Fatalf("Prog must remain visible with door closed: open=%d closed=%d", record[4], record[19]&0x07)
	}
	decoded := decodeOfflineSettings(image)
	if !decoded.Valid || decoded.Values.Flags&wantFlags != wantFlags {
		t.Fatalf("programming settings did not decode: %#v", decoded)
	}
}

func TestWriteDefaultEEPROMIntelHexDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "factory.hex")
	if err := WriteDefaultEEPROMIntelHex(path); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDefaultEEPROMIntelHex(path); err == nil {
		t.Fatal("second factory image write unexpectedly overwrote the file")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, original) {
		t.Fatal("existing factory image changed after rejected overwrite")
	}
}

func TestProgramLatchedFactoryEEPROMUsesGuardedCompleteImage(t *testing.T) {
	paths, err := HostDataPathsFor(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureHostDataPaths(paths); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = ProgramLatchedFactoryEEPROM(
		context.Background(), paths,
		Options{Method: MethodUrclock, Port: "COM18"},
		func(_ context.Context, options Options, _ io.Writer) error {
			calls++
			if options.Operation != OperationWriteEEPROM || !options.ConfirmEEPROMWrite ||
				options.OutputPath != "" {
				t.Fatalf("unsafe programming options: %#v", options)
			}
			content, readErr := os.ReadFile(options.HexPath)
			if readErr != nil {
				return readErr
			}
			image, parseErr := ParseIntelHex(bytes.NewReader(content))
			if parseErr != nil {
				return parseErr
			}
			inspection, inspectErr := image.Inspect()
			if inspectErr != nil {
				return inspectErr
			}
			record, recordErr := image.BytesAt(EEPROMSettingsAddress, EEPROMSettingsRecordBytes)
			if recordErr != nil {
				return recordErr
			}
			wantFlags := native.SettingsSilent | native.SettingsProgrammingMode
			if inspection.DataBytes != PCControllerEEPROMBytes || record[0]&wantFlags != wantFlags {
				t.Fatalf("executor did not receive complete latched image: inspection=%#v flags=0x%02X", inspection, record[0])
			}
			return nil
		}, io.Discard,
	)
	if err != nil || calls != 1 {
		t.Fatalf("latched factory programming calls=%d err=%v", calls, err)
	}
}

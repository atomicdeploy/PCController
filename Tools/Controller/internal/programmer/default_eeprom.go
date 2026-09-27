package programmer

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/native"
)

const (
	DefaultPersistentMenuPageCount = 14
	DefaultVisibleMenuMask         = uint16(1<<DefaultPersistentMenuPageCount) - 1
	defaultEEPROMCompileArtifact   = "safe-default-eeprom.hex"
)

type EEPROMProgramOperation func(context.Context, Options, io.Writer) error

// GenerateDefaultEEPROMIntelHex returns a complete, immediately usable
// ATmega328P EEPROM image. Its deployment baseline keeps heat-producing and
// user-facing outputs off while initializing every current settings field.
func GenerateDefaultEEPROMIntelHex() ([]byte, error) {
	return generateEEPROMIntelHex(native.DefaultSettings())
}

// GenerateProgrammingEEPROMIntelHex returns the same complete factory image
// with its transient programming latch armed. It is used only inside a
// guarded reinitialization transaction: every boot remains silent, outputs
// remain off, and the TM1637 reads Prog until the host verifies the new image
// and commits ordinary factory settings as the final lifecycle step.
func GenerateProgrammingEEPROMIntelHex() ([]byte, error) {
	factory := native.DefaultSettings()
	factory.Flags |= native.SettingsSilent | native.SettingsProgrammingMode
	factory.LightMode = 0
	factory.OnBrightness = 0
	factory.OffBrightness = 0
	factory.StatusBrightness = 0
	factory.OutputPersistence = 0
	factory.RelayRestoreMask = 0
	factory.DisplayClosedBrightness = factory.DisplayBrightness
	return generateEEPROMIntelHex(factory)
}

// GenerateMigratedProgrammingEEPROMIntelHex converts a validated pre-flash
// backup semantically into the compiled production schema. It preserves every
// unrelated EEPROM byte, drops only schema-2 menu-layout storage, recalculates
// CRC-8, and arms Silent/Prog before the new firmware is allowed to boot.
func GenerateMigratedProgrammingEEPROMIntelHex(manifestPath string) ([]byte, OfflineSettingsDecode, error) {
	_, backup, decoded, err := loadCurrentBackupEEPROM(manifestPath)
	if err != nil {
		return nil, OfflineSettingsDecode{}, err
	}
	values := decoded.Settings.Values
	values.Flags |= native.SettingsSilent | native.SettingsProgrammingMode
	values.IlluminationMode = 0
	values.IlluminationOnBrightness = 0
	values.IlluminationOffBrightness = 0
	values.StatusBrightness = 0
	values.OutputPersistence = 0
	values.RelayRestoreMask = 0
	values.DisplayClosedBrightness = values.DisplayBrightness
	record, err := encodeCurrentEEPROMSettingsRecord(values)
	if err != nil {
		return nil, OfflineSettingsDecode{}, fmt.Errorf("migrate EEPROM settings to schema 1: %w", err)
	}
	migrated := &IntelHexImage{data: make(map[uint32]byte, len(backup.Image.data))}
	for address, value := range backup.Image.data {
		migrated.data[address] = value
	}
	// Clear the superseded schema-2 tail so a future forensic decoder cannot
	// mistake stale menu-layout/name bytes for a second live record.
	for offset := uint32(0); offset < EEPROMMenuLayoutRecordBytes; offset++ {
		migrated.data[EEPROMSettingsAddress+offset] = 0xFF
	}
	for offset, value := range record {
		migrated.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := migrated.Canonical()
	if err != nil {
		return nil, OfflineSettingsDecode{}, fmt.Errorf("encode migrated EEPROM: %w", err)
	}
	verification, err := ParseIntelHex(strings.NewReader(string(content)))
	if err != nil {
		return nil, OfflineSettingsDecode{}, err
	}
	if err := requireFullEEPROMImage(verification); err != nil {
		return nil, OfflineSettingsDecode{}, err
	}
	confirmed := decodeOfflineSettings(verification)
	wantFlags := native.SettingsSilent | native.SettingsProgrammingMode
	if !confirmed.Valid || confirmed.Schema != EEPROMSettingsRecordSchema || confirmed.Values.Flags&wantFlags != wantFlags {
		return nil, OfflineSettingsDecode{}, fmt.Errorf("migrated EEPROM failed schema-1 Silent/Prog verification: %s", confirmed.Issue)
	}
	return content, confirmed, nil
}

func generateEEPROMIntelHex(factory native.Settings) ([]byte, error) {
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	settings, err := encodeCurrentEEPROMSettingsRecord(controllerSettingsFromNative(factory))
	if err != nil {
		return nil, fmt.Errorf("encode safe settings EEPROM: %w", err)
	}
	copy(data[EEPROMSettingsAddress:EEPROMSettingsAddress+EEPROMSettingsRecordBytes], settings)

	// A new board starts with a valid empty 20-record learned-RF store.
	header := data[EEPROMRemoteHeaderAddress : EEPROMRemoteHeaderAddress+4]
	binary.LittleEndian.PutUint16(header[0:2], 0x4C52)
	header[2] = EEPROMRemoteRecordSize
	header[3] = EEPROMRemoteCapacity
	for id := byte(0); id < EEPROMRemoteCapacity; id++ {
		start := EEPROMRemoteEntriesAddress + uint32(id)*EEPROMRemoteRecordBytes
		record := data[start : start+EEPROMRemoteRecordBytes]
		for index := range record {
			record[index] = 0
		}
		// Eleven zero value bytes have CRC-8 zero, so the twelfth remains zero.
	}

	// The rich LED palette and effects are owned by Go and installed in the
	// same factory image as settings/remotes. AVR only retains a tiny fallback.
	profiles := native.DefaultStatusProfiles(native.FactoryStatusBrightness)
	for condition, profile := range profiles {
		descriptor, err := native.StatusProfileSetPayload(byte(condition), profile)
		if err != nil {
			return nil, fmt.Errorf("encode factory status profile %d: %w", condition, err)
		}
		start := EEPROMStatusProfileAddress + uint32(condition)*EEPROMStatusProfileRecordBytes
		record := data[start : start+EEPROMStatusProfileRecordBytes]
		copy(record, descriptor[1:])
		record[EEPROMStatusProfileBytes] = avrCRC8(record[:EEPROMStatusProfileBytes])
	}

	image := &IntelHexImage{data: make(map[uint32]byte, PCControllerEEPROMBytes)}
	for address, value := range data {
		image.data[uint32(address)] = value
	}
	return image.Canonical()
}

// WriteDefaultEEPROMIntelHex publishes a user-requested factory image without
// overwriting an existing backup/artifact path.
func WriteDefaultEEPROMIntelHex(outputPath string) error {
	content, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		return err
	}
	return writeEEPROMIntelHexExclusive(outputPath, content)
}

// WriteProgrammingEEPROMIntelHex publishes the transaction-only latched
// factory image without overwriting an existing artifact or backup.
func WriteProgrammingEEPROMIntelHex(outputPath string) error {
	content, err := GenerateProgrammingEEPROMIntelHex()
	if err != nil {
		return err
	}
	return writeEEPROMIntelHexExclusive(outputPath, content)
}

func writeEEPROMIntelHexExclusive(outputPath string, content []byte) error {
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create factory EEPROM image: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write factory EEPROM image: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync factory EEPROM image: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close factory EEPROM image: %w", err)
	}
	remove = false
	return nil
}

// ProgramLatchedFactoryEEPROM writes and independently verifies the complete
// transaction-only factory image through the selected guarded programmer.
// The temporary artifact lives under Controller's state directory and is
// removed after the programmer has consumed it.
func ProgramLatchedFactoryEEPROM(
	ctx context.Context,
	paths HostDataPaths,
	base Options,
	execute EEPROMProgramOperation,
	output io.Writer,
) error {
	if execute == nil {
		return fmt.Errorf("latched factory EEPROM programming requires an executor")
	}
	temporary, err := os.CreateTemp(paths.StateDir, "programming-eeprom-*.hex")
	if err != nil {
		return fmt.Errorf("reserve latched factory EEPROM image: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("prepare latched factory EEPROM image: %w", err)
	}
	defer os.Remove(path)
	if err := WriteProgrammingEEPROMIntelHex(path); err != nil {
		return err
	}
	write := base
	write.Operation = OperationWriteEEPROM
	write.HexPath = path
	write.OutputPath = ""
	write.ConfirmEEPROMWrite = true
	if err := execute(ctx, write, output); err != nil {
		return fmt.Errorf("program latched factory EEPROM: %w", err)
	}
	return nil
}

// ProgramMigratedProgrammingEEPROM writes a semantically migrated, complete
// EEPROM image before the application flash. The programmer executor performs
// its ordinary independent read-back verification.
func ProgramMigratedProgrammingEEPROM(
	ctx context.Context,
	manifestPath string,
	paths HostDataPaths,
	base Options,
	execute EEPROMProgramOperation,
	output io.Writer,
) error {
	if execute == nil {
		return fmt.Errorf("migrated programming EEPROM requires an executor")
	}
	content, decoded, err := GenerateMigratedProgrammingEEPROMIntelHex(manifestPath)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(paths.StateDir, "migrated-programming-eeprom-*.hex")
	if err != nil {
		return fmt.Errorf("reserve migrated programming EEPROM image: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("prepare migrated programming EEPROM image: %w", err)
	}
	defer os.Remove(path)
	if err := writeEEPROMIntelHexExclusive(path, content); err != nil {
		return err
	}
	write := base
	write.Operation = OperationWriteEEPROM
	write.HexPath = path
	write.OutputPath = ""
	write.ConfirmEEPROMWrite = true
	if err := execute(ctx, write, output); err != nil {
		return fmt.Errorf("program migrated schema-%d EEPROM: %w", decoded.Schema, err)
	}
	return nil
}

// stageDefaultEEPROMCompileArtifact publishes the canonical deployment EEPROM
// image beside the application and bootloader outputs.
func stageDefaultEEPROMCompileArtifact(outputDir string) (string, error) {
	content, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		return "", fmt.Errorf("generate safe default EEPROM: %w", err)
	}
	path := filepath.Join(outputDir, defaultEEPROMCompileArtifact)
	if err := writeFileAtomicReplace(path, content, 0o644); err != nil {
		return "", fmt.Errorf("publish safe default EEPROM: %w", err)
	}
	return path, nil
}

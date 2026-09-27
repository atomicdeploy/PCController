package programmer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pairedReadbackFixtureRunner struct {
	commands        []Command
	flashContent    []byte
	eepromContent   []byte
	failTransaction bool
}

func (runner *pairedReadbackFixtureRunner) Run(
	_ context.Context,
	command Command,
	_ io.Writer,
) error {
	runner.commands = append(runner.commands, command)
	joined := strings.Join(command.Args, " ")
	if strings.Contains(joined, "-Uflash:w:") {
		if !strings.Contains(joined, "-Ueeprom:w:") {
			return errors.New("test observed a standalone flash write")
		}
		if runner.failTransaction {
			return errors.New("simulated combined programmer failure")
		}
		return nil
	}
	for _, argument := range command.Args {
		for prefix, content := range map[string][]byte{
			"-Uflash:r:":  runner.flashContent,
			"-Ueeprom:r:": runner.eepromContent,
		} {
			if strings.HasPrefix(argument, prefix) && strings.HasSuffix(argument, ":i") {
				path := strings.TrimSuffix(strings.TrimPrefix(argument, prefix), ":i")
				return os.WriteFile(path, content, 0o600)
			}
		}
	}
	return nil
}

type readbackFixtureRunner struct {
	commands []Command
	content  []byte
}

func (runner *readbackFixtureRunner) Run(
	_ context.Context,
	command Command,
	_ io.Writer,
) error {
	runner.commands = append(runner.commands, command)
	for _, argument := range command.Args {
		for _, prefix := range []string{"-Uflash:r:", "-Ueeprom:r:"} {
			if strings.HasPrefix(argument, prefix) && strings.HasSuffix(argument, ":i") {
				path := strings.TrimSuffix(strings.TrimPrefix(argument, prefix), ":i")
				return os.WriteFile(path, runner.content, 0o600)
			}
		}
	}
	return nil
}

func TestExecuteWithRunnerRequiresAndVerifiesIndependentReadback(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation Operation
		memory    string
	}{
		{name: "flash", operation: OperationWriteFlash, memory: "flash"},
		{name: "eeprom", operation: OperationWriteEEPROM, memory: "eeprom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			image := &IntelHexImage{data: map[uint32]byte{0: 0x12, 7: 0xA5}}
			content, err := image.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), test.name+".hex")
			if err := os.WriteFile(input, content, 0o600); err != nil {
				t.Fatal(err)
			}
			runner := &readbackFixtureRunner{content: content}
			options := Options{
				Method: MethodUrclock, Operation: test.operation, Port: "OFFLINE",
				HexPath: input, Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
				ConfirmEEPROMWrite: test.operation == OperationWriteEEPROM,
			}
			var log strings.Builder
			if err := ExecuteWithRunner(context.Background(), options, &log, runner); err != nil {
				t.Fatal(err)
			}
			if len(runner.commands) != 2 {
				t.Fatalf("write/read command count = %d", len(runner.commands))
			}
			joined := strings.Join(runner.commands[1].Args, " ")
			if !strings.Contains(joined, "-U"+test.memory+":r:") ||
				!strings.Contains(log.String(), "readback verified 2 written byte(s)") {
				t.Fatalf("mandatory readback was not visible: command=%s log=%s", joined, log.String())
			}
		})
	}
}

func TestExecutePairedWriteHasNoOldFirmwareBootGapAndVerifiesBothMemories(t *testing.T) {
	flashImage := &IntelHexImage{data: map[uint32]byte{0: 0x12, 7: 0xA5}}
	flashContent, err := flashImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	eepromImage := &IntelHexImage{data: map[uint32]byte{0: 0x03, 31: 0x9C}}
	eepromContent, err := eepromImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	flashPath := filepath.Join(directory, "application.hex")
	eepromPath := filepath.Join(directory, "migrated-eeprom.hex")
	if err := os.WriteFile(flashPath, flashContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eepromPath, eepromContent, 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{
		Method: MethodUrclock, Operation: OperationWriteFlash, Port: "OFFLINE",
		HexPath: flashPath, EEPROMHexPath: eepromPath,
		ConfirmEEPROMWrite: true,
		Avrdude:            "avrdude", AvrdudeConf: "avrdude.conf",
	}
	runner := &pairedReadbackFixtureRunner{
		flashContent: flashContent, eepromContent: eepromContent,
	}
	var log strings.Builder
	if err := ExecuteWithRunner(context.Background(), options, &log, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("combined write plus two readbacks produced %d commands", len(runner.commands))
	}
	writeArgs := strings.Join(runner.commands[0].Args, " ")
	flashAt := strings.Index(writeArgs, "-Uflash:w:")
	eepromAt := strings.Index(writeArgs, "-Ueeprom:w:")
	if flashAt < 0 || eepromAt <= flashAt {
		t.Fatalf("single transaction is not flash-first/EEPROM-second: %s", writeArgs)
	}
	if !strings.Contains(strings.Join(runner.commands[1].Args, " "), "-Uflash:r:") ||
		!strings.Contains(strings.Join(runner.commands[2].Args, " "), "-Ueeprom:r:") {
		t.Fatalf("both memory readbacks were not independent: %#v", runner.commands)
	}
	if !strings.Contains(log.String(), "Mandatory flash readback verified") ||
		!strings.Contains(log.String(), "Mandatory EEPROM readback verified") {
		t.Fatalf("both readback results were not reported: %s", log.String())
	}
}

func TestExecutePairedWriteFailureCannotFallThroughToStandaloneEEPROM(t *testing.T) {
	image := &IntelHexImage{data: map[uint32]byte{0: 0xA5}}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	flashPath := filepath.Join(directory, "application.hex")
	eepromPath := filepath.Join(directory, "migrated-eeprom.hex")
	for _, path := range []string{flashPath, eepromPath} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &pairedReadbackFixtureRunner{
		flashContent: content, eepromContent: content, failTransaction: true,
	}
	err = ExecuteWithRunner(context.Background(), Options{
		Method: MethodUrclock, Operation: OperationWriteFlash, Port: "OFFLINE",
		HexPath: flashPath, EEPROMHexPath: eepromPath,
		ConfirmEEPROMWrite: true,
		Avrdude:            "avrdude", AvrdudeConf: "avrdude.conf",
	}, io.Discard, runner)
	if err == nil || !strings.Contains(err.Error(), "simulated combined programmer failure") {
		t.Fatalf("combined write failure was lost: %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("failure ran additional commands and could create a reboot gap: %#v", runner.commands)
	}
	joined := strings.Join(runner.commands[0].Args, " ")
	if strings.Index(joined, "-Uflash:w:") > strings.Index(joined, "-Ueeprom:w:") {
		t.Fatalf("failed transaction was not flash-first: %s", joined)
	}
}

func TestExecuteWithRunnerRejectsMismatchAndNoVerify(t *testing.T) {
	targetImage := &IntelHexImage{data: map[uint32]byte{0: 0x11}}
	target, err := targetImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	wrongImage := &IntelHexImage{data: map[uint32]byte{0: 0x22}}
	wrong, err := wrongImage.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "firmware.hex")
	if err := os.WriteFile(input, target, 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{
		Method: MethodUrclock, Operation: OperationWriteFlash, Port: "OFFLINE",
		HexPath: input, Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	}
	runner := &readbackFixtureRunner{content: wrong}
	if err := ExecuteWithRunner(context.Background(), options, io.Discard, runner); err == nil ||
		!strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("mismatched readback was accepted: %v", err)
	}
	options.NoVerify = true
	runner.commands = nil
	if err := ExecuteWithRunner(context.Background(), options, io.Discard, runner); err == nil ||
		!strings.Contains(err.Error(), "mandatory") || len(runner.commands) != 0 {
		t.Fatalf("no-verify bypass was accepted: err=%v commands=%d", err, len(runner.commands))
	}
}

func TestVerifyWrittenProgrammerBytesAcceptsOnlyProvenUrbootRedirect(t *testing.T) {
	written := &IntelHexImage{data: map[uint32]byte{
		0x0000: 0x26, 0x0001: 0xC1, // RJMP from reset to application 0x024E.
		0x0064: 0x2E, 0x0065: 0xC1, 0x0066: 0x00, 0x0067: 0x00,
		0x0200: 0xA5,
	}}
	validReadback := map[uint32]byte{
		0x0000: 0x3F, 0x0001: 0xCF, // RJMP from reset to Urboot at 0x7E80.
		0x0064: 0x0C, 0x0065: 0x94, 0x0066: 0x27, 0x0067: 0x01,
		0x0200: 0xA5, 0x7FFA: 0x03, 0x7FFB: 0x19,
	}
	options := Options{Method: MethodUrclock, Operation: OperationWriteFlash}
	allowance, err := verifyWrittenProgrammerBytes(
		options, "flash", written, &IntelHexImage{data: cloneHexData(validReadback)},
	)
	if err != nil {
		t.Fatal(err)
	}
	redirect := allowance.UrbootRedirect
	if redirect == nil || redirect.Vector != 25 ||
		redirect.BootloaderAddress != 0x7E80 || redirect.ApplicationAddress != 0x024E {
		t.Fatalf("unexpected Urboot redirect: %+v", allowance)
	}

	for _, test := range []struct {
		name    string
		method  Method
		mutate  func(map[uint32]byte)
		message string
	}{
		{
			name: "wrong bootloader target", method: MethodUrclock,
			mutate:  func(data map[uint32]byte) { data[0x0000] = 0x3E },
			message: "0x0000",
		},
		{
			name: "wrong application target", method: MethodUrclock,
			mutate:  func(data map[uint32]byte) { data[0x0066] = 0x28 },
			message: "0x0000",
		},
		{
			name: "wrong metadata vector", method: MethodUrclock,
			mutate:  func(data map[uint32]byte) { data[0x7FFB] = 0x18 },
			message: "0x0000",
		},
		{
			name: "missing metadata", method: MethodUrclock,
			mutate:  func(data map[uint32]byte) { delete(data, 0x7FFA) },
			message: "0x0000",
		},
		{
			name: "unrelated changed byte", method: MethodUrclock,
			mutate:  func(data map[uint32]byte) { data[0x0200] = 0x5A },
			message: "0x0000",
		},
		{
			name: "same bytes through USBasp", method: MethodUSBasp,
			mutate:  func(map[uint32]byte) {},
			message: "0x0000",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := cloneHexData(validReadback)
			test.mutate(actual)
			_, err := verifyWrittenProgrammerBytes(
				Options{Method: test.method, Operation: OperationWriteFlash},
				"flash", written, &IntelHexImage{data: actual},
			)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("unsafe redirect accepted or unclear error: %v", err)
			}
		})
	}
}

func TestVerifyWrittenProgrammerBytesAcceptsMetadataDerivedCustomUrbootBase(t *testing.T) {
	written := &IntelHexImage{data: map[uint32]byte{
		0x0000: 0x26, 0x0001: 0xC1,
		0x0064: 0x2E, 0x0065: 0xC1, 0x0066: 0x00, 0x0067: 0x00,
	}}
	readback := &IntelHexImage{data: map[uint32]byte{
		0x0000: 0xFF, 0x0001: 0xCE, // Four pages place Urboot-Custom at 0x7E00.
		0x0064: 0x0C, 0x0065: 0x94, 0x0066: 0x27, 0x0067: 0x01,
		0x7FFA: 0x04, 0x7FFB: 0x19,
	}}
	allowance, err := verifyWrittenProgrammerBytes(
		Options{Method: MethodUrclock, Operation: OperationWriteFlash},
		"flash", written, readback,
	)
	if err != nil {
		t.Fatal(err)
	}
	redirect := allowance.UrbootRedirect
	if redirect == nil || redirect.BootloaderAddress != 0x7E00 || redirect.Vector != 25 {
		t.Fatalf("custom Urboot metadata was not honored: %+v", allowance)
	}
}

func TestVerifyWrittenEEPROMAllowsOnlyValidRestartJournalAdvance(t *testing.T) {
	writtenData := make(map[uint32]byte, PCControllerEEPROMBytes)
	readbackData := make(map[uint32]byte, PCControllerEEPROMBytes)
	for address := uint32(0); address < PCControllerEEPROMBytes; address++ {
		writtenData[address] = 0xFF
		readbackData[address] = 0xFF
	}
	count := uint32(1)
	base := EEPROMResetJournalAddress
	readbackData[base] = byte(count)
	readbackData[base+1] = byte(count >> 8)
	readbackData[base+2] = byte(count >> 16)
	readbackData[base+3] = byte(count >> 24)
	readbackData[base+4] = avrCRC8([]byte{1, 0, 0, 0})
	readbackData[base+5] = 0xA7

	options := Options{Method: MethodUrclock, Operation: OperationWriteEEPROM}
	allowance, err := verifyWrittenProgrammerBytes(
		options, "EEPROM", &IntelHexImage{data: writtenData},
		&IntelHexImage{data: readbackData},
	)
	if err != nil || allowance.MutableEEPROMBytes == 0 {
		t.Fatalf("valid restart journal advance rejected: allowance=%+v err=%v", allowance, err)
	}

	invalidJournal := cloneHexData(readbackData)
	invalidJournal[base+4] ^= 0x01
	if _, err := verifyWrittenProgrammerBytes(
		options, "EEPROM", &IntelHexImage{data: writtenData},
		&IntelHexImage{data: invalidJournal},
	); err == nil {
		t.Fatal("invalid reset journal mutation was accepted")
	}

	changedSetting := cloneHexData(readbackData)
	changedSetting[EEPROMSettingsAddress] = 0
	if _, err := verifyWrittenProgrammerBytes(
		options, "EEPROM", &IntelHexImage{data: writtenData},
		&IntelHexImage{data: changedSetting},
	); err == nil {
		t.Fatal("immutable EEPROM mismatch was accepted")
	}
}

func TestVerifyFlashReadbackWithRunnerNeverIssuesWrite(t *testing.T) {
	written := &IntelHexImage{data: map[uint32]byte{
		0x0000: 0x26, 0x0001: 0xC1,
		0x0064: 0x2E, 0x0065: 0xC1, 0x0066: 0x00, 0x0067: 0x00,
	}}
	writtenContent, err := written.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	readback := &IntelHexImage{data: map[uint32]byte{
		0x0000: 0x3F, 0x0001: 0xCF,
		0x0064: 0x0C, 0x0065: 0x94, 0x0066: 0x27, 0x0067: 0x01,
		0x7FFA: 0x03, 0x7FFB: 0x19,
	}}
	readbackContent, err := readback.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "firmware.hex")
	if err := os.WriteFile(path, writtenContent, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &readbackFixtureRunner{content: readbackContent}
	var output strings.Builder
	if err := VerifyFlashReadbackWithRunner(
		context.Background(),
		Options{
			Method: MethodUrclock, Port: "OFFLINE", HexPath: path,
			Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
		},
		&output,
		runner,
	); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("fresh verifier command count = %d", len(runner.commands))
	}
	joined := strings.Join(runner.commands[0].Args, " ")
	if strings.Contains(joined, ":w:") || !strings.Contains(joined, "-Uflash:r:") {
		t.Fatalf("fresh verifier issued an unsafe command: %s", joined)
	}
	if !strings.Contains(output.String(), "Urboot vector redirection verified") {
		t.Fatalf("semantic verification was not reported: %s", output.String())
	}
}

func cloneHexData(source map[uint32]byte) map[uint32]byte {
	clone := make(map[uint32]byte, len(source))
	for address, value := range source {
		clone[address] = value
	}
	return clone
}

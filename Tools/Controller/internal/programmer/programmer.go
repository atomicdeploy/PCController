package programmer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Method string
type Operation string

const (
	MethodCompile Method = "compile"
	MethodArduino Method = "toolchain"
	MethodUrclock Method = "urclock"
	MethodUSBasp  Method = "usbasp"
	MethodAvrdude Method = "avrdude"
)

const (
	OperationWriteFlash     Operation = "write-flash"
	OperationReadFlash      Operation = "read-flash"
	OperationVerifyFlash    Operation = "verify-flash"
	OperationReadEEPROM     Operation = "read-eeprom"
	OperationWriteEEPROM    Operation = "write-eeprom"
	OperationMetadata       Operation = "metadata"
	OperationProbe          Operation = "probe"
	OperationStart          Operation = "start"
	OperationCoreInfo       Operation = "core-info"
	OperationCoreProperties Operation = "core-properties"
	OperationBurnBoot       Operation = "install-bootloader"
	OperationBackup         Operation = "backup"
	OperationChipErase      Operation = "chip-erase"
)

type Options struct {
	Method                     Method
	Port                       string
	HexPath                    string
	SketchPath                 string
	OutputDir                  string
	BuildPath                  string
	FQBN                       string
	Programmer                 string
	MCU                        string
	BaudRate                   int
	ArduinoCLI                 string
	ArduinoConfig              string
	Avrdude                    string
	AvrdudeConf                string
	NoVerify                   bool
	Operation                  Operation
	OutputPath                 string
	ConfirmEEPROMWrite         bool
	ApplicationHash            uint32
	ApplicationIdentitySchema  byte
	ApplicationPackedTimestamp uint32
	CompileSourceRoot          string
	FirmwareSourceHash         uint32
	FirmwareSourceSHA256       string
	FirmwareSourceFiles        int
	FirmwareBuildTimestamp     uint32
	compilePlanned             bool
	compileStaged              bool
	// USBaspBitClockUS forces AVRDUDE's -B bit-clock period. USBaspAutoSlow
	// retries a failed USBasp exchange at MiniCore's conservative 32-microsecond
	// period. Multi-step callers may deliberately return to normal speed after
	// repairing a target clock/fuse policy.
	USBaspBitClockUS float64
	USBaspAutoSlow   bool
}

type BackupFile struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	Storage      string `json:"storage,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
}

type BackupManifest struct {
	Schema                     int          `json:"schema"`
	Status                     string       `json:"status"`
	CreatedAt                  time.Time    `json:"created_at"`
	CompletedAt                time.Time    `json:"completed_at"`
	Method                     Method       `json:"method"`
	Port                       string       `json:"port,omitempty"`
	MCU                        string       `json:"mcu"`
	Programmer                 string       `json:"programmer"`
	ApplicationHash            string       `json:"application_hash,omitempty"`
	ApplicationIdentitySchema  byte         `json:"application_identity_schema,omitempty"`
	ApplicationPackedTimestamp string       `json:"application_packed_timestamp,omitempty"`
	ApplicationTimestamp       string       `json:"application_timestamp,omitempty"`
	Reference                  string       `json:"reference,omitempty"`
	MetadataAvailable          bool         `json:"metadata_available"`
	Files                      []BackupFile `json:"files"`
	Errors                     []string     `json:"errors,omitempty"`
}

type Command struct {
	Name string
	Args []string
}

func Build(options Options) (Command, error) {
	if options.FQBN == "" {
		options.FQBN = DefaultFQBN()
	}
	if options.MCU == "" {
		options.MCU = generatedBoardMCU
	}
	if options.BaudRate == 0 {
		options.BaudRate = generatedBoardBaud
	}
	if options.Operation == "" {
		options.Operation = OperationWriteFlash
	}

	switch options.Method {
	case MethodCompile:
		var err error
		options, _, err = PlanCompile(options)
		if err != nil {
			return Command{}, err
		}
		executable, err := findExecutable(options.ArduinoCLI, "arduino-cli")
		if err != nil {
			return Command{}, err
		}
		args := []string{"compile", "--fqbn", options.FQBN}
		args = append(
			args,
			"--build-property",
			fmt.Sprintf(
				"build.extra_flags=-DPCCONTROLLER_BUILD_HASH=0x%08XUL "+
					"-DPCCONTROLLER_BUILD_TIMESTAMP=0x%08XUL "+
					"-DPCCONTROLLER_IDENTITY_ADDRESS=0x%XUL -mcall-prologues "+
					"-fmerge-all-constants -fno-split-wide-types -fno-tree-scev-cprop "+
					"-fipa-pta -fstack-usage",
				options.FirmwareSourceHash,
				options.FirmwareBuildTimestamp,
				FirmwareIdentityAddress,
			),
			"--build-property",
			fmt.Sprintf(
				"compiler.c.elf.extra_flags=-w -flto -fipa-pta -g -Wl,--relax "+
					"-Wl,--section-start=.firmware_identity=0x%X",
				FirmwareIdentityAddress,
			),
			"--warnings", "all",
			"--jobs", "1",
			"--build-path", options.BuildPath,
		)
		if options.OutputDir != "" {
			args = append(args, "--output-dir", options.OutputDir)
		}
		args = append(args, options.SketchPath)
		return Command{Name: executable, Args: toolchainCLIArguments(executable, options.ArduinoConfig, args...)}, nil

	case MethodArduino:
		executable, err := findExecutable(options.ArduinoCLI, "arduino-cli")
		if err != nil {
			return Command{}, err
		}
		switch options.Operation {
		case OperationWriteFlash:
			if options.Port == "" {
				return Command{}, errors.New("dependency CLI upload requires a serial port")
			}
			if options.SketchPath == "" {
				return Command{}, errors.New("dependency CLI upload requires a project path")
			}
			return Command{Name: executable, Args: toolchainCLIArguments(executable, options.ArduinoConfig,
				"upload", "--port", options.Port, "--fqbn", options.FQBN,
				options.SketchPath,
			)}, nil
		case OperationCoreInfo:
			return Command{Name: executable, Args: toolchainCLIArguments(executable, options.ArduinoConfig,
				"board", "details", "--fqbn", options.FQBN,
				"--full", "--list-programmers",
			)}, nil
		case OperationCoreProperties:
			if strings.TrimSpace(options.SketchPath) == "" {
				return Command{}, errors.New("core properties require a sketch path")
			}
			return Command{Name: executable, Args: toolchainCLIArguments(executable, options.ArduinoConfig,
				"compile", "--fqbn", options.FQBN,
				"--show-properties=expanded", options.SketchPath,
			)}, nil
		case OperationBurnBoot:
			if options.Programmer == "" {
				return Command{}, errors.New(
					"toolchain install-bootloader requires --programmer",
				)
			}
			args := []string{
				"burn-bootloader", "--fqbn", options.FQBN,
				"--programmer", options.Programmer, "--verify",
			}
			if options.Port != "" {
				args = append(args, "--port", options.Port)
			}
			return Command{Name: executable, Args: toolchainCLIArguments(executable, options.ArduinoConfig, args...)}, nil
		default:
			return Command{}, fmt.Errorf(
				"toolchain dependency does not support operation %s",
				options.Operation,
			)
		}

	case MethodUrclock, MethodUSBasp, MethodAvrdude:
		executable, configuration, err := FindAvrdudeWithCLIConfig(
			options.Avrdude,
			options.AvrdudeConf,
			options.ArduinoCLI,
			options.ArduinoConfig,
		)
		if err != nil {
			return Command{}, err
		}
		programmer := options.Programmer
		switch options.Method {
		case MethodUrclock:
			programmer = "urclock"
			if options.Port == "" {
				return Command{}, errors.New("urclock requires a serial port")
			}
		case MethodUSBasp:
			programmer = "usbasp"
		case MethodAvrdude:
			if programmer == "" {
				return Command{}, errors.New("avrdude requires --programmer")
			}
		}
		args := []string{"-C" + configuration, "-v", "-p" + options.MCU, "-c" + programmer}
		if programmer == "usbasp" && options.USBaspBitClockUS > 0 {
			args = append(args, "-B"+strconv.FormatFloat(options.USBaspBitClockUS, 'f', -1, 64))
		}
		if options.Port != "" && programmer != "usbasp" {
			args = append(args, "-P"+options.Port)
		}
		if options.BaudRate > 0 && programmer == "urclock" {
			args = append(args, "-b"+strconv.Itoa(options.BaudRate))
		}
		switch options.Operation {
		case OperationWriteFlash:
			if options.HexPath == "" {
				return Command{}, errors.New("write-flash requires an Intel HEX input")
			}
			if options.NoVerify {
				return Command{}, errors.New("flash readback verification is mandatory and cannot be disabled")
			}
			if programmer == "urclock" {
				args = append(args, "-D", "-xnometadata")
			}
			args = append(args, "-Uflash:w:"+options.HexPath+":i")
		case OperationReadFlash:
			if options.OutputPath == "" {
				return Command{}, errors.New("read-flash requires --output")
			}
			args = append(args, "-A", "-Uflash:r:"+options.OutputPath+":i")
		case OperationVerifyFlash:
			if options.HexPath == "" {
				return Command{}, errors.New("verify-flash requires an Intel HEX input")
			}
			args = append(args, "-Uflash:v:"+options.HexPath+":i")
		case OperationReadEEPROM:
			if options.OutputPath == "" {
				return Command{}, errors.New("read-eeprom requires --output")
			}
			args = append(args, "-A", "-Ueeprom:r:"+options.OutputPath+":i")
		case OperationWriteEEPROM:
			if options.HexPath == "" {
				return Command{}, errors.New("write-eeprom requires an Intel HEX input")
			}
			if !options.ConfirmEEPROMWrite {
				return Command{}, errors.New(
					"write-eeprom is destructive; pass --confirm-eeprom-write",
				)
			}
			args = append(args, "-Ueeprom:w:"+options.HexPath+":i")
		case OperationMetadata:
			if programmer != "urclock" {
				return Command{}, errors.New("metadata requires the urclock programmer")
			}
			args = append(args, "-xshowall")
		case OperationProbe:
			// AVRDUDE's initialization/signature check is the operation.
		case OperationStart:
			if programmer != "urclock" {
				return Command{}, errors.New("start requires the urclock programmer")
			}
			// Enter Urboot, query it, then let normal programmer shutdown hand
			// control back to the application.
			args = append(args, "-xshowversion")
		case OperationChipErase:
			if programmer == "urclock" {
				return Command{}, errors.New("chip-erase requires ISP and cannot preserve a serial bootloader")
			}
			args = append(args, "-e")
		default:
			return Command{}, fmt.Errorf("unknown programmer operation %q", options.Operation)
		}
		return Command{Name: executable, Args: args}, nil
	default:
		return Command{}, fmt.Errorf("unknown programming method %q", options.Method)
	}
}

// Execute performs an operation with safety preflights that cannot be
// represented by a single command line.
func Execute(ctx context.Context, options Options, output io.Writer) error {
	return ExecuteWithRunner(ctx, options, output, CommandRunnerFunc(Run))
}

// ExecuteWithRunner exposes the exact guarded operation flow to stable offline
// tests. Every flash/EEPROM write is followed by an independent programmer
// readback and byte comparison; callers cannot opt out of that evidence.
func ExecuteWithRunner(
	ctx context.Context,
	options Options,
	output io.Writer,
	runner CommandRunner,
) (resultErr error) {
	if runner == nil {
		return errors.New("programmer operation requires a command runner")
	}
	if options.Method == MethodUSBasp && options.USBaspAutoSlow && options.USBaspBitClockUS <= 0 {
		runner = &usbaspSlowFallbackRunner{inner: runner, output: output}
	}
	if options.Operation == "" {
		options.Operation = OperationWriteFlash
	}
	if options.Operation == OperationBackup {
		_, err := BackupWithRunner(ctx, options, output, runner)
		return err
	}
	if options.Method != MethodCompile {
		if err := validateMandatoryWriteReadback(options); err != nil {
			return err
		}
	}
	var compileIdentity CompileIdentity
	if options.Method == MethodCompile {
		var err error
		if options.FQBN == "" {
			options.FQBN = DefaultFQBN()
		}
		options, compileIdentity, err = PlanCompile(options)
		if err != nil {
			return err
		}
		compileLock, err := acquireCompileExecutionLock(ctx, compileIdentity, output)
		if err != nil {
			return err
		}
		defer func() {
			if releaseErr := compileLock.Release(); releaseErr != nil {
				resultErr = errors.Join(resultErr, releaseErr)
			}
		}()
		if err := clearCompileManifest(compileIdentity.OutputDir); err != nil {
			return err
		}
		options, compileIdentity, err = StageCompile(options)
		if err != nil {
			return err
		}
	}
	if options.Method == MethodUSBasp &&
		options.Operation == OperationWriteFlash {
		if err := verifyEESAVEWithRunner(ctx, options, output, runner); err != nil {
			return err
		}
	}
	command, err := Build(options)
	if err != nil {
		return err
	}
	if options.Method == MethodCompile {
		purged, purgeErr := purgeStackUsageFiles(compileIdentity.BuildPath)
		if purgeErr != nil {
			return fmt.Errorf("prepare firmware SRAM stack guard: %w", purgeErr)
		}
		if output != nil && purged != 0 {
			fmt.Fprintf(output, "Removed %d stale GCC stack-usage sidecar(s).\n", purged)
		}
	}
	if err := runner.Run(ctx, command, output); err != nil {
		if options.Method == MethodArduino && options.Operation == OperationBurnBoot &&
			options.USBaspAutoSlow && strings.EqualFold(options.Programmer, "usbasp") {
			slow := options
			slow.Programmer = "usbasp_slow"
			slowCommand, buildErr := Build(slow)
			if buildErr != nil {
				return errors.Join(err, fmt.Errorf("build slow USBasp bootloader retry: %w", buildErr))
			}
			if output != nil {
				fmt.Fprintln(output, "USBasp bootloader attempt failed; retrying with the core-provided usbasp_slow programmer (-B32).")
			}
			if slowErr := runner.Run(ctx, slowCommand, output); slowErr != nil {
				return errors.Join(
					fmt.Errorf("USBasp bootloader attempt: %w", err),
					fmt.Errorf("slow USBasp bootloader retry: %w", slowErr),
				)
			}
		} else {
			return err
		}
	}
	if options.Method != MethodCompile &&
		(options.Operation == OperationWriteFlash || options.Operation == OperationWriteEEPROM) {
		if err := verifyMandatoryProgrammerReadback(ctx, options, output, runner); err != nil {
			return err
		}
	}
	if options.Method == MethodCompile {
		stackBudget, err := inspectFirmwareStackBudget(compileIdentity)
		if err != nil {
			return fmt.Errorf("firmware SRAM stack guard: %w", err)
		}
		printFirmwareStackBudget(output, stackBudget)
		manifestPath, err := writeCompileManifest(options, compileIdentity, stackBudget)
		if err != nil {
			return err
		}
		if output != nil {
			fmt.Fprintln(output, "firmware manifest:", manifestPath)
		}
	}
	return nil
}

func validateMandatoryWriteReadback(options Options) error {
	if options.Operation != OperationWriteFlash && options.Operation != OperationWriteEEPROM {
		return nil
	}
	if options.NoVerify {
		return errors.New("programmer readback verification is mandatory and cannot be disabled")
	}
	switch options.Method {
	case MethodUrclock, MethodUSBasp, MethodAvrdude:
	default:
		return fmt.Errorf("%s through %q cannot provide mandatory programmer readback", options.Operation, options.Method)
	}
	capacity := ATmega328PFlashSize
	memory := "flash"
	if options.Operation == OperationWriteEEPROM {
		capacity = atmega328PEEPROMCapacity
		memory = "EEPROM"
	}
	document, err := LoadIntelHex(options.HexPath)
	if err != nil {
		return fmt.Errorf("validate %s write input: %w", memory, err)
	}
	if !document.Inspection.HasData {
		return fmt.Errorf("refusing empty %s write image", memory)
	}
	if document.Inspection.MaximumAddress >= capacity {
		return fmt.Errorf("%s write image exceeds %d-byte device capacity", memory, capacity)
	}
	return nil
}

func verifyMandatoryProgrammerReadback(
	ctx context.Context,
	options Options,
	output io.Writer,
	runner CommandRunner,
) error {
	temporary, err := os.CreateTemp("", "pccontroller-write-readback-*.hex")
	if err != nil {
		return fmt.Errorf("reserve programmer readback artifact: %w", err)
	}
	readbackPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(readbackPath)
		return err
	}
	if err := os.Remove(readbackPath); err != nil {
		return fmt.Errorf("prepare programmer readback artifact: %w", err)
	}
	defer os.Remove(readbackPath)

	readOptions := options
	readOptions.HexPath = ""
	readOptions.OutputPath = readbackPath
	readOptions.NoVerify = false
	memory := "flash"
	capacity := ATmega328PFlashSize
	readOptions.Operation = OperationReadFlash
	if options.Operation == OperationWriteEEPROM {
		memory = "EEPROM"
		capacity = atmega328PEEPROMCapacity
		readOptions.Operation = OperationReadEEPROM
	}
	command, err := Build(readOptions)
	if err != nil {
		return fmt.Errorf("build mandatory %s readback: %w", memory, err)
	}
	if output != nil {
		fmt.Fprintln(output, "Mandatory programmer readback:", command.String())
	}
	if err := runner.Run(ctx, command, output); err != nil {
		return fmt.Errorf("read back written %s: %w", memory, err)
	}
	written, err := LoadIntelHex(options.HexPath)
	if err != nil {
		return fmt.Errorf("reload written %s image: %w", memory, err)
	}
	readback, err := LoadIntelHex(readbackPath)
	if err != nil {
		return fmt.Errorf("load %s programmer readback: %w", memory, err)
	}
	if readback.Inspection.HasData && readback.Inspection.MaximumAddress >= capacity {
		return fmt.Errorf("%s programmer readback exceeds %d-byte device capacity", memory, capacity)
	}
	allowance, err := verifyWrittenProgrammerBytes(options, memory, written.Image, readback.Image)
	if err != nil {
		return err
	}
	if output != nil && allowance.UrbootRedirect != nil {
		fmt.Fprintf(
			output,
			"Urboot vector redirection verified: reset -> 0x%04X, vector %d -> application 0x%04X; all other written bytes are exact.\n",
			allowance.UrbootRedirect.BootloaderAddress,
			allowance.UrbootRedirect.Vector,
			allowance.UrbootRedirect.ApplicationAddress,
		)
	}
	if output != nil && allowance.MutableEEPROMBytes != 0 {
		fmt.Fprintf(
			output,
			"EEPROM reset journal advanced during application restart: %d mutable byte(s) changed; journal CRC/markers and every immutable EEPROM byte verified.\n",
			allowance.MutableEEPROMBytes,
		)
	}
	if output != nil {
		if allowance.MutableEEPROMBytes == 0 {
			fmt.Fprintf(
				output,
				"Mandatory %s readback verified %d written byte(s); input SHA-256 %s, readback SHA-256 %s.\n",
				memory, written.Inspection.DataBytes, written.SourceSHA256, readback.SourceSHA256,
			)
		} else {
			fmt.Fprintf(
				output,
				"Mandatory EEPROM readback verified %d immutable written byte(s) and validated %d restart-journal mutation byte(s); input SHA-256 %s, readback SHA-256 %s.\n",
				written.Inspection.DataBytes-allowance.MutableEEPROMBytes,
				allowance.MutableEEPROMBytes,
				written.SourceSHA256, readback.SourceSHA256,
			)
		}
	}
	return nil
}

// VerifyFlashReadback performs a fresh independent flash read and compares it
// with the requested application image without issuing any write operation.
func VerifyFlashReadback(
	ctx context.Context,
	options Options,
	output io.Writer,
) error {
	return VerifyFlashReadbackWithRunner(
		ctx, options, output, CommandRunnerFunc(Run),
	)
}

// VerifyFlashReadbackWithRunner is the injectable recovery form used after a
// prior write succeeded but its mandatory readback was interpreted as failed.
func VerifyFlashReadbackWithRunner(
	ctx context.Context,
	options Options,
	output io.Writer,
	runner CommandRunner,
) error {
	if runner == nil {
		return errors.New("fresh flash verification requires a command runner")
	}
	if options.Method != MethodUrclock && options.Method != MethodUSBasp && options.Method != MethodAvrdude {
		return fmt.Errorf("fresh flash verification does not support method %q", options.Method)
	}
	if strings.TrimSpace(options.HexPath) == "" {
		return errors.New("fresh flash verification requires an Intel HEX application image")
	}
	if _, err := LoadIntelHex(options.HexPath); err != nil {
		return fmt.Errorf("inspect fresh flash verification target: %w", err)
	}
	options.Operation = OperationWriteFlash
	options.NoVerify = false
	return verifyMandatoryProgrammerReadback(ctx, options, output, runner)
}

type urbootVectorRedirect struct {
	Vector             int
	BootloaderAddress  uint32
	ApplicationAddress uint32
}

type programmerReadbackAllowance struct {
	UrbootRedirect     *urbootVectorRedirect
	MutableEEPROMBytes uint32
}

// verifyWrittenProgrammerBytes keeps independent readback byte-exact, except
// for Urboot's documented reset-vector redirection on application flash writes.
func verifyWrittenProgrammerBytes(
	options Options,
	memory string,
	written *IntelHexImage,
	readback *IntelHexImage,
) (programmerReadbackAllowance, error) {
	var allowance programmerReadbackAllowance
	mismatches := make([]uint32, 0, 8)
	for address, expected := range written.data {
		actual, present := readback.Byte(address)
		if !present {
			return allowance, fmt.Errorf("%s programmer readback has no byte at 0x%04X", memory, address)
		}
		if actual != expected {
			mismatches = append(mismatches, address)
		}
	}
	if len(mismatches) == 0 {
		return allowance, nil
	}
	sort.Slice(mismatches, func(left, right int) bool { return mismatches[left] < mismatches[right] })
	if options.Method == MethodUrclock && options.Operation == OperationWriteFlash {
		if redirect, ok := recognizeUrbootVectorRedirect(written, readback, mismatches); ok {
			allowance.UrbootRedirect = redirect
			return allowance, nil
		}
	}
	if options.Operation == OperationWriteEEPROM &&
		validRestartJournalMutation(readback, mismatches) {
		allowance.MutableEEPROMBytes = uint32(len(mismatches))
		return allowance, nil
	}
	address := mismatches[0]
	expected, _ := written.Byte(address)
	actual, _ := readback.Byte(address)
	return allowance, fmt.Errorf(
		"%s programmer readback mismatch at 0x%04X: got 0x%02X require 0x%02X",
		memory, address, actual, expected,
	)
}

// The application records its reset cause immediately after an EEPROM write.
// AVRDUDE verifies the write before starting it, while our independent second
// read necessarily observes the advanced journal. Only that bounded region may
// differ, and the post-boot journal must still be structurally valid.
func validRestartJournalMutation(
	readback *IntelHexImage,
	mismatches []uint32,
) bool {
	journalEnd := EEPROMResetJournalAddress +
		uint32(EEPROMResetJournalSlots)*EEPROMResetJournalRecordSize
	for _, address := range mismatches {
		if address < EEPROMResetJournalAddress || address >= journalEnd {
			return false
		}
	}
	journal := decodeOfflineResetJournal(readback)
	return journal.Complete && journal.Valid && journal.ValidRecords != 0
}

// recognizeUrbootVectorRedirect proves the two AVR instructions Urboot writes:
// reset RJMPs to the bootloader and one interrupt vector JMPs to the app entry.
func recognizeUrbootVectorRedirect(
	written *IntelHexImage,
	readback *IntelHexImage,
	mismatches []uint32,
) (*urbootVectorRedirect, bool) {
	const (
		urbootFlashPageBytes        = uint32(128)
		urbootMetadataPagesAddress  = uint32(0x7FFA)
		urbootMetadataVectorAddress = uint32(0x7FFB)
	)
	expectedReset, ok := imageWord(written, 0)
	if !ok || expectedReset&0xF000 != 0xC000 {
		return nil, false
	}
	actualReset, ok := imageWord(readback, 0)
	if !ok {
		return nil, false
	}
	bootPages, pagesOK := readback.Byte(urbootMetadataPagesAddress)
	vectorByte, vectorOK := readback.Byte(urbootMetadataVectorAddress)
	if !pagesOK || !vectorOK || bootPages == 0 || bootPages > 16 || vectorByte == 0 || vectorByte > 25 {
		return nil, false
	}
	bootloaderAddress := ATmega328PFlashSize - uint32(bootPages)*urbootFlashPageBytes
	for address := range written.data {
		if address >= bootloaderAddress {
			return nil, false
		}
	}
	bootloaderWord := bootloaderAddress / 2
	expectedBootRJMP := uint16(0xC000 | ((bootloaderWord - 1) & 0x0FFF))
	if actualReset != expectedBootRJMP {
		return nil, false
	}

	wordCapacity := int32(ATmega328PFlashSize / 2)
	relative := int32(expectedReset & 0x0FFF)
	if relative&0x0800 != 0 {
		relative -= 0x1000
	}
	applicationWord := (1 + relative + wordCapacity) % wordCapacity

	vector := int(vectorByte)
	vectorStart := uint32(vector * 4)
	jumpOpcode, opcodeOK := imageWord(readback, vectorStart)
	jumpTarget, targetOK := imageWord(readback, vectorStart+2)
	if !opcodeOK || !targetOK || jumpOpcode != 0x940C || uint32(jumpTarget) != uint32(applicationWord) {
		return nil, false
	}
	hasChangedVectorByte := false
	for _, address := range mismatches {
		if address >= vectorStart && address < vectorStart+4 {
			hasChangedVectorByte = true
		}
		if address <= 1 || (address >= vectorStart && address < vectorStart+4) {
			continue
		}
		return nil, false
	}
	if !hasChangedVectorByte {
		return nil, false
	}
	return &urbootVectorRedirect{
		Vector:             vector,
		BootloaderAddress:  bootloaderAddress,
		ApplicationAddress: uint32(applicationWord) * 2,
	}, true
}

func imageWord(image *IntelHexImage, address uint32) (uint16, bool) {
	low, lowOK := image.Byte(address)
	high, highOK := image.Byte(address + 1)
	if !lowOK || !highOK {
		return 0, false
	}
	return uint16(low) | uint16(high)<<8, true
}

// Backup obtains programmer/bootloader metadata and independently reads flash
// and EEPROM into a new timestamped directory. Its JSON manifest is written
// even when one read fails, so a partial recovery is never mistaken for a
// complete backup.
func Backup(
	ctx context.Context,
	options Options,
	output io.Writer,
) (string, error) {
	return BackupWithRunner(ctx, options, output, CommandRunnerFunc(Run))
}

// BackupWithRunner is the injectable form used by safe pre-flash workflows
// and deterministic tests. The runner must implement the same artifact writes
// as the built AVRDUDE command (the operating-system runner naturally does).
func BackupWithRunner(
	ctx context.Context,
	options Options,
	output io.Writer,
	runner CommandRunner,
) (string, error) {
	if output == nil {
		output = io.Discard
	}
	if runner == nil {
		return "", errors.New("backup requires a command runner")
	}
	if err := ValidateBackup(options); err != nil {
		return "", err
	}
	root := strings.TrimSpace(options.OutputPath)
	if options.MCU == "" {
		options.MCU = generatedBoardMCU
	}
	operationRoot := filepath.Join(root, "operations")
	directory, err := createBackupDirectory(operationRoot, time.Now())
	if err != nil {
		return "", err
	}
	manifest := BackupManifest{
		Schema:                    1,
		Status:                    "incomplete",
		CreatedAt:                 time.Now().UTC(),
		Method:                    options.Method,
		Port:                      options.Port,
		MCU:                       options.MCU,
		Programmer:                effectiveProgrammer(options),
		ApplicationIdentitySchema: options.ApplicationIdentitySchema,
	}
	if currentIdentitySchema(options.ApplicationIdentitySchema) &&
		options.ApplicationPackedTimestamp != 0 {
		manifest.ApplicationPackedTimestamp = fmt.Sprintf("%08X", options.ApplicationPackedTimestamp)
		if timestamp, timestampErr := DecodeFirmwareTimestamp(options.ApplicationPackedTimestamp); timestampErr == nil {
			manifest.ApplicationTimestamp = timestamp.Compact
		}
	}
	if options.ApplicationHash != 0 {
		manifest.ApplicationHash = fmt.Sprintf("%08X", options.ApplicationHash)
	}
	fmt.Fprintln(output, "Backup directory:", directory)

	var failures []error
	runStep := func(kind, name string, step Options) {
		path := filepath.Join(directory, name)
		step.Operation = map[string]Operation{
			"flash":  OperationReadFlash,
			"eeprom": OperationReadEEPROM,
		}[kind]
		step.OutputPath = path
		command, buildErr := Build(step)
		if buildErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", kind, buildErr))
			manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
			return
		}
		fmt.Fprintln(output, command.String())
		if runErr := runner.Run(ctx, command, output); runErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", kind, runErr))
			manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
			return
		}
		file, fileErr := backupFile(path, kind)
		if fileErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", kind, fileErr))
			manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
			return
		}
		if kind == "flash" {
			blobRoot := filepath.Join(root, "firmware", "sha256")
			blob, blobErr := StoreFirmwareBlob(blobRoot, path)
			if blobErr != nil {
				failures = append(failures, fmt.Errorf("flash content store: %w", blobErr))
				manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
				return
			}
			relative, relativeErr := filepath.Rel(directory, blob.Path)
			if relativeErr != nil {
				failures = append(failures, fmt.Errorf("flash content reference: %w", relativeErr))
				manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
				return
			}
			if removeErr := os.Remove(path); removeErr != nil {
				failures = append(failures, fmt.Errorf("remove duplicate flash artifact: %w", removeErr))
				manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
				return
			}
			file.Name = blob.Reference
			file.Storage = "content-addressed"
			file.RelativePath = filepath.ToSlash(relative)
			manifest.Reference = "firmware-sha256:" + blob.SHA256
		} else {
			file.Storage = "operation"
			file.RelativePath = file.Name
		}
		manifest.Files = append(manifest.Files, file)
	}

	metadataPath := filepath.Join(directory, "programmer.txt")
	metadataFile, createErr := os.Create(metadataPath)
	if createErr != nil {
		failures = append(failures, fmt.Errorf("metadata: %w", createErr))
		manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
	} else {
		metadataOptions := options
		if options.Method == MethodUrclock {
			metadataOptions.Operation = OperationMetadata
		} else {
			metadataOptions.Operation = OperationProbe
		}
		command, buildErr := Build(metadataOptions)
		if buildErr == nil && options.Method != MethodUrclock {
			command.Args = append(
				command.Args,
				"-Ulfuse:r:-:h",
				"-Uhfuse:r:-:h",
				"-Uefuse:r:-:h",
				"-Ulock:r:-:h",
			)
		}
		if buildErr != nil {
			failures = append(failures, fmt.Errorf("metadata: %w", buildErr))
			manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
		} else {
			fmt.Fprintln(output, command.String())
			runErr := runner.Run(ctx, command, io.MultiWriter(output, metadataFile))
			if runErr != nil {
				failures = append(failures, fmt.Errorf("metadata: %w", runErr))
				manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
			} else {
				manifest.MetadataAvailable = true
			}
		}
		if closeErr := metadataFile.Close(); closeErr != nil {
			failures = append(failures, fmt.Errorf("metadata close: %w", closeErr))
			manifest.Errors = append(manifest.Errors, failures[len(failures)-1].Error())
		}
		if file, fileErr := backupFile(metadataPath, "metadata"); fileErr == nil {
			file.Storage = "operation"
			file.RelativePath = file.Name
			manifest.Files = append(manifest.Files, file)
		}
	}

	runStep("flash", "flash.hex", options)
	runStep("eeprom", "eeprom.hex", options)
	manifest.CompletedAt = time.Now().UTC()
	if len(failures) == 0 {
		manifest.Status = "complete"
	}
	if err := writeBackupManifest(
		filepath.Join(directory, "manifest.json"),
		manifest,
	); err != nil {
		failures = append(failures, err)
	}
	if len(failures) != 0 {
		return directory, errors.Join(failures...)
	}
	fmt.Fprintln(output, "Backup reference:", manifest.Reference)
	fmt.Fprintln(output, "Backup complete; manifest:", filepath.Join(directory, "manifest.json"))
	return directory, nil
}

func ValidateBackup(options Options) error {
	switch options.Method {
	case MethodUrclock, MethodUSBasp, MethodAvrdude:
	default:
		return fmt.Errorf(
			"backup requires urclock, usbasp, or avrdude; got %q",
			options.Method,
		)
	}
	if strings.TrimSpace(options.OutputPath) == "" {
		return errors.New("backup requires --output directory")
	}
	if options.Method == MethodUrclock &&
		strings.TrimSpace(options.Port) == "" {
		return errors.New("urclock backup requires a serial port")
	}
	if options.Method == MethodAvrdude &&
		strings.TrimSpace(options.Programmer) == "" {
		return errors.New("avrdude backup requires --programmer")
	}
	if options.ApplicationPackedTimestamp != 0 {
		if !currentIdentitySchema(options.ApplicationIdentitySchema) {
			return errors.New("packed firmware timestamp requires compact identity schema 4")
		}
		if _, err := DecodeFirmwareTimestamp(options.ApplicationPackedTimestamp); err != nil {
			return err
		}
	}
	return nil
}

func currentIdentitySchema(schema byte) bool { return schema == 4 }

func createBackupDirectory(root string, timestamp time.Time) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create backup root: %w", err)
	}
	base := "pccontroller-" + timestamp.Format("20060102-150405")
	for suffix := 0; suffix < 1000; suffix++ {
		name := base
		if suffix != 0 {
			name += "-" + strconv.Itoa(suffix+1)
		}
		path := filepath.Join(root, name)
		err := os.Mkdir(path, 0o755)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create backup directory: %w", err)
		}
	}
	return "", errors.New("could not allocate a unique backup directory")
}

func effectiveProgrammer(options Options) string {
	switch options.Method {
	case MethodUrclock:
		return "urclock"
	case MethodUSBasp:
		return "usbasp"
	default:
		return options.Programmer
	}
}

func backupFile(path, kind string) (BackupFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return BackupFile{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return BackupFile{}, err
	}
	return BackupFile{
		Name: filepath.Base(path), Kind: kind, Bytes: size,
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func writeBackupManifest(path string, manifest BackupManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("commit backup manifest: %w", err)
	}
	return nil
}

func verifyEESAVE(
	ctx context.Context,
	options Options,
	output io.Writer,
) error {
	return verifyEESAVEWithRunner(ctx, options, output, CommandRunnerFunc(Run))
}

func verifyEESAVEWithRunner(
	ctx context.Context,
	options Options,
	output io.Writer,
	runner CommandRunner,
) error {
	if runner == nil {
		return errors.New("USBasp EEPROM-preservation preflight requires a command runner")
	}
	temporary, err := os.CreateTemp("", "pccontroller-hfuse-*.txt")
	if err != nil {
		return fmt.Errorf("create high-fuse preflight file: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	defer os.Remove(path)
	executable, configuration, err := FindAvrdudeWithCLIConfig(
		options.Avrdude,
		options.AvrdudeConf,
		options.ArduinoCLI,
		options.ArduinoConfig,
	)
	if err != nil {
		return err
	}
	mcu := options.MCU
	if mcu == "" {
		mcu = generatedBoardMCU
	}
	preflight := Command{
		Name: executable,
		Args: []string{
			"-C" + configuration, "-q", "-p" + mcu, "-cusbasp",
			"-Uhfuse:r:" + path + ":h",
		},
	}
	if options.USBaspBitClockUS > 0 {
		preflight.Args = append(
			preflight.Args[:4],
			append([]string{"-B" + strconv.FormatFloat(options.USBaspBitClockUS, 'f', -1, 64)}, preflight.Args[4:]...)...,
		)
	}
	if output != nil {
		fmt.Fprintln(output, "USBasp EEPROM-preservation preflight:", preflight.String())
	}
	if err := runner.Run(ctx, preflight, output); err != nil {
		return fmt.Errorf("read high fuse before USBasp erase: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read high-fuse preflight result: %w", err)
	}
	valueText := strings.TrimSpace(string(content))
	valueText = strings.TrimPrefix(strings.ToLower(valueText), "0x")
	value, err := strconv.ParseUint(valueText, 16, 8)
	if err != nil {
		return fmt.Errorf("parse high fuse %q: %w", strings.TrimSpace(string(content)), err)
	}
	if byte(value)&0x08 != 0 {
		return fmt.Errorf(
			"refusing USBasp flash write: high fuse 0x%02X has EESAVE unprogrammed; chip erase would destroy EEPROM",
			value,
		)
	}
	if output != nil {
		fmt.Fprintf(output, "EESAVE confirmed in high fuse 0x%02X; EEPROM survives chip erase.\n", value)
	}
	return nil
}

func Run(ctx context.Context, command Command, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	process.Stdout = output
	process.Stderr = output
	process.Stdin = nil
	if err := process.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", command.String(), err)
	}
	return nil
}

const defaultUSBaspSlowBitClockUS = 32.0

type usbaspSlowFallbackRunner struct {
	inner  CommandRunner
	output io.Writer
	slow   bool
}

func (runner *usbaspSlowFallbackRunner) Run(
	ctx context.Context,
	command Command,
	output io.Writer,
) error {
	if runner == nil || runner.inner == nil {
		return errors.New("USBasp slow fallback requires a command runner")
	}
	if !isUSBaspCommand(command) || hasBitClock(command) {
		return runner.inner.Run(ctx, command, output)
	}
	if runner.slow {
		return runner.inner.Run(ctx, withUSBaspBitClock(command, defaultUSBaspSlowBitClockUS), output)
	}
	err := runner.inner.Run(ctx, command, output)
	if err == nil {
		return nil
	}
	runner.slow = true
	writer := output
	if writer == nil {
		writer = runner.output
	}
	if writer != nil {
		fmt.Fprintln(writer, "USBasp exchange failed; retrying at slow SCK (-B32) and retaining slow mode for this operation.")
	}
	slowErr := runner.inner.Run(ctx, withUSBaspBitClock(command, defaultUSBaspSlowBitClockUS), output)
	if slowErr == nil {
		return nil
	}
	return errors.Join(
		fmt.Errorf("USBasp default-speed attempt: %w", err),
		fmt.Errorf("USBasp slow-SCK retry: %w", slowErr),
	)
}

func isUSBaspCommand(command Command) bool {
	for _, argument := range command.Args {
		if strings.EqualFold(argument, "-cusbasp") {
			return true
		}
	}
	return false
}

func hasBitClock(command Command) bool {
	for _, argument := range command.Args {
		if argument == "-B" || strings.HasPrefix(strings.ToUpper(argument), "-B") {
			return true
		}
	}
	return false
}

func withUSBaspBitClock(command Command, microseconds float64) Command {
	// Avoid a len+1 capacity computation for caller-owned argument slices; the
	// append growth path performs its own checked allocation.
	result := Command{Name: command.Name}
	inserted := false
	for _, argument := range command.Args {
		result.Args = append(result.Args, argument)
		if !inserted && strings.EqualFold(argument, "-cusbasp") {
			result.Args = append(result.Args, "-B"+strconv.FormatFloat(microseconds, 'f', -1, 64))
			inserted = true
		}
	}
	if !inserted {
		result.Args = append(result.Args, "-B"+strconv.FormatFloat(microseconds, 'f', -1, 64))
	}
	return result
}

func (command Command) String() string {
	parts := []string{quote(command.Name)}
	for _, argument := range command.Args {
		parts = append(parts, quote(argument))
	}
	return strings.Join(parts, " ")
}

func FindAvrdude(executable, configuration string) (string, string, error) {
	return FindAvrdudeWithCLI(executable, configuration, "")
}

func FindAvrdudeWithCLI(
	executable,
	configuration,
	arduinoCLI string,
) (string, string, error) {
	return FindAvrdudeWithCLIConfig(executable, configuration, arduinoCLI, "")
}

func FindAvrdudeWithCLIConfig(
	executable,
	configuration,
	arduinoCLI,
	arduinoConfig string,
) (string, string, error) {
	if executable != "" {
		if configuration == "" {
			configuration = inferAvrdudeConf(executable)
		}
		if configuration == "" {
			return "", "", errors.New("avrdude configuration path is required")
		}
		return executable, configuration, nil
	}

	if resolved, err := exec.LookPath(executableName("avrdude")); err == nil {
		if configuration == "" {
			configuration = inferAvrdudeConf(resolved)
		}
		if configuration != "" {
			return resolved, configuration, nil
		}
	}
	cli, err := findExecutable(arduinoCLI, "arduino-cli")
	if err == nil {
		queryContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(queryContext, cli, toolchainCLIArguments(
			cli, arduinoConfig, "config", "get", "directories.data",
		)...)
		output, commandErr := command.Output()
		if commandErr == nil {
			dataDirectory := strings.Trim(strings.TrimSpace(string(output)), `"`)
			pattern := filepath.Join(
				dataDirectory,
				"packages", "MiniCore", "tools", "avrdude", "*", "bin",
				executableName("avrdude"),
			)
			matches, _ := filepath.Glob(pattern)
			sort.Slice(matches, func(left, right int) bool {
				return compareLooseVersion(
					avrdudeVersion(matches[left]),
					avrdudeVersion(matches[right]),
				) > 0
			})
			for _, candidate := range matches {
				candidateConfiguration := configuration
				if candidateConfiguration == "" {
					candidateConfiguration = inferAvrdudeConf(candidate)
				}
				if candidateConfiguration != "" {
					return candidate, candidateConfiguration, nil
				}
			}
		}
	}
	return "", "", errors.New(
		"avrdude not found via PATH or the Arduino CLI MiniCore data directory; configure explicit executable and avrdude.conf paths",
	)
}

func avrdudeVersion(executable string) string {
	// .../avrdude/<version>/bin/avrdude[.exe]
	return filepath.Base(filepath.Dir(filepath.Dir(executable)))
}

func compareLooseVersion(left, right string) int {
	leftParts := strings.FieldsFunc(left, func(char rune) bool {
		return char < '0' || char > '9'
	})
	rightParts := strings.FieldsFunc(right, func(char rune) bool {
		return char < '0' || char > '9'
	})
	count := len(leftParts)
	if len(rightParts) > count {
		count = len(rightParts)
	}
	for index := 0; index < count; index++ {
		var leftValue, rightValue int
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return strings.Compare(left, right)
}

func inferAvrdudeConf(executable string) string {
	root := filepath.Dir(filepath.Dir(executable))
	candidates := []string{
		filepath.Join(root, "etc", "avrdude.conf"),
		filepath.Join(root, "etc", "avrdude.conf.txt"),
		filepath.Join(filepath.Dir(executable), "avrdude.conf"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func findExecutable(explicit, fallback string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	resolved, err := exec.LookPath(executableName(fallback))
	if err != nil {
		return "", fmt.Errorf("%s not found in PATH", fallback)
	}
	return resolved, nil
}

func executableName(name string) string {
	return executableNameForOS(name, runtime.GOOS)
}

func executableNameForOS(name, goos string) string {
	if goos == "windows" && filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}

func quote(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"") {
		return value
	}
	return strconv.Quote(value)
}

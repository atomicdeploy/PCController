package programmer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const libavrdudeJSONLProtocol = "libavrdude-jsonl"

func withAvrdudeCLICompatibility(options Options) Options {
	if strings.TrimSpace(options.Avrdude) == "" && strings.TrimSpace(options.AvrdudeAdapter) != "" {
		candidate := filepath.Join(filepath.Dir(options.AvrdudeAdapter), executableName("avrdude"))
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			options.Avrdude = candidate
		}
	}
	options.AvrdudeBackend = "cli"
	options.AvrdudeAdapter = ""
	return options
}

func resolveAvrdudeAdapter(backend, explicit string) (string, error) {
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		backend = "auto"
	}
	if backend != "auto" && backend != "library" && backend != "cli" {
		return "", fmt.Errorf("unknown AVRDUDE backend %q; use auto, library, or cli", backend)
	}
	if backend == "cli" {
		if strings.TrimSpace(explicit) != "" {
			return "", errors.New("AVRDUDE adapter cannot be combined with the cli backend")
		}
		return "", nil
	}
	if strings.TrimSpace(explicit) != "" {
		resolved, err := exec.LookPath(explicit)
		if err != nil {
			return "", fmt.Errorf("configured libavrdude adapter %q is unavailable: %w", explicit, err)
		}
		return resolved, nil
	}
	name := "pccontroller-avrdude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if current, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(current), name)
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	if resolved, err := exec.LookPath(name); err == nil {
		return resolved, nil
	}
	if backend == "library" {
		return "", fmt.Errorf(
			"libavrdude backend requested but %s was not found beside the controller or on PATH",
			name,
		)
	}
	return "", nil
}

func resolveAvrdudeCommandTools(options Options) (adapter, executable, configuration string, err error) {
	adapter, err = resolveAvrdudeAdapter(options.AvrdudeBackend, options.AvrdudeAdapter)
	if err != nil {
		return "", "", "", err
	}
	if adapter == "" {
		executable, configuration, err = FindAvrdudeWithCLIConfig(
			options.Avrdude, options.AvrdudeConf, options.ArduinoCLI, options.ArduinoConfig,
		)
		return adapter, executable, configuration, err
	}
	configuration = strings.TrimSpace(options.AvrdudeConf)
	if configuration == "" {
		candidate := filepath.Join(filepath.Dir(adapter), "avrdude.conf")
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			configuration = candidate
		}
	}
	if configuration == "" {
		_, configuration, err = FindAvrdudeWithCLIConfig(
			options.Avrdude, "", options.ArduinoCLI, options.ArduinoConfig,
		)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("resolve configuration for libavrdude adapter: %w", err)
	}
	return adapter, "", configuration, nil
}

func resolvedMemoryCapacities(options Options) (uint32, uint32, error) {
	flash, eeprom := options.FlashBytes, options.EEPROMBytes
	mcu := strings.TrimSpace(options.MCU)
	if mcu == "" {
		mcu = generatedBoardMCU
	}
	if strings.EqualFold(mcu, generatedBoardMCU) {
		if flash == 0 {
			flash = generatedBoardFlashBytes
		}
		if eeprom == 0 {
			eeprom = generatedBoardEEPROMBytes
		}
	}
	if flash == 0 || eeprom == 0 {
		return 0, 0, fmt.Errorf(
			"guarded programming for MCU %q requires explicit --flash-bytes and --eeprom-bytes board geometry",
			mcu,
		)
	}
	return flash, eeprom, nil
}

func resolvedBootloaderBytes(options Options) (uint32, error) {
	if options.BootloaderBytes != 0 {
		return options.BootloaderBytes, nil
	}
	mcu := strings.TrimSpace(options.MCU)
	if mcu == "" {
		mcu = generatedBoardMCU
	}
	if strings.EqualFold(mcu, generatedBoardMCU) {
		return generatedBoardFlashBytes - generatedBoardApplicationBytes, nil
	}
	return 0, fmt.Errorf(
		"urclock programming for MCU %q requires explicit --bootloader-bytes geometry",
		mcu,
	)
}

func libavrdudeAdapterArguments(options Options, configuration, programmer string) ([]string, error) {
	args := []string{
		"--config", configuration,
		"--part", options.MCU,
		"--programmer", programmer,
	}
	if options.Port != "" && programmer != "usbasp" {
		args = append(args, "--port", options.Port)
	}
	if options.BaudRate > 0 && programmer == "urclock" {
		args = append(args, "--baud", strconv.Itoa(options.BaudRate))
	}
	if programmer == "usbasp" && options.USBaspBitClockUS > 0 {
		args = append(args, "--bitclock-us", strconv.FormatFloat(options.USBaspBitClockUS, 'f', -1, 64))
	}
	if programmer == "urclock" {
		bootloaderBytes, err := resolvedBootloaderBytes(options)
		if err != nil {
			return nil, err
		}
		args = append(args,
			"--extended", "bootsize="+strconv.FormatUint(uint64(bootloaderBytes), 10),
			"--extended", "eepromrw",
		)
	}
	var update string
	switch options.Operation {
	case OperationWriteFlash:
		if programmer == "urclock" {
			args = append(args, "--no-auto-erase")
		}
		update = "flash:w:" + options.HexPath + ":i"
	case OperationReadFlash:
		update = "flash:r:" + options.OutputPath + ":i"
	case OperationVerifyFlash:
		update = "flash:v:" + options.HexPath + ":i"
	case OperationReadEEPROM:
		update = "eeprom:r:" + options.OutputPath + ":i"
	case OperationWriteEEPROM:
		update = "eeprom:w:" + options.HexPath + ":i"
	case OperationChipErase:
		args = append(args, "--chip-erase")
	case OperationProbe, OperationStart:
		// Opening, initializing and signature validation are the operation.
	case OperationMetadata:
		return nil, errors.New("libavrdude adapter metadata is not yet supported; select the cli backend")
	default:
		return nil, fmt.Errorf("libavrdude adapter does not support operation %s", options.Operation)
	}
	if update != "" {
		args = append(args, "--update", update)
	}
	return args, nil
}

type libavrdudeEvent struct {
	Type    string `json:"type"`
	Percent int    `json:"percent"`
	Detail  string `json:"detail"`
	Message string `json:"message"`
	Level   int    `json:"level"`
	OK      bool   `json:"ok"`
}

type libavrdudeEventOutput struct {
	mu       sync.Mutex
	ctx      context.Context
	output   io.Writer
	stage    string
	buffer   []byte
	resultOK bool
}

func newLibavrdudeEventOutput(ctx context.Context, output io.Writer, stage string) *libavrdudeEventOutput {
	return &libavrdudeEventOutput{ctx: ctx, output: output, stage: stage}
}

func (writer *libavrdudeEventOutput) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.buffer = append(writer.buffer, data...)
	for {
		index := bytes.IndexByte(writer.buffer, '\n')
		if index < 0 {
			break
		}
		record := bytes.TrimSuffix(writer.buffer[:index], []byte{'\r'})
		writer.buffer = writer.buffer[index+1:]
		writer.observe(record)
	}
	if len(writer.buffer) > progressRecordLimit*8 {
		writer.buffer = writer.buffer[:0]
		return len(data), errors.New("libavrdude adapter emitted an oversized event")
	}
	return len(data), nil
}

func (writer *libavrdudeEventOutput) observe(record []byte) {
	if len(bytes.TrimSpace(record)) == 0 {
		return
	}
	var event libavrdudeEvent
	if err := json.Unmarshal(record, &event); err != nil {
		fmt.Fprintln(writer.output, string(record))
		return
	}
	switch event.Type {
	case "progress":
		detail := strings.TrimSpace(event.Detail)
		stage := writer.stage
		if detail != "" {
			stage += ":" + strings.ToLower(strings.TrimSuffix(detail, " ..."))
		}
		ReportProgress(writer.ctx, Progress{Stage: stage, Percent: event.Percent, Detail: detail})
	case "message", "error":
		if message := strings.TrimSpace(event.Message); message != "" {
			scanner := bufio.NewScanner(strings.NewReader(message))
			for scanner.Scan() {
				fmt.Fprintln(writer.output, scanner.Text())
			}
		}
	case "result":
		writer.resultOK = event.OK
	}
}

func (writer *libavrdudeEventOutput) completedSuccessfully() bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.resultOK
}

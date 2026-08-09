package programmer

import (
	"bufio"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	atmega328PSRAMCapacityBytes = uint32(2_048)
	minimumStackMarginBytes     = uint32(96)
	avrReturnAddressBytes       = uint32(2)
)

// compileManifestStackBudget records the host-only SRAM regression estimate.
// Its final-listing analysis has no runtime or flash cost on the AVR.
type compileManifestStackBudget struct {
	Analyzer                  string                       `json:"analyzer"`
	Method                    string                       `json:"method"`
	ELFPath                   string                       `json:"elfPath"`
	ListingPath               string                       `json:"listingPath"`
	StackUsageFiles           int                          `json:"stackUsageFiles"`
	StackUsageRecords         int                          `json:"stackUsageRecords"`
	StackUsageRole            string                       `json:"stackUsageRole"`
	SelectedResponseBranch    string                       `json:"selectedResponseBranch"`
	SRAMCapacityBytes         uint32                       `json:"sramCapacityBytes"`
	StaticSRAMBytes           uint32                       `json:"staticSramBytes"`
	StaticSections            []compileManifestSRAMSection `json:"staticSections"`
	SerialPathBytes           uint32                       `json:"serialPathBytes"`
	RFInterruptAllowanceBytes uint32                       `json:"rfInterruptAllowanceBytes"`
	EstimatedPeakSRAMBytes    uint32                       `json:"estimatedPeakSramBytes"`
	EstimatedFreeSRAMBytes    int32                        `json:"estimatedFreeSramBytes"`
	MinimumFreeSRAMBytes      uint32                       `json:"minimumFreeSramBytes"`
	SerialPath                []compileManifestStackStage  `json:"serialPath"`
	RFInterruptPath           []compileManifestStackStage  `json:"rfInterruptPath"`
}

type compileManifestSRAMSection struct {
	Name  string `json:"name"`
	Bytes uint32 `json:"bytes"`
}

type compileManifestStackStage struct {
	Name      string `json:"name"`
	Function  string `json:"function"`
	Bytes     uint32 `json:"bytes"`
	Qualifier string `json:"qualifier"`
	Source    string `json:"source,omitempty"`
}

// stackUsageRecord is retained for diagnostics and toolchain-change tests.
// AVR-GCC 7.3 emits empty .su files for this LTO build, so these records are
// deliberately never used as enforcement evidence.
type stackUsageRecord struct {
	Function  string
	Bytes     uint32
	Qualifier string
	Source    string
}

type avrListingInstruction struct {
	Address       uint32
	Mnemonic      string
	Operands      string
	Raw           string
	TargetAddress *uint32
}

type avrListingFunction struct {
	Name         string
	Address      uint32
	Instructions []avrListingInstruction
	RawLines     []string
}

type avrListing struct {
	Functions []*avrListingFunction
}

type listingStage struct {
	Manifest compileManifestStackStage
	Function *avrListingFunction
}

type listingFunctionSpec struct {
	Name               string
	Match              string
	Exact              bool
	InlineLabel        string
	InlineLabelAliases []string
}

type responseBranchSpec struct {
	Name     string
	Optional bool
	Stages   []listingFunctionSpec
}

var responseBranches = []responseBranchSpec{
	{Name: "hello", Stages: []listingFunctionSpec{{Name: "HELLO response", Match: "sendHello(", InlineLabel: "sendHello"}}},
	{Name: "telemetry", Stages: []listingFunctionSpec{{Name: "telemetry response", Match: "sendTelemetry(", InlineLabel: "sendTelemetry"}}},
	{Name: "settings", Stages: []listingFunctionSpec{{Name: "settings response", Match: "sendSettings(", InlineLabel: "sendSettings"}}},
	{Name: "PWM values", Stages: []listingFunctionSpec{{Name: "PWM response", Match: "sendPwmValues(", InlineLabel: "sendPwmValues"}}},
	{Name: "temperature list", Stages: []listingFunctionSpec{{Name: "temperature-list response", Match: "sendTemperatureList(", InlineLabel: "sendTemperatureList"}}},
	{Name: "front panel", Stages: []listingFunctionSpec{{Name: "front-panel response", Match: "sendFrontPanel(", InlineLabel: "sendFrontPanel"}}},
	{Name: "menu list", Optional: true, Stages: []listingFunctionSpec{{Name: "menu-list response", Match: "sendMenuList(", InlineLabel: "sendMenuList"}}},
	{Name: "menu layout", Optional: true, Stages: []listingFunctionSpec{{Name: "menu-layout response", Match: "sendMenuLayout(", InlineLabel: "sendMenuLayout"}}},
	{Name: "I2C transfer", Stages: []listingFunctionSpec{{Name: "I2C transfer response", Match: "transferI2c(", InlineLabel: "transferI2c"}}},
	{Name: "learned remotes", Stages: []listingFunctionSpec{{Name: "learned-remotes response", Match: "sendLearnedRemotes(", InlineLabel: "sendLearnedRemotes"}}},
	{Name: "ACK", Stages: []listingFunctionSpec{{Name: "ACK response", Match: "ControllerProtocol::UartProtocol::sendAck("}}},
	{Name: "error", Stages: []listingFunctionSpec{{Name: "error response", Match: "ControllerProtocol::UartProtocol::sendError("}}},
	{Name: "event", Stages: []listingFunctionSpec{{Name: "event response", Match: "ControllerEvents::send("}}},
	{Name: "macro status", Stages: []listingFunctionSpec{
		{Name: "macro opcode", Match: "MacroQueue::handle(", InlineLabel: "handle"},
		{Name: "macro status response", Match: "MacroQueue::sendStatus("},
	}},
	{Name: "macro ACK", Stages: []listingFunctionSpec{
		{Name: "macro opcode", Match: "MacroQueue::handle(", InlineLabel: "handle"},
		{Name: "macro ACK response", Match: "ControllerProtocol::UartProtocol::sendAck("},
	}},
	{Name: "macro error", Stages: []listingFunctionSpec{
		{Name: "macro opcode", Match: "MacroQueue::handle(", InlineLabel: "handle"},
		{Name: "macro error response", Match: "ControllerProtocol::UartProtocol::sendError("},
	}},
}

var (
	listingFunctionHeader = regexp.MustCompile(`^\s*([0-9A-Fa-f]+) <(.*)>:\s*$`)
	listingTargetAddress  = regexp.MustCompile(`;\s*0x([0-9A-Fa-f]+)\b`)
	listingHexImmediate   = regexp.MustCompile(`(?i)0x([0-9a-f]+)`)
)

// inspectFirmwareStackBudget joins final-ELF static SRAM with exact stack
// frames decoded from the final linked listing. Empty LTO .su sidecars are
// reported only so a toolchain change is visible in the manifest.
func inspectFirmwareStackBudget(identity CompileIdentity) (compileManifestStackBudget, error) {
	records, files, err := collectStackUsage(identity.BuildPath)
	if err != nil {
		return compileManifestStackBudget{}, err
	}
	elfPath, staticBytes, sections, err := inspectStaticSRAM(identity)
	if err != nil {
		return compileManifestStackBudget{}, err
	}
	listingPath, err := findCompileListing(identity)
	if err != nil {
		return compileManifestStackBudget{}, err
	}
	file, err := os.Open(listingPath)
	if err != nil {
		return compileManifestStackBudget{}, fmt.Errorf("open final AVR listing: %w", err)
	}
	listing, parseErr := parseAVRListing(file)
	closeErr := file.Close()
	if parseErr != nil {
		return compileManifestStackBudget{}, parseErr
	}
	if closeErr != nil {
		return compileManifestStackBudget{}, fmt.Errorf("close final AVR listing: %w", closeErr)
	}
	report, err := estimateFirmwareStackBudget(listing, staticBytes)
	if err != nil {
		return report, err
	}
	report.ELFPath = displayCompilePath(elfPath, identity.SourceRoot)
	report.ListingPath = displayCompilePath(listingPath, identity.SourceRoot)
	report.StackUsageFiles = files
	report.StackUsageRecords = len(records)
	report.StackUsageRole = "diagnostic-only; final LTO listing is enforcement evidence"
	report.StaticSections = sections
	return report, nil
}

func estimateFirmwareStackBudget(listing *avrListing, staticBytes uint32) (compileManifestStackBudget, error) {
	serial, selectedBranch, serialActive, err := buildSerialStackPath(listing)
	if err != nil {
		return compileManifestStackBudget{}, fmt.Errorf("serial response path: %w", err)
	}
	rf, rfActive, err := buildRFInterruptStackPath(listing)
	if err != nil {
		return compileManifestStackBudget{}, fmt.Errorf("RF INT0 allowance: %w", err)
	}

	serialEdges := serialActive - 1
	serialReturns := uint32(serialEdges) * avrReturnAddressBytes
	serial = append(serial, compileManifestStackStage{
		Name: "AVR call return addresses", Function: fmt.Sprintf("%d active CALL edges", serialEdges),
		Bytes: serialReturns, Qualifier: "architecture allowance",
	})
	serialBytes := sumStackStages(serial)

	// Hardware pushes one return PC on interrupt entry. Each subsequently
	// active ISR call adds another two-byte PC.
	rfCallEdges := rfActive - 1
	rfReturns := uint32(rfCallEdges+1) * avrReturnAddressBytes
	rf = append(rf, compileManifestStackStage{
		Name:     "AVR INT0/call return addresses",
		Function: fmt.Sprintf("interrupt entry + %d active CALL edges", rfCallEdges),
		Bytes:    rfReturns, Qualifier: "architecture allowance",
	})
	rfBytes := sumStackStages(rf)

	peak := uint64(staticBytes) + uint64(serialBytes) + uint64(rfBytes)
	remaining := int64(atmega328PSRAMCapacityBytes) - int64(peak)
	report := compileManifestStackBudget{
		Analyzer:                  "final-avr-listing/serial-response-plus-int0-v2",
		Method:                    "exact final prologue frames; conservative explicit/indirect critical topology",
		SelectedResponseBranch:    selectedBranch,
		StackUsageRole:            "diagnostic-only; final LTO listing is enforcement evidence",
		SRAMCapacityBytes:         atmega328PSRAMCapacityBytes,
		StaticSRAMBytes:           staticBytes,
		SerialPathBytes:           serialBytes,
		RFInterruptAllowanceBytes: rfBytes,
		EstimatedPeakSRAMBytes:    uint32(minUint64(peak, uint64(^uint32(0)))),
		EstimatedFreeSRAMBytes:    int32(maxInt64(remaining, int64(-1<<31))),
		MinimumFreeSRAMBytes:      minimumStackMarginBytes,
		SerialPath:                serial,
		RFInterruptPath:           rf,
	}
	if remaining < int64(minimumStackMarginBytes) {
		return report, fmt.Errorf(
			"SRAM stack guard: static %d + serial path %d + RF INT0 allowance %d = %d/%d bytes, leaving %d bytes; minimum safe margin is %d bytes",
			staticBytes, serialBytes, rfBytes, peak, atmega328PSRAMCapacityBytes,
			remaining, minimumStackMarginBytes,
		)
	}
	return report, nil
}

func buildSerialStackPath(listing *avrListing) ([]compileManifestStackStage, string, int, error) {
	mainStage, err := requiredListingStage(listing, listingFunctionSpec{Name: "Arduino main", Match: "main", Exact: true})
	if err != nil {
		return nil, "", 0, err
	}
	loopStage, err := functionOrInlineStage(listing, mainStage.Function, listingFunctionSpec{
		Name: "sketch loop", Match: "loop", Exact: true, InlineLabel: "loop",
		InlineLabelAliases: []string{"serviceController"},
	})
	if err != nil {
		return nil, "", 0, err
	}
	serviceStage, err := requiredListingStage(listing, listingFunctionSpec{
		Name: "UART service", Match: "ControllerProtocol::UartProtocol::service(",
	})
	if err != nil {
		return nil, "", 0, err
	}
	serviceCaller := mainStage.Function
	if loopStage.Function != nil {
		serviceCaller = loopStage.Function
	}
	if !functionCalls(serviceCaller, serviceStage.Function) {
		return nil, "", 0, fmt.Errorf("final listing has no %s -> %s CALL edge", serviceCaller.Name, serviceStage.Function.Name)
	}

	decodeStage, err := functionOrInlineStage(listing, serviceStage.Function, listingFunctionSpec{
		Name: "COBS frame dispatch", Match: "ControllerProtocol::UartProtocol::processEncodedFrame(", InlineLabel: "processEncodedFrame",
	})
	if err != nil {
		return nil, "", 0, err
	}
	handlerStage, err := requiredListingStage(listing, listingFunctionSpec{Name: "opcode handler", Match: "handleProtocolFrame("})
	if err != nil {
		return nil, "", 0, err
	}
	dispatcher := serviceStage.Function
	if decodeStage.Function != nil {
		if !functionCalls(serviceStage.Function, decodeStage.Function) {
			return nil, "", 0, fmt.Errorf("final listing has no UART service -> frame dispatch CALL edge")
		}
		dispatcher = decodeStage.Function
	}
	if !functionCalls(dispatcher, handlerStage.Function) && !functionHasMnemonic(dispatcher, "icall") {
		return nil, "", 0, fmt.Errorf("final listing has neither direct nor indirect opcode-handler dispatch")
	}

	sendStage, err := requiredListingStage(listing, listingFunctionSpec{Name: "UART response", Match: "ControllerProtocol::UartProtocol::send("})
	if err != nil {
		return nil, "", 0, err
	}
	writeCobsStage, err := functionOrInlineStage(listing, sendStage.Function, listingFunctionSpec{
		Name: "COBS response writer", Match: "ControllerProtocol::UartProtocol::writeCobs(", InlineLabel: "writeCobs",
	})
	if err != nil {
		return nil, "", 0, err
	}
	printStage, err := requiredListingStage(listing, listingFunctionSpec{Name: "Print buffer writer", Match: "Print::write(unsigned char const*"})
	if err != nil {
		return nil, "", 0, err
	}
	hardwareStage, err := requiredListingStage(listing, listingFunctionSpec{Name: "HardwareSerial byte writer", Match: "HardwareSerial::write(unsigned char)"})
	if err != nil {
		return nil, "", 0, err
	}
	if !functionHasMnemonic(sendStage.Function, "icall") {
		return nil, "", 0, errors.New("UART send no longer has the expected virtual Print write edge")
	}
	if !functionHasMnemonic(printStage.Function, "icall") {
		return nil, "", 0, errors.New("Print buffer writer no longer has the expected virtual byte-write edge")
	}

	prefix := []listingStage{mainStage, loopStage, serviceStage, decodeStage, handlerStage}
	common := []listingStage{sendStage, writeCobsStage, printStage, hardwareStage}
	if drain, ok, drainErr := optionalCalledListingStage(listing, hardwareStage.Function, listingFunctionSpec{
		Name: "HardwareSerial TX drain", Match: "HardwareSerial::_tx_udr_empty_irq(",
	}); drainErr != nil {
		return nil, "", 0, drainErr
	} else if ok {
		common = append(common, drain)
	}

	var selected []listingStage
	selectedName := ""
	selectedBytes := uint32(0)
	selectedActive := 0
	for _, branch := range responseBranches {
		if branch.Optional && !responseBranchPresent(listing, handlerStage.Function, branch) {
			continue
		}
		branchStages, active, branchErr := resolveResponseBranch(listing, handlerStage.Function, sendStage.Function, branch)
		if branchErr != nil {
			return nil, "", 0, branchErr
		}
		candidate := append(append(append([]listingStage{}, prefix...), branchStages...), common...)
		active += activeListingStages(prefix) + activeListingStages(common)
		edges := active - 1
		bytes := sumListingStages(candidate) + uint32(edges)*avrReturnAddressBytes
		if selected == nil || bytes > selectedBytes {
			selected = candidate
			selectedName = branch.Name
			selectedBytes = bytes
			selectedActive = active
		}
	}
	if selected == nil {
		return nil, "", 0, errors.New("no UART response branches were modeled")
	}
	manifest := make([]compileManifestStackStage, 0, len(selected)+1)
	manifest = append(manifest, compileManifestStackStage{
		Name: "selected response branch", Function: selectedName,
		Qualifier: "maximum final-listing branch",
	})
	for _, stage := range selected {
		manifest = append(manifest, stage.Manifest)
	}
	return manifest, selectedName, selectedActive, nil
}

func responseBranchPresent(listing *avrListing, parent *avrListingFunction, spec responseBranchSpec) bool {
	if len(spec.Stages) == 0 {
		return false
	}
	stage := spec.Stages[0]
	if len(matchingListingFunctions(listing, stage)) != 0 {
		return true
	}
	for _, label := range append([]string{stage.InlineLabel}, stage.InlineLabelAliases...) {
		if label != "" && functionHasSourceLabel(parent, label) {
			return true
		}
	}
	return false
}

func resolveResponseBranch(
	listing *avrListing,
	handler *avrListingFunction,
	send *avrListingFunction,
	spec responseBranchSpec,
) ([]listingStage, int, error) {
	parent := handler
	var stages []listingStage
	active := 0
	for _, functionSpec := range spec.Stages {
		stage, err := functionOrInlineStage(listing, parent, functionSpec)
		if err != nil {
			return nil, 0, fmt.Errorf("response branch %q: %w", spec.Name, err)
		}
		stages = append(stages, stage)
		if stage.Function == nil {
			continue
		}
		if !functionCalls(parent, stage.Function) {
			return nil, 0, fmt.Errorf("response branch %q has no %s -> %s CALL edge", spec.Name, parent.Name, stage.Function.Name)
		}
		parent = stage.Function
		active++
	}
	if !functionCalls(parent, send) {
		return nil, 0, fmt.Errorf("response branch %q has no %s -> UART send CALL edge", spec.Name, parent.Name)
	}
	return stages, active, nil
}

func buildRFInterruptStackPath(listing *avrListing) ([]compileManifestStackStage, int, error) {
	vector, err := requiredListingStage(listing, listingFunctionSpec{Name: "INT0 interrupt wrapper", Match: "__vector_1", Exact: true})
	if err != nil {
		return nil, 0, err
	}
	handler, err := requiredListingStage(listing, listingFunctionSpec{Name: "rc-switch edge handler", Match: "RCSwitch::handleInterrupt("})
	if err != nil {
		return nil, 0, err
	}
	micros, err := requiredListingStage(listing, listingFunctionSpec{Name: "edge timestamp", Match: "micros", Exact: true})
	if err != nil {
		return nil, 0, err
	}
	if !functionHasMnemonic(vector.Function, "icall") && !functionCalls(vector.Function, handler.Function) {
		return nil, 0, errors.New("INT0 wrapper no longer dispatches an interrupt callback")
	}
	if !functionCalls(handler.Function, micros.Function) {
		return nil, 0, errors.New("rc-switch handler no longer calls micros()")
	}
	stages := []listingStage{vector, handler, micros}
	manifest := make([]compileManifestStackStage, 0, len(stages))
	for _, stage := range stages {
		manifest = append(manifest, stage.Manifest)
	}
	return manifest, activeListingStages(stages), nil
}

func requiredListingStage(listing *avrListing, spec listingFunctionSpec) (listingStage, error) {
	functions := matchingListingFunctions(listing, spec)
	if len(functions) == 0 {
		return listingStage{}, fmt.Errorf("required final-listing function %q is missing", spec.Name)
	}
	var selected *avrListingFunction
	var selectedBytes uint32
	for _, function := range functions {
		bytes, err := listingStackFrame(listing, function)
		if err != nil {
			return listingStage{}, fmt.Errorf("%s: %w", spec.Name, err)
		}
		if selected == nil || bytes > selectedBytes {
			selected = function
			selectedBytes = bytes
		}
	}
	return listingStage{
		Function: selected,
		Manifest: compileManifestStackStage{
			Name: spec.Name, Function: selected.Name, Bytes: selectedBytes,
			Qualifier: "exact final linked frame",
			Source:    fmt.Sprintf("listing address 0x%04X", selected.Address),
		},
	}, nil
}

func functionOrInlineStage(listing *avrListing, parent *avrListingFunction, spec listingFunctionSpec) (listingStage, error) {
	functions := matchingListingFunctions(listing, spec)
	if len(functions) != 0 {
		return requiredListingStage(listing, spec)
	}
	inlineLabel := ""
	for _, candidate := range append([]string{spec.InlineLabel}, spec.InlineLabelAliases...) {
		if candidate != "" && functionHasSourceLabel(parent, candidate) {
			inlineLabel = candidate
			break
		}
	}
	if inlineLabel == "" {
		return listingStage{}, fmt.Errorf("required function/inlined marker %q is missing", spec.Name)
	}
	return listingStage{Manifest: compileManifestStackStage{
		Name: spec.Name, Function: inlineLabel + "()",
		Qualifier: "inlined; frame included by " + parent.Name,
		Source:    fmt.Sprintf("listing parent address 0x%04X", parent.Address),
	}}, nil
}

func optionalCalledListingStage(
	listing *avrListing,
	caller *avrListingFunction,
	spec listingFunctionSpec,
) (listingStage, bool, error) {
	functions := matchingListingFunctions(listing, spec)
	for _, function := range functions {
		if !functionCalls(caller, function) {
			continue
		}
		stage, err := requiredListingStage(listing, spec)
		return stage, err == nil, err
	}
	return listingStage{}, false, nil
}

func matchingListingFunctions(listing *avrListing, spec listingFunctionSpec) []*avrListingFunction {
	if listing == nil {
		return nil
	}
	var matches []*avrListingFunction
	for _, function := range listing.Functions {
		base := listingFunctionBase(function.Name)
		if (spec.Exact && base == spec.Match) || (!spec.Exact && strings.Contains(base, spec.Match)) {
			matches = append(matches, function)
		}
	}
	return matches
}

func listingFunctionBase(function string) string {
	function = strings.TrimSpace(function)
	if index := strings.Index(function, " [clone "); index >= 0 {
		function = function[:index]
	}
	return strings.TrimSpace(function)
}

func functionCalls(caller, target *avrListingFunction) bool {
	if caller == nil || target == nil {
		return false
	}
	for _, instruction := range caller.Instructions {
		if instruction.Mnemonic != "call" && instruction.Mnemonic != "rcall" {
			continue
		}
		if instruction.TargetAddress != nil && *instruction.TargetAddress == target.Address {
			return true
		}
	}
	return false
}

func functionHasMnemonic(function *avrListingFunction, mnemonic string) bool {
	if function == nil {
		return false
	}
	for _, instruction := range function.Instructions {
		if instruction.Mnemonic == mnemonic {
			return true
		}
	}
	return false
}

func functionHasSourceLabel(function *avrListingFunction, label string) bool {
	if function == nil {
		return false
	}
	want := label + "():"
	for _, line := range function.RawLines {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func listingStackFrame(listing *avrListing, function *avrListingFunction) (uint32, error) {
	if function == nil || len(function.Instructions) == 0 {
		return 0, errors.New("function has no listing instructions")
	}
	if bytes, found, err := sharedPrologueFrame(listing, function); found || err != nil {
		return bytes, err
	}

	var frame uint32
	started := false
	limit := len(function.Instructions)
	if limit > 48 {
		limit = 48
	}
	for _, instruction := range function.Instructions[:limit] {
		switch instruction.Mnemonic {
		case "push":
			frame++
			started = true
		case "rcall":
			if strings.HasPrefix(strings.TrimSpace(instruction.Operands), ".+0") {
				frame += avrReturnAddressBytes
				started = true
				continue
			}
			return frame, nil
		case "sbiw":
			if started && strings.HasPrefix(strings.TrimSpace(instruction.Operands), "r28") {
				value, err := lastInstructionImmediate(instruction.Operands)
				if err != nil {
					return 0, fmt.Errorf("decode local stack allocation in %s: %w", function.Name, err)
				}
				frame += value
				continue
			}
			return frame, nil
		case "in", "out", "cli":
			if started {
				continue
			}
			return 0, nil
		case "eor":
			if started && strings.ReplaceAll(instruction.Operands, " ", "") == "r1,r1" {
				continue
			}
			return frame, nil
		default:
			return frame, nil
		}
	}
	return frame, nil
}

func sharedPrologueFrame(listing *avrListing, function *avrListingFunction) (uint32, bool, error) {
	var low, high uint32
	var haveLow, haveHigh bool
	limit := len(function.Instructions)
	if limit > 10 {
		limit = 10
	}
	for _, instruction := range function.Instructions[:limit] {
		if instruction.Mnemonic == "ldi" {
			register, value, ok := decodeLDI(instruction.Operands)
			if ok && register == "r26" {
				low, haveLow = value, true
			}
			if ok && register == "r27" {
				high, haveHigh = value, true
			}
		}
		if instruction.Mnemonic != "jmp" || !strings.Contains(instruction.Raw, "__prologue_saves__") {
			continue
		}
		if !haveLow || !haveHigh || instruction.TargetAddress == nil {
			return 0, true, fmt.Errorf("cannot decode shared prologue setup for %s", function.Name)
		}
		prologueMatches := matchingListingFunctions(listing, listingFunctionSpec{Match: "__prologue_saves__", Exact: true})
		if len(prologueMatches) != 1 {
			return 0, true, errors.New("final listing has no unique __prologue_saves__")
		}
		pushes := uint32(0)
		foundTarget := false
		for _, prologueInstruction := range prologueMatches[0].Instructions {
			if prologueInstruction.Address == *instruction.TargetAddress {
				foundTarget = true
			}
			if !foundTarget {
				continue
			}
			if prologueInstruction.Mnemonic != "push" {
				break
			}
			pushes++
		}
		if !foundTarget || pushes == 0 {
			return 0, true, fmt.Errorf("shared prologue target 0x%04X for %s has no push sequence", *instruction.TargetAddress, function.Name)
		}
		return low + high*256 + pushes, true, nil
	}
	return 0, false, nil
}

func decodeLDI(operands string) (string, uint32, bool) {
	parts := strings.Split(operands, ",")
	if len(parts) < 2 {
		return "", 0, false
	}
	value, err := instructionImmediate(parts[1])
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(parts[0]), value, true
}

func lastInstructionImmediate(operands string) (uint32, error) {
	parts := strings.Split(operands, ",")
	if len(parts) < 2 {
		return 0, fmt.Errorf("missing immediate in %q", operands)
	}
	return instructionImmediate(parts[len(parts)-1])
}

func instructionImmediate(value string) (uint32, error) {
	value = strings.TrimSpace(value)
	if match := listingHexImmediate.FindStringSubmatch(value); match != nil {
		parsed, err := strconv.ParseUint(match[1], 16, 32)
		return uint32(parsed), err
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, errors.New("empty immediate")
	}
	parsed, err := strconv.ParseUint(fields[0], 10, 32)
	return uint32(parsed), err
}

func parseAVRListing(input io.Reader) (*avrListing, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	listing := &avrListing{}
	var current *avrListingFunction
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		if match := listingFunctionHeader.FindStringSubmatch(line); match != nil {
			address, err := strconv.ParseUint(match[1], 16, 32)
			if err != nil {
				return nil, fmt.Errorf("parse final AVR listing line %d function address: %w", lineNumber, err)
			}
			current = &avrListingFunction{Name: match[2], Address: uint32(address)}
			listing.Functions = append(listing.Functions, current)
			continue
		}
		if current == nil {
			continue
		}
		current.RawLines = append(current.RawLines, line)
		if instruction, ok := parseListingInstruction(line); ok {
			current.Instructions = append(current.Instructions, instruction)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read final AVR listing: %w", err)
	}
	if len(listing.Functions) == 0 {
		return nil, errors.New("final AVR listing contains no function symbols")
	}
	return listing, nil
}

func parseListingInstruction(line string) (avrListingInstruction, bool) {
	trimmed := strings.TrimSpace(line)
	colon := strings.IndexByte(trimmed, ':')
	if colon <= 0 {
		return avrListingInstruction{}, false
	}
	address, err := strconv.ParseUint(trimmed[:colon], 16, 32)
	if err != nil {
		return avrListingInstruction{}, false
	}
	rest := strings.TrimSpace(trimmed[colon+1:])
	fields := strings.Fields(rest)
	index := 0
	for index < len(fields) && isHexOpcodeByte(fields[index]) {
		index++
	}
	if index == 0 || index >= len(fields) {
		return avrListingInstruction{}, false
	}
	mnemonic := strings.ToLower(fields[index])
	operands := strings.Join(fields[index+1:], " ")
	if comment := strings.IndexByte(operands, ';'); comment >= 0 {
		operands = strings.TrimSpace(operands[:comment])
	}
	instruction := avrListingInstruction{
		Address: uint32(address), Mnemonic: mnemonic,
		Operands: operands, Raw: line,
	}
	if match := listingTargetAddress.FindStringSubmatch(line); match != nil {
		if target, parseErr := strconv.ParseUint(match[1], 16, 32); parseErr == nil {
			value := uint32(target)
			instruction.TargetAddress = &value
		}
	} else if (mnemonic == "call" || mnemonic == "jmp") && strings.HasPrefix(operands, "0x") {
		if target, parseErr := strconv.ParseUint(strings.TrimPrefix(strings.Fields(operands)[0], "0x"), 16, 32); parseErr == nil {
			value := uint32(target)
			instruction.TargetAddress = &value
		}
	}
	return instruction, true
}

func isHexOpcodeByte(value string) bool {
	if len(value) != 2 {
		return false
	}
	_, err := strconv.ParseUint(value, 16, 8)
	return err == nil
}

func activeListingStages(stages []listingStage) int {
	active := 0
	for _, stage := range stages {
		if stage.Function != nil {
			active++
		}
	}
	return active
}

func sumListingStages(stages []listingStage) uint32 {
	var total uint32
	for _, stage := range stages {
		total += stage.Manifest.Bytes
	}
	return total
}

// purgeStackUsageFiles prevents diagnostic records from an older compile from
// being mistaken for files emitted by the current LTO build.
func purgeStackUsageFiles(buildPath string) (int, error) {
	if strings.TrimSpace(buildPath) == "" {
		return 0, errors.New("stack-usage purge requires CompileIdentity.BuildPath")
	}
	if _, err := os.Stat(buildPath); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("inspect Arduino build path before stack-usage purge: %w", err)
	}
	var paths []string
	err := filepath.WalkDir(buildPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".su") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("enumerate stale stack-usage files: %w", err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return 0, fmt.Errorf("remove stale stack-usage file %s: %w", path, err)
		}
	}
	return len(paths), nil
}

func collectStackUsage(buildPath string) ([]stackUsageRecord, int, error) {
	if strings.TrimSpace(buildPath) == "" {
		return nil, 0, errors.New("stack-usage diagnostics require CompileIdentity.BuildPath")
	}
	var paths []string
	err := filepath.WalkDir(buildPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".su") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("enumerate GCC stack-usage files: %w", err)
	}
	sort.Strings(paths)
	var records []stackUsageRecord
	for _, path := range paths {
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil, 0, fmt.Errorf("open stack-usage file %s: %w", path, openErr)
		}
		parsed, parseErr := parseStackUsage(file, path)
		closeErr := file.Close()
		if parseErr != nil {
			return nil, 0, parseErr
		}
		if closeErr != nil {
			return nil, 0, fmt.Errorf("close stack-usage file %s: %w", path, closeErr)
		}
		records = append(records, parsed...)
	}
	return records, len(paths), nil
}

func parseStackUsage(input io.Reader, path string) ([]stackUsageRecord, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 256*1024)
	var records []stackUsageRecord
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			fields = strings.Fields(line)
			if len(fields) < 3 {
				return nil, fmt.Errorf("parse stack-usage file %s:%d: expected function, bytes, and qualifier", path, lineNumber)
			}
			fields = []string{strings.Join(fields[:len(fields)-2], " "), fields[len(fields)-2], fields[len(fields)-1]}
		}
		bytes, err := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("parse stack-usage file %s:%d byte count %q: %w", path, lineNumber, fields[1], err)
		}
		location, function := splitStackLocation(strings.TrimSpace(fields[0]))
		if function == "" {
			return nil, fmt.Errorf("parse stack-usage file %s:%d: empty function", path, lineNumber)
		}
		records = append(records, stackUsageRecord{
			Function: function, Bytes: uint32(bytes),
			Qualifier: strings.TrimSpace(strings.Join(fields[2:], " ")),
			Source:    location,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stack-usage file %s: %w", path, err)
	}
	return records, nil
}

func splitStackLocation(value string) (string, string) {
	// GCC emits path:line:column:function. Scan numeric fields from the left so
	// Windows drive-letter and C++ namespace colons remain intact.
	for index := 0; index < len(value); index++ {
		if value[index] != ':' {
			continue
		}
		lineEnd := strings.IndexByte(value[index+1:], ':')
		if lineEnd < 0 {
			continue
		}
		lineEnd += index + 1
		if _, err := strconv.Atoi(value[index+1 : lineEnd]); err != nil {
			continue
		}
		columnEnd := strings.IndexByte(value[lineEnd+1:], ':')
		if columnEnd < 0 {
			continue
		}
		columnEnd += lineEnd + 1
		if _, err := strconv.Atoi(value[lineEnd+1 : columnEnd]); err != nil {
			continue
		}
		return value[:columnEnd], strings.TrimSpace(value[columnEnd+1:])
	}
	return "", strings.TrimSpace(value)
}

func inspectStaticSRAM(identity CompileIdentity) (string, uint32, []compileManifestSRAMSection, error) {
	path, err := findCompileELF(identity)
	if err != nil {
		return "", 0, nil, err
	}
	document, err := elf.Open(path)
	if err != nil {
		return "", 0, nil, fmt.Errorf("open AVR ELF for static SRAM analysis: %w", err)
	}
	defer document.Close()
	wanted := map[string]bool{".data": true, ".bss": true, ".noinit": true}
	var total uint64
	var sections []compileManifestSRAMSection
	for _, section := range document.Sections {
		if !wanted[section.Name] {
			continue
		}
		if section.Size > uint64(^uint32(0)) {
			return "", 0, nil, fmt.Errorf("AVR ELF section %s is too large", section.Name)
		}
		sections = append(sections, compileManifestSRAMSection{Name: section.Name, Bytes: uint32(section.Size)})
		total += section.Size
	}
	if len(sections) == 0 {
		return "", 0, nil, errors.New("AVR ELF has none of .data, .bss, or .noinit")
	}
	if total > uint64(^uint32(0)) {
		return "", 0, nil, errors.New("AVR ELF static SRAM total overflows uint32")
	}
	sort.Slice(sections, func(left, right int) bool { return sections[left].Name < sections[right].Name })
	return path, uint32(total), sections, nil
}

func findCompileELF(identity CompileIdentity) (string, error) {
	var paths []string
	for _, root := range []string{identity.OutputDir, identity.BuildPath} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries, err := filepath.Glob(filepath.Join(root, "*.elf"))
		if err != nil {
			return "", fmt.Errorf("enumerate AVR ELF files in %s: %w", root, err)
		}
		paths = append(paths, entries...)
		if len(paths) != 0 {
			break
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "", errors.New("successful Arduino compile produced no AVR ELF for static SRAM analysis")
	}
	for _, path := range paths {
		if strings.EqualFold(filepath.Base(path), "PCController.ino.elf") {
			return path, nil
		}
	}
	return paths[0], nil
}

func findCompileListing(identity CompileIdentity) (string, error) {
	var paths []string
	for _, root := range []string{identity.BuildPath, identity.OutputDir} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries, err := filepath.Glob(filepath.Join(root, "*.lst"))
		if err != nil {
			return "", fmt.Errorf("enumerate final AVR listings in %s: %w", root, err)
		}
		paths = append(paths, entries...)
		if len(paths) != 0 {
			break
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "", errors.New("successful Arduino compile produced no final AVR .lst listing for stack analysis")
	}
	for _, path := range paths {
		if strings.Contains(strings.ToLower(filepath.Base(path)), "pccontroller.ino") {
			return path, nil
		}
	}
	return paths[0], nil
}

func printFirmwareStackBudget(output io.Writer, report compileManifestStackBudget) {
	if output == nil {
		return
	}
	fmt.Fprintf(output,
		"SRAM stack guard: static %d + serial response %d + RF INT0 %d = %d/%d bytes; estimated margin %d bytes (minimum %d).\n",
		report.StaticSRAMBytes, report.SerialPathBytes,
		report.RFInterruptAllowanceBytes, report.EstimatedPeakSRAMBytes,
		report.SRAMCapacityBytes, report.EstimatedFreeSRAMBytes,
		report.MinimumFreeSRAMBytes,
	)
	fmt.Fprintf(output, "Stack evidence: %s; selected response branch %s; listing %s\n",
		report.Analyzer, report.SelectedResponseBranch, report.ListingPath)
	fmt.Fprintf(output, "Stack-usage diagnostics: %d records from %d .su files (%s)\n",
		report.StackUsageRecords, report.StackUsageFiles, report.StackUsageRole)
	for _, stage := range append(append([]compileManifestStackStage{}, report.SerialPath...), report.RFInterruptPath...) {
		fmt.Fprintf(output, "  %-31s %3d B  %s\n", stage.Name+":", stage.Bytes, stage.Function)
	}
}

func sumStackStages(stages []compileManifestStackStage) uint32 {
	var total uint32
	for _, stage := range stages {
		total += stage.Bytes
	}
	return total
}

func displayCompilePath(path, sourceRoot string) string {
	relative, err := filepath.Rel(sourceRoot, path)
	if err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative)
	}
	return path
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

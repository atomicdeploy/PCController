package programmer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndCollectGCCStackUsageDiagnostics(t *testing.T) {
	root := t.TempDir()
	populated := filepath.Join(root, "sketch", "PCController.ino.cpp.su")
	empty := filepath.Join(root, "libraries", "rc-switch", "RCSwitch.cpp.su")
	for _, path := range []string{populated, empty} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(populated, []byte(
		"C:\\work\\PCController.ino:2487:6:handleProtocolFrame(ControllerProtocol::Frame const&, void*)\t52\tstatic\n"+
			"C:\\work\\PCController.ino:3001:6:loop()\t4\tdynamic,bounded\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	records, files, err := collectStackUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 || len(records) != 2 {
		t.Fatalf("files=%d records=%#v", files, records)
	}
	if records[0].Bytes != 52 || records[0].Source != `C:\work\PCController.ino:2487:6` {
		t.Fatalf("Windows/C++ record parsed incorrectly: %#v", records[0])
	}
	if records[1].Qualifier != "dynamic,bounded" {
		t.Fatalf("bounded qualifier lost: %#v", records[1])
	}
}

func TestEmptyLTOStackUsageSidecarsRemainDiagnosticOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sketch", "PCController.ino.cpp.su")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	records, files, err := collectStackUsage(root)
	if err != nil || files != 1 || len(records) != 0 {
		t.Fatalf("empty LTO sidecar diagnostics: files=%d records=%#v err=%v", files, records, err)
	}
}

func TestPurgeStackUsageFilesRemovesOnlySidecars(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "sketch", "one.su"),
		filepath.Join(root, "libraries", "two.SU"),
	}
	keep := filepath.Join(root, "sketch", "keep.o")
	for _, path := range append(paths, keep) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("evidence"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := purgeStackUsageFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed=%d", removed)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar remains: %s (%v)", path, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("non-sidecar was removed: %v", err)
	}
}

func TestFinalListingFrameParserAndBudget(t *testing.T) {
	listing := parseListingFixture(t, completeAVRListingFixture())
	checks := map[string]uint32{
		"main":                         8,
		"handleProtocolFrame(Frame)":   24,
		"sendTelemetry(unsigned char)": 17,
		"ControllerProtocol::UartProtocol::send(unsigned char)":                           1,
		"ControllerProtocol::UartProtocol::sendTimestamped(unsigned char, unsigned long)": 2,
		"__vector_1":                  3,
		"RCSwitch::handleInterrupt()": 14,
		"micros":                      0,
	}
	for name, want := range checks {
		function := exactFixtureFunction(t, listing, name)
		got, err := listingStackFrame(listing, function)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s frame=%d want=%d", name, got, want)
		}
	}

	report, err := estimateFirmwareStackBudget(listing, 1800)
	if err != nil {
		t.Fatal(err)
	}
	if report.SelectedResponseBranch != "telemetry" ||
		report.SerialPathBytes != 73 || report.RFInterruptAllowanceBytes != 23 ||
		report.EstimatedPeakSRAMBytes != 1896 || report.EstimatedFreeSRAMBytes != 152 {
		t.Fatalf("unexpected budget: %#v", report)
	}
	serialAllowance := report.SerialPath[len(report.SerialPath)-1]
	if serialAllowance.Bytes != 16 || serialAllowance.Function != "8 active CALL edges" {
		t.Fatalf("serial return allowance=%#v", serialAllowance)
	}
	rfAllowance := report.RFInterruptPath[len(report.RFInterruptPath)-1]
	if rfAllowance.Bytes != 6 || rfAllowance.Function != "interrupt entry + 2 active CALL edges" {
		t.Fatalf("RF return allowance=%#v", rfAllowance)
	}
}

func TestFinalListingAcceptsModularLifecycleInlineMarker(t *testing.T) {
	fixture := strings.Replace(
		completeAVRListingFixture(),
		"loop():\n",
		"serviceController():\n",
		1,
	)
	report, err := estimateFirmwareStackBudget(parseListingFixture(t, fixture), 1800)
	if err != nil {
		t.Fatal(err)
	}
	if report.SerialPath[2].Function != "serviceController()" ||
		report.SerialPathBytes != 73 {
		t.Fatalf("modular lifecycle marker changed stack topology: %#v", report)
	}
}

func TestFinalListingBudgetRejectsLessThanNinetySixBytes(t *testing.T) {
	listing := parseListingFixture(t, completeAVRListingFixture())
	_, err := estimateFirmwareStackBudget(listing, 1857)
	if err == nil || !strings.Contains(err.Error(), "leaving 95 bytes") ||
		!strings.Contains(err.Error(), "minimum safe margin is 96 bytes") {
		t.Fatalf("unexpected guard failure: %v", err)
	}
	if report, err := estimateFirmwareStackBudget(listing, 1856); err != nil ||
		report.EstimatedFreeSRAMBytes != 96 {
		t.Fatalf("exact minimum margin should pass: report=%#v err=%v", report, err)
	}
}

func TestFinalListingComputesCallEdgesFromSelectedPath(t *testing.T) {
	withoutDrain := strings.Replace(
		completeAVRListingFixture(),
		"    1502:  0e 94 00 0b  call 0x1600 ; 0x1600 <HardwareSerial::_tx_udr_empty_irq()>\n",
		"    1502:  08 95        ret\n",
		1,
	)
	report, err := estimateFirmwareStackBudget(parseListingFixture(t, withoutDrain), 1800)
	if err != nil {
		t.Fatal(err)
	}
	allowance := report.SerialPath[len(report.SerialPath)-1]
	if report.SerialPathBytes != 71 || allowance.Bytes != 14 || allowance.Function != "7 active CALL edges" {
		t.Fatalf("computed call edges did not follow topology: report=%#v allowance=%#v", report, allowance)
	}
}

func TestFinalListingBudgetRejectsMissingCriticalTopology(t *testing.T) {
	missingDispatch := strings.Replace(
		completeAVRListingFixture(),
		"    1104:  09 95        icall\n",
		"    1104:  08 95        ret\n",
		1,
	)
	if _, err := estimateFirmwareStackBudget(parseListingFixture(t, missingDispatch), 1500); err == nil ||
		!strings.Contains(err.Error(), "opcode-handler dispatch") {
		t.Fatalf("missing dispatch was not rejected: %v", err)
	}

	missingMicros := strings.Replace(
		completeAVRListingFixture(),
		"    210a:  0e 94 00 11  call 0x2200 ; 0x2200 <micros>\n",
		"    210a:  00 00        nop\n",
		1,
	)
	if _, err := estimateFirmwareStackBudget(parseListingFixture(t, missingMicros), 1500); err == nil ||
		!strings.Contains(err.Error(), "no longer calls micros") {
		t.Fatalf("missing RF timestamp edge was not rejected: %v", err)
	}

	withoutOptionalMenus := strings.NewReplacer(
		"sendMenuList():\n", "",
		"sendMenuLayout():\n", "",
	).Replace(completeAVRListingFixture())
	if _, err := estimateFirmwareStackBudget(parseListingFixture(t, withoutOptionalMenus), 1500); err != nil {
		t.Fatalf("feature-disabled optional response branches were rejected: %v", err)
	}
}

func TestParseStackUsageRejectsMalformedDiagnostic(t *testing.T) {
	_, err := parseStackUsage(strings.NewReader("function\tnot-a-number\tstatic\n"), "bad.su")
	if err == nil || !strings.Contains(err.Error(), "bad.su:1") {
		t.Fatalf("malformed diagnostic error=%v", err)
	}
}

func TestPrintFirmwareStackBudgetShowsFinalEvidence(t *testing.T) {
	report, err := estimateFirmwareStackBudget(parseListingFixture(t, completeAVRListingFixture()), 1800)
	if err != nil {
		t.Fatal(err)
	}
	report.StackUsageFiles = 4
	report.StackUsageRecords = 0
	report.ListingPath = ".build/firmware/PCController.lst"
	var output bytes.Buffer
	printFirmwareStackBudget(&output, report)
	for _, expected := range []string{
		"static 1800 + serial response 73 + RF INT0 23 = 1896/2048 bytes",
		"estimated margin 152 bytes (minimum 96)",
		"selected response branch telemetry",
		"0 records from 4 .su files",
		"opcode handler:", "edge timestamp:",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in output:\n%s", expected, output.String())
		}
	}
}

func TestProductionListingTimestampedResponseTopology(t *testing.T) {
	path := os.Getenv("PCCONTROLLER_FINAL_AVR_LISTING")
	if path == "" {
		t.Skip("set PCCONTROLLER_FINAL_AVR_LISTING to validate a linked production listing")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := parseAVRListing(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("parse=%v close=%v", err, closeErr)
	}
	report, err := estimateFirmwareStackBudget(listing, 1465)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, stage := range report.SerialPath {
		if stage.Name == "timestamped UART response" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("timestamped UART stage missing: %#v", report.SerialPath)
	}
}

func parseListingFixture(t *testing.T, value string) *avrListing {
	t.Helper()
	listing, err := parseAVRListing(strings.NewReader(value))
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func exactFixtureFunction(t *testing.T, listing *avrListing, name string) *avrListingFunction {
	t.Helper()
	for _, function := range listing.Functions {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("fixture function %q missing", name)
	return nil
}

func completeAVRListingFixture() string {
	return `firmware.elf: file format elf32-avr

00001000 <main>:
main():
    1000:  a4 e0        ldi r26, 0x04
    1002:  b0 e0        ldi r27, 0x00
    1004:  e0 e0        ldi r30, 0x00
    1006:  f0 e0        ldi r31, 0x00
    1008:  0c 94 00 48  jmp 0x9000 ; 0x9000 <__prologue_saves__>
loop():
    100c:  0e 94 80 08  call 0x1100 ; 0x1100 <ControllerProtocol::UartProtocol::service() [clone .constprop.1]>

00001100 <ControllerProtocol::UartProtocol::service() [clone .constprop.1]>:
service():
    1100:  0f 93        push r16
    1102:  1f 93        push r17
processEncodedFrame():
    1104:  09 95        icall

00001200 <handleProtocolFrame(Frame)>:
    1200:  a4 e1        ldi r26, 0x14
    1202:  b0 e0        ldi r27, 0x00
    1204:  e0 e0        ldi r30, 0x00
    1206:  f0 e0        ldi r31, 0x00
    1208:  0c 94 00 48  jmp 0x9000 ; 0x9000 <__prologue_saves__>
sendSettings():
sendPwmValues():
sendTemperatureList():
sendFrontPanel():
sendMenuList():
sendMenuLayout():
transferI2c():
sendLearnedRemotes():
handle():
    120c:  0e 94 80 0b  call 0x1700 ; 0x1700 <sendHello(unsigned char)>
    1210:  0e 94 00 0c  call 0x1800 ; 0x1800 <sendTelemetry(unsigned char)>
    1214:  0e 94 80 0c  call 0x1900 ; 0x1900 <ControllerProtocol::UartProtocol::sendAck(unsigned char, unsigned char)>
    1218:  0e 94 00 0d  call 0x1a00 ; 0x1a00 <ControllerProtocol::UartProtocol::sendError(unsigned char, unsigned char, Error)>
    121c:  0e 94 80 0d  call 0x1b00 ; 0x1b00 <ControllerEvents::send(unsigned char const*, unsigned char)>
    1220:  0e 94 00 0e  call 0x1c00 ; 0x1c00 <MacroQueue::sendStatus(unsigned char, unsigned char)>
    1224:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001300 <ControllerProtocol::UartProtocol::send(unsigned char)>:
    1300:  0f 93        push r16
	1302:  0e 94 c0 09  call 0x1380 ; 0x1380 <ControllerProtocol::UartProtocol::sendTimestamped(unsigned char, unsigned long)>

00001380 <ControllerProtocol::UartProtocol::sendTimestamped(unsigned char, unsigned long)>:
	1380:  0f 93        push r16
	1382:  1f 93        push r17
writeCobs():
	1384:  09 95        icall

00001400 <Print::write(unsigned char const*, unsigned int)>:
    1400:  0f 93        push r16
    1402:  1f 93        push r17
    1404:  09 95        icall

00001500 <HardwareSerial::write(unsigned char)>:
    1500:  0f 93        push r16
    1502:  0e 94 00 0b  call 0x1600 ; 0x1600 <HardwareSerial::_tx_udr_empty_irq()>

00001600 <HardwareSerial::_tx_udr_empty_irq()>:
    1600:  08 95        ret

00001700 <sendHello(unsigned char)>:
    1700:  0f 93        push r16
    1702:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001800 <sendTelemetry(unsigned char)>:
    1800:  0f 93        push r16
    1802:  cf 93        push r28
    1804:  df 93        push r29
    1806:  cd b7        in r28, 0x3d
    1808:  de b7        in r29, 0x3e
    180a:  2e 97        sbiw r28, 0x0e
    180c:  de bf        out 0x3e, r29
    180e:  cd bf        out 0x3d, r28
    1810:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001900 <ControllerProtocol::UartProtocol::sendAck(unsigned char, unsigned char)>:
    1900:  0f 93        push r16
    1902:  cf 93        push r28
    1904:  df 93        push r29
    1906:  00 d0        rcall .+0 ; 0x1908 <ControllerProtocol::UartProtocol::sendAck(unsigned char, unsigned char)+0x8>
    1908:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001a00 <ControllerProtocol::UartProtocol::sendError(unsigned char, unsigned char, Error)>:
    1a00:  0f 93        push r16
    1a02:  cf 93        push r28
    1a04:  df 93        push r29
    1a06:  00 d0        rcall .+0 ; 0x1a08 <ControllerProtocol::UartProtocol::sendError(unsigned char, unsigned char, Error)+0x8>
    1a08:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001b00 <ControllerEvents::send(unsigned char const*, unsigned char)>:
    1b00:  0f 93        push r16
    1b02:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00001c00 <MacroQueue::sendStatus(unsigned char, unsigned char)>:
    1c00:  0f 93        push r16
    1c02:  0e 94 80 09  call 0x1300 ; 0x1300 <Uart::send(unsigned char)>

00002000 <__vector_1>:
    2000:  1f 92        push r1
    2002:  0f 92        push r0
    2004:  0f b6        in r0, 0x3f
    2006:  0f 92        push r0
    2008:  11 24        eor r1, r1
    200a:  09 95        icall

00002100 <RCSwitch::handleInterrupt()>:
    2100:  aa e0        ldi r26, 0x0a
    2102:  b0 e0        ldi r27, 0x00
    2104:  e0 e0        ldi r30, 0x00
    2106:  f0 e0        ldi r31, 0x00
    2108:  0c 94 00 48  jmp 0x9000 ; 0x9000 <__prologue_saves__>
    210a:  0e 94 00 11  call 0x2200 ; 0x2200 <micros>

00002200 <micros>:
    2200:  08 95        ret

00009000 <__prologue_saves__>:
    9000:  0f 93        push r16
    9002:  1f 93        push r17
    9004:  cf 93        push r28
    9006:  df 93        push r29
    9008:  cd b7        in r28, 0x3d
`
}

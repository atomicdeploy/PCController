package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"testing"
)

func TestCOBSRoundTrip(t *testing.T) {
	cases := [][]byte{
		{0},
		{1},
		{1, 2, 3},
		{0, 1, 0, 2, 0},
		bytes.Repeat([]byte{0x55}, 254),
	}
	for _, input := range cases {
		encoded := COBSEncode(input)
		if bytes.Contains(encoded, []byte{0}) {
			t.Fatalf("encoded COBS contains zero: % X", encoded)
		}
		decoded, err := COBSDecode(encoded)
		if err != nil {
			t.Fatalf("decode % X: %v", input, err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("round trip got % X want % X", decoded, input)
		}
	}
}

func TestFrameRoundTripRandomPayloads(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	for length := 0; length <= MaxPayload; length++ {
		for sample := 0; sample < 20; sample++ {
			payload := make([]byte, length)
			_, _ = random.Read(payload)
			frame := Frame{Opcode: OpPWMSet, Seq: byte(sample + 1), Payload: payload}
			encoded, err := Encode(frame)
			if err != nil {
				t.Fatal(err)
			}
			if encoded[len(encoded)-1] != 0 {
				t.Fatal("encoded frame has no zero delimiter")
			}
			decoded, err := Decode(encoded)
			if err != nil {
				t.Fatalf("decode length %d: %v", length, err)
			}
			if decoded.Opcode != frame.Opcode || decoded.Seq != frame.Seq ||
				!bytes.Equal(decoded.Payload, payload) {
				t.Fatalf("round trip mismatch: %#v != %#v", decoded, frame)
			}
		}
	}
}

func TestDecodeRejectsCRCAndLength(t *testing.T) {
	encoded, err := Encode(Frame{Opcode: OpHello, Seq: 4, Payload: []byte{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := COBSDecode(encoded[:len(encoded)-1])
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0x40
	_, err = Decode(COBSEncode(raw))
	if !errors.Is(err, ErrBadCRC) {
		t.Fatalf("got %v, want ErrBadCRC", err)
	}

	raw[4] = 9
	raw[len(raw)-1] = CRC8(raw[:len(raw)-1])
	_, err = Decode(COBSEncode(raw))
	if !errors.Is(err, ErrBadLength) {
		t.Fatalf("got %v, want ErrBadLength", err)
	}
}

func TestDecodeAcceptsAdvisoryEnvelopeRevision(t *testing.T) {
	encoded, err := Encode(Frame{Opcode: OpHello, Seq: 9})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := COBSDecode(encoded[:len(encoded)-1])
	if err != nil {
		t.Fatal(err)
	}
	raw[1] = 0x7F
	raw[len(raw)-1] = CRC8(raw[:len(raw)-1])
	decoded, err := Decode(append(COBSEncode(raw), 0))
	if err != nil || decoded.Opcode != OpHello || decoded.Seq != 9 {
		t.Fatalf("advisory envelope revision was rejected: frame=%#v err=%v", decoded, err)
	}
}

func TestStreamingDecoderRecoversAfterMalformedFrame(t *testing.T) {
	first, _ := Encode(Frame{Opcode: OpHello, Seq: 1})
	second, _ := Encode(Frame{Opcode: OpGetStatus, Seq: 2})
	stream := append([]byte{3, 1, 0}, first...)
	stream = append(stream, second...)

	var decoder Decoder
	frames, errs := decoder.Feed(stream[:4])
	more, moreErrs := decoder.Feed(stream[4:])
	frames = append(frames, more...)
	errs = append(errs, moreErrs...)

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if len(frames) != 2 || frames[0].Opcode != OpHello ||
		frames[1].Opcode != OpGetStatus {
		t.Fatalf("unexpected frames: %#v", frames)
	}
}

func TestPayloadBuildersValidateRanges(t *testing.T) {
	if _, err := PWMSetPayload(16, 1); err == nil {
		t.Fatal("expected bad channel error")
	}
	if _, err := PWMSetPayload(0, 4096); err == nil {
		t.Fatal("expected bad value error")
	}
	if _, err := RelayPayload(8, true); err == nil {
		t.Fatal("expected bad relay error")
	}
	if _, err := RelaySidePayload(0, 3); err == nil {
		t.Fatal("expected bad motion error")
	}
	if _, err := RFTxPayload(1, 24, MaxRFProtocol+1, 0); err == nil {
		t.Fatal("expected unsupported RF protocol error")
	}
	if _, err := RFTxPayload(1, 24, MaxRFProtocol, 0); err != nil {
		t.Fatalf("expected maximum RF protocol to pass: %v", err)
	}
	if _, err := RFLearnStartPayload(RFLearnModeTimer, MaxRFLearnSeconds+1); err == nil {
		t.Fatal("expected oversized RF learn timeout error")
	}
	if payload, err := RFLearnStartPayload(RFLearnModeTimer, MaxRFLearnSeconds); err != nil {
		t.Fatalf("expected maximum RF learn timeout to pass: %v", err)
	} else if !bytes.Equal(payload, []byte{RFLearnModeTimer, MaxRFLearnSeconds}) {
		t.Fatalf("timer RF learn payload=% X", payload)
	}
	if payload, err := RFLearnStartPayload(RFLearnModeIndefinite, 0); err != nil {
		t.Fatalf("expected indefinite RF learning to pass: %v", err)
	} else if !bytes.Equal(payload, []byte{RFLearnModeIndefinite, 0}) {
		t.Fatalf("indefinite RF learn payload=% X", payload)
	}
	if _, err := RFLearnStartPayload(RFLearnModeIndefinite, 1); err == nil {
		t.Fatal("expected indefinite RF timeout to be rejected")
	}
	if _, err := StreamPeriodPayload(MinimumStreamPeriodMS - 1); err == nil {
		t.Fatal("expected short stream period error")
	}
	for _, period := range []uint16{0, MinimumStreamPeriodMS} {
		if _, err := StreamPeriodPayload(period); err != nil {
			t.Fatalf("expected stream period %d to pass: %v", period, err)
		}
	}
	if _, err := RelayTestPayload(MinimumRelayTestPeriodMS - 1); err == nil {
		t.Fatal("expected short relay test period error")
	}
	for _, period := range []uint16{0, MinimumRelayTestPeriodMS} {
		if _, err := RelayTestPayload(period); err != nil {
			t.Fatalf("expected relay test period %d to pass: %v", period, err)
		}
	}
	invalidSettings := Settings{
		LightMode:          1,
		StreamPeriodMS:     MinimumStreamPeriodMS - 1,
		MotionBreakMSValue: 1,
	}
	if _, err := invalidSettings.Payload(); err == nil {
		t.Fatal("expected settings stream period error")
	}
	if _, err := DisplayTextPayload(DisplayLCD, 1000, string(bytes.Repeat([]byte{'x'}, 41))); err == nil {
		t.Fatal("expected oversized display text error")
	}
	scheduled, err := ScheduledSegmentPayload(ScheduledSegmentOptions{
		SpeedMS: 220, HoldMS: 5000, IntervalSecond: 30,
		Repeat: SegmentRepeatInterval, ForceScroll: true,
	}, "door is open")
	if err != nil || len(scheduled) != 20 || scheduled[0] != DisplayScheduledSegments ||
		binary.LittleEndian.Uint16(scheduled[1:3]) != 220 || scheduled[3] != 12 ||
		scheduled[4] != SegmentForceScroll|SegmentRepeatInterval ||
		binary.LittleEndian.Uint16(scheduled[5:7]) != 5000 || scheduled[7] != 30 ||
		string(scheduled[8:]) != "door is open" {
		t.Fatalf("scheduled segment payload=% X err=%v", scheduled, err)
	}
	if _, err := ScheduledSegmentPayload(ScheduledSegmentOptions{
		SpeedMS: 220, Repeat: SegmentRepeatInterval,
	}, "message"); err == nil {
		t.Fatal("expected zero interval to be rejected")
	}
	if _, err := MacroStepPayload(MacroPWM, 11, 1); err == nil {
		t.Fatal("expected macro PWM channel error")
	}
	if payload, err := MacroStepPayload(MacroFinish, 0, 0); err != nil ||
		!bytes.Equal(payload, []byte{MacroFinish, 0, 0, 0}) {
		t.Fatalf("unexpected macro finish payload % X err=%v", payload, err)
	}
	if payload, err := AddressableLEDPayload(10, 128, 64, 255, 127); err != nil ||
		!bytes.Equal(payload, []byte{10, 64, 32, 127, 255}) {
		t.Fatalf("unexpected addressable LED payload % X err=%v", payload, err)
	}
	if _, err := AddressableLEDPayload(11, 1, 2, 3, 4); err == nil {
		t.Fatal("expected addressable LED pixel validation error")
	}
}

func TestParseHelloCompactIdentitySchema3(t *testing.T) {
	payload := []byte{
		0x03, 0x01, 0x00, 0x00, 0x00, 0x00, 0x1C,
		0xF8, 0xD9, 0x2F, 0x5D, 0x9D, 0x01, 0x35,
	}
	hello, err := ParseHello(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.IsPCController() || hello.IdentitySchema != IdentitySchemaCompact ||
		hello.BoardKind != BoardKindPCController || hello.Name != "PCController" ||
		hello.Capabilities != 0 || hello.BuildHash != 0x2FD9F81C ||
		hello.BuildTimestamp != 0x35019D5D || hello.BuildStamp != "260801194258" {
		t.Fatalf("unexpected compact HELLO: %#v", hello)
	}
	if _, err := ParseHello(payload[:13]); err == nil {
		t.Fatal("truncated compact HELLO was accepted")
	}
	profileAware := append(append([]byte(nil), payload...), 0x00, 0xD9)
	profileAware[0] = 4
	profileHello, err := ParseHello(profileAware)
	if err != nil || profileHello.IdentitySchema != 4 ||
		profileHello.FeatureProfile != 0 || profileHello.BuildFeatures != 0xD9 {
		t.Fatalf("unexpected profile-aware HELLO: %#v err=%v", profileHello, err)
	}
	wrongSchema := append([]byte(nil), payload...)
	wrongSchema[0] = 2
	if _, err := ParseHello(wrongSchema); err == nil {
		t.Fatal("unpublished expanded HELLO schema was accepted")
	}
	invalidTimestamp := append([]byte(nil), payload...)
	binary.LittleEndian.PutUint32(invalidTimestamp[10:14], 0x341F0000)
	if _, err := ParseHello(invalidTimestamp); err == nil {
		t.Fatal("invalid packed build timestamp was accepted")
	}
}

func TestConfirmedResponseSchemas(t *testing.T) {
	settings := Settings{
		Flags: 3, LightMode: 1, OnBrightness: 128, OffBrightness: 4,
		DisplayBrightness: 5, StatusBrightness: 90,
		OutputPersistence: OutputPersistUserRelays | OutputPersistUserPWM,
		StreamPeriodMS:    750, DefaultPage: 4,
		ExtendedFlags: SettingsSaveLastPage, DisplayClosedBrightness: 2,
		MotionExitHoldSeconds: 9, RelayRestoreMask: 0xF0,
		MotionBreakMSValue: 37,
	}
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	decodedSettings, err := ParseSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	settings.Persisted = true // legacy 15-byte response is conservatively persisted
	if decodedSettings != settings {
		t.Fatalf("settings got %#v want %#v", decodedSettings, settings)
	}
	if _, err := ParseSettings(payload[:14]); err == nil {
		t.Fatal("truncated 14-byte SETTINGS payload was accepted")
	}
	wrongShape := append([]byte(nil), payload...)
	wrongShape[0]++
	if _, err := ParseSettings(wrongShape); err == nil {
		t.Fatal("unsupported SETTINGS shape was accepted")
	}

	statusPayload := make([]byte, StatusPayloadSize+2)
	binary.LittleEndian.PutUint16(statusPayload[24:26],
		StatusINA219Available|StatusPWMAvailable|StatusTLEDAvailable|
			StatusProgramRunning|StatusHostOffline|StatusHot)
	statusPayload[31] = 1
	statusPayload[33] = 1
	statusPayload[34] = 7
	statusPayload[35] = 0x34
	statusPayload[36] = 0x12
	statusPayload[42] = 9
	statusPayload[43] = 0x0A
	binary.LittleEndian.PutUint32(statusPayload[44:48], 0x12345678)
	status, err := ParseStatus(statusPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !status.DoorOpen || !status.ProgramRunning || !status.HostOffline || !status.Hot ||
		!status.INA219Available || !status.PWMAvailable || !status.TLEDAvailable || status.TBTAvailable ||
		status.PWMChannel != 7 ||
		status.PWMValue != 0x1234 || status.CRCErrors != 0x0900 ||
		status.ResetCause != 0x0A || status.ResetCount != 0x12345678 {
		t.Fatalf("unexpected STATUS: %#v", status)
	}
	if CapabilityBluetoothAudio != 1<<11 || CapabilityProgramState != 1<<24 || SupportsHostMenuOverlay(Hello{Capabilities: CapabilityProgramState}) {
		t.Fatal("capability bit 24 must identify PROGRAM_STATE, not host-menu overlay")
	}
	if got := ProgramStatePayload(false); !bytes.Equal(got, []byte{ProgramStateIdle}) {
		t.Fatalf("idle PROGRAM_STATE payload=% X", got)
	}
	if got := ProgramStatePayload(true); !bytes.Equal(got, []byte{ProgramStateRunning}) {
		t.Fatalf("running PROGRAM_STATE payload=% X", got)
	}
	if _, err := ParseStatus(statusPayload[:StatusPayloadSize-1]); err == nil {
		t.Fatal("STATUS without reset telemetry was accepted")
	}
}

func TestStatusAvailabilityRejectsSentinelAndOutOfRangeMeasurements(t *testing.T) {
	status := Status{
		Flags:    StatusINA219Available | StatusTLEDAvailable | StatusTBTAvailable,
		SupplyMV: -1, BusMV: -1, CurrentMA: -1, PowerMW: -1,
		TLEDCenti: -32768, TBTCenti: 12501,
	}
	status.applyAvailability()
	if status.INA219Available || status.TLEDAvailable || status.TBTAvailable {
		t.Fatalf("invalid measurements advertised as available: %#v", status)
	}
}

func TestSettingsDisplayOptionsPacking(t *testing.T) {
	settings := Settings{
		DisplayBrightness:       6,
		DisplayClosedBrightness: 5,
		MotionExitHoldSeconds:   9,
		MotionBreakMSValue:      1,
	}
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if payload[12] != (9<<3)|5 {
		t.Fatalf("packed display options = 0x%02X, want 0x4D", payload[12])
	}
	decoded, err := ParseSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.DisplayClosedBrightness != 5 || decoded.MotionExitHoldSeconds != 9 {
		t.Fatalf("decoded packed display options: %#v", decoded)
	}

	// The current wire encoding reserves zero for the configured 2-second default.
	payload[12] = 5
	decoded, err = ParseSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.DisplayClosedBrightness != 5 ||
		decoded.MotionExitHoldSeconds != SettingsDefaultMotionExitHoldSeconds {
		t.Fatalf("decoded compact default display options: %#v", decoded)
	}
	repacked, err := decoded.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if repacked[12] != 5 {
		t.Fatalf("default hold was not compactly encoded: 0x%02X", repacked[12])
	}
}

func TestRemoteKeyGesturePayloadUsesCurrentTwoByteSchema(t *testing.T) {
	payload, err := RemoteKeyGesturePayload(MenuIncrease, KeyEventUp)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload[0] != MenuIncrease || payload[1] != KeyEventUp {
		t.Fatalf("remote key gesture payload = %v", payload)
	}
	if _, err := RemoteKeyGesturePayload(MenuIncrease+1, KeyEventClick); err == nil {
		t.Fatal("out-of-range remote action was accepted")
	}
	if _, err := RemoteKeyGesturePayload(MenuPrevious, KeyEventUp+1); err == nil {
		t.Fatal("out-of-range key event was accepted")
	}
}

func TestSettingsExtendedFlagAccessorsPreserveAdjacentFields(t *testing.T) {
	settings := Settings{ExtendedFlags: SettingsSaveLastPage}
	if settings.VoltageDecimals() != 2 || settings.CurrentDecimals() != 2 {
		t.Fatalf(
			"zero/default decimal encoding = voltage %d current %d, want 2/2",
			settings.VoltageDecimals(),
			settings.CurrentDecimals(),
		)
	}
	if err := settings.SetStatusColor(4); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetVoltageDecimals(0); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCurrentDecimals(1); err != nil {
		t.Fatal(err)
	}
	if settings.ExtendedFlags != 0x99 {
		t.Fatalf("extended flags = 0x%02X, want 0x99", settings.ExtendedFlags)
	}
	if !settings.SaveLastPage() || settings.StatusColor() != 4 ||
		settings.VoltageDecimals() != 0 || settings.CurrentDecimals() != 1 {
		t.Fatalf("decoded settings = %#v", settings)
	}

	settings.SetSaveLastPage(false)
	if settings.ExtendedFlags != 0x98 {
		t.Fatalf(
			"disabling save-last changed another field: 0x%02X",
			settings.ExtendedFlags,
		)
	}
	before := settings.ExtendedFlags
	if err := settings.SetStatusColor(8); err == nil {
		t.Fatal("expected invalid status-color error")
	}
	if err := settings.SetVoltageDecimals(3); err == nil {
		t.Fatal("expected invalid voltage-decimals error")
	}
	if err := settings.SetCurrentDecimals(3); err == nil {
		t.Fatal("expected invalid current-decimals error")
	}
	if settings.ExtendedFlags != before {
		t.Fatalf(
			"invalid setter changed flags from 0x%02X to 0x%02X",
			before,
			settings.ExtendedFlags,
		)
	}
	if err := settings.SetVoltageDecimals(2); err != nil {
		t.Fatal(err)
	}
	if settings.ExtendedFlags&SettingsVoltageDecimalMask != 0x30 ||
		settings.VoltageDecimals() != 2 {
		t.Fatalf(
			"explicit two-decimal encoding is 0x%02X",
			settings.ExtendedFlags,
		)
	}
	settings.MotionExitHoldSeconds = SettingsDefaultMotionExitHoldSeconds
	settings.Persisted = true
	settings.MotionBreakMSValue = 1
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	// A zero semantic value uses the compact wire encoding for the current
	// two-second default, so compare against the normalized setting.
	settings.MotionExitHoldSeconds = SettingsDefaultMotionExitHoldSeconds
	if decoded != settings {
		t.Fatalf("extended settings round trip got %#v want %#v", decoded, settings)
	}
}

func TestSettingsMotionBreakUsesExactByteRange(t *testing.T) {
	settings := Settings{DisplayBrightness: 5, MotionBreakMSValue: 1}
	for _, value := range []uint16{1, 37, 100, 255} {
		if err := settings.SetMotionBreakMS(value); err != nil {
			t.Fatalf("set %d ms: %v", value, err)
		}
		payload, err := settings.Payload()
		if err != nil {
			t.Fatalf("payload %d ms: %v", value, err)
		}
		if payload[14] != byte(value) {
			t.Fatalf("wire break=%d, want %d", payload[14], value)
		}
		decoded, err := ParseSettings(payload)
		if err != nil || decoded.MotionBreakMS() != value {
			t.Fatalf("round trip %d ms: decoded=%#v err=%v", value, decoded, err)
		}
	}
	for _, value := range []uint16{0, 256} {
		if err := settings.SetMotionBreakMS(value); err == nil {
			t.Fatalf("invalid %d ms accepted", value)
		}
	}
}

func TestTemperatureAndDeviceEventSchemas(t *testing.T) {
	payload := []byte{TemperatureSchema, 2, 0}
	payload = append(payload, []byte{0x28, 1, 2, 3, 4, 5, 6, 0xAA}...)
	payload = append(payload, 0xD2, 0x09)
	payload = append(payload, 1)
	payload = append(payload, []byte{0x28, 7, 8, 9, 10, 11, 12, 0xBB}...)
	payload = append(payload, 0xF6, 0x09)
	sensors, err := ParseTemperatures(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(sensors) != 2 || sensors[0].Role != 0 ||
		sensors[1].Role != 1 || sensors[0].CelsiusCenti != 2514 {
		t.Fatalf("unexpected sensors: %#v", sensors)
	}
	if sensors[0].Identifier() != "28-010203040506-AA" {
		t.Fatalf("unexpected ROM ID %q", sensors[0].Identifier())
	}

	event, err := ParseDeviceEvent([]byte{EventDoor, 1})
	if err != nil || !event.DoorOpen {
		t.Fatalf("door event=%#v err=%v", event, err)
	}
	macro, err := ParseDeviceEvent([]byte{EventMacro, MacroEventCompleted, 7})
	if err != nil || macro.MacroID != 7 ||
		macro.MacroState != MacroEventCompleted {
		t.Fatalf("macro event=%#v err=%v", macro, err)
	}
	key, err := ParseDeviceEvent([]byte{
		EventKey, 2, 5, InputSourceRF, 9,
	})
	if err != nil || key.Key != 2 || key.Gesture != 5 ||
		key.Source != InputSourceRF || key.SourceID != 9 {
		t.Fatalf("key event=%#v err=%v", key, err)
	}
	rfPayload := []byte{
		EventRFReceived,
		0x78, 0x56, 0x34, 0x12,
		24, 1,
		0x5E, 0x01,
		7,
	}
	rfEvent, err := ParseDeviceEvent(rfPayload)
	if err != nil || rfEvent.RFCode != 0x12345678 ||
		rfEvent.RFBits != 24 || rfEvent.RFProtocol != 1 ||
		rfEvent.RFPulseUS != 350 || rfEvent.RFLearnedID != 7 {
		t.Fatalf("RF received event=%#v err=%v", rfEvent, err)
	}
	if _, err := ParseDeviceEvent(rfPayload[:9]); err == nil {
		t.Fatal("expected exact RF received event length validation")
	}
	learning, err := ParseDeviceEvent([]byte{
		EventRFLearning, RFLearningStarted, 12,
		RFLearnModeTimer, 30, 30,
	})
	if err != nil || learning.RFLearnState != RFLearningStarted ||
		learning.RFLearnCount != 12 ||
		learning.RFLearnMode != RFLearnModeTimer ||
		learning.RFLearnTotalSeconds != 30 ||
		learning.RFLearnRemainingSeconds != 30 {
		t.Fatalf("RF learning event=%#v err=%v", learning, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventRFLearning, RFLearningEnded}); err == nil {
		t.Fatal("expected exact RF learning event length validation")
	}
	indefinite, err := ParseDeviceEvent([]byte{
		EventRFLearning, RFLearningProgress, 7,
		RFLearnModeIndefinite, 0, 0,
	})
	if err != nil || indefinite.RFLearnMode != RFLearnModeIndefinite {
		t.Fatalf("indefinite RF learning event=%#v err=%v", indefinite, err)
	}
	if _, err := ParseDeviceEvent([]byte{
		EventRFLearning, RFLearningProgress, 7,
		RFLearnModeTimer, 20, 21,
	}); err == nil {
		t.Fatal("RF learning remaining time above total was accepted")
	}
	relay, err := ParseDeviceEvent([]byte{EventRelay, 0xA5})
	if err != nil || relay.RelayMask != 0xA5 {
		t.Fatalf("relay event=%#v err=%v", relay, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventRelay}); err == nil {
		t.Fatal("expected exact relay event length validation")
	}
	alert, err := ParseDeviceEvent([]byte{EventAlert, AlertHot, 1})
	if err != nil || alert.AlertKind != AlertHot || !alert.AlertActive {
		t.Fatalf("alert event=%#v err=%v", alert, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventAlert, 0, 1}); err == nil {
		t.Fatal("invalid alert kind was accepted")
	}
	if _, err := ParseDeviceEvent([]byte{EventAlert, AlertFault, 2}); err == nil {
		t.Fatal("invalid alert state was accepted")
	}
	reset, err := ParseDeviceEvent([]byte{
		EventReset, 0x0A, 0x78, 0x56, 0x34, 0x12,
	})
	if err != nil || reset.ResetCause != 0x0A ||
		reset.ResetCount != 0x12345678 {
		t.Fatalf("reset event=%#v err=%v", reset, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventReset, 0x0A}); err == nil {
		t.Fatal("expected exact reset event length validation")
	}
}

func TestAppNavigationDeviceEventIsVersionlessAndTargeted(t *testing.T) {
	event, err := ParseDeviceEvent(append(
		[]byte{EventAppNavigation, AppNavigationWebUI}, []byte("settings")...,
	))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != EventAppNavigation || event.AppTarget != "webui" || event.AppPage != "settings" {
		t.Fatalf("event=%#v", event)
	}
	for _, payload := range [][]byte{
		{EventAppNavigation, AppNavigationAll},
		{EventAppNavigation, 9, 'e'},
		{EventAppNavigation, AppNavigationTUI, 'b', 'a', 'd', ' '},
	} {
		if _, err := ParseDeviceEvent(payload); err == nil {
			t.Fatalf("invalid navigation payload accepted: % X", payload)
		}
	}
}

func TestTemperatureCountRejectsProtocolOverflow(t *testing.T) {
	payload := make([]byte, MaxPayload)
	payload[0] = TemperatureSchema
	payload[1] = byte(temperatureMaximumEntries + 1)
	if _, err := ParseTemperatures(payload); err == nil {
		t.Fatal("temperature count beyond the protocol capacity was accepted")
	}
}

func TestMenuListResponseSchema(t *testing.T) {
	payload := []byte{
		MenuListSchema, 14, 2, 2,
		0, 34, 'V', 'O', 'L', 'T',
		1, 35, 'C', 'U', 'R', 'R',
	}
	page, err := ParseMenuList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 14 || page.NextCursor != 2 || len(page.Entries) != 2 ||
		page.Entries[0].ID != 0 || page.Entries[0].Mode != 34 ||
		page.Entries[0].Label != "VOLT" || page.Entries[1].Label != "CURR" {
		t.Fatalf("unexpected menu page: %#v", page)
	}
	if _, err := ParseMenuList(payload[:len(payload)-1]); err == nil {
		t.Fatal("truncated menu list was accepted")
	}
	badSchema := append([]byte(nil), payload...)
	badSchema[0]++
	if _, err := ParseMenuList(badSchema); err == nil {
		t.Fatal("unsupported menu-list schema was accepted")
	}
}

func TestMenuListCountRejectsProtocolOverflow(t *testing.T) {
	payload := make([]byte, MaxPayload)
	payload[0] = MenuListSchema
	payload[3] = byte(menuListMaximumEntries + 1)
	if _, err := ParseMenuList(payload); err == nil {
		t.Fatal("menu-list count beyond the protocol capacity was accepted")
	}
}

func TestMenuLayoutSchemaRoundTripAndValidation(t *testing.T) {
	order := []byte{0, 3, 4, 1, 2, 5, 6, 7, 11, 12, 13, 8, 9, 10}
	payload, err := EncodeMenuLayout(MenuLayout{
		Schema: MenuLayoutSchema, VisibleMask: 0x3FFF, Order: order,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 11 || payload[1] != 14 || payload[4] != 0x30 || payload[10] != 0xA9 {
		t.Fatalf("encoded MENU_LAYOUT=% X", payload)
	}
	decoded, err := ParseMenuLayout(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.VisibleMask != 0x3FFF || len(decoded.Order) != 14 || decoded.Order[1] != 3 {
		t.Fatalf("decoded MENU_LAYOUT=%#v", decoded)
	}
	if _, err := EncodeMenuLayout(MenuLayout{Order: make([]byte, 17)}); err == nil {
		t.Fatal("MENU_LAYOUT with more than 16 entries was accepted")
	}

	tests := [][]byte{
		payload[:len(payload)-1],
		append([]byte{MenuLayoutSchema + 1}, payload[1:]...),
		append([]byte(nil), payload...),
		append([]byte(nil), payload...),
		append([]byte(nil), payload...),
		append([]byte(nil), payload...),
	}
	tests[2][2], tests[2][3] = 0, 0 // No visible page.
	tests[3][4] = 0x00              // Duplicate ID.
	tests[4][4] = 0xF0              // Unknown ID for count 14.
	tests[5][3] |= 0x40             // Visibility bit 14 is out of range.
	for index, invalid := range tests {
		if _, err := ParseMenuLayout(invalid); err == nil {
			t.Fatalf("invalid MENU_LAYOUT %d was accepted: % X", index, invalid)
		}
	}
	obsolete := []byte{1, 15, 0xFF, 0x7F, 0, 3, 4, 1, 2, 5, 6, 7, 11, 12, 13, 8, 9, 10, 14}
	if _, err := ParseMenuLayout(obsolete); err == nil {
		t.Fatal("obsolete schema-1 MENU_LAYOUT was accepted")
	}
}

func TestParseChangedDisplayAndBuzzerPushes(t *testing.T) {
	segments, err := ParseSegmentState([]byte{0x06, 0x5B, 0x4F, 0x66, 7})
	if err != nil {
		t.Fatal(err)
	}
	if segments.RawSegments != [4]byte{0x06, 0x5B, 0x4F, 0x66} ||
		segments.Brightness != 7 {
		t.Fatalf("segments=%#v", segments)
	}
	if _, err := ParseSegmentState([]byte{1, 2, 3, 4}); err == nil {
		t.Fatal("truncated SEGMENT_CHANGED payload was accepted")
	}

	compact, err := ParseBuzzerState([]byte{0xB8, 0x01, 0xDC, 0x00, 0})
	if err != nil {
		t.Fatal(err)
	}
	if compact.Timed || compact.DeviceMicros != 0 || compact.FrequencyHz != 440 || compact.DurationMS != 220 || compact.Muted {
		t.Fatalf("compact buzzer state=%+v", compact)
	}
	if _, err := ParseBuzzerState([]byte{0, 0, 0, 0, 2, 0, 0, 0, 0}); err == nil {
		t.Fatal("invalid BUZZER_CHANGED muted flag was accepted")
	}
	timed, err := ParseBuzzerState([]byte{0x70, 0x03, 125, 0, 1, 0x78, 0x56, 0x34, 0x12})
	if err != nil {
		t.Fatal(err)
	}
	if !timed.Timed || timed.DeviceMicros != 0x12345678 || timed.FrequencyHz != 880 || timed.DurationMS != 125 || !timed.Muted {
		t.Fatalf("timed buzzer state=%+v", timed)
	}
	if _, err := ParseBuzzerState([]byte{0, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("invalid six-byte BUZZER_CHANGED payload was accepted")
	}
}

func TestStatusProfilePayloadPreservesLivingPrefixAndPersistenceSuffix(t *testing.T) {
	want := DefaultStatusProfiles(173)[StatusConditionBluetoothOff]
	payload, err := StatusProfileSetPayload(StatusConditionBluetoothOff, want)
	if err != nil {
		t.Fatal(err)
	}
	response := append(append([]byte(nil), payload...), 0)
	parsed, err := ParseStatusProfile(response)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != StatusConditionBluetoothOff || parsed.Persisted || parsed.Effect != want {
		t.Fatalf("parsed status profile = %#v, want missing %#v", parsed, want)
	}
	legacy, err := ParseStatusProfile(payload)
	if err != nil || !legacy.Persisted || legacy.Effect != want {
		t.Fatalf("legacy status profile = %#v, err=%v", legacy, err)
	}
}

func TestBoardNamePayloadRoundTrip(t *testing.T) {
	for _, name := range []string{"", "EDGE-01", "A B"} {
		setPayload, err := SettingsWithBoardNamePayload(DefaultSettings(), name)
		if err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
		if len(setPayload) != 16+len(name) || setPayload[15] != byte(len(name)) {
			t.Fatalf("name %q set payload = %v", name, setPayload)
		}
		response := append([]byte{}, setPayload[:15]...)
		response = append(response, 1, 1, byte(len(name)))
		response = append(response, name...)
		if _, err := ParseSettings(response); err != nil {
			t.Fatalf("name %q extended settings: %v", name, err)
		}
		decoded, err := ParseBoardNameFromSettings(response)
		if err != nil || decoded.Name != name || !decoded.Persisted {
			t.Fatalf("name %q decoded=%#v err=%v", name, decoded, err)
		}
	}
	for _, invalid := range []string{"123456789", " leading", "trailing ", "café"} {
		if _, err := SettingsWithBoardNamePayload(DefaultSettings(), invalid); err == nil {
			t.Fatalf("invalid board name %q was accepted", invalid)
		}
	}
	base, err := DefaultSettings().Payload()
	if err != nil {
		t.Fatal(err)
	}
	truncated := append([]byte{}, base...)
	truncated = append(truncated, 1, 1, 2, 'A')
	if _, err := ParseBoardNameFromSettings(truncated); err == nil {
		t.Fatal("truncated board-name response was accepted")
	}
}

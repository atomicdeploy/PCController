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
	if _, err := RFLearnStartPayload(MaxRFLearnSeconds + 1); err == nil {
		t.Fatal("expected oversized RF learn timeout error")
	}
	if _, err := RFLearnStartPayload(MaxRFLearnSeconds); err != nil {
		t.Fatalf("expected maximum RF learn timeout to pass: %v", err)
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
		LightMode:      1,
		PWMBootMode:    PWMOff,
		StreamPeriodMS: MinimumStreamPeriodMS - 1,
	}
	if _, err := invalidSettings.Payload(); err == nil {
		t.Fatal("expected settings stream period error")
	}
	if _, err := DisplayTextPayload(DisplayLCD, 1000, string(bytes.Repeat([]byte{'x'}, 41))); err == nil {
		t.Fatal("expected oversized display text error")
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
		hello.BuildTimestamp != 0x35019D5D || hello.BuildStamp != "260801194258" ||
		hello.FirmwareMajor != 0 || hello.FirmwareMinor != 0 || hello.FirmwarePatch != 0 {
		t.Fatalf("unexpected compact HELLO: %#v", hello)
	}
	if _, err := ParseHello(payload[:13]); err == nil {
		t.Fatal("truncated compact HELLO was accepted")
	}
}

func TestConfirmedResponseSchemas(t *testing.T) {
	name := []byte("PCController")
	helloPayload := []byte{2, 3, 4, BoardKindPCController, 0x78, 0x56, 0x34, 0x12, byte(len(name))}
	helloPayload = append(helloPayload, name...)
	hello, err := ParseHello(helloPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.IsPCController() || hello.Capabilities != 0x12345678 {
		t.Fatalf("unexpected HELLO: %#v", hello)
	}

	identity := append([]byte(nil), helloPayload...)
	identity = append(identity, IdentitySchemaLegacy, 0x78, 0x56, 0x34, 0x12)
	identity = append(identity, []byte("Jul 31 2026")...)
	identity = append(identity, []byte("21:22:23")...)
	hello, err = ParseHello(identity)
	if err != nil {
		t.Fatal(err)
	}
	if hello.BuildHash != 0x12345678 ||
		hello.BuildDate != "Jul 31 2026" ||
		hello.BuildTime != "21:22:23" {
		t.Fatalf("unexpected build identity: %#v", hello)
	}

	// 2026-08-01 19:42:58 uses the ASA/DOS date<<16|time layout.
	const packedTimestamp uint32 = 0x35019D5D
	identity = append([]byte(nil), helloPayload...)
	identity = append(identity, IdentitySchema, 0x78, 0x56, 0x34, 0x12)
	identity = binary.LittleEndian.AppendUint32(identity, packedTimestamp)
	hello, err = ParseHello(identity)
	if err != nil {
		t.Fatal(err)
	}
	if hello.BuildHash != 0x12345678 ||
		hello.BuildTimestamp != packedTimestamp ||
		hello.BuildStamp != "260801194258" {
		t.Fatalf("unexpected packed build identity: %#v", hello)
	}

	invalidIdentity := append([]byte(nil), identity[:len(identity)-4]...)
	invalidIdentity = binary.LittleEndian.AppendUint32(invalidIdentity, 0x341F0000)
	if _, err := ParseHello(invalidIdentity); err == nil {
		t.Fatal("invalid schema-2 timestamp was accepted")
	}

	settings := Settings{
		Flags: 3, LightMode: 1, OnBrightness: 128, OffBrightness: 4,
		DisplayBrightness: 5, StatusBrightness: 90, PWMBootMode: PWMAuto,
		StreamPeriodMS: 750, DefaultPage: 4,
		ExtendedFlags: SettingsSaveLastPage,
	}
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	decodedSettings, err := ParseSettings(append(payload, 0xEE))
	if err != nil {
		t.Fatal(err)
	}
	if decodedSettings != settings {
		t.Fatalf("settings got %#v want %#v", decodedSettings, settings)
	}
	legacySettings := append([]byte(nil), payload[:10]...)
	legacySettings[0] = SettingsSchemaLegacy
	legacyDecoded, err := ParseSettings(legacySettings)
	if err != nil {
		t.Fatal(err)
	}
	if legacyDecoded.DefaultPage != 0 || legacyDecoded.SaveLastPage() {
		t.Fatalf("legacy settings gained extended values: %#v", legacyDecoded)
	}
	if legacyDecoded.StatusColor() != 0 ||
		legacyDecoded.VoltageDecimals() != SettingsDefaultDecimals ||
		legacyDecoded.CurrentDecimals() != SettingsDefaultDecimals {
		t.Fatalf(
			"legacy settings did not receive safe display defaults: %#v",
			legacyDecoded,
		)
	}

	statusPayload := make([]byte, StatusPayloadSize+2)
	statusPayload[31] = 1
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
	if !status.DoorOpen || status.PWMChannel != 7 ||
		status.PWMValue != 0x1234 || status.CRCErrors != 0x0900 ||
		status.ResetCause != 0x0A || status.ResetCount != 0x12345678 {
		t.Fatalf("unexpected STATUS: %#v", status)
	}
	legacyStatus, err := ParseStatus(statusPayload[:StatusPayloadSizeLegacy])
	if err != nil || legacyStatus.ResetCause != 0 || legacyStatus.ResetCount != 0 {
		t.Fatalf("legacy STATUS=%#v err=%v", legacyStatus, err)
	}
	if _, err := ParseStatus(statusPayload[:StatusPayloadSizeLegacy+1]); err == nil {
		t.Fatal("expected truncated reset telemetry extension rejection")
	}
}

func TestSettingsExtendedFlagAccessorsPreserveAdjacentFields(t *testing.T) {
	settings := Settings{ExtendedFlags: SettingsSaveLastPage}
	if settings.VoltageDecimals() != 2 || settings.CurrentDecimals() != 2 {
		t.Fatalf(
			"zero/legacy decimal encoding = voltage %d current %d, want 2/2",
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
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseSettings(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != settings {
		t.Fatalf("extended settings round trip got %#v want %#v", decoded, settings)
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
	})
	if err != nil || learning.RFLearnState != RFLearningStarted ||
		learning.RFLearnCount != 12 {
		t.Fatalf("RF learning event=%#v err=%v", learning, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventRFLearning, RFLearningEnded}); err == nil {
		t.Fatal("expected exact RF learning event length validation")
	}
	relay, err := ParseDeviceEvent([]byte{EventRelay, 0xA5})
	if err != nil || relay.RelayMask != 0xA5 {
		t.Fatalf("relay event=%#v err=%v", relay, err)
	}
	if _, err := ParseDeviceEvent([]byte{EventRelay}); err == nil {
		t.Fatal("expected exact relay event length validation")
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

func TestMenuListResponseSchema(t *testing.T) {
	payload := []byte{
		MenuListSchema, 15, 2, 2,
		0, 34, 'V', 'O', 'L', 'T',
		1, 35, 'C', 'U', 'R', 'R',
	}
	page, err := ParseMenuList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 15 || page.NextCursor != 2 || len(page.Entries) != 2 ||
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

func TestMenuLayoutSchemaRoundTripAndValidation(t *testing.T) {
	order := []byte{0, 3, 4, 1, 2, 5, 6, 7, 11, 12, 13, 8, 9, 10, 14}
	payload, err := EncodeMenuLayout(MenuLayout{
		Schema: MenuLayoutSchema, VisibleMask: 0x5FFF, Order: order,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 12 || payload[1] != 15 || payload[4] != 0x30 || payload[11] != 0xFE {
		t.Fatalf("encoded MENU_LAYOUT=% X", payload)
	}
	decoded, err := ParseMenuLayout(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.VisibleMask != 0x5FFF || len(decoded.Order) != 15 || decoded.Order[1] != 3 {
		t.Fatalf("decoded MENU_LAYOUT=%#v", decoded)
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
	tests[4][4] = 0xF0              // Unknown ID for count 15.
	tests[5][11] &^= 0xF0           // Odd-count padding is not 0xF.
	for index, invalid := range tests {
		if _, err := ParseMenuLayout(invalid); err == nil {
			t.Fatalf("invalid MENU_LAYOUT %d was accepted: % X", index, invalid)
		}
	}
	legacy := []byte{1, 15, 0xFF, 0x7F, 0, 3, 4, 1, 2, 5, 6, 7, 11, 12, 13, 8, 9, 10, 14}
	legacyDecoded, err := ParseMenuLayout(legacy)
	if err != nil || legacyDecoded.Schema != MenuLayoutLegacySchema || legacyDecoded.Order[14] != 14 {
		t.Fatalf("legacy MENU_LAYOUT decode=%#v err=%v", legacyDecoded, err)
	}
}

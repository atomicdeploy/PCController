package native

import "testing"

// The generated registry owns numbers; this test protects the longstanding
// human-facing name behavior used by the CLI, TUI, logs, and IPC responses.
func TestOpcodeNameUsesGeneratedConstantsWithoutChangingPublicNames(t *testing.T) {
	tests := []struct {
		opcode byte
		want   string
	}{
		{OpHello, "HELLO"},
		{OpHelloResp, "HELLO"},
		{OpHostMenuDirectory, "HOST_MENU_DIRECTORY"},
		{OpHostMenuContent, "HOST_MENU_CONTENT"},
		{OpHostMenuStateGet, "HOST_MENU_STATE_GET"},
		{OpStatusLEDChanged, "STATUS_LED_CHANGED"},
		{0x13, "UNKNOWN"},
		{0xFF, "UNKNOWN"},
	}
	for _, test := range tests {
		if got := OpcodeName(test.opcode); got != test.want {
			t.Fatalf("OpcodeName(0x%02X) = %q, want %q", test.opcode, got, test.want)
		}
	}
}

func TestGeneratedErrorValuesMatchNativeWireContract(t *testing.T) {
	if ErrorNoError != 0 || ErrorBadEnvelope != 1 || ErrorUnsupported != 2 ||
		ErrorBadPayload != 3 || ErrorHardwareUnavailable != 4 || ErrorBusy != 5 ||
		ErrorUnsafe != 6 {
		t.Fatalf("generated error registry drifted from the native wire contract")
	}
}

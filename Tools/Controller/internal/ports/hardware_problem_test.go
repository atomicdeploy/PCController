package ports

import "testing"

func TestClassifyHardwareProblem(t *testing.T) {
	tests := []struct {
		name       string
		problem    uint32
		hardwareID []string
		want       string
	}{
		{
			name:       "descriptor failure",
			problem:    43,
			hardwareID: []string{`USB\DEVICE_DESCRIPTOR_FAILURE`},
			want:       HardwareProblemUSBDescriptorFailure,
		},
		{
			name:       "descriptor failure fallback identity",
			problem:    43,
			hardwareID: []string{`USB\VID_0000&PID_0002\PHYSICAL-INSTANCE`},
			want:       HardwareProblemUSBDescriptorFailure,
		},
		{name: "cannot start", problem: 10, want: HardwareProblemCannotStart},
		{name: "driver missing", problem: 28, want: HardwareProblemDriverMissing},
		{name: "reported problem", problem: 43, want: HardwareProblemReported},
		{name: "unknown", problem: 99, want: HardwareProblemUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyHardwareProblem(test.problem, test.hardwareID); got != test.want {
				t.Fatalf("classifyHardwareProblem() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHardwareProblemMatchesPhysicalInstanceAcrossDescriptorFailure(t *testing.T) {
	filter := Filter{Port: "COM3"}
	related := []string{`USB\VID_1A86&PID_7523\5&1330824A&0&2`}
	if !hardwareProblemMatches(
		`USB\VID_0000&PID_0002\5&1330824A&0&2`,
		[]string{`USB\DEVICE_DESCRIPTOR_FAILURE`},
		filter,
		related,
	) {
		t.Fatal("expected the failed descriptor to match the remembered physical USB instance")
	}
}

func TestHardwareProblemRejectsUnrelatedDevice(t *testing.T) {
	filter := Filter{
		Port: "COM3",
		Preferred: Identity{
			VID: "1A86", PID: "7523",
			InstanceID: `USB\VID_1A86&PID_7523\5&1330824A&0&2`,
		},
	}
	if hardwareProblemMatches(
		`USB\VID_0000&PID_0002\8&DEADBEEF&0&7`,
		[]string{`USB\DEVICE_DESCRIPTOR_FAILURE`},
		filter,
		nil,
	) {
		t.Fatal("an unrelated failed USB device must not be blamed on the controller")
	}
}

func TestHardwareProblemRejectsSameModelSiblingWhenSelectedInstanceExists(t *testing.T) {
	filter := Filter{
		VID: "1A86", PID: "7523",
		Preferred: Identity{
			VID: "1A86", PID: "7523",
			InstanceID: `USB\VID_1A86&PID_7523\SELECTED-CONTROLLER`,
		},
	}
	if hardwareProblemMatches(
		`USB\VID_1A86&PID_7523\BROKEN-SIBLING`,
		[]string{`USB\VID_1A86&PID_7523`},
		filter,
		[]string{`USB\VID_1A86&PID_7523\OLD-COM-ASSIGNMENT`},
	) {
		t.Fatal("same-model sibling overrode the selected controller instance")
	}
}

func TestHardwareProblemRejectsVIDFallbackWhenCOMHistoryExists(t *testing.T) {
	filter := Filter{Port: "COM3", VID: "1A86", PID: "7523"}
	if hardwareProblemMatches(
		`USB\VID_1A86&PID_7523\BROKEN-SIBLING`,
		nil,
		filter,
		[]string{`USB\VID_1A86&PID_7523\CONTROLLER-ON-COM3`},
	) {
		t.Fatal("VID/PID fallback overrode the configured COM identity history")
	}
}

func TestHardwareProblemMatchesVIDPIDWhenInstanceIsUnavailable(t *testing.T) {
	filter := Filter{VID: "1a86", PID: "7523"}
	if !hardwareProblemMatches(
		`USB\VID_1A86&PID_7523\A1`,
		nil,
		filter,
		nil,
	) {
		t.Fatal("expected an exact configured VID/PID match")
	}
}

func TestHardwareProblemOrderingPutsErrorsBeforeWarnings(t *testing.T) {
	errorProblem := HardwareProblem{Code: "z-error", Severity: "error"}
	warningProblem := HardwareProblem{Code: "a-warning", Severity: "warning"}
	if !hardwareProblemLess(errorProblem, warningProblem) {
		t.Fatal("error did not sort ahead of warning")
	}
	if hardwareProblemLess(warningProblem, errorProblem) {
		t.Fatal("warning sorted ahead of error")
	}
}

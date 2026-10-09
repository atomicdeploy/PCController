package programmer

import (
	"strconv"
	"testing"
)

func TestGeneratedBoardTargetOwnsProgrammingDefaults(t *testing.T) {
	target := DefaultBoardTarget()
	if target.MCU != generatedBoardMCU || target.ClockHz != generatedBoardClockHz ||
		target.Bootloader != generatedBoardBootloader || target.Baud != generatedBoardBaud {
		t.Fatalf("generated board identity drifted: %#v", target)
	}
	if target.ApplicationLimitBytes != urbootApplicationCapacity ||
		target.FlashBytes != ATmega328PFlashSize ||
		target.EEPROMBytes != PCControllerEEPROMBytes {
		t.Fatalf("generated board capacities drifted: %#v", target)
	}

	command, err := Build(Options{
		Method: MethodUrclock, Operation: OperationProbe, Port: "DO_NOT_OPEN",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantMCU := "-p" + target.MCU
	wantBaud := "-b" + strconv.Itoa(target.Baud)
	if !containsArgument(command.Args, wantMCU) || !containsArgument(command.Args, wantBaud) {
		t.Fatalf("program command %q does not use generated target %q/%q", command.Args, wantMCU, wantBaud)
	}
}

func containsArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
}

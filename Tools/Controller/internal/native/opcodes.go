package native

const (
	MenuPrevious byte = iota
	MenuNext
	MenuDecrease
	MenuIncrease
)

const (
	ResetApplication byte = iota
	ResetBootloader
)

const (
	ProgramStateIdle byte = iota
	ProgramStateRunning
)

func OpcodeName(opcode byte) string {
	if name, ok := opcodeNames[opcode]; ok {
		return name
	}
	return "UNKNOWN"
}

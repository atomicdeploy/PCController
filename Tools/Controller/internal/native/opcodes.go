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

var opcodeNames = map[byte]string{
	OpHello: "HELLO", OpGetStatus: "GET_STATUS", OpSetStream: "SET_STREAM",
	OpGetSettings: "GET_SETTINGS", OpSetSettings: "SET_SETTINGS",
	OpTemperatureList: "TEMPERATURE_LIST",
	OpBuzzer:          "BUZZER", OpPWMSet: "PWM_SET", OpPWMAllOff: "PWM_ALL_OFF",
	OpStatusRGB: "STATUS_RGB", OpPWMGet: "PWM_GET",
	OpAddressableLED: "ADDRESSABLE_LED", OpStatusEffect: "STATUS_EFFECT",
	OpStatusProfileGet: "STATUS_PROFILE_GET", OpStatusProfileSet: "STATUS_PROFILE_SET",
	OpRFTx: "RF_TX", OpRFLearnStart: "RF_LEARN_START", OpRFLearnCancel: "RF_LEARN_CANCEL",
	OpRFLearnClear: "RF_LEARN_CLEAR", OpRFLearnList: "RF_LEARN_LIST",
	OpRFLearnRemove: "RF_LEARN_REMOVE",
	OpMenuAction:    "MENU_ACTION", OpRelaySet: "RELAY_SET",
	OpRelaySide: "RELAY_SIDE", OpRelayAllOff: "RELAY_ALL_OFF",
	OpRelayTest: "RELAY_TEST", OpReset: "RESET", OpI2CTransfer: "I2C_TRANSFER",
	OpMenuSetPage: "MENU_SET_PAGE", OpDisplayText: "DISPLAY_TEXT",
	OpMacroStart: "MACRO_START", OpMacroCancel: "MACRO_CANCEL",
	OpMacroStep: "MACRO_STEP", OpFrontPanelGet: "FRONT_PANEL_GET",
	OpRemoteKeyGesture: "REMOTE_KEY_GESTURE", OpMenuList: "MENU_LIST",
	OpRFLearnReplace: "RF_LEARN_REPLACE", OpMenuLayoutGet: "MENU_LAYOUT_GET",
	OpMenuLayoutSet: "MENU_LAYOUT_SET", OpHostMenuDirectory: "HOST_MENU_DIRECTORY",
	OpHostMenuContent: "HOST_MENU_CONTENT", OpHostMenuStateGet: "HOST_MENU_STATE_GET",
	OpProgramState: "PROGRAM_STATE",
	OpACK:          "ACK", OpHelloResp: "HELLO", OpError: "ERROR", OpStatus: "STATUS",
	OpSettings: "SETTINGS", OpPWMValues: "PWM_VALUES",
	OpI2CTransferResp: "I2C_TRANSFER", OpRFEntries: "RF_ENTRIES",
	OpTemperatures: "TEMPERATURES", OpFrontPanel: "FRONT_PANEL",
	OpMenuListResp:    "MENU_LIST",
	OpMacroStatus:     "MACRO_STATUS",
	OpMenuLayoutResp:  "MENU_LAYOUT",
	OpHostMenuRequest: "HOST_MENU_REQUEST", OpHostMenuState: "HOST_MENU_STATE",
	OpSegmentChanged: "SEGMENT_CHANGED", OpBuzzerChanged: "BUZZER_CHANGED",
	OpStatusLEDChanged: "STATUS_LED_CHANGED",
	OpStatusProfile:    "STATUS_PROFILE",
	OpEvent:            "EVENT",
}

func OpcodeName(opcode byte) string {
	if name, ok := opcodeNames[opcode]; ok {
		return name
	}
	return "UNKNOWN"
}

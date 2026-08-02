package native

const (
	OpHello           byte = 0x01
	OpGetStatus       byte = 0x02
	OpSetStream       byte = 0x03
	OpGetSettings     byte = 0x04
	OpSetSettings     byte = 0x05
	OpTemperatureList byte = 0x06

	OpBuzzer    byte = 0x10
	OpPWMSet    byte = 0x11
	OpPWMAllOff byte = 0x12
	// 0x13 is reserved; the current firmware has no PWM mode/auto-demo state.
	OpStatusRGB      byte = 0x14
	OpPWMGet         byte = 0x15
	OpAddressableLED byte = 0x16

	OpRFTx          byte = 0x20
	OpRFLearnStart  byte = 0x21
	OpRFLearnCancel byte = 0x22
	OpRFLearnClear  byte = 0x23
	OpRFLearnList   byte = 0x24
	OpRFLearnRemove byte = 0x25
	// 0x26 is reserved; mappings are updated as complete records through 0x3F.

	OpMenuAction        byte = 0x30
	OpRelaySet          byte = 0x31
	OpRelaySide         byte = 0x32
	OpRelayAllOff       byte = 0x33
	OpRelayTest         byte = 0x34
	OpReset             byte = 0x35
	OpI2CTransfer       byte = 0x36
	OpMenuSetPage       byte = 0x37
	OpDisplayText       byte = 0x38
	OpMacroStart        byte = 0x39
	OpMacroCancel       byte = 0x3A
	OpMacroStep         byte = 0x3B
	OpFrontPanelGet     byte = 0x3C
	OpRemoteKeyGesture  byte = 0x3D
	OpMenuList          byte = 0x3E
	OpRFLearnReplace    byte = 0x3F
	OpMenuLayoutGet     byte = 0x40
	OpMenuLayoutSet     byte = 0x41
	OpHostMenuDirectory byte = 0x42
	OpHostMenuContent   byte = 0x43
	OpHostMenuStateGet  byte = 0x44
	// OpProgramState carries the semantic prefix [0=Idle, 1=Running]. Firmware
	// deliberately ignores any future tail it does not understand.
	OpProgramState byte = 0x45

	OpACK             byte = 0x80
	OpHelloResp       byte = 0x81
	OpError           byte = 0x82
	OpStatus          byte = 0x90
	OpSettings        byte = 0x91
	OpPWMValues       byte = 0x92
	OpI2CTransferResp byte = 0x93
	OpRFEntries       byte = 0x94
	OpTemperatures    byte = 0x95
	OpFrontPanel      byte = 0x96
	OpMenuListResp    byte = 0x97
	OpMacroStatus     byte = 0x98
	OpMenuLayoutResp  byte = 0x99
	OpHostMenuRequest byte = 0x9A
	OpHostMenuState   byte = 0x9B
	OpEvent           byte = 0xA0
)

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
	OpAddressableLED: "ADDRESSABLE_LED",
	OpRFTx:           "RF_TX", OpRFLearnStart: "RF_LEARN_START", OpRFLearnCancel: "RF_LEARN_CANCEL",
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
	OpEvent: "EVENT",
}

func OpcodeName(opcode byte) string {
	if name, ok := opcodeNames[opcode]; ok {
		return name
	}
	return "UNKNOWN"
}

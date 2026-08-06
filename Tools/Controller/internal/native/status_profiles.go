package native

// Status profile conditions are stable opcode operands rather than API
// versions. The Go host owns the rich factory table and provisions only
// missing EEPROM records; firmware carries a tiny safety fallback.
const (
	StatusConditionOff byte = iota
	StatusConditionBoot
	StatusConditionReady
	StatusConditionLearning
	StatusConditionHot
	StatusConditionFault
	StatusConditionCustom
	StatusConditionBluetoothConnected
	StatusConditionBluetoothOff
	StatusConditionBluetoothWaiting
	StatusConditionRunning
	StatusConditionDoorOpen
	StatusConditionDoorClosed
	StatusConditionBluetooth
	StatusConditionMenu
	StatusConditionRadio
	StatusConditionSave
	StatusConditionDiscard
	StatusConditionReset
)

const FactoryStatusBrightness byte = 128

// DefaultStatusProfiles returns the canonical host-owned factory palette and
// procedural effects. Colors remain data: no effect name implies a color.
func DefaultStatusProfiles(brightness byte) [StatusProfileCount]StatusEffectOptions {
	static := func(red, green, blue byte) StatusEffectOptions {
		return StatusEffectOptions{Red: red, Green: green, Blue: blue, Brightness: brightness}
	}
	effect := func(kind, red, green, blue, alternateRed, alternateGreen, alternateBlue byte, period uint16) StatusEffectOptions {
		return StatusEffectOptions{
			Kind: kind, Red: red, Green: green, Blue: blue,
			AlternateRed: alternateRed, AlternateGreen: alternateGreen, AlternateBlue: alternateBlue,
			Brightness: brightness, PeriodMS: period,
		}
	}
	return [StatusProfileCount]StatusEffectOptions{
		StatusConditionOff:                static(0, 0, 0),
		StatusConditionBoot:               static(255, 72, 0),
		StatusConditionReady:              static(255, 255, 255),
		StatusConditionLearning:           effect(StatusEffectBreathe, 190, 0, 255, 0, 0, 0, 1600),
		StatusConditionHot:                effect(StatusEffectBreathe, 255, 96, 0, 0, 0, 0, 1600),
		StatusConditionFault:              effect(StatusEffectFlash, 255, 0, 0, 0, 0, 0, 640),
		StatusConditionCustom:             static(0, 0, 0),
		StatusConditionBluetoothConnected: static(16, 72, 255),
		StatusConditionBluetoothOff:       effect(StatusEffectCycle, 0, 255, 80, 255, 0, 0, 2000),
		StatusConditionBluetoothWaiting:   effect(StatusEffectBreathe, 16, 72, 255, 0, 0, 0, 1600),
		StatusConditionRunning:            static(255, 144, 0),
		StatusConditionDoorOpen:           static(255, 120, 12),
		StatusConditionDoorClosed:         static(0, 255, 80),
		StatusConditionBluetooth:          static(16, 72, 255),
		StatusConditionMenu:               static(0, 180, 255),
		StatusConditionRadio:              static(190, 0, 255),
		StatusConditionSave:               static(0, 255, 24),
		StatusConditionDiscard:            static(255, 12, 0),
		StatusConditionReset:              static(255, 48, 0),
	}
}

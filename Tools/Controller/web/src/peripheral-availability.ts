import type { Snapshot } from './types'

const statusINA219Available = 1 << 0
const statusPWMAvailable = 1 << 1
const statusTemperatureLEDAvailable = 1 << 2
const statusTemperatureBTAudioAvailable = 1 << 3
const capabilityINA219 = 1 << 0
const capabilityTemperature = 1 << 1
const capabilityPWM = 1 << 2
const capabilityRelays = 1 << 3
const capabilityRF = 1 << 4
const capabilitySegments = 1 << 5
const capabilityLCD = 1 << 6
const capabilityStatusLED = 1 << 7
const capabilitySettings = 1 << 8
const capabilityMenus = 1 << 9
const capabilityBluetoothAudio = 1 << 11
const capabilityBuzzerBusy = 1 << 20

export interface PeripheralAvailability {
  ina219: boolean
  pwm: boolean
  temperatureLED: boolean
  temperatureBTAudio: boolean
  lcd: boolean
  bluetoothAudio: boolean
  relays: boolean
  rf: boolean
  segments: boolean
  statusLED: boolean
  settings: boolean
  menus: boolean
  buzzer: boolean
  invalidINA219: boolean
  invalidTemperatureLED: boolean
  invalidTemperatureBTAudio: boolean
}

function bounded(value: number, minimum: number, maximum: number): boolean {
  return Number.isFinite(value) && value >= minimum && value <= maximum
}

// STATUS flags are the compact wire truth. Typed booleans make the public host
// contract easier to consume, while the bit fallback keeps the WebUI correct
// during a graceful host update from an older executable.
export function peripheralAvailability(snapshot: Snapshot): PeripheralAvailability {
  const { status } = snapshot
  const capabilities = snapshot.hello.capabilities ?? 0
  const ready = snapshot.connected && snapshot.have_status
  const inaAdvertised = ready && Boolean(capabilities & capabilityINA219) && Boolean(status.ina219_available || (status.flags & statusINA219Available))
  const temperatureLEDAdvertised = ready && Boolean(capabilities & capabilityTemperature) && Boolean(status.temperature_led_available || (status.flags & statusTemperatureLEDAvailable))
  const temperatureBTAudioAdvertised = ready && Boolean(capabilities & capabilityTemperature) && Boolean(status.temperature_bt_audio_available || (status.flags & statusTemperatureBTAudioAvailable))
  const validINA219 = bounded(status.supply_mv, 0, 100_000) && bounded(status.bus_mv, 0, 100_000) &&
    bounded(status.current_ma, -100_000, 100_000) && bounded(status.power_mw, -10_000_000, 10_000_000)
  const validTemperatureLED = bounded(status.temperature_led_centi_c, -5_500, 12_500)
  const validTemperatureBTAudio = bounded(status.temperature_bt_audio_centi_c, -5_500, 12_500)
  return {
	ina219: inaAdvertised && validINA219,
	pwm: ready && Boolean(capabilities & capabilityPWM) && Boolean(status.pwm_available || (status.flags & statusPWMAvailable)),
	temperatureLED: temperatureLEDAdvertised && validTemperatureLED,
	temperatureBTAudio: temperatureBTAudioAdvertised && validTemperatureBTAudio,
	lcd: ready && Boolean(capabilities & capabilityLCD) && Boolean(snapshot.front_panel?.lcd_available || snapshot.front_panel?.lcd_address || status.lcd_address),
	bluetoothAudio: ready && Boolean(capabilities & capabilityBluetoothAudio),
	relays: ready && Boolean(capabilities & capabilityRelays),
	rf: ready && Boolean(capabilities & capabilityRF),
	segments: ready && Boolean(capabilities & capabilitySegments),
	statusLED: ready && Boolean(capabilities & capabilityStatusLED),
	settings: ready && Boolean(capabilities & capabilitySettings),
	menus: ready && Boolean(capabilities & capabilityMenus),
	buzzer: ready && Boolean(capabilities & capabilityBuzzerBusy),
	invalidINA219: inaAdvertised && !validINA219,
	invalidTemperatureLED: temperatureLEDAdvertised && !validTemperatureLED,
	invalidTemperatureBTAudio: temperatureBTAudioAdvertised && !validTemperatureBTAudio,
  }
}

import type { Snapshot } from './types'

const statusINA219Available = 1 << 0
const statusPWMAvailable = 1 << 1
const statusTemperatureLEDAvailable = 1 << 2
const statusTemperatureBTAudioAvailable = 1 << 3

export interface PeripheralAvailability {
  ina219: boolean
  pwm: boolean
  temperatureLED: boolean
  temperatureBTAudio: boolean
  lcd: boolean
}

// STATUS flags are the compact wire truth. Typed booleans make the public host
// contract easier to consume, while the bit fallback keeps the WebUI correct
// during a graceful host update from an older executable.
export function peripheralAvailability(snapshot: Snapshot): PeripheralAvailability {
  const { status } = snapshot
  return {
    ina219: Boolean(status.ina219_available || (status.flags & statusINA219Available)),
    pwm: Boolean(status.pwm_available || (status.flags & statusPWMAvailable)),
    temperatureLED: Boolean(status.temperature_led_available || (status.flags & statusTemperatureLEDAvailable)),
    temperatureBTAudio: Boolean(status.temperature_bt_audio_available || (status.flags & statusTemperatureBTAudioAvailable)),
    lcd: Boolean(snapshot.front_panel?.lcd_available || snapshot.front_panel?.lcd_address || status.lcd_address),
  }
}

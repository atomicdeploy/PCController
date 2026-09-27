import type { ControllerStatus } from './types'

function booleanValue(value: string | undefined): boolean | undefined {
  if (value === 'true') return true
  if (value === 'false') return false
  return undefined
}

function integerValue(value: string | undefined, radix = 10): number | undefined {
  if (!value) return undefined
  const pattern = radix === 16 ? /^[\da-f]+$/i : /^\d+$/
  if (!pattern.test(value)) return undefined
  const parsed = Number.parseInt(value, radix)
  return Number.isFinite(parsed) ? parsed : undefined
}

function scaledValue(value: string | undefined, suffix: string, multiplier: number): number | undefined {
  if (!value?.endsWith(suffix)) return undefined
  const parsed = Number(value.slice(0, -suffix.length))
  return Number.isFinite(parsed) ? Math.round(parsed * multiplier) : undefined
}

function durationMilliseconds(value: string | undefined): number | undefined {
  if (!value) return undefined
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(value)
  if (!match || (!match[1] && !match[2] && !match[3])) return undefined
  return Math.round((Number(match[1] ?? 0) * 3600 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0)) * 1000)
}

function hexadecimal(value: string | undefined): number | undefined {
  return value && /^0x[\da-f]+$/i.test(value) ? integerValue(value.slice(2), 16) : undefined
}

function boundedInteger(value: number | undefined, minimum: number, maximum: number): number | undefined {
  return value !== undefined && value >= minimum && value <= maximum ? value : undefined
}

/** Parses the compact human `status` command without treating arbitrary output as state. */
export function parseStatusCommandOutput(output: string): Partial<ControllerStatus> | null {
  const values = new Map<string, string>()
  for (const token of output.trim().split(/\s+/)) {
    const separator = token.indexOf('=')
    if (separator > 0) values.set(token.slice(0, separator), token.slice(separator + 1))
  }
  // A status line must carry three independently parsed identity fields. Key
  // presence alone is not enough: arbitrary command text must never become
  // authoritative controller state.
  const uptimeMS = durationMilliseconds(values.get('uptime'))
  const supplyMV = scaledValue(values.get('supply'), 'V', 1000)
  const activeRelays = boundedInteger(hexadecimal(values.get('relays')), 0, 0xff)
  if (uptimeMS === undefined || supplyMV === undefined || supplyMV < 0 || activeRelays === undefined) return null

  const status: Partial<ControllerStatus> = {}
  const assign = <K extends keyof ControllerStatus>(key: K, value: ControllerStatus[K] | undefined) => {
    if (value !== undefined) status[key] = value
  }
  assign('uptime_ms', uptimeMS)
  assign('uptime', values.get('uptime'))
  assign('supply_mv', supplyMV)
  assign('bus_mv', scaledValue(values.get('bus'), 'V', 1000))
  assign('current_ma', scaledValue(values.get('current'), 'mA', 1))
  assign('power_mw', scaledValue(values.get('power'), 'mW', 1))
  assign('temperature_led_centi_c', scaledValue(values.get('tLED'), 'C', 100))
  assign('temperature_bt_audio_centi_c', scaledValue(values.get('tBT'), 'C', 100))
  assign('flags', hexadecimal(values.get('flags')))
  assign('program_running', booleanValue(values.get('running')))
  assign('host_offline', booleanValue(values.get('host_offline')))
  assign('hot', booleanValue(values.get('hot')))
  assign('raw_inputs', hexadecimal(values.get('inputs')))
  assign('active_keys', hexadecimal(values.get('keys')))
  assign('active_relays', activeRelays)
  assign('menu_page', integerValue(values.get('menu')))
  assign('program_mode', integerValue(values.get('mode')))
  assign('door_open', booleanValue(values.get('door')))
  assign('bluetooth_audio_state', integerValue(values.get('bt')))
  assign('pwm_available', booleanValue(values.get('available')))
  assign('pwm_channel', integerValue(values.get('channel')))
  assign('pwm_value', integerValue(values.get('value')))
  assign('pwm_errors', integerValue(values.get('errors')))
  assign('lcd_address', hexadecimal(values.get('LCD')))
  assign('framing_errors', integerValue(values.get('framing')))
  assign('crc_errors', integerValue(values.get('crc')))
  assign('reset_cause', hexadecimal(values.get('reset_cause')))
  assign('reset_count', integerValue(values.get('reset_count')))
  return status
}

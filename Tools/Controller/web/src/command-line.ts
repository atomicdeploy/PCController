import type { ControllerSettings } from './types'

export function shellArgument(value: string): string {
  if (value.length > 0 && !/[\s'"\\]/.test(value)) return value
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

export function redactSensitiveCommand(value: string): string {
  const power = value.match(/^(\s*os\s+power\s+\S+)(\s+[\s\S]+)$/i)
  return power ? `${power[1].trim()} [REDACTED]` : value
}

export function settingsSetCommand(
  current: ControllerSettings,
  displayBrightness: number,
  displayClosedBrightness: number,
  statusBrightness: number,
  streamPeriodMS: number,
  motionExitHoldSeconds: number,
  outputPersistence = current.output_persistence,
  relayRestoreMask = current.relay_restore_mask,
): string {
  const saveLastPage = (current.extended_flags & 1) !== 0 ? 1 : 0
  const decodeDecimals = (encoded: number) => encoded === 0 ? 2 : encoded - 1
  const statusColor = (current.extended_flags >> 1) & 0x07
  const voltageDecimals = decodeDecimals((current.extended_flags >> 4) & 0x03)
  const currentDecimals = decodeDecimals((current.extended_flags >> 6) & 0x03)
  return [
    'settings', 'set', current.flags, current.light_mode,
    current.on_brightness, current.off_brightness, displayBrightness,
    displayClosedBrightness, statusBrightness, outputPersistence,
    streamPeriodMS, current.default_page, saveLastPage, statusColor,
    voltageDecimals, currentDecimals, motionExitHoldSeconds, relayRestoreMask,
  ].join(' ')
}

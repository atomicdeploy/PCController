import type { ControllerSettings } from './types'

export function shellArgument(value: string): string {
  if (value.length > 0 && !/[\s"\\]/.test(value)) return value
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

export function settingsSetCommand(
  current: ControllerSettings,
  displayBrightness: number,
  statusBrightness: number,
  streamPeriodMS: number,
): string {
  const saveLastPage = (current.extended_flags & 1) !== 0 ? 1 : 0
  return [
    'settings', 'set', current.flags, current.light_mode,
    current.on_brightness, current.off_brightness, displayBrightness,
    statusBrightness, current.pwm_boot_mode, streamPeriodMS,
    current.default_page, saveLastPage,
  ].join(' ')
}

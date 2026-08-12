import type { HotkeySettingsResponse } from './types'

export interface ShortcutCaptureEvent {
  key: string
  code?: string
  repeat?: boolean
  isComposing?: boolean
  keyCode?: number
  ctrlKey?: boolean
  altKey?: boolean
  shiftKey?: boolean
  metaKey?: boolean
  getModifierState?: (key: string) => boolean
}

export interface RecordedShortcut {
  canonical: string
  keys: string[]
}

export type RegistrarReconciliation =
  | { state: 'active'; detail: string }
  | { state: 'idle'; detail: string }
  | { state: 'pending'; detail: string }
  | { state: 'error'; detail: string }
  | { state: 'unavailable'; detail: string }

export interface SafeHotkeyCommand {
  value: string
  group: 'navigation' | 'diagnostics' | 'safety'
  label: { en: string; fa: string }
}

export const safeHotkeyCommands: SafeHotkeyCommand[] = [
  { value: 'app page dashboard', group: 'navigation', label: { en: 'Open dashboard', fa: 'باز کردن داشبورد' } },
  { value: 'app page controls', group: 'navigation', label: { en: 'Open controls', fa: 'باز کردن کنترل‌ها' } },
  { value: 'app page workbench', group: 'navigation', label: { en: 'Open workbench', fa: 'باز کردن میزکار' } },
  { value: 'app page updates', group: 'navigation', label: { en: 'Open updates', fa: 'باز کردن به‌روزرسانی‌ها' } },
  { value: 'app page settings', group: 'navigation', label: { en: 'Open settings', fa: 'باز کردن تنظیمات' } },
  { value: 'app page events', group: 'navigation', label: { en: 'Open event timeline', fa: 'باز کردن رویدادها' } },
  { value: 'status', group: 'diagnostics', label: { en: 'Read controller status', fa: 'خواندن وضعیت کنترلر' } },
  { value: 'os facts system', group: 'diagnostics', label: { en: 'Read host system facts', fa: 'خواندن اطلاعات سیستم میزبان' } },
  { value: 'os facts serial', group: 'diagnostics', label: { en: 'Inspect host serial ports', fa: 'بررسی درگاه‌های سریال میزبان' } },
  { value: 'relay off', group: 'safety', label: { en: 'Turn all outputs off', fa: 'خاموش کردن همه خروجی‌ها' } },
]

const modifierKeys = new Set([
  'Alt', 'AltGraph', 'Control', 'Meta', 'OS', 'Shift', 'Super', 'Win', 'Windows',
])

const namedKeys: Record<string, string> = {
  Backspace: 'BACKSPACE',
  Tab: 'TAB',
  Enter: 'ENTER',
  Escape: 'ESC',
  Esc: 'ESC',
  ' ': 'SPACE',
  Space: 'SPACE',
  Spacebar: 'SPACE',
  PageUp: 'PAGEUP',
  PageDown: 'PAGEDOWN',
  End: 'END',
  Home: 'HOME',
  ArrowLeft: 'LEFT',
  Left: 'LEFT',
  ArrowUp: 'UP',
  Up: 'UP',
  ArrowRight: 'RIGHT',
  Right: 'RIGHT',
  ArrowDown: 'DOWN',
  Down: 'DOWN',
  Insert: 'INSERT',
  Delete: 'DELETE',
  Del: 'DELETE',
  AudioVolumeMute: 'VOLUME_MUTE',
  VolumeMute: 'VOLUME_MUTE',
  AudioVolumeDown: 'VOLUME_DOWN',
  VolumeDown: 'VOLUME_DOWN',
  AudioVolumeUp: 'VOLUME_UP',
  VolumeUp: 'VOLUME_UP',
  MediaTrackNext: 'MEDIA_NEXT',
  MediaNextTrack: 'MEDIA_NEXT',
  MediaTrackPrevious: 'MEDIA_PREVIOUS',
  MediaPreviousTrack: 'MEDIA_PREVIOUS',
  MediaStop: 'MEDIA_STOP',
  MediaPlayPause: 'MEDIA_PLAY_PAUSE',
}

const displayKeys: Record<string, string> = {
  ESC: 'Esc',
  BACKSPACE: 'Backspace',
  TAB: 'Tab',
  ENTER: 'Enter',
  SPACE: 'Space',
  PAGEUP: 'Page Up',
  PAGEDOWN: 'Page Down',
  END: 'End',
  HOME: 'Home',
  LEFT: '←',
  UP: '↑',
  RIGHT: '→',
  DOWN: '↓',
  INSERT: 'Insert',
  DELETE: 'Delete',
  VOLUME_MUTE: 'Mute',
  VOLUME_DOWN: 'Volume Down',
  VOLUME_UP: 'Volume Up',
  MEDIA_NEXT: 'Media Next',
  MEDIA_PREVIOUS: 'Media Previous',
  MEDIA_STOP: 'Media Stop',
  MEDIA_PLAY_PAUSE: 'Play/Pause',
}

function canonicalNonModifier(event: ShortcutCaptureEvent): string | null {
  const code = event.code ?? ''
  if (/^Key[A-Z]$/.test(code)) return code.slice(3)
  if (/^Digit[0-9]$/.test(code)) return code.slice(5)
  if (/^F(?:[1-9]|1[0-9]|2[0-4])$/.test(code)) return code

  const named = namedKeys[event.key]
  if (named) return named
  if (/^F(?:[1-9]|1[0-9]|2[0-4])$/i.test(event.key)) return event.key.toUpperCase()
  if (/^[a-z0-9]$/i.test(event.key)) return event.key.toUpperCase()
  return null
}

export function recordShortcut(event: ShortcutCaptureEvent): RecordedShortcut | null {
  if (
    event.repeat || event.isComposing || event.keyCode === 229 ||
    event.key === 'Process' || event.key === 'Dead' || event.key === 'Unidentified' ||
    modifierKeys.has(event.key) || event.getModifierState?.('AltGraph')
  ) return null

  const key = canonicalNonModifier(event)
  if (!key) return null
  const modifiers: string[] = []
  if (event.ctrlKey) modifiers.push('Ctrl')
  if (event.altKey) modifiers.push('Alt')
  if (event.shiftKey) modifiers.push('Shift')
  if (event.metaKey) modifiers.push('Win')
  if (modifiers.length === 0 && !/^F(?:[1-9]|1[0-9]|2[0-4])$/.test(key)) return null

  const canonical = [...modifiers, key].join('+')
  return { canonical, keys: displayChordKeys(canonical) }
}

export function displayChordKeys(chord: string): string[] {
  return chord
    .split('+')
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => displayKeys[part.toUpperCase()] ?? ({
      CTRL: 'Ctrl', CONTROL: 'Ctrl', ALT: 'Alt', OPTION: 'Alt', SHIFT: 'Shift',
      WIN: 'Win', WINDOWS: 'Win', META: 'Win', SUPER: 'Win',
    }[part.toUpperCase()] ?? part))
}

function utf8Length(value: string): number {
  return new TextEncoder().encode(value).length
}

function truncateUTF8(value: string, maximumBytes: number): string {
  let result = ''
  for (const character of value) {
    if (utf8Length(result + character) > maximumBytes) break
    result += character
  }
  return result
}

export function normalizeHotkeyName(value: string): string {
  const normalized = value
    .normalize('NFKC')
    .replace(/[\u0000-\u001f\u007f-\u009f\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069]/g, '')
    .replace(/\s+/g, ' ')
    .trimStart()
  return truncateUTF8(normalized, 64)
}

export function isSafeHotkeyCommand(value: string): boolean {
  return safeHotkeyCommands.some((command) => command.value === value)
}

export function hotkeyConfigurationMatches(response: HotkeySettingsResponse): RegistrarReconciliation {
  const status = response.status
  if (!status) return { state: 'unavailable', detail: 'The host did not expose registrar status.' }
  if (status.last_error) return { state: 'error', detail: status.last_error }
  if (!status.supported) return { state: 'error', detail: 'Global hotkeys are not supported by this host.' }

  const expected = response.bindings.filter((binding) => binding.enabled)
  const actual = status.bindings ?? []
  if (expected.length === 0) {
    return !status.running && actual.length === 0
      ? { state: 'idle', detail: 'No global shortcuts are enabled.' }
      : { state: 'pending', detail: 'The host is removing the previous shortcuts.' }
  }
  if (!status.running) return { state: 'pending', detail: 'The host registrar has not started yet.' }
  if (expected.length !== actual.length) return { state: 'pending', detail: 'The host is reconciling the shortcut set.' }

  const active = new Map(actual.map((binding) => [binding.name.toLocaleLowerCase('en-US'), binding]))
  for (const binding of expected) {
    const registered = active.get(binding.name.toLocaleLowerCase('en-US'))
    if (
      !registered || registered.accelerator !== binding.chord ||
      registered.command !== binding.command
    ) return { state: 'pending', detail: `The host has not applied ${binding.name} yet.` }
  }
  return { state: 'active', detail: `${expected.length} global shortcut${expected.length === 1 ? '' : 's'} active.` }
}

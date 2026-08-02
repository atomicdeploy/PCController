import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { HotkeyChord } from './hotkey-settings-editor'
import {
  displayChordKeys,
  hotkeyConfigurationMatches,
  isSafeHotkeyCommand,
  normalizeHotkeyName,
  recordShortcut,
} from './hotkey-editor'
import type { HotkeySettingsResponse } from './types'

describe('global shortcut recorder', () => {
  it('canonicalizes modifiers and the physical letter key', () => {
    expect(recordShortcut({
      key: '¥',
      code: 'KeyD',
      ctrlKey: true,
      altKey: true,
      shiftKey: true,
      metaKey: true,
    })).toEqual({
      canonical: 'Ctrl+Alt+Shift+Win+D',
      keys: ['Ctrl', 'Alt', 'Shift', 'Win', 'D'],
    })
  })

  it('ignores IME, repeats, dead keys, AltGraph, and modifier-only events', () => {
    expect(recordShortcut({ key: 'Process', code: 'KeyP', ctrlKey: true })).toBeNull()
    expect(recordShortcut({ key: 'p', code: 'KeyP', ctrlKey: true, isComposing: true })).toBeNull()
    expect(recordShortcut({ key: 'p', code: 'KeyP', ctrlKey: true, keyCode: 229 })).toBeNull()
    expect(recordShortcut({ key: 'p', code: 'KeyP', ctrlKey: true, repeat: true })).toBeNull()
    expect(recordShortcut({ key: 'Dead', code: 'KeyP', ctrlKey: true })).toBeNull()
    expect(recordShortcut({ key: 'Control', code: 'ControlLeft', ctrlKey: true })).toBeNull()
    expect(recordShortcut({ key: 'p', code: 'KeyP', ctrlKey: true, altKey: true, getModifierState: (key) => key === 'AltGraph' })).toBeNull()
  })

  it('allows only function keys without a modifier', () => {
    expect(recordShortcut({ key: 'p', code: 'KeyP' })).toBeNull()
    expect(recordShortcut({ key: 'F13', code: 'F13' })).toEqual({ canonical: 'F13', keys: ['F13'] })
    expect(recordShortcut({ key: 'AudioVolumeUp', ctrlKey: true })).toEqual({
      canonical: 'Ctrl+VOLUME_UP',
      keys: ['Ctrl', 'Volume Up'],
    })
  })
})

describe('shortcut presentation and input safety', () => {
  it('renders one kbd per physical key with plus signs outside', () => {
    const markup = renderToStaticMarkup(<HotkeyChord chord="Ctrl+Alt+Shift+D" />)
    expect(markup.match(/<kbd>/g)).toHaveLength(4)
    expect(markup).toContain('<kbd>Ctrl</kbd>')
    expect(markup).toContain('<kbd>D</kbd>')
    expect(markup).not.toContain('<kbd>Ctrl+Alt')
    expect(markup.match(/key-combo__separator/g)).toHaveLength(3)
  })

  it('normalizes unsafe names and enforces the approved action catalog', () => {
    expect(normalizeHotkeyName('  open\u202e\n   dashboard  ')).toBe('open dashboard ')
    expect(new TextEncoder().encode(normalizeHotkeyName('ک'.repeat(80))).length).toBeLessThanOrEqual(64)
    expect(isSafeHotkeyCommand('app page dashboard')).toBe(true)
    expect(isSafeHotkeyCommand('quit')).toBe(false)
    expect(displayChordKeys('Win+PAGEUP')).toEqual(['Win', 'Page Up'])
  })
})

describe('registrar reconciliation', () => {
  const configured: HotkeySettingsResponse = {
    apply_pending: false,
    bindings: [{ name: 'open-dashboard', enabled: true, chord: 'F13', command: 'app page dashboard' }],
    status: {
      supported: true,
      running: true,
      bindings: [{ name: 'open-dashboard', accelerator: 'F13', command: 'app page dashboard' }],
    },
  }

  it('reports active only when live registrar state exactly matches configuration', () => {
    expect(hotkeyConfigurationMatches(configured).state).toBe('active')
    expect(hotkeyConfigurationMatches({
      ...configured,
      status: { ...configured.status!, bindings: [] },
    }).state).toBe('pending')
  })

  it('surfaces registrar failures and supports the zero-binding idle state', () => {
    expect(hotkeyConfigurationMatches({
      ...configured,
      status: { supported: true, running: false, last_error: 'register F13: access denied' },
    })).toEqual({ state: 'error', detail: 'register F13: access denied' })
    expect(hotkeyConfigurationMatches({
      bindings: [],
      apply_pending: false,
      status: { supported: true, running: false, bindings: [] },
    }).state).toBe('idle')
    expect(hotkeyConfigurationMatches({ bindings: [], apply_pending: false }).state).toBe('unavailable')
  })
})

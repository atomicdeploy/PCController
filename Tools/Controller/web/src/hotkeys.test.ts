import { describe, expect, it } from 'vitest'
import {
  adjacentPageHotkey,
  ignoresGlobalHotkeys,
  isFreshAppAction,
  isEditableTarget,
  pageFromAppAction,
  pageFromGoChord,
  pageFromNumberHotkey,
} from './hotkeys'

function key(overrides: Partial<KeyboardEvent> = {}): KeyboardEvent {
  return {
    altKey: false,
    ctrlKey: false,
    defaultPrevented: false,
    isComposing: false,
    key: '',
    keyCode: 0,
    metaKey: false,
    repeat: false,
    shiftKey: false,
    target: null,
    ...overrides,
  } as KeyboardEvent
}

describe('global shortcut safety', () => {
  it('ignores editable targets, IME composition, and already claimed events', () => {
    const editable = { closest: () => ({ tagName: 'INPUT' }) } as unknown as EventTarget
    expect(isEditableTarget(editable)).toBe(true)
    expect(ignoresGlobalHotkeys(key({ target: editable }))).toBe(true)
    expect(ignoresGlobalHotkeys(key({ isComposing: true }))).toBe(true)
    expect(ignoresGlobalHotkeys(key({ keyCode: 229 }))).toBe(true)
    expect(ignoresGlobalHotkeys(key({ defaultPrevented: true }))).toBe(true)
  })

  it('does not mistake ordinary non-editable targets for fields', () => {
    const panel = { closest: () => null } as unknown as EventTarget
    expect(isEditableTarget(panel)).toBe(false)
    expect(ignoresGlobalHotkeys(key({ target: panel }))).toBe(false)
  })
})

describe('page routing', () => {
  it('maps native TUI page names into the closest web surface', () => {
    expect(pageFromAppAction('Dashboard')).toBe('dashboard')
    expect(pageFromAppAction('outputs')).toBe('controls')
    expect(pageFromAppAction('App')).toBe('settings')
    expect(pageFromAppAction('9')).toBe('events')
    expect(pageFromAppAction('local_device')).toBe('device')
    expect(pageFromAppAction('unknown')).toBeNull()
    expect(isFreshAppAction('2026-08-02T10:00:00.000Z', Date.parse('2026-08-02T10:00:04.000Z'))).toBe(true)
    expect(isFreshAppAction('2026-08-02T09:59:30.000Z', Date.parse('2026-08-02T10:00:04.000Z'))).toBe(false)
  })

  it('supports deliberate numeric, adjacent, and go-chord navigation', () => {
    expect(pageFromNumberHotkey(key({ altKey: true, key: '4' }))).toBe('device')
    expect(pageFromNumberHotkey(key({ altKey: true, ctrlKey: true, key: '4' }))).toBeNull()
    expect(adjacentPageHotkey(key({ ctrlKey: true, shiftKey: true, key: 'ArrowRight' }), 'dashboard', 'ltr')).toBe('controls')
    expect(adjacentPageHotkey(key({ ctrlKey: true, shiftKey: true, key: 'ArrowRight' }), 'dashboard', 'rtl')).toBe('settings')
    expect(pageFromGoChord('V')).toBe('device')
    expect(pageFromGoChord('B')).toBe('workbench')
    expect(pageFromGoChord('x')).toBeNull()
  })
})

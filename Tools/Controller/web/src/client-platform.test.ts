import { describe, expect, it } from 'vitest'
import { primaryShortcutARIA, primaryShortcutModifier } from './client-platform'

describe('client platform shortcuts', () => {
  it('shows only the modifier appropriate to the detected operating system', () => {
    expect(primaryShortcutModifier('Windows NT')).toBe('Ctrl')
    expect(primaryShortcutModifier('Linux x86_64')).toBe('Ctrl')
    expect(primaryShortcutModifier('MacIntel')).toBe('⌘')
    expect(primaryShortcutARIA('Macintosh')).toBe('Meta+K')
    expect(primaryShortcutARIA('Windows')).toBe('Control+K')
  })
})

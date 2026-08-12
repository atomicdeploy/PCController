import { describe, expect, it } from 'vitest'
import { formatDuration } from './i18n'

describe('formatDuration', () => {
  it('retains uptime seconds for the WebUI just like the TUI', () => {
    expect(formatDuration('en', 42_987)).toBe('42s')
    expect(formatDuration('en', 9_001_583)).toBe('2h 30m 1s')
    expect(formatDuration('fa', 9_001_583)).toBe('۲س ۳۰د ۱ث')
  })

  it('keeps invalid uptime distinct from a real zero-second device', () => {
    expect(formatDuration('en', 0)).toBe('—')
    expect(formatDuration('en', Number.NaN)).toBe('—')
  })
})

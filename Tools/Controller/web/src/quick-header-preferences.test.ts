import { describe, expect, it } from 'vitest'
import {
  defaultQuickHeaderPreferences,
  normalizeQuickHeaderPreferences,
} from './quick-header-preferences'

describe('quick header preferences', () => {
  it('keeps controls visible by default and repairs incomplete or invalid stored data', () => {
    expect(normalizeQuickHeaderPreferences(null)).toEqual(defaultQuickHeaderPreferences)
    expect(normalizeQuickHeaderPreferences({ language: false, theme: 'hidden' })).toEqual({
      ...defaultQuickHeaderPreferences,
      language: false,
    })
  })

  it('ignores unknown keys so stored browser state cannot invent controls', () => {
    expect(normalizeQuickHeaderPreferences({ theme: false, unknown: false })).toEqual({
      ...defaultQuickHeaderPreferences,
      theme: false,
    })
  })
})

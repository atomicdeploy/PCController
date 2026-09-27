import { describe, expect, it } from 'vitest'
import { defaultQuickHeaderPreferences, normalizeQuickHeaderPreferences } from './quick-header-preferences'

describe('quick header preferences', () => {
  it('keeps every control visible by default and repairs incomplete stored data', () => {
    expect(normalizeQuickHeaderPreferences(null)).toEqual(defaultQuickHeaderPreferences)
    expect(normalizeQuickHeaderPreferences({ language: false, theme: 'no' })).toEqual({ ...defaultQuickHeaderPreferences, language: false })
  })
})

import { describe, expect, it } from 'vitest'
import { configuredMelodyDetail, normalizeConfiguredMelodies } from './melody-catalog'

describe('configured melody catalog', () => {
  it('keeps only complete validated host entries', () => {
    expect(normalizeConfiguredMelodies([
      { name: 'arrival', notes: [{ frequency_hz: 440, duration_ms: 120, gap_ms: 20 }] },
      { name: 'broken', notes: [{ frequency_hz: 5, duration_ms: 120 }] },
      { name: '', notes: [] },
    ])).toEqual([{ name: 'arrival', notes: [{ frequency_hz: 440, duration_ms: 120, gap_ms: 20 }] }])
  })

  it('derives contextual labels from the live catalog', () => {
    const melody = { name: 'arrival', notes: [{ frequency_hz: 440, duration_ms: 120, gap_ms: 20 }] }
    expect(configuredMelodyDetail(melody, 'en')).toBe('1 notes · 140 ms')
    expect(configuredMelodyDetail(melody, 'fa')).toContain('140')
  })
})

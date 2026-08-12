import { describe, expect, it } from 'vitest'
import { configuredMelodyOptions, normalizeConfiguredMelodies } from './melody-catalog'

describe('configured melody options', () => {
  it('keeps the host catalog authoritative and derives concise timing detail', () => {
    expect(configuredMelodyOptions([
      { name: 'ready', notes: [{ frequency_hz: 440, duration_ms: 80, gap_ms: 20 }, { frequency_hz: 660, duration_ms: 120 }] },
      { name: '   ', notes: [] },
    ], 'en')).toEqual([{ value: 'ready', label: 'ready', detail: '2 notes · 220 ms' }])
  })

  it('drops malformed RPC entries before they reach the selector', () => {
    expect(normalizeConfiguredMelodies([
      { name: ' valid ', notes: [{ frequency_hz: 440, duration_ms: 80 }] },
      { name: 'rest', notes: [{ frequency_hz: 0, duration_ms: 75, gap_ms: 25 }] },
      { name: '', notes: [] },
      { name: 'bad-note', notes: [{ frequency_hz: 2, duration_ms: 80 }] },
      { name: 'too-long', notes: [{ frequency_hz: 440, duration_ms: 5_001 }] },
      null,
    ])).toEqual([
      { name: 'valid', notes: [{ frequency_hz: 440, duration_ms: 80, gap_ms: 0 }] },
      { name: 'rest', notes: [{ frequency_hz: 0, duration_ms: 75, gap_ms: 25 }] },
    ])
  })
})

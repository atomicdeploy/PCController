import { describe, expect, it } from 'vitest'
import { formatByteProgress, formatBytes } from './byte-format'

describe('human byte formatting', () => {
  it('uses binary units consistently without hiding exact transfer completion', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1024)).toBe('1.00 KiB')
    expect(formatBytes(6_419_456)).toBe('6.12 MiB')
    expect(formatByteProgress(6_419_456, 6_419_456)).toBe('6.12 MiB / 6.12 MiB')
  })

  it('labels unavailable counters honestly', () => {
    expect(formatBytes(undefined)).toBe('unknown')
    expect(formatByteProgress(undefined, undefined, '—')).toBe('— / —')
  })
})

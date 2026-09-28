import { describe, expect, it } from 'vitest'
import { formatDuration, formatMeasurementFreshness } from './i18n'

describe('live measurement copy', () => {
  it('keeps uptime seconds visible in English and Persian', () => {
    expect(formatDuration('en', 0)).toBe('0s')
    expect(formatDuration('en', 65_432)).toBe('1m 5s')
    expect(formatDuration('en', 90_061_000)).toBe('1d 1h 1m 1s')
    expect(formatDuration('fa', 65_432)).toBe('۱د ۵ث')
    expect(formatDuration('en', -1)).toBe('—')
  })

  it('uses an exclusive freshness boundary and reports exact stale age', () => {
    const observed = new Date('2026-09-28T10:00:00.000Z')
    const base = observed.getTime()
    expect(formatMeasurementFreshness('en', observed, 1500, base + 1499)).toBe('Live')
    expect(formatMeasurementFreshness('en', observed, 1500, base + 1500)).toBe('1.5 s ago')
    expect(formatMeasurementFreshness('en', observed, 1500, base + 9999)).toBe('10.0 s ago')
    expect(formatMeasurementFreshness('en', observed, 1500, base + 10_000)).toBe('10 s ago')
    expect(formatMeasurementFreshness('fa', observed, 1500, base + 1500)).toBe('۱٫۵ ثانیه پیش')
    expect(formatMeasurementFreshness('fa', undefined, 1500, base)).toBe('در انتظار دستگاه')
    expect(formatMeasurementFreshness('en', 0, 1500, 1500)).toBe('1.5 s ago')
  })
})

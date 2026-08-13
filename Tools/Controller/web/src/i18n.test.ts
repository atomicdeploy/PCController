import { describe, expect, it } from 'vitest'
import { formatDuration, formatMeasurementFreshness } from './i18n'

describe('live time formatting', () => {
	it('always includes device uptime seconds with intentional zero semantics', () => {
		expect(formatDuration('en', 0)).toBe('0s')
		expect(formatDuration('en', 999)).toBe('0s')
		expect(formatDuration('en', 42_987)).toBe('42s')
		expect(formatDuration('en', 9_001_583)).toBe('2h 30m 1s')
		expect(formatDuration('en', 90_061_000)).toBe('1d 1h 1m 1s')
		expect(formatDuration('fa', 42_987)).toBe('۴۲ث')
		expect(formatDuration('en', Number.NaN)).toBe('—')
		expect(formatDuration('en', -1)).toBe('—')
	})

	it('uses the canonical exclusive freshness boundary without flicker', () => {
		const now = Date.parse('2026-08-13T12:00:00.000Z')
		expect(formatMeasurementFreshness('en', now - 1499, 1500, now)).toBe('Live')
		expect(formatMeasurementFreshness('en', now - 1500, 1500, now)).toBe('1.5 s ago')
		expect(formatMeasurementFreshness('fa', now - 1500, 1500, now)).toBe('۱٫۵ ثانیه پیش')
		expect(formatMeasurementFreshness('en', undefined, 1500, now)).toBe('Waiting for device')
	})
})

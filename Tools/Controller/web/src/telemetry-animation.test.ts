import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { telemetryAnimation } from './telemetry-chart'

describe('streaming telemetry animations', () => {
  it('uses OS-aware animation and honors the app reduced-motion setting', () => {
    expect(telemetryAnimation().isAnimationActive).toBe('auto')
    expect(telemetryAnimation(true).isAnimationActive).toBe(false)
    expect(telemetryAnimation().animationDuration).toBe(250)
    expect(telemetryAnimation().animationBegin).toBe(0)
  })
  it('keeps the matching function stable between updates', () => {
    expect(telemetryAnimation().animationMatchBy).toBe(telemetryAnimation().animationMatchBy)
  })
  it('applies animation to all six series without shifting spline tangents', () => {
    const source = readFileSync(new URL('./telemetry-chart.tsx', import.meta.url), 'utf8')
    expect(source.match(/\{\.\.\.animation\}/g)).toHaveLength(6)
    expect(source.match(/type="linear"/g)).toHaveLength(6)
    expect(source).toContain("matchByDataKey('at')")
    expect(source).not.toContain('isAnimationActive={false}')
  })
})

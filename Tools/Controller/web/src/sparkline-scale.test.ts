import { describe, expect, it } from 'vitest'
import { semanticSparklineDomain, sparklinePoints } from './sparkline-scale'

describe('semantic metric sparkline scale', () => {
  it('places a normal 12 V rail well above the floor instead of magnifying noise', () => {
    const points = sparklinePoints([12.18, 12.22], 'supply')
    expect(semanticSparklineDomain([12.18, 12.22], 'supply')).toEqual([0, 13])
    expect(points.at(-1)?.y).toBeLessThan(18)
    expect(points.at(-1)?.y).toBeGreaterThan(8)
  })

  it('makes 57 C visibly hot and near the top of the thermal card', () => {
    const points = sparklinePoints([56.5, 57], 'temperature')
    expect(semanticSparklineDomain([56.5, 57], 'temperature')).toEqual([20, 60])
    expect(points.at(-1)?.y).toBeLessThan(20)
  })

  it('anchors current at zero so a few milliamps of jitter do not fill the chart', () => {
    const points = sparklinePoints([382, 387, 384], 'current')
    expect(semanticSparklineDomain([382, 387, 384], 'current')).toEqual([0, 500])
    expect(Math.max(...points.map(({ y }) => y)) - Math.min(...points.map(({ y }) => y))).toBeLessThan(1)
  })
})

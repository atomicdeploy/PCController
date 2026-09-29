import { describe, expect, it } from 'vitest'
import { semanticSparklineDomain, sparklinePoints } from './sparkline-scale'

describe('semantic metric sparkline scale', () => {
  it('places a normal 12 V rail near its expected operating headroom', () => {
    const points = sparklinePoints([12.18, 12.22], 'supply')
    expect(semanticSparklineDomain([12.18, 12.22], 'supply')).toEqual([0, 13])
    expect(points.at(-1)?.y).toBeGreaterThan(8)
    expect(points.at(-1)?.y).toBeLessThan(18)
  })

  it('makes 57 C visibly hot while expanding for readings outside the normal range', () => {
    expect(semanticSparklineDomain([56.5, 57], 'temperature')).toEqual([20, 60])
    expect(sparklinePoints([56.5, 57], 'temperature').at(-1)?.y).toBeLessThan(20)
    expect(semanticSparklineDomain([72], 'temperature')).toEqual([20, 75])
  })

  it('anchors current at zero so small jitter does not fill the card', () => {
    const points = sparklinePoints([382, 387, 384], 'current')
    expect(semanticSparklineDomain([382, 387, 384], 'current')).toEqual([0, 500])
    expect(Math.max(...points.map(({ y }) => y)) - Math.min(...points.map(({ y }) => y))).toBeLessThan(1)
  })
})

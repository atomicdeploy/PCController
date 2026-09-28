import { describe, expect, it } from 'vitest'
import {
  focusedCurrentDomain,
  focusedThermalDomain,
  focusedVoltageDomain,
  medianSmoothTelemetrySamples,
  normalizeTelemetrySamples,
  stabilizeCurrentSeries,
} from './telemetry-filter'
import type { MetricSample } from './types'

const sample = (at: number, supply = 12): MetricSample => ({ at, supply, bus: supply - 0.1, current: 100, power: 1.2, ledTemp: 33, btTemp: 31 })

describe('telemetry chart filtering', () => {
  it('orders retained samples, sanitizes fields, and keeps the newest duplicate', () => {
    const result = normalizeTelemetrySamples([
      sample(30, 10), sample(10, 11), sample(30, 12),
      { at: Number.NaN, supply: 10 }, { at: 40, supply: Number.NaN, current: 5 },
      { at: 50, supply: Number.NaN },
    ])
    expect(result.map((item) => item.at)).toEqual([10, 30, 40])
    expect(result[1].supply).toBe(12)
    expect(result[2]).toEqual({ at: 40, current: 5 })
  })

  it('rejects an isolated spike while preserving unavailable-field gaps', () => {
    const values = [sample(1, 12), { ...sample(2, 99), ledTemp: undefined }, sample(3, 12)]
    const result = medianSmoothTelemetrySamples(values)
    expect(result.map((item) => item.at)).toEqual([1, 2, 3])
    expect(result[1].supply).toBe(12)
    expect(result[1].ledTemp).toBeUndefined()
  })

  it('uses meaningful domains for nominal voltage, heat, and current headroom', () => {
    const values = [{ ...sample(0, 12), ledTemp: 52, btTemp: 51 }]
    expect(focusedVoltageDomain(values)).toEqual([11, 12.5])
    expect(focusedThermalDomain(values)).toEqual([20, 55])
    expect(focusedCurrentDomain([{ ...sample(0), current: 387 }])).toEqual([0, 500])
  })

  it('stabilizes current drawing without mutating raw samples or bridging gaps', () => {
    const samples = [10, 15, 9, 14, 10].map((current, index) => ({ ...sample(index), current }))
    const stable = stabilizeCurrentSeries(samples)
    expect(samples.map((value) => value.current)).toEqual([10, 15, 9, 14, 10])
    expect(stable.at(-1)?.current).toBeLessThan(12)
    expect(Math.max(...stable.map((value) => value.current ?? 0)) - Math.min(...stable.map((value) => value.current ?? 0))).toBeLessThan(5)
    expect(stabilizeCurrentSeries([{ at: 1, current: 10 }, { at: 2 }, { at: 3, current: 30 }])).toEqual([
      { at: 1, current: 10 }, { at: 2 }, { at: 3, current: 30 },
    ])
  })
})

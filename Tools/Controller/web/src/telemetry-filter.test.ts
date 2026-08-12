import { describe, expect, it } from 'vitest'
import { focusedCurrentDomain, focusedThermalDomain, focusedVoltageDomain, medianSmoothTelemetrySamples, normalizeTelemetrySamples, stabilizeCurrentSeries } from './telemetry-filter'

const sample = (at: number, supply = 12): any => ({ at, supply, bus: supply - 0.1, current: 100, power: 1.2, ledTemp: 33, btTemp: 31 })

describe('telemetry chart filtering', () => {
  it('orders retained samples, drops invalid entries, and keeps the latest duplicate', () => {
    const result = normalizeTelemetrySamples([sample(30, 10), sample(10, 11), sample(30, 12), sample(Number.NaN)])
    expect(result.map((item) => item.at)).toEqual([10, 30])
    expect(result[1].supply).toBe(12)
  })

  it('uses a local median to reject an isolated metric spike without moving time', () => {
    const result = medianSmoothTelemetrySamples([sample(1, 12), sample(2, 99), sample(3, 12)])
    expect(result.map((item) => item.at)).toEqual([1, 2, 3])
    expect(result[1].supply).toBe(12)
  })

  it('keeps nominal supply high and hot temperatures visibly near their meaningful bounds', () => {
    const values = [{ ...sample(0, 12), ledTemp: 52, btTemp: 51 }]
    expect(focusedVoltageDomain(values)).toEqual([11, 12.5])
    expect(focusedThermalDomain(values)).toEqual([20, 55])
  })

  it('stabilizes noisy current drawing without altering raw telemetry', () => {
    const samples = [10, 15, 9, 14, 10].map((current, index) => ({ ...sample(index), current }))
    const stable = stabilizeCurrentSeries(samples)
    expect(samples.map((value) => value.current)).toEqual([10, 15, 9, 14, 10])
    expect(stable.at(-1)?.current).toBeLessThan(12)
    expect(Math.max(...stable.map((value) => value.current)) - Math.min(...stable.map((value) => value.current))).toBeLessThan(5)
    expect(focusedCurrentDomain([{ ...sample(0), current: 387 }])).toEqual([0, 500])
  })
})

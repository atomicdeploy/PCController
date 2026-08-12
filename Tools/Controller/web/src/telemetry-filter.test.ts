import { describe, expect, it } from 'vitest'
import { medianSmoothTelemetrySamples, normalizeTelemetrySamples } from './telemetry-filter'

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
})

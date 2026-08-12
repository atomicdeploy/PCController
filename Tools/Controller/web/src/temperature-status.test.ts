import { describe, expect, it } from 'vitest'
import {
  audioTemperatureAvailableFlag,
  controllerTemperatureCelsius,
  controllerTemperatureSample,
  lightingTemperatureAvailableFlag,
} from './temperature-status'

describe('controller temperature validity', () => {
  it('accepts the physical sensor range', () => {
    expect(controllerTemperatureCelsius(-5500)).toBe(-55)
    expect(controllerTemperatureCelsius(3641)).toBe(36.41)
    expect(controllerTemperatureCelsius(12500)).toBe(125)
  })

  it('turns disconnected sentinels and impossible values into missing data', () => {
    expect(controllerTemperatureCelsius(-32768)).toBeNull()
    expect(controllerTemperatureCelsius(12501)).toBeNull()
    expect(Number.isNaN(controllerTemperatureSample(-32768))).toBe(true)
  })

  it('honors the board availability flags even when a stale numeric value remains', () => {
    const flags = lightingTemperatureAvailableFlag
    expect(controllerTemperatureCelsius(2350, flags, lightingTemperatureAvailableFlag)).toBe(23.5)
    expect(controllerTemperatureCelsius(2350, flags, audioTemperatureAvailableFlag)).toBeNull()
    expect(Number.isNaN(controllerTemperatureSample(2350, flags, audioTemperatureAvailableFlag))).toBe(true)
  })
})

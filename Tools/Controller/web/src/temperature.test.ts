import { describe, expect, it } from 'vitest'
import { formatTemperatureValue, temperatureCelsius } from './temperature'

describe('temperature presentation', () => {
  it('does not turn the firmware unavailable sentinel into a physical temperature', () => {
    expect(temperatureCelsius(-32768)).toBeNull()
    expect(formatTemperatureValue(-32768, (value) => value.toFixed(1))).toBe('—')
  })

  it('keeps valid signed centi-degree readings', () => {
    expect(temperatureCelsius(-550)).toBe(-5.5)
    expect(formatTemperatureValue(3889, (value) => value.toFixed(1))).toBe('38.9')
  })
})

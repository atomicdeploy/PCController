/** DS18B20-compatible board sensors report centi-degrees Celsius. */
export const minimumControllerTemperatureCentiC = -5500
export const maximumControllerTemperatureCentiC = 12500
export const lightingTemperatureAvailableFlag = 0x04
export const audioTemperatureAvailableFlag = 0x08

/**
 * Rejects disconnected-sensor sentinels (notably -32768) and impossible
 * readings before they reach cards, sparklines, or chart domains.
 */
export function controllerTemperatureCelsius(value: number, flags?: number, availableFlag?: number): number | null {
  if (availableFlag !== undefined && (flags === undefined || (flags & availableFlag) === 0)) return null
  if (!Number.isInteger(value) || value < minimumControllerTemperatureCentiC || value > maximumControllerTemperatureCentiC) return null
  return value / 100
}

export function controllerTemperatureSample(value: number, flags?: number, availableFlag?: number): number {
  return controllerTemperatureCelsius(value, flags, availableFlag) ?? Number.NaN
}

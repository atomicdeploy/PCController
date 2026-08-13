export const unavailableTemperatureCenti = -32768

export function temperatureCelsius(centi: number): number | null {
  return Number.isFinite(centi) && centi !== unavailableTemperatureCenti ? centi / 100 : null
}

export function formatTemperatureValue(
  centi: number,
  format: (value: number) => string,
): string {
  const value = temperatureCelsius(centi)
  return value === null ? '—' : format(value)
}

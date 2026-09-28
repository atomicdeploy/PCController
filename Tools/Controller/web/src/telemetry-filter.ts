import type { MetricSample } from './types'

const metricFields = ['supply', 'bus', 'current', 'power', 'ledTemp', 'btTemp'] as const

export type ChartDomain = readonly [number, number]

function finite(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function sanitizedSample(sample: MetricSample): MetricSample | null {
  if (!finite(sample.at)) return null
  const clean: MetricSample = { at: sample.at }
  for (const field of metricFields) {
    const value = sample[field]
    if (finite(value)) clean[field] = value
  }
  return metricFields.some((field) => clean[field] !== undefined) ? clean : null
}

function median(values: readonly number[]): number | undefined {
  const ordered = values.filter(finite).sort((left, right) => left - right)
  if (!ordered.length) return undefined
  const middle = Math.floor(ordered.length / 2)
  return ordered.length % 2 === 1 ? ordered[middle] : (ordered[middle - 1] + ordered[middle]) / 2
}

// Retained status may be replayed after reconnecting. Keep the newest value
// for each timestamp and remove malformed fields without inventing readings
// for capabilities that the board did not advertise.
export function normalizeTelemetrySamples(samples: readonly MetricSample[]): MetricSample[] {
  const byTime = new Map<number, MetricSample>()
  for (const sample of samples) {
    const clean = sanitizedSample(sample)
    if (clean) byTime.set(clean.at, clean)
  }
  return [...byTime.values()].sort((left, right) => left.at - right.at)
}

// A local three-sample median rejects isolated transport/sensor spikes. A
// missing field stays missing at its timestamp so charts never bridge an
// unavailable capability with a fabricated value.
export function medianSmoothTelemetrySamples(samples: readonly MetricSample[]): MetricSample[] {
  return samples.map((sample, index) => {
    const window = samples.slice(Math.max(0, index - 1), Math.min(samples.length, index + 2))
    const smoothed: MetricSample = { ...sample }
    for (const field of metricFields) {
      if (!finite(sample[field])) continue
      const value = median(window.map((item) => item[field]).filter(finite))
      if (value !== undefined) smoothed[field] = value
    }
    return smoothed
  })
}

// Current sensors commonly jitter by a few milliamps. Raw history remains
// untouched; only the rendered series receives a bounded EMA and deadband.
export function stabilizeCurrentSeries(samples: readonly MetricSample[], alpha = 0.2, deadband = 1): MetricSample[] {
  let previous: number | undefined
  return samples.map((sample) => {
    const raw = sample.current
    if (!finite(raw)) {
      previous = undefined
      return sample
    }
    const next = previous === undefined ? raw : previous + alpha * (raw - previous)
    const stable = previous !== undefined && Math.abs(next - previous) < deadband ? previous : next
    previous = stable
    return { ...sample, current: stable }
  })
}

function roundedDown(value: number, step: number): number {
  return Math.floor(value / step) * step
}

function roundedUp(value: number, step: number): number {
  return Math.ceil(value / step) * step
}

function fieldValues(samples: readonly MetricSample[], fields: readonly (keyof Omit<MetricSample, 'at'>)[]): number[] {
  return samples.flatMap((sample) => fields.map((field) => sample[field]).filter(finite))
}

export function focusedVoltageDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = fieldValues(samples, ['supply', 'bus'])
  if (!values.length) return [0, 1]
  const low = Math.min(...values)
  const high = Math.max(...values)
  return [
    Math.max(0, roundedDown(low - Math.max(0.8, high * 0.075), 0.25)),
    roundedUp(high + Math.max(0.2, high * 0.025), 0.25),
  ]
}

export function focusedThermalDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = fieldValues(samples, ['ledTemp', 'btTemp'])
  if (!values.length) return [20, 55]
  const low = Math.min(...values)
  const high = Math.max(...values)
  return [Math.max(0, Math.min(20, roundedDown(low - 8, 1))), Math.max(55, roundedUp(high + 3, 1))]
}

export function focusedCurrentDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = fieldValues(samples, ['current'])
  if (!values.length) return [0, 100]
  return [0, Math.max(100, roundedUp(Math.max(0, ...values) * 1.2, 50))]
}

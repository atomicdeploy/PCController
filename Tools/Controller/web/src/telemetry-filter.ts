import type { MetricSample } from './types'

const metricFields = ['supply', 'bus', 'current', 'power', 'ledTemp', 'btTemp'] as const

export type ChartDomain = readonly [number, number]

function finiteSample(sample: MetricSample): boolean {
  return Number.isFinite(sample.at) && metricFields.every((field) => Number.isFinite(sample[field]))
}

function median(values: readonly number[]): number {
  const ordered = [...values].sort((left, right) => left - right)
  const middle = Math.floor(ordered.length / 2)
  return ordered.length % 2 === 1 ? ordered[middle] : (ordered[middle - 1] + ordered[middle]) / 2
}

// The transport can replay a retained status after reconnecting. Keep the last
// value for a timestamp, reject malformed samples, and give chart rendering a
// stable chronological series without changing the authoritative runtime state.
export function normalizeTelemetrySamples(samples: readonly MetricSample[]): MetricSample[] {
  const byTime = new Map<number, MetricSample>()
  for (const sample of samples) {
    if (finiteSample(sample)) byTime.set(sample.at, sample)
  }
  return [...byTime.values()].sort((left, right) => left.at - right.at)
}

// A three-sample median rejects isolated sensor/transport spikes while keeping
// genuine steps visible. Raw remains available for diagnostics and calibration.
export function medianSmoothTelemetrySamples(samples: readonly MetricSample[]): MetricSample[] {
  return samples.map((sample, index) => {
    const start = Math.max(0, index - 1)
    const end = Math.min(samples.length, index + 2)
    const window = samples.slice(start, end)
    const smoothed: MetricSample = { ...sample }
    for (const field of metricFields) smoothed[field] = median(window.map((item) => item[field]))
    return smoothed
  })
}

// Current sensors tend to jitter by a few milliamps.  Keep raw samples in the
// runtime/history and only make the drawn series calm: bounded EMA plus a
// 1 mA deadband avoids a full chart animation for insignificant noise.
export function stabilizeCurrentSeries(samples: readonly MetricSample[], alpha = 0.2, deadband = 1): MetricSample[] {
  let previous: number | undefined
  return samples.map((sample) => {
    const raw = sample.current
    const next = previous === undefined ? raw : previous + alpha * (raw - previous)
    const stable = previous !== undefined && Math.abs(next - previous) < deadband ? previous : next
    previous = stable
    return { ...sample, current: stable }
  })
}

function finite(values: readonly number[]): number[] {
  return values.filter((value) => Number.isFinite(value))
}

function roundedDown(value: number, step: number): number {
  return Math.floor(value / step) * step
}

function roundedUp(value: number, step: number): number {
  return Math.ceil(value / step) * step
}

// These views communicate operating headroom, not merely the mathematical
// minimum: a nominal 12 V rail belongs high in its view, and 52 °C reads hot.
export function focusedVoltageDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = finite(samples.flatMap((sample) => [sample.supply, sample.bus]))
  if (!values.length) return [0, 1]
  const low = Math.min(...values)
  const high = Math.max(...values)
  return [
    Math.max(0, roundedDown(low - Math.max(0.8, high * 0.075), 0.25)),
    roundedUp(high + Math.max(0.2, high * 0.025), 0.25),
  ]
}

export function focusedThermalDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = finite(samples.flatMap((sample) => [sample.ledTemp, sample.btTemp]))
  if (!values.length) return [20, 55]
  const low = Math.min(...values)
  const high = Math.max(...values)
  return [
    Math.max(0, Math.min(20, roundedDown(low - 8, 1))),
    Math.max(55, roundedUp(high + 3, 1)),
  ]
}

export function focusedCurrentDomain(samples: readonly MetricSample[]): ChartDomain {
  const values = finite(samples.map((sample) => sample.current))
  if (!values.length) return [0, 100]
  const high = Math.max(0, ...values)
  return [0, Math.max(100, roundedUp(high * 1.2, 50))]
}

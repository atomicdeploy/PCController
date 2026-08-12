import type { MetricSample } from './types'

const metricFields = ['supply', 'bus', 'current', 'power', 'ledTemp', 'btTemp'] as const

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

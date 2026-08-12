export type SparklineDomain = readonly [number, number]
export type SparklineScale = 'auto' | 'supply' | 'current' | 'temperature'

export interface SparklinePoint {
  x: number
  y: number
}

function finiteValues(values: readonly number[]): number[] {
  return values.filter(Number.isFinite)
}

/**
 * Small metric charts communicate operating headroom, not just the min/max of
 * the most recent noise. Semantic domains keep a normal 12 V rail visibly
 * high, a 57 C sensor visibly hot, and current jitter from filling the card.
 */
export function semanticSparklineDomain(values: readonly number[], scale: SparklineScale): SparklineDomain {
  const finite = finiteValues(values)
  if (scale === 'supply') return [0, 13]
  if (scale === 'temperature') return [20, 60]
  if (scale === 'current') {
    const peak = finite.length ? Math.max(0, ...finite) : 0
    return [0, Math.max(100, Math.ceil((peak * 1.2) / 50) * 50)]
  }
  if (!finite.length) return [0, 1]
  const minimum = Math.min(...finite)
  const maximum = Math.max(...finite)
  return maximum > minimum ? [minimum, maximum] : [minimum, minimum + 1]
}

export function sparklinePoints(
  values: readonly number[],
  scale: SparklineScale = 'auto',
  width = 300,
  height = 92,
): SparklinePoint[] {
  const data = finiteValues(values)
  const plotted = data.length > 1 ? data : data.length === 1 ? [data[0], data[0]] : [0, 0]
  const [minimum, maximum] = semanticSparklineDomain(plotted, scale)
  const span = Math.max(Number.EPSILON, maximum - minimum)
  return plotted.map((value, index) => {
    const normalized = Math.max(0, Math.min(1, (value - minimum) / span))
    return {
      x: (index / Math.max(1, plotted.length - 1)) * width,
      y: height - 8 - normalized * (height - 20),
    }
  })
}

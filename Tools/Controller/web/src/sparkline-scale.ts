export type SparklineDomain = readonly [number, number]
export type SparklineScale = 'auto' | 'supply' | 'current' | 'temperature'

export interface SparklinePoint {
  x: number
  y: number
}

function finiteValues(values: readonly number[]): number[] {
  return values.filter(Number.isFinite)
}

export function semanticSparklineDomain(values: readonly number[], scale: SparklineScale): SparklineDomain {
  const finite = finiteValues(values)
  if (!finite.length) return scale === 'temperature' ? [20, 60] : scale === 'supply' ? [0, 13] : [0, 1]
  const low = Math.min(...finite)
  const high = Math.max(...finite)
  if (scale === 'supply') return [0, Math.max(13, Math.ceil(high * 1.05 * 2) / 2)]
  if (scale === 'temperature') return [Math.min(20, Math.floor(low - 5)), Math.max(60, Math.ceil(high + 3))]
  if (scale === 'current') return [0, Math.max(100, Math.ceil((Math.max(0, high) * 1.2) / 50) * 50)]
  if (high > low) return [low, high]
  const padding = Math.max(1, Math.abs(low) * 0.05)
  return [low - padding, high + padding]
}

export function sparklinePoints(values: readonly number[], scale: SparklineScale = 'auto', width = 300, height = 92): SparklinePoint[] {
  const data = finiteValues(values)
  const plotted = data.length > 1 ? data : data.length === 1 ? [data[0], data[0]] : [0, 0]
  const [minimum, maximum] = semanticSparklineDomain(plotted, scale)
  const span = Math.max(Number.EPSILON, maximum - minimum)
  return plotted.map((value, index) => ({
    x: (index / Math.max(1, plotted.length - 1)) * width,
    y: height - 8 - Math.max(0, Math.min(1, (value - minimum) / span)) * (height - 20),
  }))
}

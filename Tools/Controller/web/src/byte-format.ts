export function formatBytes(value: number | undefined | null, unknown = 'unknown'): string {
  if (!Number.isFinite(value) || value === undefined || value === null || value < 0) return unknown
  if (value === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)))
  const scaled = value / 1024 ** exponent
  const digits = exponent === 0 ? 0 : scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2
  return `${scaled.toFixed(digits)} ${units[exponent]}`
}

export function formatByteProgress(done: number | undefined | null, total: number | undefined | null, unknown = 'unknown'): string {
  return `${formatBytes(done, unknown)} / ${formatBytes(total, unknown)}`
}

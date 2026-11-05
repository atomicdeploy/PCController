export function stableParts(value) {
  const normalized = String(value).trim().replace(/^go|^v|^u/, '')
  if (!/^\d+(?:\.\d+){1,3}$/.test(normalized)) return null
  return normalized.split('.').map(Number)
}

export function compareVersions(left, right) {
  const a = stableParts(left)
  const b = stableParts(right)
  if (!a || !b) return String(left).localeCompare(String(right))
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const delta = (a[index] ?? 0) - (b[index] ?? 0)
    if (delta) return Math.sign(delta)
  }
  return 0
}

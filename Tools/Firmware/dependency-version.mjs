// Compatibility floors are not exact dependency pins.
export function satisfiesMinimumVersion(version, minimum) {
  if (!/^\d+\.\d+\.\d+$/u.test(version ?? '') || !/^\d+\.\d+\.\d+$/u.test(minimum ?? '')) return false
  const have = version.split('.').map(Number), need = minimum.split('.').map(Number)
  for (let index = 0; index < 3; index++) {
    if (have[index] !== need[index]) return have[index] > need[index]
  }
  return true
}

export function matchesAppTarget(target: string | undefined, instanceID: string, surface: string): boolean {
  const normalized = (target ?? '').trim().toLowerCase()
  return normalized === '' || normalized === '*' ||
    normalized === instanceID.trim().toLowerCase() ||
    normalized === surface.trim().toLowerCase()
}

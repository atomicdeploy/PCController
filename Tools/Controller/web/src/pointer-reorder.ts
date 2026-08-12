/**
 * Pointer capture keeps producing move samples while layout animation moves the
 * dragged item underneath a stationary pointer. Apply at most one reorder per
 * distinct non-source target so the same hover cannot invert itself repeatedly.
 */
export function pointerReorderTargetChanged(
  previousTarget: string | null,
  source: string,
  candidate: string | null | undefined,
): candidate is string {
  return Boolean(candidate && candidate !== source && candidate !== previousTarget)
}

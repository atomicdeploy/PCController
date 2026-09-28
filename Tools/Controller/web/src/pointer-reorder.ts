/**
 * Pointer capture keeps producing move samples while layout changes move the
 * dragged item underneath a stationary pointer. Accept a target only when it
 * differs from both the source and the last accepted target.
 */
export function pointerReorderTargetChanged<CardID extends string>(
  previousTarget: CardID | null,
  source: CardID,
  candidate: CardID | null | undefined,
): candidate is CardID {
  return Boolean(candidate && candidate !== source && candidate !== previousTarget)
}

export function keyboardReorderOffset(key: string, direction: 'ltr' | 'rtl'): -1 | 1 | null {
  if (key === 'ArrowUp') return -1
  if (key === 'ArrowDown') return 1
  if (key === 'ArrowLeft') return direction === 'rtl' ? 1 : -1
  if (key === 'ArrowRight') return direction === 'rtl' ? -1 : 1
  return null
}

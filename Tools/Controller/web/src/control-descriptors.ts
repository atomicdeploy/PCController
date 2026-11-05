import type { ControlDescriptor } from './types'

export function motionControlKey(controls: readonly ControlDescriptor[] | undefined, side: 'left' | 'right'): string | undefined {
  const suffix = side === 'left' ? 'a' : 'b'
  return controls?.find(control => control.key === `seat.${suffix}` || control.key === `motion.${suffix}`)?.key
}

export function resolvedControlIcon(controls: readonly ControlDescriptor[] | undefined, key: string): string | undefined {
  return controls?.find(control => control.key === key)?.icon?.trim() || undefined
}

export function resolvedControlName(
  controls: readonly ControlDescriptor[] | undefined,
  key: string,
  fallback: string,
): string {
  return controls?.find((control) => control.key === key)?.name.trim() || fallback
}

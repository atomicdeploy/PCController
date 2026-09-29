import type { ControlDescriptor } from './types'

export function resolvedControlName(
  controls: readonly ControlDescriptor[] | undefined,
  key: string,
  fallback: string,
): string {
  return controls?.find((control) => control.key === key)?.name.trim() || fallback
}

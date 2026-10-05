import type { MotionDirection, MotionSideState } from './types'

export type PresentedMotionDirection = MotionDirection | 'unknown'

/**
 * Present semantic intent during the firmware's safe break-before-make window.
 * Raw relay bits remain available separately for electrical diagnostics.
 */
export function presentedMotionDirection(state?: MotionSideState): PresentedMotionDirection {
  if (!state) return 'unknown'
  return state.transitioning ? state.requested : state.applied
}

import { describe, expect, it } from 'vitest'
import { macroLifecycleLabel } from './macro-library'

describe('macro library state labels', () => {
  it('prefers errors, then live playback, then the host lifecycle', () => {
    expect(macroLifecycleLabel({ running: false, lifecycle: 'completed', last_error: 'timeout' })).toBe('error')
    expect(macroLifecycleLabel({ running: true, lifecycle: 'completed' })).toBe('playing')
    expect(macroLifecycleLabel({ running: false, lifecycle: 'completed' })).toBe('completed')
  })
})

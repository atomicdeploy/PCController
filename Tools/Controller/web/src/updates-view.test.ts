import { describe, expect, it } from 'vitest'
import { activeUpdateProgress } from './updates-view'
import type { UpdateStatus } from './updates-api'

function status(state: UpdateStatus['state'], progress_percent = 0): UpdateStatus {
  return { id: 'update-test', kind: 'firmware', state, progress_percent }
}

describe('update progress truth', () => {
  it('does not present progress for idle or terminal results', () => {
    expect(activeUpdateProgress(null)).toBeNull()
    for (const state of ['downloaded', 'staged', 'completed', 'failed'] as const) {
      expect(activeUpdateProgress(status(state, 100))).toBeNull()
    }
  })

  it('allows zero percent only for an active operation and clamps bad producers', () => {
    expect(activeUpdateProgress(status('queued', 0))).toBe(0)
    expect(activeUpdateProgress(status('programming', 42))).toBe(42)
    expect(activeUpdateProgress(status('verifying', 130))).toBe(100)
  })
})

import { describe, expect, it } from 'vitest'
import { presentedMotionDirection } from './motion-feedback'

describe('presentedMotionDirection', () => {
  it('keeps requested direction visible during a safe reversal', () => {
    expect(presentedMotionDirection({
      requested: 'down',
      applied: 'stop',
      transitioning: true,
      revision: 8,
    })).toBe('down')
  })

  it('uses board-applied direction after reconciliation', () => {
    expect(presentedMotionDirection({
      requested: 'down',
      applied: 'down',
      transitioning: false,
      revision: 9,
    })).toBe('down')
  })

  it('does not invent state for older controller snapshots', () => {
    expect(presentedMotionDirection()).toBe('unknown')
  })
})

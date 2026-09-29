import { describe, expect, it } from 'vitest'

import { displayPresentationCommand } from './display-command'

describe('displayPresentationCommand', () => {
  it('preserves arbitrary printable text and the complete marquee policy', () => {
    expect(displayPresentationCommand({
      target: 'segments', text: '  HELLO WORLD  ', speedMS: 180,
      durationMS: 1200, repeat: 'interval', intervalMS: 30000, scroll: true,
    })).toBe('display segments --speed 180ms --duration 1200ms --repeat interval --interval 30000ms --scroll -- "  HELLO WORLD  "')
  })

  it('omits interval timing for once and does not force an LCD marquee', () => {
    expect(displayPresentationCommand({
      target: 'lcd', text: 'READY', speedMS: 220,
      durationMS: 5000, repeat: 'once', intervalMS: 30000, scroll: true,
    })).toBe('display lcd --speed 220ms --duration 5000ms --repeat once -- READY')
  })
})

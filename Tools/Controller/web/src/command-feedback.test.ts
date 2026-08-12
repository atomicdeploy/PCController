import { describe, expect, it } from 'vitest'
import { commandSuccessShouldToast } from './command-feedback'

describe('command success feedback', () => {
  it('keeps terminal and live-display traffic quiet', () => {
    for (const command of ['status', 'help', 'menu next', 'display segments TEST', 'buzzer 880 80', 'relay 5 on']) {
      expect(commandSuccessShouldToast(command)).toBe(false)
    }
  })

  it('retains explicit and non-live operation confirmations', () => {
    expect(commandSuccessShouldToast('status', 'Status refreshed')).toBe(true)
    expect(commandSuccessShouldToast('reconnect')).toBe(true)
  })
})

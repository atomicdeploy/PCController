import { describe, expect, it } from 'vitest'
import { shouldToastControllerEvent } from './event-notification-policy'

describe('controller event toast policy', () => {
  it('only toasts relay changes that originated outside the host', () => {
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'physical' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'rf' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'webui' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'macro' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'automation' })).toBe(false)
  })
  it('retains fault and door safety notifications', () => {
    expect(shouldToastControllerEvent({ kind: 'motion.fault', source: 'host' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'door', source: 'physical' })).toBe(true)
  })
})

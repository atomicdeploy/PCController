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

  it('does not toast parsed HELLO or status traffic', () => {
    expect(shouldToastControllerEvent({ kind: 'hello.parsed', text: 'HELLO PCController' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'status', text: 'STATUS relay=0' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'serial.line', text: 'HELLO PCController' })).toBe(false)
  })
})

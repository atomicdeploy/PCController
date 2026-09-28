import { describe, expect, it } from 'vitest'
import { shouldToastControllerEvent } from './event-notification-policy'

describe('controller event toast policy', () => {
  it('only toasts relay changes initiated outside the host', () => {
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'physical' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'rf' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'webui' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'board', metadata: { source: 'physical' } })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'relay.changed', source: 'board', metadata: { source: 'automation' } })).toBe(false)
  })

  it('retains one-shot safety events but suppresses continuous transport traffic', () => {
    expect(shouldToastControllerEvent({ kind: 'motion.fault', source: 'host' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'door', source: 'physical' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'hello.parsed', text: 'HELLO PCController' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'status', text: 'STATUS relay=0' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'telemetry.sample' })).toBe(false)
  })

  it('presents only messages explicitly targeted to the Web surface or all clients', () => {
    expect(shouldToastControllerEvent({ kind: 'message', target: 'web' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', target: 'native, all' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', target: 'native,tui' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'message.delivery', target: 'web' })).toBe(false)
  })
})

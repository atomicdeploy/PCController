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
    expect(shouldToastControllerEvent({ kind: 'hardware.problem', source: 'host' })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'bridge.peer.offline', source: 'bridge' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'hello.parsed', text: 'HELLO PCController' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'status', text: 'STATUS relay=0' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'telemetry.sample' })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'transport.frame.recovered', source: 'board' })).toBe(false)
  })

  it('presents only messages explicitly targeted to this Web capability, surface, or instance', () => {
    expect(shouldToastControllerEvent({ kind: 'message', targets: ['surface:webui'] })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', targets: ['surface:desktop', 'all'] })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', targets: ['capability:messages'] })).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', targets: ['tab:one'] }, 'tab:one')).toBe(true)
    expect(shouldToastControllerEvent({ kind: 'message', targets: ['surface:desktop', 'tui'] })).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'message', target: 'webui' } as never)).toBe(false)
    expect(shouldToastControllerEvent({ kind: 'message.delivery', targets: ['surface:webui'] })).toBe(false)
  })
})

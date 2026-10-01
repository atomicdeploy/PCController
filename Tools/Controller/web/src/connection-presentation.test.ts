import { describe, expect, it } from 'vitest'
import { connectionPresentation, hardwareResetAvailable } from './connection-presentation'
import { emptySnapshot } from './types'

describe('connectionPresentation', () => {
  it('allows a hardware reset for an enumerated candidate before firmware authentication', () => {
    expect(hardwareResetAvailable({
      ...emptySnapshot,
      connected: false,
      connection_candidate: { name: 'COM3', friendly_name: 'USB-SERIAL CH340' },
    })).toBe(true)
    expect(hardwareResetAvailable({ ...emptySnapshot, connected: false })).toBe(false)
  })

  it('distinguishes an active bounded attempt from retry backoff', () => {
    const now = Date.parse('2026-09-29T09:00:05Z')
    const attempting = connectionPresentation({
      ...emptySnapshot,
      connection_phase: 'attempting',
      connection_attempt: 4,
      connection_attempt_started: '2026-09-29T09:00:02Z',
      connection_candidate: { name: 'COM3', friendly_name: 'USB-SERIAL CH340' },
    }, 'en', now)
    expect(attempting.title).toBe('Contacting controller board')
    expect(attempting.detail).toContain('USB-SERIAL CH340')
    expect(attempting.timing).toContain('3')
    expect(attempting.retryDisabled).toBe(true)
    expect(attempting.animated).toBe(true)

    const waiting = connectionPresentation({
      ...emptySnapshot,
      connection_phase: 'waiting_retry',
      connection_attempt: 4,
      connection_next_retry: '2026-09-29T09:00:12Z',
      connection_reason: 'COM3: application HELLO timed out',
      connection_candidate: { name: 'COM3' },
    }, 'en', now)
    expect(waiting.title).toBe('Board did not answer')
    expect(waiting.detail).toContain('HELLO timed out')
    expect(waiting.timing).toContain('7')
    expect(waiting.action).toBe('Try now')
    expect(waiting.retryDisabled).toBe(false)
    expect(waiting.animated).toBe(false)
  })

  it('does not claim that a paused or blocked connection is connecting', () => {
    expect(connectionPresentation({
      ...emptySnapshot,
      paused: true,
      connection_phase: 'paused',
      connection_reason: 'closed by host',
    }, 'en').title).toBe('Board connection is closed')
    expect(connectionPresentation({
      ...emptySnapshot,
      connection_phase: 'blocked',
      connection_reason: 'CancelIoEx failed',
    }, 'en').title).toBe('Serial cleanup needs attention')
  })
})

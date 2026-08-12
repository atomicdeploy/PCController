import { describe, expect, it } from 'vitest'
import {
  isSignificantControllerEvent,
  prependSignificantControllerEvent,
  significantControllerEvents,
} from './significant-events'
import type { ControllerEvent } from './types'

function event(id: number, kind: string, stream?: ControllerEvent['stream']): ControllerEvent {
	return { id, kind, stream, text: kind, time: `2026-08-02T00:00:0${id}Z` }
}

describe('significant controller events', () => {
	it.each(['telemetry', ' TELEMETRY ', 'rx', ' Rx\t', 'TX', '\ttx ', 'front_panel.segment', 'status_led.changed', 'buzzer.note', 'action.applied', 'device event 13'])(
    'keeps routine %j transport activity out of human-facing feeds',
    (kind) => expect(isSignificantControllerEvent(event(1, kind))).toBe(false),
	)

	it('uses the host stream classification and keeps explicit debug opt-in separate', () => {
		expect(isSignificantControllerEvent(event(1, 'future.frame', 'state'))).toBe(false)
		expect(isSignificantControllerEvent(event(2, 'future.event', 'telemetry'))).toBe(false)
		expect(isSignificantControllerEvent(event(3, 'future.event', 'debug'))).toBe(false)
		expect(isSignificantControllerEvent(event(4, 'future.event', 'activity'))).toBe(true)
		expect(isSignificantControllerEvent(event(5, 'device event 13', 'activity'))).toBe(false)
	})

	it('hides unchanged app-instance heartbeats but keeps material lifecycle changes', () => {
		expect(isSignificantControllerEvent({ ...event(1, 'app.instance.changed', 'activity'), lifecycle: 'updated' })).toBe(false)
		expect(isSignificantControllerEvent({ ...event(2, 'app.instance.changed', 'activity'), state: 'heartbeat' })).toBe(false)
		expect(isSignificantControllerEvent({ ...event(3, 'app.instance.changed', 'activity'), lifecycle: 'joined' })).toBe(true)
		expect(isSignificantControllerEvent({ ...event(4, 'app.instance.changed', 'activity'), lifecycle: 'left' })).toBe(true)
		expect(isSignificantControllerEvent({ ...event(5, 'app.instance.changed', 'activity'), metadata: { change: 'disconnected' } })).toBe(true)
		expect(isSignificantControllerEvent({ ...event(6, 'app.instance.changed', 'activity'), metadata: { change: 'updated' } })).toBe(false)
	})

  it.each(['door', 'rf.received', 'macro.completed', 'connection', 'warning', 'app.page'])(
    'retains significant %s activity',
    (kind) => expect(isSignificantControllerEvent(event(1, kind))).toBe(true),
  )

  it('filters history and cross-tab residue without losing significant order', () => {
    const history = [event(1, 'door'), event(2, ' telemetry '), event(3, 'RF'), event(4, 'TX')]
    expect(significantControllerEvents(history).map(({ id }) => id)).toEqual([1, 3])

    const next = prependSignificantControllerEvent(history, event(5, 'macro.completed'))
    expect(next.map(({ id }) => id)).toEqual([5, 1, 3])
    expect(prependSignificantControllerEvent(next, event(6, ' RX '))).toEqual(next)
  })

  it('deduplicates retransmitted significant events and applies the feed limit', () => {
    const next = prependSignificantControllerEvent(
      [event(3, 'door'), event(2, 'rf.received'), event(1, 'warning')],
      event(2, 'rf.updated'),
      2,
    )
    expect(next.map(({ id, kind }) => [id, kind])).toEqual([[2, 'rf.updated'], [3, 'door']])
  })
})

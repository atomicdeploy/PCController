import { describe, expect, it } from 'vitest'
import {
  isSignificantControllerEvent,
  prependSignificantControllerEvent,
  significantControllerEvents,
} from './significant-events'
import type { ControllerEvent } from './types'

function event(id: number, kind: string): ControllerEvent {
  return { id, kind, text: kind, time: `2026-08-02T00:00:0${id}Z` }
}

describe('significant controller events', () => {
  it.each(['telemetry', ' TELEMETRY ', 'rx', ' Rx\t', 'TX', '\ttx '])(
    'keeps routine %j transport activity out of human-facing feeds',
    (kind) => expect(isSignificantControllerEvent(event(1, kind))).toBe(false),
  )

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

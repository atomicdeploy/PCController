import { describe, expect, it } from 'vitest'
import { applyPushedOutputEvent, applyStatusLEDEvent, segmentStateFromEvent, statusLEDFromEvent } from './status-led-event'
import { emptySnapshot } from './types'

describe('pushed status LED events', () => {
  it('updates the snapshot immediately without a refresh poll', () => {
    const event = {
      id: 4,
      time: '2026-08-03T10:00:00Z',
      kind: 'status_led.changed',
      text: 'changed',
      metadata: { red: '18', green: '52', blue: '86', brightness: '200', effect: '3', condition: '8' },
    }
    const state = statusLEDFromEvent(event)
    expect(state).toEqual({ red: 18, green: 52, blue: 86, brightness: 200, effect: 3, condition: 8 })
    const snapshot = applyStatusLEDEvent(emptySnapshot, event)
    expect(snapshot.have_status_led).toBe(true)
    expect(snapshot.status_led).toEqual(state)
  })

  it('applies changed-only segment events without polling', () => {
    const event = { id: 5, time: '2026-08-03T10:00:01Z', kind: 'front_panel.segment', text: 'changed', metadata: { raw_segments: '065B4F66', brightness: '7' } }
    expect(segmentStateFromEvent(event)).toEqual({ raw_segments: [0x06, 0x5B, 0x4F, 0x66], brightness: 7 })
    const snapshot = applyPushedOutputEvent(emptySnapshot, event)
    expect(snapshot.front_panel?.raw_segments).toEqual([0x06, 0x5B, 0x4F, 0x66])
    expect(snapshot.have_front_panel).toBe(true)
  })

  it('ignores incomplete or unrelated events', () => {
    expect(statusLEDFromEvent({ id: 1, time: '', kind: 'status_led.changed', text: '', metadata: { red: '1' } })).toBeNull()
    expect(statusLEDFromEvent({ id: 2, time: '', kind: 'buzzer.note', text: '' })).toBeNull()
  })
})

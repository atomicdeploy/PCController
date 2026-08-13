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

	it('does not promote a changed-only segment event to an exact panel', () => {
		const event = { id: 5, time: '2026-08-03T10:00:01Z', kind: 'front_panel.segment', text: 'changed', metadata: { raw_segments: '065B4F66', brightness: '7' } }
		expect(segmentStateFromEvent(event)).toEqual({ raw_segments: [0x06, 0x5B, 0x4F, 0x66], brightness: 7 })
		const snapshot = applyPushedOutputEvent(emptySnapshot, event)
		expect(snapshot).toBe(emptySnapshot)
		expect(snapshot.front_panel?.raw_segments).toEqual([0, 0, 0, 0])
		expect(snapshot.front_panel?.brightness).toBe(0)
		expect(snapshot.have_front_panel).toBe(false)
	})

	it('merges changed segment fields into a previously fetched exact panel', () => {
		const event = { id: 6, time: '2026-08-03T10:00:02Z', kind: 'front_panel.segment', text: 'changed', metadata: { raw_segments: '065B4F66', brightness: '7' } }
		const current = {
			...emptySnapshot,
			have_front_panel: true,
			front_panel: {
				schema: 2, raw_segments: [1, 2, 3, 4] as [number, number, number, number], brightness: 3,
				blink: false, segments_active: true, category_selector: false,
				lcd_address: 0x27, lcd_available: true, lcd_backlight: true,
				lcd_line_1: 'exact', lcd_line_2: 'readback', pressed_keys: 4,
				menu_page: 9, program_mode: 7, host_captured: false, host_state: 0, host_editable_value: 0,
			},
		}
		const snapshot = applyPushedOutputEvent(current, event)
		expect(snapshot.front_panel?.raw_segments).toEqual([0x06, 0x5B, 0x4F, 0x66])
		expect(snapshot.front_panel?.brightness).toBe(7)
		expect(snapshot.front_panel?.menu_page).toBe(9)
		expect(snapshot.front_panel?.program_mode).toBe(7)
		expect(snapshot.front_panel?.pressed_keys).toBe(4)
		expect(snapshot.have_front_panel).toBe(true)
		expect(snapshot.front_panel_updated).toBe(event.time)
	})

  it('ignores incomplete or unrelated events', () => {
    expect(statusLEDFromEvent({ id: 1, time: '', kind: 'status_led.changed', text: '', metadata: { red: '1' } })).toBeNull()
    expect(statusLEDFromEvent({ id: 2, time: '', kind: 'buzzer.note', text: '' })).toBeNull()
  })
})

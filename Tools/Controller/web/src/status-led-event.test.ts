import { describe, expect, it } from 'vitest'
import { advanceStatusLEDSource, applyPushedOutputEvent, applyStatusLEDEvent, mergeStatusLEDSnapshot, segmentStateFromEvent, statusLEDSnapshotMatchesSource, statusLEDFromEvent, statusLEDSourceUnchanged } from './status-led-event'
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

	it('retains changed-only segment authority without promoting it to an exact panel', () => {
		const event = { id: 5, time: '2026-08-03T10:00:01Z', kind: 'front_panel.segment', text: 'changed', metadata: { raw_segments: '065B4F66', brightness: '7' } }
		expect(segmentStateFromEvent(event)).toEqual({ raw_segments: [0x06, 0x5B, 0x4F, 0x66], brightness: 7 })
		const snapshot = applyPushedOutputEvent(emptySnapshot, event)
		expect(snapshot.front_panel?.raw_segments).toEqual([0x06, 0x5B, 0x4F, 0x66])
		expect(snapshot.front_panel?.brightness).toBe(7)
		expect(snapshot.have_front_panel).toBe(false)
		expect(snapshot.have_front_panel_segments).toBe(true)
		expect(snapshot.front_panel_updated).toBe(event.time)
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
		expect(snapshot.have_front_panel_segments).toBe(true)
		expect(snapshot.front_panel_updated).toBe(event.time)
	})

  it('ignores incomplete or unrelated events', () => {
    expect(statusLEDFromEvent({ id: 1, time: '', kind: 'status_led.changed', text: '', metadata: { red: '1' } })).toBeNull()
    expect(statusLEDFromEvent({ id: 2, time: '', kind: 'buzzer.note', text: '' })).toBeNull()
  })

  it('orders frames by transport epoch and source revision rather than wall time', () => {
    const current = {
      ...emptySnapshot,
      host_instance_id: 'primary-a',
      have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_updated: '2026-08-03T10:00:05Z',
      status_led_revision: 10,
      status_led_epoch: 1,
    }
    const delayed = {
      id: 9, time: '2026-08-03T11:00:00Z', kind: 'status_led.changed', text: 'older rise',
      metadata: { red: '0', green: '0', blue: '18', brightness: '145', effect: '1', condition: '9', revision: '9' },
    }
    expect(applyStatusLEDEvent(current, delayed, { epoch: 1, instanceID: 'primary-a' })).toBe(current)

    const newer = applyStatusLEDEvent(current, {
      ...delayed,
      id: 11,
      time: '2026-08-03T09:00:00Z',
      metadata: { ...delayed.metadata, blue: '100', revision: '11' },
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(newer).toMatchObject({ status_led: { blue: 100 }, status_led_revision: 11 })
  })

  it('accepts a lower revision from a newly acknowledged host process', () => {
    const current = {
      ...emptySnapshot,
      host_instance_id: 'primary-a',
      have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 100,
      status_led_epoch: 1,
    }
    const restarted = applyStatusLEDEvent(current, {
      id: 1, time: 'clock-regressed', kind: 'status_led.changed', text: 'off',
      metadata: { red: '0', green: '0', blue: '0', brightness: '0', effect: '0', condition: '255', revision: '1' },
    }, { epoch: 2, instanceID: 'primary-b' })
    expect(restarted).toMatchObject({
      host_instance_id: 'primary-b',
      status_led: { red: 0, green: 0, blue: 0, brightness: 0, effect: 0, condition: 255 },
      status_led_revision: 1,
      status_led_epoch: 2,
    })
  })

  it('merges HTTP snapshots without letting stale identities or revisions overwrite live state', () => {
    const current = {
      ...emptySnapshot,
      host_instance_id: 'primary-b',
      have_status_led: true,
      status_led: { red: 0, green: 0, blue: 80, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 12,
      status_led_epoch: 2,
    }
    const staleIdentity = mergeStatusLEDSnapshot(current, {
      ...current,
      host_instance_id: 'primary-a',
      status_led: { ...current.status_led, blue: 18 },
      status_led_revision: 99,
    }, { epoch: 2, instanceID: 'primary-a', authoritativeInstanceID: 'primary-b' })
    expect(staleIdentity).toBe(current)

    const staleRevision = mergeStatusLEDSnapshot(current, {
      ...current,
      status_led: { ...current.status_led, blue: 40 },
      status_led_revision: 11,
    }, { epoch: 2, instanceID: 'primary-b', authoritativeInstanceID: 'primary-b' })
    expect(staleRevision).toMatchObject({ status_led: { blue: 80 }, status_led_revision: 12 })

    const newPrimary = mergeStatusLEDSnapshot(current, {
      ...current,
      host_instance_id: 'primary-c',
      status_led: { ...current.status_led, blue: 8 },
      status_led_revision: 1,
    }, { epoch: 3, instanceID: 'primary-c', authoritativeInstanceID: 'primary-c' })
    expect(newPrimary).toMatchObject({
      host_instance_id: 'primary-c', status_led: { blue: 8 }, status_led_revision: 1, status_led_epoch: 3,
    })
  })

  it('tracks acknowledged sources and rejects a snapshot completed after the source changed', () => {
    const connecting = advanceStatusLEDSource({ epoch: 1, instanceID: 'primary-a' }, { epoch: 2 })
    expect(connecting).toEqual({ epoch: 2 })
    const open = advanceStatusLEDSource(connecting!, { epoch: 2, instanceID: 'primary-b' })
    expect(open).toEqual({ epoch: 2, instanceID: 'primary-b' })
    expect(advanceStatusLEDSource(open!, { epoch: 1, instanceID: 'primary-a' })).toBeNull()
    expect(statusLEDSourceUnchanged(
      { epoch: 1, instanceID: 'primary-a' },
      { epoch: 2, instanceID: 'primary-b' },
    )).toBe(false)
    expect(statusLEDSnapshotMatchesSource(
      { host_instance_id: 'primary-a' },
      { epoch: 2, instanceID: 'primary-b' },
    )).toBe(false)
  })
})

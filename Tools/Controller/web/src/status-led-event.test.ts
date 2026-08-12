import { describe, expect, it } from 'vitest'
import { advanceStatusLEDSource, applyPushedOutputEvent, applyStatusLEDEvent, mergeStatusLEDSnapshot, segmentStateFromEvent, statusLEDFromEvent, statusLEDSnapshotMatchesSource, statusLEDSourceUnchanged } from './status-led-event'
import { emptySnapshot } from './types'

describe('pushed status LED events', () => {
  it('updates the snapshot immediately without a refresh poll', () => {
    const event = {
      id: 4,
      time: '2026-08-03T10:00:00Z',
      kind: 'status_led.changed',
      text: 'changed',
      metadata: { red: '18', green: '52', blue: '86', brightness: '200', effect: '3', condition: '8', revision: '1' },
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

  it('keeps a newer snapshot ahead of a delayed rising frame without using wall time', () => {
    const peak = {
      ...emptySnapshot,
      host_instance_id: 'primary-a',
      have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_updated: '2026-08-03T10:00:05Z',
      status_led_revision: 10,
      status_led_epoch: 1,
    }
    const delayed = {
      id: 9,
      time: '2026-08-03T10:00:01Z',
      kind: 'status_led.changed',
      text: 'older rise',
      metadata: { red: '0', green: '0', blue: '18', brightness: '145', effect: '1', condition: '9', revision: '9' },
    }
    expect(applyStatusLEDEvent(peak, delayed, { epoch: 1, instanceID: 'primary-a' })).toBe(peak)

    const fallingWithRegressedClock = {
      ...delayed,
      id: 11,
      time: '2026-08-03T09:00:00Z',
      text: 'newer fall after clock step',
      metadata: { ...delayed.metadata, blue: '100', revision: '11' },
    }
    const falling = applyStatusLEDEvent(peak, fallingWithRegressedClock, { epoch: 1, instanceID: 'primary-a' })
    expect(falling.status_led?.blue).toBe(100)
    expect(falling.status_led_revision).toBe(11)
    expect(falling.status_led_updated).toBe('2026-08-03T09:00:00Z')
  })

  it('advances an identical newer watermark before rejecting an older different frame', () => {
    const blue = { red: 0, green: 0, blue: 120, brightness: 120, effect: 1, condition: 9 }
    const current = {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: true,
      status_led: blue, status_led_revision: 5, status_led_epoch: 1,
    }
    const identical = applyStatusLEDEvent(current, {
      id: 6, time: 'earlier-clock', kind: 'status_led.changed', text: 'same',
      metadata: { red: '0', green: '0', blue: '120', brightness: '120', effect: '1', condition: '9', revision: '6' },
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(identical.status_led_revision).toBe(6)
    const delayed = applyStatusLEDEvent(identical, {
      id: 5, time: 'later-clock', kind: 'status_led.changed', text: 'old',
      metadata: { red: '0', green: '0', blue: '18', brightness: '120', effect: '1', condition: '9', revision: '5' },
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(delayed).toBe(identical)
  })

  it('keeps latest-value semantics for same-epoch legacy revisionless events', () => {
    const current = {
      ...emptySnapshot, host_instance_id: 'legacy-primary', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 8, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 0, status_led_epoch: 2,
    }
    const first = applyStatusLEDEvent(current, {
      id: 1, time: 'one', kind: 'status_led.changed', text: 'first',
      metadata: { red: '0', green: '0', blue: '18', brightness: '145', effect: '1', condition: '9' },
    }, { epoch: 2, instanceID: 'legacy-primary' })
    const second = applyStatusLEDEvent(first, {
      id: 2, time: 'two', kind: 'status_led.changed', text: 'second',
      metadata: { red: '0', green: '0', blue: '36', brightness: '145', effect: '1', condition: '9' },
    }, { epoch: 2, instanceID: 'legacy-primary' })
    expect(first.status_led?.blue).toBe(18)
    expect(second).toMatchObject({ status_led: { blue: 36 }, status_led_revision: 0, status_led_epoch: 2 })
  })

  it('accepts a lower revision in a new transport epoch, including explicit off', () => {
    const current = {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 100, status_led_epoch: 1,
    }
    const restarted = applyStatusLEDEvent(current, {
      id: 1, time: 'clock-regressed', kind: 'status_led.changed', text: 'off',
      metadata: { red: '0', green: '0', blue: '0', brightness: '0', effect: '0', condition: '255', revision: '1' },
    }, { epoch: 2, instanceID: 'primary-b' })
    expect(restarted).toMatchObject({
      host_instance_id: 'primary-b', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 0, brightness: 0, effect: 0, condition: 255 },
      status_led_revision: 1, status_led_epoch: 2,
    })
  })

  it('treats the first acknowledged stream as newer than its startup snapshot', () => {
    const startup = {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 100, status_led_epoch: 0,
    }
    const restarted = applyStatusLEDEvent(startup, {
      id: 1, time: 'clock-regressed', kind: 'status_led.changed', text: 'off',
      metadata: { red: '0', green: '0', blue: '0', brightness: '0', effect: '0', condition: '255', revision: '1' },
    }, { epoch: 1, instanceID: 'primary-b' })
    expect(restarted).toMatchObject({
      host_instance_id: 'primary-b', status_led: { blue: 0 },
      status_led_revision: 1, status_led_epoch: 1,
    })
  })

  it('rejects stale stream callbacks and clears identity until a newer ACK', () => {
    const newerConnecting = advanceStatusLEDSource(
      { epoch: 1, instanceID: 'primary-a' },
      { epoch: 2 },
    )
    expect(newerConnecting).toEqual({ epoch: 2 })
    const newerOpen = advanceStatusLEDSource(newerConnecting!, {
      epoch: 2, instanceID: 'primary-b',
    })
    expect(newerOpen).toEqual({ epoch: 2, instanceID: 'primary-b' })
    expect(advanceStatusLEDSource(newerOpen!, {
      epoch: 1, instanceID: 'primary-a',
    })).toBeNull()
  })

  it('does not relabel retained old-primary state before new authority arrives', () => {
	const retained = {
	  ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: false,
	  status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
	  status_led_revision: 100, status_led_epoch: 1,
	}
	const restartedSnapshot = {
	  ...emptySnapshot, host_instance_id: 'primary-b', have_status_led: true,
	  status_led: { red: 0, green: 0, blue: 8, brightness: 145, effect: 1, condition: 9 },
	  status_led_revision: 1,
	}
	const merged = mergeStatusLEDSnapshot(retained, restartedSnapshot, {
	  epoch: 2, instanceID: 'primary-b', authoritativeInstanceID: 'primary-b',
	})
	expect(merged).toMatchObject({
	  host_instance_id: 'primary-b', have_status_led: true,
	  status_led: { blue: 8 }, status_led_revision: 1, status_led_epoch: 2,
	})
  })

  it('discards an in-flight old-primary snapshot after the acknowledged source changes', () => {
	expect(statusLEDSourceUnchanged(
	  { epoch: 1, instanceID: 'primary-a' },
	  { epoch: 2, instanceID: 'primary-b' },
	)).toBe(false)
	expect(statusLEDSourceUnchanged(
	  { epoch: 2, instanceID: 'primary-b' },
	  { epoch: 2, instanceID: 'primary-b' },
	)).toBe(true)
	expect(statusLEDSnapshotMatchesSource(
	  { host_instance_id: 'primary-a' },
	  { epoch: 2, instanceID: 'primary-b' },
	)).toBe(false)
  })

  it('advances a missing new-primary watermark before rejecting old-primary replay', () => {
    const retained = {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 145, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 100, status_led_epoch: 1,
    }
    const missingPrimaryB = mergeStatusLEDSnapshot(retained, {
      ...emptySnapshot, host_instance_id: 'primary-b', have_status_led: false,
      status_led_revision: 0,
    }, { epoch: 2, instanceID: 'primary-b', authoritativeInstanceID: 'primary-b' })
    expect(missingPrimaryB).toMatchObject({
      host_instance_id: 'primary-b', have_status_led: true,
      status_led: { blue: 145 }, status_led_revision: 0, status_led_epoch: 2,
    })

    const delayedPrimaryA = applyStatusLEDEvent(missingPrimaryB, {
      id: 101, time: 'later-clock', kind: 'status_led.changed', text: 'old primary',
      metadata: { red: '0', green: '0', blue: '18', brightness: '145', effect: '1', condition: '9', revision: '101' },
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(delayedPrimaryA).toBe(missingPrimaryB)

    const explicitOff = applyStatusLEDEvent(delayedPrimaryA, {
      id: 1, time: 'regressed-clock', kind: 'status_led.changed', text: 'new primary off',
      metadata: { red: '0', green: '0', blue: '0', brightness: '0', effect: '0', condition: '255', revision: '1' },
    }, { epoch: 2, instanceID: 'primary-b' })
    expect(explicitOff).toMatchObject({
      host_instance_id: 'primary-b', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 0, brightness: 0, effect: 0, condition: 255 },
      status_led_revision: 1, status_led_epoch: 2,
    })
  })

  it('orders snapshot and live paths while preserving missing LED state', () => {
    const current = {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: true,
      status_led: { red: 0, green: 0, blue: 100, brightness: 145, effect: 1, condition: 9 },
      status_led_revision: 11, status_led_epoch: 1,
    }
    const missing = mergeStatusLEDSnapshot(current, {
      ...emptySnapshot, host_instance_id: 'primary-a', have_status_led: false,
      status_led_revision: 12,
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(missing).toMatchObject({ have_status_led: true, status_led: current.status_led, status_led_revision: 11 })

    const stale = mergeStatusLEDSnapshot(current, {
      ...current,
      status_led: { ...current.status_led!, blue: 18 },
      status_led_revision: 10,
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(stale.status_led).toEqual(current.status_led)

    const newer = mergeStatusLEDSnapshot(current, {
      ...current,
      status_led: { ...current.status_led!, blue: 80 },
      status_led_updated: 'backward-clock',
      status_led_revision: 12,
    }, { epoch: 1, instanceID: 'primary-a' })
    expect(newer).toMatchObject({ status_led: { blue: 80 }, status_led_revision: 12, status_led_updated: 'backward-clock' })

	const delayedPrimaryA = mergeStatusLEDSnapshot({
	  ...current, host_instance_id: 'primary-b', status_led_epoch: 2,
	  status_led_revision: 2,
	}, {
	  ...current, host_instance_id: 'primary-a', status_led_revision: 99,
	}, { epoch: 2, instanceID: 'primary-a', authoritativeInstanceID: 'primary-b' })
	expect(delayedPrimaryA).toMatchObject({
	  host_instance_id: 'primary-b', status_led_epoch: 2, status_led_revision: 2,
	})
	const delayedIntoMissing = mergeStatusLEDSnapshot({
	  ...emptySnapshot, host_instance_id: 'primary-b', have_status_led: false,
	  status_led_epoch: 2, status_led_revision: 0,
	}, {
	  ...current, host_instance_id: 'primary-a', status_led_revision: 99,
	}, { epoch: 2, instanceID: 'primary-a', authoritativeInstanceID: 'primary-b' })
	expect(delayedIntoMissing).toMatchObject({
	  host_instance_id: 'primary-b', have_status_led: false,
	  status_led_epoch: 2, status_led_revision: 0,
	})

	const legitimatePrimaryB = mergeStatusLEDSnapshot({
	  ...current, host_instance_id: 'primary-a', status_led_epoch: 1,
	  status_led_revision: 100,
	}, {
	  ...current, host_instance_id: 'primary-b',
	  status_led: { ...current.status_led!, blue: 8 }, status_led_revision: 1,
	}, { epoch: 2, instanceID: 'primary-b', authoritativeInstanceID: 'primary-b' })
	expect(legitimatePrimaryB).toMatchObject({
	  host_instance_id: 'primary-b', status_led: { blue: 8 },
	  status_led_epoch: 2, status_led_revision: 1,
	})
  })
})

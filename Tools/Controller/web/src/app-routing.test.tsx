import { describe, expect, it } from 'vitest'
import {
  canonicalPageHash,
  canonicalPageURL,
  connectionTransitionCue,
  metricSamplesAfterSnapshot,
  controllerConnectionLabel,
  isCompletedHostUpdate,
  navigation,
  normalizeAppearance,
  pageFromHash,
  pageViewFor,
  shouldOpenSetup,
	shouldNavigateToUpdates,
  snapshotAfterTransportLoss,
  transportReconnectAvailable,
} from './app'
import { pageOrder } from './hotkeys'
import { emptySnapshot } from './types'

describe('web page routing', () => {
  it('maps every navigation ID to exactly one intended domain view', () => {
    expect(navigation.map((item) => item.id)).toEqual([...pageOrder])
    expect(new Set(navigation.map((item) => item.id)).size).toBe(pageOrder.length)
    expect(new Set(navigation.map((item) => item.view)).size).toBe(pageOrder.length)
    for (const page of pageOrder) {
      expect(pageViewFor(page), page).toBe(navigation.find((item) => item.id === page)?.view)
    }
  })

  it('normalizes initial hashes and produces stable history destinations', () => {
    expect(pageFromHash('#/settings')).toBe('settings')
    expect(pageFromHash('#settings')).toBe('settings')
    expect(pageFromHash('#/settings/appearance')).toBe('settings')
    expect(pageFromHash('#/not-a-page')).toBe('dashboard')
    expect(pageFromHash('')).toBe('dashboard')
    expect(canonicalPageHash('events')).toBe('#/events')
    expect(canonicalPageURL('events', '/control', '?demo=1')).toBe('/control?demo=1#/events')
  })
})

describe('transport truth', () => {
  it('invalidates a previously connected snapshot as soon as the host stream is lost', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      connection_state: 'connected',
      connection_reason: '',
    }
    const waiting = snapshotAfterTransportLoss(connected, 'waiting', 'retrying')
    expect(waiting.connected).toBe(false)
    expect(waiting.connection_state).toBe('disconnected')
    expect(waiting.connection_reason).toBe('retrying')
    expect(waiting.have_status).toBe(false)
    expect(waiting.hello).toEqual({})
    expect(waiting.status).toEqual(emptySnapshot.status)
    expect(snapshotAfterTransportLoss(connected, 'connecting').connected).toBe(false)
  })

  it('replaces telemetry with the newly advertised peer identity after reconnect', () => {
    const first = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      port: { name: 'COM4', serial_number: 'board-a' },
      hello: { capabilities: 1 << 0, build_hash: 0x11111111 },
      status: { ...emptySnapshot.status, supply_mv: 12_000, flags: 1 << 0 },
    }
    const second = {
      ...first,
      port: { name: 'COM8', serial_number: 'board-b' },
      hello: { capabilities: 1 << 1, build_hash: 0x22222222 },
      status: { ...emptySnapshot.status, temperature_led_centi_c: 3250, flags: 1 << 2 },
    }
    const samples = metricSamplesAfterSnapshot(
      [{ at: 1, supply: 12 }], first, second, 2,
    )
    expect(samples).toEqual([{ at: 2, ledTemp: 32.5 }])
  })

  it('offers immediate reconnect only while transport is genuinely disconnected', () => {
    expect(transportReconnectAvailable('waiting')).toBe(true)
    expect(transportReconnectAvailable('closed')).toBe(true)
    expect(transportReconnectAvailable('connecting')).toBe(false)
    expect(transportReconnectAvailable('open')).toBe(false)
    expect(transportReconnectAvailable('waiting', true)).toBe(false)
  })

  it('uses factual no-board state and never claims the alpha host is ready', () => {
    const label = controllerConnectionLabel(
      { connected: false, connection_state: 'disconnected' },
      'open',
      'unavailable',
      'en',
    )
    expect(label).toBe('No controller')
    expect(label).not.toBe('Host ready')
  })

  it('refreshes embedded resources only after a completed host replacement', () => {
    expect(isCompletedHostUpdate({ kind: 'update.completed', metadata: { kind: 'host' } })).toBe(true)
    expect(isCompletedHostUpdate({ kind: 'update.completed', metadata: { kind: 'firmware' } })).toBe(false)
    expect(isCompletedHostUpdate({ kind: 'update.failed', metadata: { kind: 'host' } })).toBe(false)
  })

	it('opens the updates page only for fresh pushed update lifecycle events', () => {
		const now = Date.parse('2026-08-03T00:00:10.000Z')
		expect(shouldNavigateToUpdates({ kind: 'update.programming', time: '2026-08-03T00:00:09.000Z' }, 'dashboard', now)).toBe(true)
		expect(shouldNavigateToUpdates({ kind: 'update.programming', time: '2026-08-03T00:00:09.000Z' }, 'updates', now)).toBe(false)
		expect(shouldNavigateToUpdates({ kind: 'update.completed', time: '2026-08-02T23:59:00.000Z' }, 'dashboard', now)).toBe(false)
		expect(shouldNavigateToUpdates({ kind: 'status', time: '2026-08-03T00:00:09.000Z' }, 'dashboard', now)).toBe(false)
	})

  it('keeps initial connection truth silent and cues only real later transitions', () => {
    expect(connectionTransitionCue(null, true)).toBeNull()
    expect(connectionTransitionCue(false, false)).toBeNull()
    expect(connectionTransitionCue(false, true)).toBe('connect')
    expect(connectionTransitionCue(true, false)).toBe('disconnect')
    expect(connectionTransitionCue(false, true, false)).toBeNull()
    expect(connectionTransitionCue(false, true, true, true)).toBeNull()
  })
})

describe('first-run setup selection', () => {
  it('uses the required current-host setup state or demo preview', () => {
    expect(shouldOpenSetup({ setup_complete: false })).toBe(true)
    expect(shouldOpenSetup({ setup_complete: true })).toBe(false)
    expect(shouldOpenSetup(null)).toBe(false)
    expect(shouldOpenSetup({ setup_complete: true }, true)).toBe(true)
  })
})

describe('host-owned appearance normalization', () => {
  it('preserves explicit false and zero while rejecting malformed cached enums', () => {
    expect(normalizeAppearance({
      theme: 'dark', locale: 'fa', direction: 'rtl', reduceMotion: false,
      compactNumbers: false, audioMuted: false, audioVolume: 0,
    })).toEqual({
      theme: 'dark', locale: 'fa', direction: 'rtl', reduceMotion: false,
      compactNumbers: false, audioMuted: false, audioVolume: 0,
    })
    expect(normalizeAppearance({
      theme: 'neon' as 'dark', locale: 'de' as 'en', direction: 'sideways' as 'ltr', audioVolume: Number.NaN,
    })).toMatchObject({ theme: 'system', locale: 'en', direction: 'auto', audioVolume: 0.42 })
  })
})

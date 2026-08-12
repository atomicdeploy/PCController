import { describe, expect, it } from 'vitest'
import {
  canonicalPageHash,
  canonicalLocationHash,
  canonicalPageURL,
  connectionTransitionCue,
  isCompletedHostUpdate,
  navigation,
  normalizeAppearance,
  pageFromHash,
  pageViewFor,
  shouldOpenSetup,
	shouldNavigateToUpdates,
  snapshotAfterTransportLoss,
  snapshotAfterStatusRecovery,
} from './app'
import { pageOrder } from './hotkeys'
import { emptySnapshot } from './types'

describe('web page routing', () => {
  it('maps every navigation ID to exactly one intended domain view', () => {
    const routedPages = pageOrder.filter((page) => page !== 'settings')
    expect(navigation.map((item) => item.id)).toEqual(routedPages)
    expect(new Set(navigation.map((item) => item.id)).size).toBe(routedPages.length)
    expect(new Set(navigation.map((item) => item.view)).size).toBe(routedPages.length)
    for (const page of routedPages) {
      expect(pageViewFor(page), page).toBe(navigation.find((item) => item.id === page)?.view)
    }
  })

  it('preserves settings hashes as modal destinations while rejecting unknown pages', () => {
    expect(pageFromHash('#/settings')).toBe('settings')
    expect(pageFromHash('#settings')).toBe('settings')
    expect(pageFromHash('#/settings/appearance')).toBe('settings')
    expect(pageFromHash('#/not-a-page')).toBe('dashboard')
    expect(pageFromHash('')).toBe('dashboard')
    expect(canonicalPageHash('events')).toBe('#/events')
    expect(canonicalPageURL('events', '/control', '?demo=1')).toBe('/control?demo=1#/events')
    expect(canonicalLocationHash('#/workbench/sensors/temperature')).toBe('#/workbench/sensors/temperature')
    expect(canonicalLocationHash('#workbench/interface/audio')).toBe('#/workbench/interface/audio')
    expect(canonicalLocationHash('#/workbench/not-real')).toBe('#/workbench')
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
    expect(snapshotAfterTransportLoss(connected, 'connecting').connected).toBe(false)
  })

  it('restores coherent connection truth on the first valid status after an error', () => {
    const lost = snapshotAfterTransportLoss({
      ...emptySnapshot, connected: true, connection_state: 'connected', have_settings: true,
    }, 'waiting', 'controller disconnected')
    const recovered = snapshotAfterStatusRecovery(lost, { ...emptySnapshot.status, supply_mv: 12_000 }, '2026-08-12T12:00:00Z')
    expect(recovered).toMatchObject({
      connected: true, connection_state: 'connected', connection_reason: '', have_status: true,
      status: { supply_mv: 12_000 },
    })
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

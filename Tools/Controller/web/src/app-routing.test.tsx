import { describe, expect, it } from 'vitest'
import {
  canonicalPageHash,
  canonicalPageURL,
  connectionTransitionCue,
  isCompletedHostUpdate,
  navigation,
  normalizeAppearance,
  pageFromHash,
  pageViewFor,
  shouldOpenSetup,
  snapshotAfterTransportLoss,
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
    expect(snapshotAfterTransportLoss(connected, 'connecting').connected).toBe(false)
  })

  it('refreshes embedded resources only after a completed host replacement', () => {
    expect(isCompletedHostUpdate({ kind: 'update.completed', metadata: { kind: 'host' } })).toBe(true)
    expect(isCompletedHostUpdate({ kind: 'update.completed', metadata: { kind: 'firmware' } })).toBe(false)
    expect(isCompletedHostUpdate({ kind: 'update.failed', metadata: { kind: 'host' } })).toBe(false)
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

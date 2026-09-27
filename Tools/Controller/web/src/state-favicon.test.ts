import { describe, expect, it } from 'vitest'
import { controllerFaviconDataURL, controllerFaviconState } from './state-favicon'
import { emptySnapshot } from './types'

describe('dynamic controller favicon', () => {
  it('derives connection state from the board snapshot rather than host transport', () => {
    expect(controllerFaviconState(emptySnapshot)).toBe('offline')
    expect(controllerFaviconState({ ...emptySnapshot, connection_state: 'reconnecting' })).toBe('connecting')
    expect(controllerFaviconState({ ...emptySnapshot, connected: true, connection_state: 'connected' })).toBe('connected')
    expect(controllerFaviconState({ ...emptySnapshot, connected: true, have_status: true, status: { ...emptySnapshot.status, hot: true } })).toBe('fault')
    expect(controllerFaviconState({ ...emptySnapshot, connection_reason: 'authentication rejected' })).toBe('fault')
  })

	it('keeps the shared real product mark and adds only a compact state dot', () => {
    const url = controllerFaviconDataURL('offline')
    expect(url.startsWith('data:image/svg+xml,')).toBe(true)
    const svg = decodeURIComponent(url.slice(url.indexOf(',') + 1))
    expect(svg).toContain('Controller offline')
		expect(svg).toContain('linearGradient id="mark"')
		expect(svg).toContain('<circle cx="51" cy="51"')
		expect(svg).not.toContain('M18 17h18')
  })
})

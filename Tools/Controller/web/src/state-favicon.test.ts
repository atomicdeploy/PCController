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

  it('keeps the real icon fallback and supplies a compact neutral-violet state SVG', () => {
    const url = controllerFaviconDataURL('offline')
    expect(url.startsWith('data:image/svg+xml,')).toBe(true)
    const svg = decodeURIComponent(url.slice(url.indexOf(',') + 1))
    expect(svg).toContain('Controller offline')
    expect(svg).toContain('#8b6de0')
    expect(svg).not.toMatch(/grid|radialGradient|#00ffff|cyan|teal/i)
  })
})

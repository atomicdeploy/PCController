import { afterEach, describe, expect, it, vi } from 'vitest'
import { getSnapshot, getUIConfig, HTTPResponseError, responseErrorDetail, streamFailureState, transportFailureDetail } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('API error details', () => {
  it('extracts nested JSON-RPC messages returned with an HTTP error status', () => {
    expect(responseErrorDetail({
      jsonrpc: '2.0',
      error: { code: -32000, message: 'unknown command "sample"' },
    }, 'Bad Request', 400)).toBe('unknown command "sample"')
  })

  it('retains text and HTTP fallbacks', () => {
    expect(responseErrorDetail('not allowed', 'Forbidden', 403)).toBe('not allowed')
    expect(responseErrorDetail(null, 'Not Found', 404)).toBe('Not Found')
  })

  it('preserves an HTTP authentication challenge as a distinct transport state', async () => {
    vi.stubGlobal('location', new URL('http://server:8787/'))
    vi.stubGlobal('sessionStorage', {
      getItem: () => null,
      setItem: () => undefined,
      removeItem: () => undefined,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: 'authentication required' }),
      { status: 401, statusText: 'Unauthorized' },
    )))

    let failure: unknown
    try { await getSnapshot() } catch (cause) { failure = cause }

    expect(streamFailureState(failure)).toBe('authentication-required')
    expect(transportFailureDetail(failure)).toBe('HTTP 401 · authentication required')
  })

  it('recognizes the host authentication-specific 403 without hiding unrelated denial', () => {
    const challenge = new Response(
      JSON.stringify({ error: 'remote requests without Origin require an Authorization or X-PCController-Token header' }),
      { status: 403, statusText: 'Forbidden' },
    )
    const denied = new Response(JSON.stringify({ error: 'request Origin is not allowed' }), { status: 403, statusText: 'Forbidden' })

    expect(streamFailureState(new HTTPResponseError(
      'remote requests without Origin require an Authorization or X-PCController-Token header',
      challenge.status,
    ))).toBe('authentication-required')
    expect(streamFailureState(new HTTPResponseError('request Origin is not allowed', denied.status))).toBe('waiting')
  })

  it('requires the current setup contract while tolerating future fields', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ name: 'Controller' }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        name: 'Controller', setup_complete: true, websocket_path: '/ipc', session_ticket_path: '/api/session/ticket', auth_required: false,
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        name: 'Controller', setup_complete: true, websocket_path: '/ipc', auth_required: true,
        appearance: { theme: 'dark', locale: 'fa', direction: 'rtl', reduceMotion: true, compactNumbers: false, audioMuted: true, audioVolume: 0 },
        appearance_etag: 'b'.repeat(64),
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        name: 'Controller',
        setup_complete: true,
        websocket_path: '/ipc',
        session_ticket_path: '/api/session/ticket',
        auth_required: true,
        appearance: { theme: 'dark', locale: 'fa', direction: 'rtl', reduceMotion: true, compactNumbers: false, audioMuted: true, audioVolume: 0 },
        appearance_etag: 'b'.repeat(64),
        reset_on_reconnect: false,
        future_capability: 'accepted',
      }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getUIConfig()).rejects.toThrow('missing required setup_complete')
    await expect(getUIConfig()).rejects.toThrow('missing host-authoritative appearance')
    await expect(getUIConfig()).rejects.toThrow('missing a safe session-ticket path')
    await expect(getUIConfig()).resolves.toMatchObject({ setup_complete: true, future_capability: 'accepted' })
  })
})

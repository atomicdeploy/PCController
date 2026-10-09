import { afterEach, describe, expect, it, vi } from 'vitest'
import { getUIConfig, responseErrorDetail } from './api'

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

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
        name: 'Controller',
        setup_complete: true,
        api_version: 1,
        websocket_path: '/ipc',
        auth_required: true,
        reset_on_reconnect: false,
        future_capability: 'accepted',
      }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getUIConfig()).rejects.toThrow('missing required setup_complete')
    await expect(getUIConfig()).resolves.toMatchObject({ setup_complete: true, future_capability: 'accepted' })
  })
})

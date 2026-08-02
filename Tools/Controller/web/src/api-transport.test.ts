import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectStream, downloadIntegration, getUIConfig, rpc } from './api'

type Listener = (event: any) => void

class FakeWebSocket {
  static readonly OPEN = 1
  readonly sent: string[] = []
  readyState = 0
  private readonly listeners = new Map<string, Listener[]>()

  constructor(readonly url: string, readonly protocols?: string | string[]) {
    queueMicrotask(() => {
      this.readyState = FakeWebSocket.OPEN
      this.emit('open', {})
    })
  }

  addEventListener(type: string, listener: Listener): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener])
  }

  send(raw: string): void {
    this.sent.push(raw)
    const request = JSON.parse(raw) as { id: number; method: string }
		if (request.method === 'controller.command.execute') {
      queueMicrotask(() => this.emit('message', {
        data: JSON.stringify({ jsonrpc: '2.0', id: request.id, result: { output: 'ws-ok' } }),
      }))
    }
  }

  close(): void {
    if (this.readyState === 0) return
    this.readyState = 0
    this.emit('close', { reason: 'closed by test' })
  }

  private emit(type: string, event: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }
}

function ticketResponse(ticket = 'b'.repeat(64)): Response {
  return new Response(JSON.stringify({
    ticket,
    protocol: 'pccontroller.v1',
    expires_at: new Date(Date.now() + 15_000).toISOString(),
    expires_in_ms: 15_000,
    principal: 'controller-operator',
    correlation_id: 'ws-test-correlation',
  }), { status: 201 })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('Web IPC transport', () => {
  it('correlates RPC responses over the already-open event WebSocket', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:18887' })
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const fetchSpy = vi.fn().mockResolvedValue(ticketResponse())
    vi.stubGlobal('fetch', fetchSpy)

    const stop = connectStream({
      name: 'PCController', setup_complete: false, api_version: 1, websocket_path: '/ipc', auth_required: false,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await vi.waitFor(() => expect(sockets).toHaveLength(1))

		await expect(rpc<{ output: string }>('controller.command.execute', { command: 'status' })).resolves.toEqual({ output: 'ws-ok' })
    expect(fetchSpy).toHaveBeenCalledOnce()
    expect(fetchSpy.mock.calls[0]?.[0]).toBe('/api/v1/session/ticket')
    expect(sockets).toHaveLength(1)
		expect(sockets[0].url).toBe('ws://127.0.0.1:18887/ipc')
		expect(sockets[0].url).not.toContain('access_token')
		expect(sockets[0].protocols).toEqual(['pccontroller.v1', `pccontroller.ticket.${'b'.repeat(64)}`])
		expect(sockets[0].sent.map((raw) => JSON.parse(raw).method)).toEqual(['controller.subscribe', 'controller.command.execute'])
    stop()
  })

  it('uses one validated external host for canonical REST and WebSocket paths', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', new URL('http://localhost:4177/?controller=http%3A%2F%2F127.0.0.1%3A8787'))
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const fetchSpy = vi.fn().mockImplementation((target: string) => {
      if (target.endsWith('/api/v1/ui-config')) {
        return Promise.resolve(new Response(JSON.stringify({
          name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: false,
          appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
          appearance_etag: 'a'.repeat(64),
        }), { status: 200 }))
      }
      return Promise.resolve(ticketResponse())
    })
    vi.stubGlobal('fetch', fetchSpy)

    await getUIConfig()
    expect(fetchSpy).toHaveBeenCalledWith('http://127.0.0.1:8787/api/v1/ui-config', expect.anything())

    const stop = connectStream({
      name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: false,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await vi.waitFor(() => expect(sockets).toHaveLength(1))
    expect(String(sockets[0]?.url)).toBe('ws://127.0.0.1:8787/ipc')
    expect(String(sockets[0]?.url)).not.toContain('/api/v1/')
    expect(fetchSpy.mock.calls[1]?.[0]).toBe('http://127.0.0.1:8787/api/v1/session/ticket')
    stop()
  })

  it('fetches a distinct one-use ticket for every reconnect without storing it', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:8787' })
    const storage = { getItem: vi.fn(() => 'durable-session-token'), setItem: vi.fn(), removeItem: vi.fn() }
    vi.stubGlobal('sessionStorage', storage)
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    vi.spyOn(Math, 'random').mockReturnValue(0)
    let issuance = 0
    const fetchSpy = vi.fn().mockImplementation(() => {
      issuance += 1
      return Promise.resolve(ticketResponse((issuance === 1 ? 'c' : 'd').repeat(64)))
    })
    vi.stubGlobal('fetch', fetchSpy)

    const stop = connectStream({
      name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: true,
      session_ticket_path: '/api/v1/session/ticket',
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await vi.waitFor(() => expect(sockets).toHaveLength(1))
    sockets[0].close()
    await vi.waitFor(() => expect(sockets).toHaveLength(2), { timeout: 2_000 })

    expect(fetchSpy).toHaveBeenCalledTimes(2)
    expect(sockets[0].protocols).toEqual(['pccontroller.v1', `pccontroller.ticket.${'c'.repeat(64)}`])
    expect(sockets[1].protocols).toEqual(['pccontroller.v1', `pccontroller.ticket.${'d'.repeat(64)}`])
    expect(sockets.every((socket) => !socket.url.includes('?'))).toBe(true)
    expect(storage.setItem).not.toHaveBeenCalled()
    const [, firstInit] = fetchSpy.mock.calls[0] as [string, RequestInit]
    expect(firstInit.headers).toMatchObject({ Authorization: 'Bearer durable-session-token' })
    stop()
  })

  it('keeps RPC on authenticated REST when ticket issuance is unavailable', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:8787' })
    vi.stubGlobal('sessionStorage', { getItem: () => 'session-secret', setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const states: Array<[string, string | undefined]> = []
    const fetchSpy = vi.fn().mockImplementation((target: string) => {
      if (target.endsWith('/api/v1/session/ticket')) {
        return Promise.resolve(new Response(JSON.stringify({ error: 'ticket service unavailable' }), { status: 503 }))
      }
      if (target.endsWith('/api/v1/rpc')) {
        return Promise.resolve(new Response(JSON.stringify({ jsonrpc: '2.0', id: 1, result: { output: 'rest-ok' } }), { status: 200 }))
      }
      throw new Error(`unexpected request ${target}`)
    })
    vi.stubGlobal('fetch', fetchSpy)

    const stop = connectStream({
      name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: true,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, {
      status: () => undefined,
      event: () => undefined,
      state: (state, detail) => states.push([state, detail]),
    })
    await vi.waitFor(() => expect(states.some(([state]) => state === 'waiting')).toBe(true))
    await expect(rpc<{ output: string }>('controller.command.execute', { command: 'status' })).resolves.toEqual({ output: 'rest-ok' })
    expect(sockets).toHaveLength(0)
    expect(states.at(-1)?.[1]).toContain('REST commands remain active')
    const rpcCall = fetchSpy.mock.calls.find(([target]) => String(target).endsWith('/api/v1/rpc')) as [string, RequestInit]
    expect(rpcCall[1].headers).toMatchObject({ Authorization: 'Bearer session-secret' })
    stop()
  })

  it('keeps download authorization in a header instead of a portable URL', async () => {
    vi.stubGlobal('location', new URL('http://localhost:4177/?controller=http%3A%2F%2F127.0.0.1%3A8787'))
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('sessionStorage', { getItem: () => 'session-secret', setItem: () => undefined, removeItem: () => undefined })
    const link = { href: '', download: '', rel: '', click: vi.fn(), remove: vi.fn() }
    vi.stubGlobal('document', { createElement: () => link, body: { append: vi.fn() } })
    const fetchSpy = vi.fn().mockResolvedValue(new Response('download body', {
      status: 200,
      headers: { 'Content-Disposition': 'attachment; filename="status.json"' },
    }))
    vi.stubGlobal('fetch', fetchSpy)

    await downloadIntegration('datahub', 'v1/status')
    const [target, init] = fetchSpy.mock.calls[0] as [string, RequestInit]
    expect(target).toBe('http://127.0.0.1:8787/api/v1/integrations/datahub/v1/status')
    expect(target).not.toContain('session-secret')
    expect(init.headers).toMatchObject({ Authorization: 'Bearer session-secret' })
    expect(link.download).toBe('status.json')
    expect(link.click).toHaveBeenCalledOnce()
  })
})

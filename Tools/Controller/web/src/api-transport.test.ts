import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectStream, downloadIntegration, getUIConfig, rpc, streamRetryDelay } from './api'

type Listener = (event: any) => void

class FakeWebSocket {
  static readonly OPEN = 1
  readonly sent: string[] = []
  readyState = 0
  private readonly listeners = new Map<string, Listener[]>()

  constructor(readonly url: string, readonly protocols: string | string[] = []) {
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

  pushMessage(value: unknown): void {
    this.emit('message', { data: JSON.stringify(value) })
  }

  private emit(type: string, event: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('Web IPC transport', () => {
  it('uses bounded default exponential reconnect backoff with jitter', () => {
    expect(streamRetryDelay(1, () => 0)).toBe(1_000)
    expect(streamRetryDelay(2, () => 0)).toBe(2_000)
    expect(streamRetryDelay(4, () => 0.5)).toBe(8_125)
    expect(streamRetryDelay(5, () => 0.999)).toBe(12_000)
    expect(streamRetryDelay(100, () => 0.999)).toBe(12_000)
  })

  it('correlates RPC responses over the already-open event WebSocket', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string) { super(url); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:18887' })
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const events: Array<{ kind: string; stream?: string }> = []
    const stop = connectStream({
      name: 'PCController', setup_complete: false, websocket_path: '/ipc', session_ticket_path: '/api/session/ticket', auth_required: false,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: (value) => events.push(value), state: () => undefined })
    await new Promise((resolve) => setTimeout(resolve, 0))

    await expect(rpc<{ output: string }>('controller.command.execute', { command: 'status' })).resolves.toEqual({ output: 'ws-ok' })
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(sockets).toHaveLength(1)
    expect(sockets[0].sent.map((raw) => JSON.parse(raw).method)).toEqual(['controller.subscribe', 'controller.command.execute'])
    expect(JSON.parse(sockets[0].sent[0]).params.topics).toEqual(['events', 'state', 'status'])
    sockets[0].pushMessage({
      jsonrpc: '2.0',
      method: 'controller.state',
      params: { id: 7, kind: 'status_led.changed', stream: 'state', text: '#12AB34', time: '2026-08-03T00:00:00Z' },
    })
    expect(events).toEqual([{ id: 7, kind: 'status_led.changed', stream: 'state', text: '#12AB34', time: '2026-08-03T00:00:00Z' }])
    stop()
  })

  it('uses one validated external host for canonical REST and WebSocket paths', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string) { super(url); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', new URL('http://localhost:4177/?controller=http%3A%2F%2F127.0.0.1%3A8787'))
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const fetchSpy = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      name: 'PCController', setup_complete: true, websocket_path: '/ipc', session_ticket_path: '/api/session/ticket', auth_required: false,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchSpy)

    await getUIConfig()
    expect(fetchSpy).toHaveBeenCalledWith('http://127.0.0.1:8787/api/ui-config', expect.anything())

    const stop = connectStream({
      name: 'PCController', setup_complete: true, websocket_path: '/ipc', session_ticket_path: '/api/session/ticket', auth_required: false,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(String(sockets[0]?.url)).toBe('ws://127.0.0.1:8787/ipc')
    expect(String(sockets[0]?.url)).not.toContain('/api/')
    stop()
  })

  it('does not request a dormant ticket when an alpha host reports auth disabled', async () => {
	const sockets: FakeWebSocket[] = []
	class CapturingSocket extends FakeWebSocket {
		constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
	}
	Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
	vi.stubGlobal('WebSocket', CapturingSocket)
	vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:18887' })
	vi.stubGlobal('sessionStorage', { getItem: () => 'stale-old-host-token', setItem: () => undefined, removeItem: () => undefined })
	vi.stubGlobal('window', { setTimeout, clearTimeout })
	const fetchSpy = vi.fn()
	vi.stubGlobal('fetch', fetchSpy)
	const stop = connectStream({
		name: 'PCController', setup_complete: true, websocket_path: '/ipc',
		session_ticket_path: '/api/session/ticket', auth_required: false,
		appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
		appearance_etag: 'a'.repeat(64),
	}, { status: () => undefined, event: () => undefined, state: () => undefined })
	await new Promise((resolve) => setTimeout(resolve, 0))
	expect(fetchSpy).not.toHaveBeenCalled()
	expect(sockets[0]?.protocols).toEqual([])
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
    expect(target).toBe('http://127.0.0.1:8787/api/integrations/datahub/v1/status')
    expect(target).not.toContain('session-secret')
    expect(init.headers).toMatchObject({ Authorization: 'Bearer session-secret' })
    expect(link.download).toBe('status.json')
    expect(link.click).toHaveBeenCalledOnce()
  })

  it('exchanges the durable header credential for a one-use WebSocket subprotocol ticket', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:18887' })
    vi.stubGlobal('sessionStorage', { getItem: () => 'durable-session-secret', setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    const ticket = 'a'.repeat(64)
    const fetchSpy = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ticket,
      protocol: 'pccontroller',
      expires_at: '2026-08-02T12:00:00Z',
      expires_in_ms: 15_000,
      principal: 'remote-operator',
    }), { status: 201 }))
    vi.stubGlobal('fetch', fetchSpy)

    const stop = connectStream({
      name: 'PCController', setup_complete: true, websocket_path: '/ipc',
      session_ticket_path: '/api/session/ticket', auth_required: true,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(fetchSpy).toHaveBeenCalledWith('/api/session/ticket', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ Authorization: 'Bearer durable-session-secret' }),
      body: JSON.stringify({ transport: 'websocket' }),
    }))
    expect(sockets).toHaveLength(1)
    expect(String(sockets[0].url)).toBe('ws://127.0.0.1:18887/ipc')
    expect(String(sockets[0].url)).not.toContain('durable-session-secret')
    expect(sockets[0].protocols).toEqual(['pccontroller', `pccontroller.ticket.${ticket}`])
    stop()
  })

  it('rejects an unexpected ticket protocol before opening a socket', async () => {
    const sockets: FakeWebSocket[] = []
    class CapturingSocket extends FakeWebSocket {
      constructor(url: string, protocols?: string | string[]) { super(url, protocols); sockets.push(this) }
    }
    Object.defineProperty(CapturingSocket, 'OPEN', { value: 1 })
    vi.stubGlobal('WebSocket', CapturingSocket)
    vi.stubGlobal('location', { protocol: 'http:', host: '127.0.0.1:18887' })
    vi.stubGlobal('sessionStorage', { getItem: () => 'durable-session-secret', setItem: () => undefined, removeItem: () => undefined })
    vi.stubGlobal('window', { setTimeout, clearTimeout })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ticket: 'a'.repeat(64),
      protocol: 'unexpected.v1',
      expires_at: '2026-08-02T12:00:00Z',
      expires_in_ms: 15_000,
      principal: 'remote-operator',
    }), { status: 201 })))
    const states: string[] = []

    const stop = connectStream({
      name: 'PCController', setup_complete: true, websocket_path: '/ipc',
      session_ticket_path: '/api/session/ticket', auth_required: true,
      appearance: { theme: 'system', locale: 'en', direction: 'auto', reduceMotion: false, compactNumbers: false, audioMuted: false, audioVolume: 0.42 },
      appearance_etag: 'a'.repeat(64),
    }, { status: () => undefined, event: () => undefined, state: (state, detail) => states.push(`${state}:${detail ?? ''}`) })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(sockets).toHaveLength(0)
    expect(states.some((value) => value.includes('invalid WebSocket session ticket'))).toBe(true)
    stop()
  })
})

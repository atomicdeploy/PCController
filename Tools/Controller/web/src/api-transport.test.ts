import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectStream, downloadIntegration, getUIConfig, rpc } from './api'

type Listener = (event: any) => void

class FakeWebSocket {
  static readonly OPEN = 1
  readonly sent: string[] = []
  readyState = 0
  private readonly listeners = new Map<string, Listener[]>()

  constructor(readonly url: string) {
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

afterEach(() => vi.unstubAllGlobals())

describe('Web IPC transport', () => {
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

    const stop = connectStream({
      name: 'PCController', setup_complete: false, api_version: 1, websocket_path: '/ipc', auth_required: false,
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await new Promise((resolve) => setTimeout(resolve, 0))

		await expect(rpc<{ output: string }>('controller.command.execute', { command: 'status' })).resolves.toEqual({ output: 'ws-ok' })
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(sockets).toHaveLength(1)
		expect(sockets[0].sent.map((raw) => JSON.parse(raw).method)).toEqual(['controller.subscribe', 'controller.command.execute'])
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
      name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: false,
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchSpy)

    await getUIConfig()
    expect(fetchSpy).toHaveBeenCalledWith('http://127.0.0.1:8787/api/v1/ui-config', expect.anything())

    const stop = connectStream({
      name: 'PCController', setup_complete: true, api_version: 1, websocket_path: '/ipc', auth_required: false,
    }, { status: () => undefined, event: () => undefined, state: () => undefined })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(String(sockets[0]?.url)).toBe('ws://127.0.0.1:8787/ipc')
    expect(String(sockets[0]?.url)).not.toContain('/api/v1/')
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

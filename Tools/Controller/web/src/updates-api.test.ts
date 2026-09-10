import { afterEach, describe, expect, it, vi } from 'vitest'
import { ControllerRPCError } from './api'
import { adoptPeerHostUpdateIntent, compareBuildIdentity, peerHostUpdateIdempotencyKey, sha256File, startFlashRestore, startPeerHostUpdate, uploadArtifact } from './updates-api'

afterEach(() => vi.restoreAllMocks())

describe('firmware artifact adapter', () => {
  it('computes the digest before an artifact is staged', async () => {
    const file = new Blob(['controller-image'])
    expect(await sha256File(file)).toBe('3abac84d0fbac67d5a4e1abd07d22a4c73841c61ef523da81bfc04844c3cbc40')
  })

  it('stages a browser selection without starting programming', async () => {
    Object.defineProperty(globalThis, 'location', { configurable: true, value: new URL('http://127.0.0.1:8787/') })
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
    })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      operation: { id: 'stage-1', kind: 'download', state: 'downloaded', progress_percent: 100 },
    }), { status: 200 }))
    const file = new File(['hex'], 'board.hex', { type: 'application/octet-stream' })
    await uploadArtifact(file, 'firmware', 'a'.repeat(64))
    const [url, init] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/api/artifacts/upload?')
    expect(String(url)).toContain('kind=firmware')
    expect(String(url)).toContain(`sha256=${'a'.repeat(64)}`)
    expect(String(url)).toContain('bytes=3')
    expect(init?.method).toBe('POST')
    expect(init?.body).toBeInstanceOf(FormData)
    expect(String(url)).not.toContain('/updates/firmware')
  })

  it('compares compact build timestamps only after hash equality is checked', () => {
    const current = { sha256: 'a'.repeat(64), build_timestamp: '35019D5C' }
    expect(compareBuildIdentity(current, { sha256: 'a'.repeat(64), build_timestamp: 'FFFFFFFF' })).toBe('same')
    expect(compareBuildIdentity(current, { sha256: 'b'.repeat(64), build_timestamp: '35019D5D' })).toBe('newer')
    expect(compareBuildIdentity(current, { sha256: 'c'.repeat(64), build_timestamp: '35019D5B' })).toBe('older')
    expect(compareBuildIdentity(current, { sha256: 'd'.repeat(64) })).toBe('different')
    expect(compareBuildIdentity(current, { sha256: 'e'.repeat(64), build_timestamp: '35019D5C' })).toBe('different')
  })

  it('restores flash readbacks through the dedicated RPC contract', async () => {
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
    })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const request = JSON.parse(String(init?.body)) as { id: number }
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        result: { operation: { id: 'restore-1', kind: 'flash-restore', state: 'queued', progress_percent: 0 } },
      }), { status: 200 })
    })
    await startFlashRestore({ artifact_sha256: 'b'.repeat(64), authorized: true, method: 'urclock', port: 'COM18' })
    const [, init] = fetchMock.mock.calls[0]
    const request = JSON.parse(String(init?.body)) as { method: string; params: Record<string, unknown> }
    expect(request.method).toBe('controller.restore.flash')
    expect(request.method).not.toBe('controller.update.firmware')
    expect(request.params).toMatchObject({ authorized: true, method: 'urclock', port: 'COM18' })
  })

  it('retains one intent key across transport uncertainty and rotates after known success', async () => {
    const stored = new Map<string, string>()
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
        removeItem: (key: string) => stored.delete(key),
      },
    })
    const digest = 'c'.repeat(64)
    let call = 0
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      call += 1
      if (call === 1) throw new TypeError('connection closed before response')
      const request = JSON.parse(String(init?.body)) as { id: number }
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        result: {
          peer: 'edge', stage: 'remote-queued', terminal_verified: false,
          artifact: { kind: 'host-executable', sha256: digest },
          operation: { id: `remote-${call}`, kind: 'host', state: 'queued', progress_percent: 0 },
        },
      }), { status: 200 })
    })
    await expect(startPeerHostUpdate('edge', digest)).rejects.toThrow('connection closed')
    await startPeerHostUpdate('edge', digest)
    await startPeerHostUpdate('edge', digest)
    const keys = fetchMock.mock.calls.map(([, init]) => {
      const request = JSON.parse(String(init?.body)) as { params: { idempotency_key: string } }
      return request.params.idempotency_key
    })
    expect(keys[0]).toBe(keys[1])
    expect(keys[2]).not.toBe(keys[1])
    expect(keys[0]).toMatch(/^peer-host-[0-9a-f]{12}-[0-9a-f]{32}$/)
    expect(stored.size).toBe(0)
  })

  it('rotates the intent key after an authoritative HTTP JSON-RPC rejection', async () => {
    const stored = new Map<string, string>()
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
        removeItem: (key: string) => stored.delete(key),
      },
    })
    const digest = 'd'.repeat(64)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const request = JSON.parse(String(init?.body)) as { id: number }
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        error: { code: -32000, message: 'peer host staging is failed' },
      }), { status: 400 })
    })

    await expect(startPeerHostUpdate('edge', digest)).rejects.toBeInstanceOf(ControllerRPCError)
    await expect(startPeerHostUpdate('edge', digest)).rejects.toThrow('peer host staging is failed')
    const keys = fetchMock.mock.calls.map(([, init]) => {
      const request = JSON.parse(String(init?.body)) as { params: { idempotency_key: string } }
      return request.params.idempotency_key
    })
    expect(keys[1]).not.toBe(keys[0])
    expect(stored.size).toBe(0)
  })

  it('reuses the intent key when the source reports an uncertain peer outcome', async () => {
    const stored = new Map<string, string>()
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
        removeItem: (key: string) => stored.delete(key),
      },
    })
    const digest = 'f'.repeat(64)
    let call = 0
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const request = JSON.parse(String(init?.body)) as { id: number }
      call += 1
      if (call === 1) {
        return new Response(JSON.stringify({
          jsonrpc: '2.0', id: request.id,
          error: { code: -32004, message: 'bridge session closed before response' },
        }), { status: 400 })
      }
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        result: {
          peer: 'edge', stage: 'remote-queued', terminal_verified: false,
          artifact: { kind: 'host-executable', sha256: digest },
          operation: { id: 'remote-retry', kind: 'host', state: 'queued', progress_percent: 0 },
        },
      }), { status: 200 })
    })

    await expect(startPeerHostUpdate('edge', digest)).rejects.toMatchObject({
      code: -32004, message: 'bridge session closed before response',
    })
    expect(stored.size).toBe(1)
    await startPeerHostUpdate('edge', digest)
    const keys = fetchMock.mock.calls.map(([, init]) => {
      const request = JSON.parse(String(init?.body)) as { params: { idempotency_key: string } }
      return request.params.idempotency_key
    })
    expect(keys[1]).toBe(keys[0])
    expect(stored.size).toBe(0)
  })

  it('adopts the coordinator-owned uncertain intent across distinct client sessions', async () => {
    const digest = '9'.repeat(64)
    const firstClient = new Map<string, string>()
    const secondClient = new Map<string, string>()
    const installStorage = (stored: Map<string, string>) => Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
        removeItem: (key: string) => stored.delete(key),
      },
    })

    installStorage(firstClient)
    const coordinatorKey = peerHostUpdateIdempotencyKey('edge', digest)
    installStorage(secondClient)
    const secondClientKey = peerHostUpdateIdempotencyKey('edge', digest)
    expect(secondClientKey).not.toBe(coordinatorKey)

    // A pushed peer-update.outcome-uncertain event carries this key. The
    // receiving client adopts it even if it had prepared a different key.
    expect(adoptPeerHostUpdateIntent('edge', digest, coordinatorKey)).toBe(true)
    expect(peerHostUpdateIdempotencyKey('edge', digest)).toBe(coordinatorKey)

    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const request = JSON.parse(String(init?.body)) as { id: number }
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        result: {
          peer: 'edge', stage: 'remote-queued', terminal_verified: false,
          artifact: { kind: 'host-executable', sha256: digest },
          operation: { id: 'remote-shared-retry', kind: 'host', state: 'queued', progress_percent: 0 },
        },
      }), { status: 200 })
    })
    await startPeerHostUpdate('edge', digest)
    const request = JSON.parse(String(fetchMock.mock.calls[0][1]?.body)) as {
      params: { idempotency_key: string }
    }
    expect(request.params.idempotency_key).toBe(coordinatorKey)
    expect(secondClient.size).toBe(0)
  })

  it.each([
    ['an empty success envelope', (_id: number) => new Response('{}', { status: 200 })],
    ['a mismatched success ID', (id: number) => new Response(JSON.stringify({
      jsonrpc: '2.0', id: id + 1, result: { peer: 'edge' },
    }), { status: 200 })],
    ['a mismatched HTTP error ID', (id: number) => new Response(JSON.stringify({
      jsonrpc: '2.0', id: id + 1, error: { code: -32003, message: 'stale denial' },
    }), { status: 400 })],
  ])('retains the intent key after %s', async (_name, uncertainResponse) => {
    const stored = new Map<string, string>()
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
        removeItem: (key: string) => stored.delete(key),
      },
    })
    const digest = 'e'.repeat(64)
    let call = 0
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
      const request = JSON.parse(String(init?.body)) as { id: number }
      call += 1
      if (call === 1) return uncertainResponse(request.id)
      return new Response(JSON.stringify({
        jsonrpc: '2.0', id: request.id,
        result: {
          peer: 'edge', stage: 'remote-queued', terminal_verified: false,
          artifact: { kind: 'host-executable', sha256: digest },
          operation: { id: 'remote-retry', kind: 'host', state: 'queued', progress_percent: 0 },
        },
      }), { status: 200 })
    })

    let firstError: unknown
    try {
      await startPeerHostUpdate('edge', digest)
    } catch (cause) {
      firstError = cause
    }
    expect(firstError).toBeInstanceOf(Error)
    expect(firstError).not.toBeInstanceOf(ControllerRPCError)
    expect(stored.size).toBe(1)
    await startPeerHostUpdate('edge', digest)
    const keys = fetchMock.mock.calls.map(([, init]) => {
      const request = JSON.parse(String(init?.body)) as { params: { idempotency_key: string } }
      return request.params.idempotency_key
    })
    expect(keys[1]).toBe(keys[0])
    expect(stored.size).toBe(0)
  })
})

import { afterEach, describe, expect, it, vi } from 'vitest'
import { compareBuildIdentity, sha256File, startFlashRestore, uploadArtifact } from './updates-api'

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
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      jsonrpc: '2.0', id: 1,
      result: { operation: { id: 'restore-1', kind: 'flash-restore', state: 'queued', progress_percent: 0 } },
    }), { status: 200 }))
    await startFlashRestore({ artifact_sha256: 'b'.repeat(64), authorized: true, method: 'urclock', port: 'COM18' })
    const [, init] = fetchMock.mock.calls[0]
    const request = JSON.parse(String(init?.body)) as { method: string; params: Record<string, unknown> }
    expect(request.method).toBe('controller.restore.flash')
    expect(request.method).not.toBe('controller.update.firmware')
    expect(request.params).toMatchObject({ authorized: true, method: 'urclock', port: 'COM18' })
  })
})

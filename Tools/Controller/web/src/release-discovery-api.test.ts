import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  checkReleaseCandidate,
  discoverRelease,
  discoverWorkflow,
  stageReleaseCandidate,
} from './release-discovery-api'

afterEach(() => vi.restoreAllMocks())

function installTransport(result: unknown) {
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
  })
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    jsonrpc: '2.0', id: 1, result,
  }), { status: 200 }))
}

function rpcRequest(fetchMock: ReturnType<typeof vi.spyOn>) {
  const [, init] = fetchMock.mock.calls[0]
  return JSON.parse(String(init?.body)) as { method: string; params: Record<string, unknown> }
}

describe('release discovery adapter', () => {
  it('forwards workflow selectors and packed build identity without downloading', async () => {
    const fetchMock = installTransport({ source: 'github-workflow', checked_at: '2026-08-02T00:00:00Z', candidates: [] })
    await discoverWorkflow({
      repository: 'owner/repository', workflow: 'Build', branch: 'main', kind: 'firmware',
      packed_timestamp: 0x35019d5c, bearer_token: 'ephemeral-token',
    })
    expect(rpcRequest(fetchMock)).toMatchObject({
      method: 'controller.discovery.github.workflow',
      params: {
        repository: 'owner/repository', workflow: 'Build', branch: 'main', kind: 'firmware',
        packed_timestamp: 0x35019d5c, bearer_token: 'ephemeral-token',
      },
    })
  })

  it('discovers releases and compares candidates through read-only RPCs', async () => {
    const fetchMock = installTransport({ source: 'github-release', checked_at: '2026-08-02T00:00:00Z', candidates: [] })
    await discoverRelease({ repository: 'owner/repository', tag: 'v1.2.3', kind: 'host-executable', platform: 'windows/amd64' })
    expect(rpcRequest(fetchMock).method).toBe('controller.discovery.github.release')

    fetchMock.mockClear()
    fetchMock.mockResolvedValue(new Response(JSON.stringify({
      jsonrpc: '2.0', id: 2, result: { status: 'newer', reason: 'candidate packed timestamp is newer' },
    }), { status: 200 }))
    await checkReleaseCandidate({ sha256: 'a'.repeat(64), packed_timestamp: 10 }, {
      id: 'candidate', source: 'manifest', kind: 'firmware', name: 'board.hex', url: 'https://example.invalid/board.hex', packed_timestamp: 11,
    })
    expect(rpcRequest(fetchMock).method).toBe('controller.discovery.check')
  })

  it('uses a deterministic idempotency key and keeps staging separate from programming', async () => {
    const fetchMock = installTransport({ operation: { id: 'stage-1', state: 'queued', progress_percent: 0 } })
    await stageReleaseCandidate({
      id: 'candidate-42', source: 'manifest', kind: 'firmware', name: 'board.hex',
      url: 'https://example.invalid/board.hex', sha256: 'b'.repeat(64),
    }, 'temporary-token')
    const request = rpcRequest(fetchMock)
    expect(request.method).toBe('controller.discovery.stage')
    expect(request.params).toMatchObject({
      idempotency_key: `stage:candidate-42:${'b'.repeat(64)}`,
      bearer_token: 'temporary-token',
    })
    expect(request.method).not.toContain('update.firmware')
  })
})

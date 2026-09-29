import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { describe, expect, it, vi } from 'vitest'

const source = readFileSync(new URL('../public/service-worker.js', import.meta.url), 'utf8')

function fixture(cached: unknown = undefined) {
  const handlers: Record<string, (event: any) => void> = {}
  const match = vi.fn(async () => cached)
  const fetch = vi.fn(async () => ({ ok: true, type: 'basic', clone: () => ({}) }))
  const self = {
    location: { origin: 'https://controller.example' },
    addEventListener: (name: string, handler: (event: any) => void) => { handlers[name] = handler },
    skipWaiting: vi.fn(),
    clients: { claim: vi.fn() },
  }
  runInNewContext(source, {
    self,
    caches: {
      match,
      open: vi.fn(async () => ({ addAll: vi.fn(), put: vi.fn() })),
      keys: vi.fn(async () => []),
      delete: vi.fn(),
    },
    fetch,
    URL,
    Response,
    Set,
    Promise,
  })
  return { handlers, match, fetch }
}

function request(pathname: string) {
  return {
    method: 'GET',
    url: `https://controller.example${pathname}`,
    mode: 'cors',
    destination: 'script',
  }
}

describe('service-worker asset freshness', () => {
  it('does not intercept the stable recovery script', () => {
    const { handlers, match, fetch } = fixture({ body: 'stale recovery script' })
    const respondWith = vi.fn()
    handlers.fetch({ request: request('/ui-recovery.js'), respondWith })
    expect(respondWith).not.toHaveBeenCalled()
    expect(match).not.toHaveBeenCalled()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('keeps content-hashed production assets immutable and cache-first', async () => {
    const cached = { body: 'hashed application bundle' }
    const { handlers, match, fetch } = fixture(cached)
    const respondWith = vi.fn()
    const asset = request('/assets/index-CONTENTHASH.js')
    handlers.fetch({ request: asset, respondWith })
    expect(respondWith).toHaveBeenCalledTimes(1)
    await expect(respondWith.mock.calls[0][0]).resolves.toBe(cached)
    expect(match).toHaveBeenCalledWith(asset)
    expect(fetch).not.toHaveBeenCalled()
  })
})

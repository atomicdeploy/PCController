import { describe, expect, it } from 'vitest'
import {
  embeddedResourcesMismatch,
  hostResourceIdentity,
  reloadForResourceMismatch,
  resourceReloadStorageKey,
} from './resource-version'

class MemoryStorage {
  readonly values = new Map<string, string>()

  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

describe('embedded host resource identity', () => {
  const embedded = { hostVersion: '1.4.0', buildTime: '2026-08-02T03:20:00Z' }

  it('reloads only for a complete differing packaged-host identity', () => {
    expect(embeddedResourcesMismatch({ host_version: '1.4.0', build_time: '2026-08-02T03:20:00Z' }, embedded)).toBe(false)
    expect(embeddedResourcesMismatch({ host_version: '1.4.1', build_time: '2026-08-02T03:21:00Z' }, embedded)).toBe(true)
    expect(embeddedResourcesMismatch({ host_version: '1.4.0', build_time: '2026-08-02T03:21:00Z' }, embedded)).toBe(true)
  })

  it('does not loop on incomplete development identities', () => {
    expect(embeddedResourcesMismatch({ host_version: 'development', build_time: 'unknown' }, embedded)).toBe(false)
    expect(embeddedResourcesMismatch({ host_version: '', build_time: '' }, embedded)).toBe(false)
    expect(hostResourceIdentity({ host_version: ' 1.4.0 ', build_time: ' stamp ' })).toBe('1.4.0|stamp')
  })

  it('reloads once per authoritative identity and announces before navigation', () => {
    const storage = new MemoryStorage()
    const effects: string[] = []
    const config = { host_version: '1.4.1', build_time: '2026-08-02T03:21:00Z' }
    const environment = {
      embedded,
      storage,
      beforeReload: (identity: string) => effects.push(`announce:${identity}`),
      reload: () => effects.push('reload'),
    }

    expect(reloadForResourceMismatch(config, environment)).toBe(true)
    expect(effects).toEqual(['announce:1.4.1|2026-08-02T03:21:00Z', 'reload'])
    expect(storage.getItem(resourceReloadStorageKey)).toBe('1.4.1|2026-08-02T03:21:00Z')

    expect(reloadForResourceMismatch(config, environment)).toBe(false)
    expect(effects).toHaveLength(2)
  })

  it('clears the reload marker after the exact bundle arrives', () => {
    const storage = new MemoryStorage()
    storage.setItem(resourceReloadStorageKey, 'older|identity')
    const reloads: string[] = []

    expect(reloadForResourceMismatch(
      { host_version: embedded.hostVersion, build_time: embedded.buildTime },
      { embedded, storage, reload: () => reloads.push('reload') },
    )).toBe(false)
    expect(storage.getItem(resourceReloadStorageKey)).toBeNull()
    expect(reloads).toEqual([])
  })

  it('refuses an unbounded reload when per-tab storage is unavailable', () => {
    const reloads: string[] = []
    expect(reloadForResourceMismatch(
      { host_version: '1.4.1', build_time: '2026-08-02T03:21:00Z' },
      { embedded, storage: null, reload: () => reloads.push('reload') },
    )).toBe(false)
    expect(reloads).toEqual([])
  })
})

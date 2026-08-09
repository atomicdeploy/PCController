import { describe, expect, it } from 'vitest'
import { embeddedResourcesMismatch, hostResourceIdentity } from './resource-version'

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
})

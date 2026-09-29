import { describe, expect, it } from 'vitest'
import { embeddedResourcesMismatch, hostResourceIdentity } from './resource-version'

describe('embedded host resource identity', () => {
  const embedded = { hostVersion: '1.4.0', resourcePath: '/assets/app-current.js' }

  it('reloads only for a complete differing packaged-host identity', () => {
    expect(embeddedResourcesMismatch({ host_version: '1.4.0', web_resource_path: '/assets/app-current.js' }, embedded)).toBe(false)
    expect(embeddedResourcesMismatch({ host_version: '1.4.1', web_resource_path: '/assets/app-next.js' }, embedded)).toBe(true)
    expect(embeddedResourcesMismatch({ host_version: '1.4.0', web_resource_path: '/assets/app-next.js' }, embedded)).toBe(true)
  })

  it('does not loop on incomplete development identities', () => {
    expect(embeddedResourcesMismatch({ host_version: 'development', web_resource_path: '' }, embedded)).toBe(false)
    expect(embeddedResourcesMismatch({ host_version: '', web_resource_path: '' }, embedded)).toBe(false)
    expect(hostResourceIdentity({ host_version: ' 1.4.0 ', web_resource_path: ' /assets/app-current.js ' })).toBe('1.4.0|/assets/app-current.js')
  })
})

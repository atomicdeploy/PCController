import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ReleaseDiscovery } from './release-discovery'

describe('release discovery view', () => {
  it('renders provider selection and a stage-only authorization boundary', () => {
    const html = renderToStaticMarkup(<ReleaseDiscovery
      manifest={null}
      events={[]}
      locale="en"
      openDialog={() => undefined}
      onArtifactsChanged={() => undefined}
    />)
    expect(html).toContain('GitHub release')
    expect(html).toContain('Workflow artifact')
    expect(html).toContain('HTTP manifest')
    expect(html).toContain('Discover metadata')
    expect(html).toContain('Discover build artifacts without hardcoded product names')
    expect(html).not.toContain('Authorize board programming')
  })

  it('renders its workflow in Persian when requested', () => {
    const html = renderToStaticMarkup(<ReleaseDiscovery
      manifest={null}
      events={[]}
      locale="fa"
      openDialog={() => undefined}
      onArtifactsChanged={() => undefined}
    />)
    expect(html).toContain('کشف انتشار و گردش‌کار')
    expect(html).toContain('کشف فراداده')
    expect(html).toContain('خروجی گردش‌کار')
    expect(html).not.toContain('Discover metadata')
  })
})

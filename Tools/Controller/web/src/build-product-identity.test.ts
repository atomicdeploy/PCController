import { describe, expect, it } from 'vitest'

import { escapeProductHTML } from '../vite.config'

describe('build product identity', () => {
  it('escapes every HTML-significant presentation character', () => {
    expect(escapeProductHTML(`Lab & <Controller> "Builder's"`)).toBe(
      'Lab &amp; &lt;Controller&gt; &quot;Builder&#39;s&quot;'
    )
  })
})

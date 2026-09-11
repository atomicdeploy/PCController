import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('recovered search shortcut layout', () => {
  it('lets the search label grow without stretching nested shortcut keys', () => {
    const css = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')
    expect(css).toContain('.command-trigger > span:not(.key-combo) { flex: 1;')
    expect(css).toContain('.command-trigger > .key-combo { flex: none;')
    expect(css).not.toMatch(/\.command-trigger span\s*\{[^}]*flex:\s*1/)
  })
})

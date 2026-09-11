import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { House } from 'lucide-react'
import { describe, expect, it, vi } from 'vitest'
import { NavButton } from './components'

const css = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')

describe('collapsed navigation layout contract', () => {
  it.each([undefined, '8'])('keeps an accessible name when the visible label is removed (badge=%s)', badge => {
    const html = renderToStaticMarkup(<NavButton icon={House} label="Overview" active badge={badge} onClick={() => {}} />)
    expect(html).toContain('aria-label="Overview"')
    expect(html).toContain('aria-current="page"')
    expect(html).toContain('class="nav-button__label">Overview</span>')
  })

  it('preserves activation and does not mark inactive navigation as the current page', () => {
    const onClick = vi.fn()
    const button = NavButton({ icon: House, label: 'Overview', onClick })
    button.props.onClick()
    expect(onClick).toHaveBeenCalledOnce()
    expect(button.props['aria-current']).toBeUndefined()
  })

  it('uses one fixed square target without inherited inter-row gaps', () => {
    const compact = css.match(/\.is-sidebar-compact \.nav-button\s*\{([^}]+)\}/)?.[1]
    expect(compact).toContain('inline-size: 44px')
    expect(compact).toContain('block-size: 44px')
    expect(compact).toContain('min-block-size: 44px')
    expect(compact).toContain('gap: 0')
    expect(compact).toContain('flex: none')
  })

  it('removes all non-icon grid items only in the compact state', () => {
    const hidden = css.match(/([^{}]+)\{\s*display:\s*none;\s*\}/g)?.find(rule =>
      rule.includes('.is-sidebar-compact .nav-button__label'))
    expect(hidden).toContain('.is-sidebar-compact .nav-button__label')
    expect(hidden).toContain('.is-sidebar-compact .nav-group__label')
    expect(hidden).toContain('.is-sidebar-compact .nav-button__badge')
    expect(hidden).toContain('.is-sidebar-compact .nav-button__chevron')
  })
})

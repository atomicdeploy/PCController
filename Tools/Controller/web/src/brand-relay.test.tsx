import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { BrandIcon, RelayToggle } from './components'

describe('restored brand and relay controls', () => {
  it('renders the canonical icon instead of unconditional initials', () => {
    const html = renderToStaticMarkup(<BrandIcon fallback="PC" />)
    expect(html).toContain('src="/favicon.svg"')
    expect(html).not.toContain('>PC<')
  })
  it('places the visual knob inside a real actionable button', () => {
    const toggle = vi.fn()
    const element = RelayToggle({ active: false, disabled: false, label: 'Toggle relay 5', onToggle: toggle })
    expect(element.type).toBe('button')
    expect(element.props.children.type).toBe('i')
    expect(element.props.children.props.children.type).toBe('b')
    element.props.onClick()
    expect(toggle).toHaveBeenCalledOnce()
    expect(element.props['aria-pressed']).toBe(false)
  })
  it('disables the physical action when disconnected', () => {
    const html = renderToStaticMarkup(<RelayToggle active disabled label="Toggle relay 5" onToggle={() => {}} />)
    expect(html).toContain('disabled=""')
    expect(html).toContain('aria-pressed="true"')
  })
})

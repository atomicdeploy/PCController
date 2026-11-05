import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { StripOrderSelect } from './strip-color-order'

describe('pixel color order', () => {
  const value = { color_order: 'BRG', firmware_order: 'BRG', orders: ['GRB', 'RGB', 'BRG', 'BGR', 'RBG', 'GBR'] }
  it('uses the host common-first catalog without changing the saved selection', () => {
    const markup = renderToStaticMarkup(<StripOrderSelect value={value} busy={false} title="Pixel color order" onChange={vi.fn()} />)
    expect([...markup.matchAll(/<option value="([RGB]+)"/g)].map(match => match[1])).toEqual(value.orders)
    expect(markup).toContain('value="BRG" selected=""')
    expect(markup).toContain('dir="ltr"')
    expect(markup).not.toContain('disabled')
  })
  it('disables edits while loading or saving, without inventing a selection', () => {
    const loading = renderToStaticMarkup(<StripOrderSelect value={null} busy={false} title="Pixel color order" onChange={vi.fn()} />)
    expect(loading).toContain('disabled=""')
    expect(loading).not.toContain('value="RGB"')
    const saving = renderToStaticMarkup(<StripOrderSelect value={value} busy title="ترتیب رنگ پیکسل" onChange={vi.fn()} />)
    expect(saving).toContain('disabled=""')
    expect(saving).toContain('value="BRG" selected=""')
  })
})

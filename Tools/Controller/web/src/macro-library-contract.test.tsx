import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { MacroCatalog } from './macro-library'

describe('macro catalog DOM contract', () => {
  it('renders typed name, category, color, step count, and exact duration', () => {
    const markup = renderToStaticMarkup(<MacroCatalog
      locale="en"
      selectedReference="9"
      onSelect={vi.fn()}
      macros={[{
        id: 9,
        name: 'Quiet close',
        category: 'Motion',
        color: 'violet',
        steps: [
          { kind: 'relay', at_us: 0, target: 1, value: 1 },
          { kind: 'motion', at_us: 125_500, target: 2, value: 0 },
        ],
      }]}
    />)
    expect(markup).toContain('Quiet close')
    expect(markup).toContain('#9 · Motion')
    expect(markup).toContain('is-violet')
    expect(markup).toContain('>2<')
    expect(markup).toContain('125.5 ms')
    expect(markup).toContain('aria-selected="true"')
  })
})

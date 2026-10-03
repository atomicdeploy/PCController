import { readFileSync } from 'node:fs'
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

  it('keeps split-path terminology and narration out of the effect source contract', () => {
    const source = [
      readFileSync(new URL('./macro-library.tsx', import.meta.url), 'utf8'),
      readFileSync(new URL('./macro-live.ts', import.meta.url), 'utf8'),
      readFileSync(new URL('./workbench.tsx', import.meta.url), 'utf8'),
    ].join('\n')
    expect(source).not.toMatch(/legacy|قدیمی/i)
    expect(source).not.toContain('shouldUseCommandSurfaceFallback')
    expect(source).not.toContain('host-advertised command surface')
    expect(source).toContain('commandSurface={run}')
  })

  it('gives the framed effect library the full workbench width', () => {
    const styles = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')
    expect(styles).toContain('.workbench-grid > [data-layout-card-id="macros"] { grid-column: 1 / -1; }')
    expect(styles).not.toContain('.workbench-grid > .macro-card { grid-column: 1 / -1; }')
  })
})

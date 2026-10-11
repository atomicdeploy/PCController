import { readFileSync } from 'node:fs'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { MacroCatalog, MacroLibraryPanel } from './macro-library'

describe('macro catalog DOM contract', () => {
  it.each(['en', 'fa'] as const)('does not offer an unwired empty capture in %s', (locale) => {
    const command = vi.fn()
    const markup = renderToStaticMarkup(<MacroLibraryPanel locale={locale} online events={[]} commandSurface={command} />)
    const pending = markup.match(/<button[^>]*data-capture-selection="unavailable"[^>]*>/g) ?? []
    expect(pending).toHaveLength(3)
    expect(pending.every((button) => button.includes('disabled=""'))).toBe(true)
    expect(markup).toContain('effect record start-json')
    expect(command).not.toHaveBeenCalled()
    const source = readFileSync(new URL('./macro-library.tsx', import.meta.url), 'utf8')
    expect(source).not.toContain('`effect record start ')
    expect(source).not.toContain('`effect record start-board ')
    expect(source).not.toContain('`effect record import-board ')
    expect(source).toContain("'effect record save'")
    expect(source).toContain("'effect record discard'")
  })

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

  it('includes duration tails and whole-effect repeat intervals in catalogue span', () => {
    const markup = renderToStaticMarkup(<MacroCatalog locale="en" selectedReference="" onSelect={vi.fn()} macros={[{
      id: 4, name: 'Repeat', repeat_count: 3, repeat_interval_ms: 200,
      steps: [{ kind: 'beep', at_us: 50_000, duration_ms: 100 }],
    }]} />)
    expect(markup).toContain('550 ms')
  })

  it('does not invent a duration for an invalid repeat', () => {
    const markup = renderToStaticMarkup(<MacroCatalog locale="en" selectedReference="" onSelect={vi.fn()} macros={[{
      id: 4, name: 'Invalid', repeat_count: 0, steps: [{ kind: 'relay', at_us: 0 }],
    }]} />)
    expect(markup).toContain('Invalid timing')
  })

  it('composes step repeats inside the complete whole-effect span', () => {
    const markup = renderToStaticMarkup(<MacroCatalog locale="en" selectedReference="" onSelect={vi.fn()} macros={[{
      id: 4, name: 'Nested', repeat_count: 2,
      steps: [{ kind: 'beep', at_us: 50_000, duration_ms: 100, repeat_count: 3, repeat_interval_ms: 200 }],
    }]} />)
    expect(markup).toContain('1.1 s')
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

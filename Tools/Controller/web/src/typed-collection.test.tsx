import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { EventList } from './event-collection'
import { StructuredValue, TypedCollection } from './typed-collection'
import {
  anchoredScrollTop,
  collectionToCSV,
  collectionToJSON,
  discoverCollectionFields,
  filterCollectionEntries,
  isKeyboardContextGesture,
  nextGridFocus,
  reconnectedScrollTop,
  reconcileFieldRegistry,
  sortCollectionEntries,
  structuredJSONStringify,
  structuredValueSummary,
  type CollectionEntry,
} from './typed-collection-model'
import type { ControllerEvent } from './types'

describe('typed collection structured values', () => {
  it('formats objects, arrays, non-JSON values, and cycles without implicit object text', () => {
    const cyclic: Record<string, unknown> = { enabled: true, ports: [1, 2] }
    cyclic.self = cyclic
    expect(structuredValueSummary({ zone: 'front' })).toBe('Object · 1 field')
    expect(structuredValueSummary([1, { state: 'ready' }])).toBe('Array · 2 items')
    const text = structuredJSONStringify({ cyclic, missing: undefined, large: 42n }, true)
    expect(() => JSON.parse(text)).not.toThrow()
    expect(text).toContain('[circular]')
    expect(text).toContain('42n')
    expect(text).not.toContain('[object Object]')
  })

  it('renders recursive values as an accessible disclosure', () => {
    const markup = renderToStaticMarkup(<StructuredValue
      label="Expand diagnostics"
      value={{ identity: { code: 2048, flags: ['ready', 'paired'] }, active: true }}
    />)
    expect(markup).toContain('<details')
    expect(markup).toContain('aria-label="Expand diagnostics"')
    expect(markup).toContain('<dt>identity</dt>')
    expect(markup).toContain('<dt>code</dt>')
    expect(markup).toContain('paired')
    expect(markup).not.toContain('[object Object]')
  })
})

describe('typed collection registry, sorting, and filtering', () => {
  const rows = [
    { id: 'b', score: 12, metadata: { zone: 'rear' } },
    { id: 'a', score: 3, metadata: { zone: 'front' } },
    { id: 'c', score: 12, metadata: { zone: 'front' } },
  ]
  const entries: CollectionEntry<(typeof rows)[number]>[] = rows.map((row, sourceIndex) => ({
    key: row.id,
    row,
    sourceIndex,
  }))

  it('keeps field order and user settings stable as new fields arrive', () => {
    const first = reconcileFieldRegistry([], discoverCollectionFields(rows.slice(0, 1), ['id', 'score']))
    const customized = [
      { ...first[1], visible: false, width: 312 },
      first[0],
      first[2],
    ]
    const next = reconcileFieldRegistry(customized, discoverCollectionFields([
      ...rows,
      { id: 'd', score: 4, metadata: { zone: 'side' }, status: 'new' } as (typeof rows)[number] & { status: string },
    ], ['id', 'score']))
    expect(next.map((field) => field.id)).toEqual(['score', 'id', 'metadata', 'status'])
    expect(next[0]).toMatchObject({ visible: false, width: 312 })
  })

  it('sorts numeric values stably and searches recursively through metadata', () => {
    const fields = reconcileFieldRegistry([], discoverCollectionFields(rows, ['id', 'score']))
    expect(sortCollectionEntries(entries, fields, { field: 'score', direction: 'asc' }, 'en')
      .map((entry) => entry.key)).toEqual(['a', 'b', 'c'])
    expect(sortCollectionEntries(entries, fields, { field: 'score', direction: 'desc' }, 'en')
      .map((entry) => entry.key)).toEqual(['b', 'c', 'a'])
    expect(filterCollectionEntries(entries, fields, 'front', 'en').map((entry) => entry.key)).toEqual(['a', 'c'])
  })
})

describe('typed collection exports', () => {
  const fields = [
    { id: 'name' },
    { id: 'metadata' },
    { id: 'note' },
  ]
  const rows = [
    { name: 'alpha,beta', metadata: { enabled: true, channels: [1, 2] }, note: 'line one\nline two' },
    { name: 'gamma', metadata: null, note: 'plain' },
  ]

  it('produces valid all and filtered JSON without losing nested values', () => {
    expect(JSON.parse(collectionToJSON(rows))).toEqual(rows)
    expect(JSON.parse(collectionToJSON(rows.slice(0, 1)))).toEqual(rows.slice(0, 1))
  })

  it('produces RFC-style CSV quoting and structured JSON cells', () => {
    const csv = collectionToCSV(rows, fields)
    expect(csv.startsWith('name,metadata,note\r\n')).toBe(true)
    expect(csv).toContain('"alpha,beta"')
    expect(csv).toContain('"{""enabled"":true,""channels"":[1,2]}"')
    expect(csv).toContain('"line one\nline two"')
    expect(csv).not.toContain('[object Object]')
  })
})

describe('typed collection keyboard, context, bounds, and direction', () => {
  it('recognizes Menu and Shift+F10 and applies visual RTL arrow movement', () => {
    expect(isKeyboardContextGesture({ key: 'ContextMenu', shiftKey: false })).toBe(true)
    expect(isKeyboardContextGesture({ key: 'Apps', shiftKey: false })).toBe(true)
    expect(isKeyboardContextGesture({ key: 'F10', shiftKey: true })).toBe(true)
    expect(isKeyboardContextGesture({ key: 'F10', shiftKey: false })).toBe(false)
    const focus = { area: 'row' as const, field: 1, row: 2 }
    expect(nextGridFocus(focus, 'ArrowRight', 8, 4, 'ltr')).toMatchObject({ field: 2, row: 2 })
    expect(nextGridFocus(focus, 'ArrowRight', 8, 4, 'rtl')).toMatchObject({ field: 0, row: 2 })
    expect(nextGridFocus({ area: 'header', field: 1, row: 0 }, 'ArrowDown', 8, 4, 'rtl'))
      .toEqual({ area: 'row', field: 1, row: 0 })
  })

  it('keeps the prior visible anchor stable after a live prepend', () => {
    expect(anchoredScrollTop(360, 18, 114)).toBe(456)
    expect(anchoredScrollTop(4, 20, -30)).toBe(0)
    expect(reconnectedScrollTop(460, 900, 300)).toBe(460)
    expect(reconnectedScrollTop(760, 900, 300)).toBe(600)
  })

  it('renders a bounded screen-reader-aware window with roving cells and context semantics', () => {
    const rows = Array.from({ length: 250 }, (_, id) => ({ id, state: id % 2 ? 'ready' : 'idle' }))
    const markup = renderToStaticMarkup(<TypedCollection
      records={rows}
      fields={[{ id: 'id' }, { id: 'state' }]}
      locale="fa"
      direction="rtl"
      ariaLabel="رکوردها"
      rowKey={(row) => `row-${row.id}`}
      windowSize={40}
    />)
    expect(markup).toContain('dir="rtl"')
    expect(markup).toContain('role="grid"')
    expect(markup).toContain('aria-multiselectable="true"')
    expect(markup).toContain('aria-rowcount="251"')
    expect(markup).toContain('aria-colcount="3"')
    expect(markup.match(/data-collection-row-key=/g)).toHaveLength(40)
    expect(markup).toContain('1–40')
    expect(markup).toContain('aria-haspopup="menu"')
    expect(markup).toContain('type="checkbox"')
    expect(markup.match(/tabindex="0"/g)).toHaveLength(1)
    expect(markup).toContain('تغییر عرض ستون')
  })
})

describe('controller event metadata collection', () => {
  it('retains metadata in an accessible disclosure in both LTR and RTL', () => {
    const event: ControllerEvent = {
      id: 17,
      time: '2026-08-02T08:20:00Z',
      kind: 'controller.ready',
      text: 'Controller synchronized',
      source: 'bridge',
      metadata: { port: 'COM18', lifecycle: 'connected' },
    }
    const english = renderToStaticMarkup(<EventList events={[event]} locale="en" t={(key) => key} limit={500} toolbar />)
    const compact = renderToStaticMarkup(<EventList events={[event]} locale="en" t={(key) => key} />)
    const persian = renderToStaticMarkup(<EventList events={[event]} locale="fa" t={(key) => key} limit={500} toolbar />)
    expect(english).toContain('Expand event metadata')
    expect(english).toContain('<dt>port</dt>')
    expect(english).toContain('COM18')
    expect(english).not.toContain('[object Object]')
    expect(compact).toContain('Expand event metadata')
    expect(persian).toContain('dir="rtl"')
    expect(persian).toContain('نمایش فرادادهٔ رویداد')
  })
})

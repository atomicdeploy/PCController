import { describe, expect, it } from 'vitest'
import {
  defaultCardLayout,
  loadCardLayout,
  moveCard,
  normalizeCardLayout,
  resetCardLayout,
  saveCardLayout,
  toggleCard,
} from './dashboard-layout'

const cards = ['telemetry', 'outputs', 'events'] as const

describe('card layout persistence', () => {
  it('keeps allowed cards exactly once and appends newly introduced cards', () => {
    expect(normalizeCardLayout({
      order: ['events', 'events', 'unknown'],
      collapsed: ['events', 'unknown'],
      hidden: ['outputs', 'outputs', 'unknown'],
    }, cards)).toEqual({
      order: ['events', 'telemetry', 'outputs'],
      collapsed: ['events'],
      hidden: ['outputs'],
    })
  })

  it('loads malformed storage as the current living default', () => {
    const storage = { getItem: () => '{broken' }
    expect(loadCardLayout('dashboard', cards, storage)).toEqual(defaultCardLayout(cards))
  })

  it('saves only normalized bounded state and resets reversibly', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    }
    saveCardLayout('dashboard', cards, {
      order: ['events', 'events', 'outputs'] as any,
      collapsed: ['telemetry', 'missing'] as any,
      hidden: [],
    }, storage)
    expect(loadCardLayout('dashboard', cards, storage)).toEqual({
      order: ['events', 'outputs', 'telemetry'],
      collapsed: ['telemetry'],
      hidden: [],
    })
    resetCardLayout('dashboard', storage)
    expect(loadCardLayout('dashboard', cards, storage)).toEqual(defaultCardLayout(cards))
  })

  it('moves and toggles cards without losing unrelated layout state', () => {
    const initial = { ...defaultCardLayout(cards), collapsed: ['events' as const] }
    expect(moveCard(initial, 'events', 'telemetry')).toEqual({
      ...initial,
      order: ['events', 'telemetry', 'outputs'],
    })
    expect(toggleCard([], 'outputs')).toEqual(['outputs'])
    expect(toggleCard(['outputs'], 'outputs')).toEqual([])
  })
})

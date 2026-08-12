import { describe, expect, it } from 'vitest'
import { defaultDashboardLayout, moveDashboardCard, normalizeDashboardLayout, toggleDashboardCard } from './dashboard-layout'

describe('dashboard layout', () => {
  it('keeps known cards exactly once and appends newly introduced cards', () => {
    expect(normalizeDashboardLayout({ order: ['events', 'events', 'unknown'] as any }).order).toEqual(['events', 'telemetry', 'outputs', 'overview', 'actions'])
  })
  it('moves and toggles a card without losing layout state', () => {
    const initial = defaultDashboardLayout()
    expect(moveDashboardCard(initial, 'events', 'telemetry').order[0]).toBe('events')
    expect(toggleDashboardCard([], 'overview')).toEqual(['overview'])
  })
})

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

describe('WebUI acceptance regression contracts', () => {
  it('keeps the collapsed connection status visible and portals its dismissible menu', () => {
    const app = source('./app.tsx')
    const css = source('./styles.css')
    expect(app).toContain('createPortal(<div')
    expect(app).toContain('sidebarStatusMenuRef.current?.contains(target)')
    const hiddenRule = css.match(/\.is-sidebar-compact \.brand__copy[^}]*\{ display: none; \}/s)?.[0] ?? ''
    expect(hiddenRule).not.toContain('.sidebar__status')
    expect(css).toContain('.is-sidebar-compact .sidebar__status { width: 44px;')
    expect(css).toMatch(/\.sidebar__status-menu \{[^}]*position: fixed/s)
  })

  it('reserves space for the PWM scrollbar and does not give text fields a hand cursor', () => {
    const css = source('./styles.css')
    expect(css).toMatch(/\.pwm-mixer \{[^}]*scrollbar-gutter: stable;[^}]*padding-inline-end: 18px/s)
    expect(css).toMatch(/textarea:not\(:disabled\), \[contenteditable="true"\] \{ cursor: text; \}/)
  })

  it('uses bounded live chart interpolation instead of disabling every animation', () => {
    const chart = source('./telemetry-chart.tsx')
    expect(chart).not.toContain('isAnimationActive={false}')
    expect(chart.match(/animationDuration=\{260\}/g)?.length).toBeGreaterThanOrEqual(6)
    expect(chart).toContain('focusedCurrentDomain')
  })

  it('uses pushed state as the ordinary command completion path', () => {
    const app = source('./app.tsx')
    expect(app).toContain('notifyOnSuccess = true, refreshAfter = false')
    expect(app).toContain('if (refreshAfter) void refresh()')
    expect(app).not.toContain('notifyOnSuccess = true, refreshAfter = true')
  })
})

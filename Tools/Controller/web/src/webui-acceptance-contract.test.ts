import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

describe('WebUI acceptance regression contracts', () => {
  it('keeps the compact connection status actionable and portals a dismissible menu', () => {
    const app = source('./app.tsx')
    const css = source('./styles.css')
    expect(app).toContain("void runCommand('reconnect')")
    expect(app).toContain('createPortal(<div')
    expect(app).toContain('sidebarStatusMenuRef.current?.contains(target)')
    expect(css).toContain('.is-sidebar-compact .sidebar__status {')
    expect(css).toMatch(/\.sidebar__status-menu \{[^}]*position: fixed/s)
  })

  it('reserves PWM scrollbar space and uses text cursors for editable text', () => {
    const css = source('./styles.css')
    expect(css).toMatch(/\.pwm-mixer \{[^}]*scrollbar-gutter: stable;[^}]*padding-inline-end:/s)
    expect(css).toMatch(/textarea:not\(:disabled\), \[contenteditable="true"\] \{ cursor: text; \}/)
  })

  it('combines semantic filtering with the current keyed reduced-motion animation', () => {
    const chart = source('./telemetry-chart.tsx')
    expect(chart).toContain('normalizeTelemetrySamples')
    expect(chart).toContain('focusedCurrentDomain')
    expect(chart).toContain("type=\"linear\"")
    expect(chart.match(/\{\.\.\.animation\}/g)).toHaveLength(6)
    expect(chart).toContain("value={smoothing}")
  })

  it('uses pushed state for ordinary commands and refreshes only when requested', () => {
    const app = source('./app.tsx')
    expect(app).toContain('refreshAfter = false')
    expect(app).toContain('if (refreshAfter) void refresh()')
    expect(app).not.toContain('notifyOnSuccess = true, refreshAfter = true')
  })

  it('keeps dashboard rendering behind verified state and capabilities', () => {
    const views = source('./views.tsx')
    expect(views).toContain("const boardReady = props.transport.boardState === 'ready' && snapshot.connected && snapshot.have_status")
    expect(views).toContain('{boardReady && haveMetricCards &&')
    expect(views).toContain('{boardReady && available.relays &&')
    expect(views).not.toContain("loadDashboardLayout")
  })
})

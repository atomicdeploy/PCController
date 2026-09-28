import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('recovered search shortcut layout', () => {
  it('keeps PWM controls out of the dashboard metric summary', () => {
    const source = readFileSync(new URL('./views.tsx', import.meta.url), 'utf8')
    const dashboard = source.split('export function DashboardView')[1].split('export function ControlsView')[0]
    expect(dashboard).not.toContain('label="PWM"')
    expect(dashboard).not.toContain('haveMeasurements || available.pwm')
  })
  it('lets the search label grow without stretching nested shortcut keys', () => {
    const css = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')
    expect(css).toContain('.command-trigger > span:not(.key-combo) { flex: 1;')
    expect(css).toContain('.command-trigger > .key-combo { flex: none;')
    expect(css).not.toMatch(/\.command-trigger span\s*\{[^}]*flex:\s*1/)
  })
})

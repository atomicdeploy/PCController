import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const index = readFileSync(new URL('../index.html', import.meta.url), 'utf8')
const manifest = JSON.parse(readFileSync(new URL('../public/manifest.webmanifest', import.meta.url), 'utf8')) as Record<string, unknown>
const worker = readFileSync(new URL('../public/service-worker.js', import.meta.url), 'utf8')
const startup = readFileSync(new URL('./main.tsx', import.meta.url), 'utf8')
const themeBootstrap = readFileSync(new URL('../public/theme-init.js', import.meta.url), 'utf8')
const touch = readFileSync(new URL('./touch-activation.ts', import.meta.url), 'utf8')

describe('PWA and touch capability contract', () => {
  it('declares an installable standalone mobile application with safe viewport and theme metadata', () => {
    expect(index).toContain('viewport-fit=cover, interactive-widget=resizes-content')
    expect(index).toContain('name="mobile-web-app-capable" content="yes"')
    expect(index).toContain('name="apple-mobile-web-app-capable" content="yes"')
    expect(index).toContain('name="theme-color" content="#0b0a0e"')
    expect(index).toContain('data-runtime-theme')
    expect(index).toContain('id="startup-preloader"')
    expect(index).toContain('startup-fuji')
    expect(manifest.display).toBe('standalone')
    expect(manifest.start_url).toBe('/#/dashboard')
    expect(manifest.scope).toBe('/')
    expect(manifest.theme_color).toBe('#0b0a0e')
    expect(manifest.prefer_related_applications).toBe(false)
    expect(themeBootstrap).toContain("querySelector('meta[data-runtime-theme]')")
  })

  it('caches only the UI shell and refuses to cache live controller state', () => {
    expect(worker).toContain("const shellCache = 'pccontroller-shell-v2'")
    expect(worker).toContain("'/manifest.webmanifest'")
    expect(worker).toContain("'/theme-init.js'")
    expect(worker).toContain("pathname === '/ipc'")
    expect(worker).toContain("pathname === '/api'")
    expect(worker).toContain("pathname === '/controller-config.js'")
    expect(worker).toContain("request.mode === 'navigate'")
    expect(worker).toContain("url.pathname.startsWith('/assets/')")
  })

  it('registers the worker, removes the first-paint shell after render, and installs touch-down activation', () => {
    expect(startup).toContain("navigator.serviceWorker.register('/service-worker.js'")
    expect(startup).toContain('installImmediateTouchActivation()')
    expect(startup).toContain("getElementById('startup-preloader')?.remove()")
    expect(touch).toContain("root.addEventListener('pointerdown', onPointerDown, true)")
    expect(touch).toContain("[data-touch-mode=\"hold\"]")
    expect(touch).toContain('target.click()')
  })
})

import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { describe, expect, it } from 'vitest'

const source = readFileSync(new URL('../public/ui-recovery.js', import.meta.url), 'utf8')
function fixture(stored: string | null = null, blocked = false) {
  const handlers: Record<string, (event: any) => void> = {}
  let reloads = 0
  let panels = 0
  class Script { type = 'module'; src = '/assets/app.js' }
  const element = () => ({ style: {}, setAttribute() {}, append() {}, focus() {} })
  runInNewContext(source, {
    window: { addEventListener: (name: string, handler: (event: any) => void) => { handlers[name] = handler }, location: { reload: () => reloads++ } },
    sessionStorage: { getItem: () => { if (blocked) throw Error('blocked'); return stored }, setItem: (_key: string, value: string) => { stored = value } },
    document: { getElementById: () => null, createElement: element, documentElement: { lang: 'en' }, body: { append: () => panels++ } },
    HTMLScriptElement: Script,
  })
  return { handlers, Script, reloads: () => reloads, panels: () => panels }
}
const event = () => ({ preventDefault() {} })
describe('deployment bundle recovery', () => {
  it('reloads once for a missing lazy chunk and coalesces simultaneous failures', () => {
    const f = fixture()
    f.handlers['vite:preloadError'](event())
    f.handlers['vite:preloadError'](event())
    expect(f.reloads()).toBe(1)
  })
  it('shows an explicit retry instead of looping or relying on unavailable storage', () => {
    for (const f of [fixture(String(Date.now())), fixture(null, true)]) {
      f.handlers['vite:preloadError'](event())
      expect(f.reloads()).toBe(0)
      expect(f.panels()).toBe(1)
    }
  })
  it('handles entry-module failure but ignores ordinary runtime errors', () => {
    const f = fixture()
    f.handlers.error({ ...event(), target: {} })
    expect(f.reloads()).toBe(0)
    f.handlers.error({ ...event(), target: new f.Script() })
    expect(f.reloads()).toBe(1)
  })
  it('recovers browser import rejections without retrying other failures', () => {
    const f = fixture()
    f.handlers.unhandledrejection({ ...event(), reason: new Error('command failed') })
    expect(f.reloads()).toBe(0)
    f.handlers.unhandledrejection({ ...event(), reason: new Error('Failed to fetch dynamically imported module: /assets/old.js') })
    expect(f.reloads()).toBe(1)
  })
})

import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AppPreferencesDialog } from './app-preferences-dialog'
import { defaultQuickHeaderPreferences } from './quick-header-preferences'
import type { Appearance } from './types'

const appearance: Appearance = {
  theme: 'system',
  locale: 'en',
  direction: 'auto',
  reduceMotion: false,
  compactNumbers: false,
  audioMuted: false,
  audioVolume: 0.42,
}

describe('application preferences dialog', () => {
  it('renders nothing while closed', () => {
    expect(renderToStaticMarkup(<AppPreferencesDialog open={false} locale="en" appearance={appearance} quickHeader={defaultQuickHeaderPreferences} onAppearance={vi.fn()} onQuickHeader={vi.fn()} onClose={vi.fn()} />)).toBe('')
  })

  it('presents host-only contextual settings with an accessible modal contract', () => {
    const markup = renderToStaticMarkup(<AppPreferencesDialog open locale="en" appearance={appearance} quickHeader={defaultQuickHeaderPreferences} onAppearance={vi.fn()} onQuickHeader={vi.fn()} onClose={vi.fn()} />)
    expect(markup).toContain('role="dialog"')
    expect(markup).toContain('aria-modal="true"')
    expect(markup).toContain('Application preferences')
    expect(markup).toContain('Interaction volume')
    expect(markup).toContain('Controller settings remain on the Settings page.')
    expect(markup).not.toMatch(/sample|simulat|legacy/i)
  })

  it('keeps Persian copy in the contextual dialog', () => {
    const markup = renderToStaticMarkup(<AppPreferencesDialog open locale="fa" appearance={{ ...appearance, locale: 'fa' }} quickHeader={defaultQuickHeaderPreferences} onAppearance={vi.fn()} onQuickHeader={vi.fn()} onClose={vi.fn()} />)
    expect(markup).toContain('ترجیحات برنامه')
    expect(markup).toContain('تنظیمات کنترلر')
  })
})

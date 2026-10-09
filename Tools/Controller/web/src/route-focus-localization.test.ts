import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'
import { focusPageDestination } from './components'
import { translator } from './i18n'

describe('route transition focus', () => {
  it('moves focus to the page heading without scrolling the application shell', () => {
    const focus = vi.fn()
    const heading = {
      dataset: {},
      focus,
      hasAttribute: vi.fn(() => false),
      setAttribute: vi.fn(),
    }
    const container = {
      isConnected: true,
      querySelector: vi.fn(() => heading),
    }

    expect(focusPageDestination(container as unknown as HTMLElement)).toBe(true)
    expect(container.querySelector).toHaveBeenCalledWith('h1')
    expect(heading.setAttribute).toHaveBeenCalledWith('tabindex', '-1')
    expect(heading.dataset).toEqual({ routeFocusTarget: '' })
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })
  })

  it('does not focus a page surface that AnimatePresence has detached', () => {
    expect(focusPageDestination(null)).toBe(false)
    expect(focusPageDestination({ isConnected: false } as HTMLElement)).toBe(false)
  })

  it('focuses only the present incoming surface after its animation and hides only that programmatic outline', () => {
    const componentSource = readFileSync(new URL('./components.tsx', import.meta.url), 'utf8')
    const styles = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')
    expect(componentSource).toContain('onAnimationComplete')
    expect(componentSource).toContain('if (isPresent && shouldFocusOnEnter.current)')
    expect(componentSource).toContain('focusOnEnter={initialPageRendered.current}')
    expect(styles).toContain('.page[data-route-focus-target]:focus, .page [data-route-focus-target]:focus { outline: none; }')
  })
})

describe('command discovery localization', () => {
  it('provides complete English and Persian phrases instead of mixed-language fragments', () => {
    const en = translator('en')
    const fa = translator('fa')
    expect(en('searchCommands')).toBe('Search commands and pages')
    expect(en('commandPalettePlaceholder')).toBe('Navigate or run a safe command…')
    expect(fa('searchCommands')).toBe('جستجوی فرمان‌ها و صفحه‌ها')
    expect(fa('commandPalettePlaceholder')).toBe('رفتن به صفحه یا اجرای فرمان امن…')
    expect(fa('quickCommandsLabel')).toBe('فرمان‌های سریع')
    expect(fa('navigate')).toBe('پیمایش')
    expect(fa('select')).toBe('انتخاب')
    expect(fa('primaryNavigation')).toBe('پیمایش اصلی')
    expect(fa('mobileNavigation')).toBe('پیمایش موبایل')
    expect(fa('toggleTheme')).toBe('تغییر پوسته')
    expect(fa('collapseNavigation')).toBe('جمع‌کردن نوار کناری')
    expect(fa('dashboardLink')).toBe('داشبورد')
    expect(fa('enableAudio')).toBe('فعال‌کردن صدای تعامل')
    expect(fa('keyboardShortcuts')).toBe('میان‌برهای صفحه‌کلید')
  })
})

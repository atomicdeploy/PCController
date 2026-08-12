import { useEffect, useRef, useState } from 'react'
import { Languages, PanelTop, Settings, Volume2, X } from 'lucide-react'
import { Button, Segmented, Toggle } from './components'
import type { Appearance, Locale } from './types'
import type { QuickHeaderControlID, QuickHeaderPreferences } from './quick-header-preferences'

type PreferenceTab = 'appearance' | 'header'

export function AppPreferencesDialog({
  open,
  locale,
  appearance,
  quickHeader,
  onAppearance,
  onQuickHeader,
  onClose,
}: {
  open: boolean
  locale: Locale
  appearance: Appearance
  quickHeader: QuickHeaderPreferences
  onAppearance: (value: Appearance) => void
  onQuickHeader: (value: QuickHeaderPreferences) => void
  onClose: () => void
}) {
  const [tab, setTab] = useState<PreferenceTab>('appearance')
  const dialog = useRef<HTMLElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  useEffect(() => { if (open) setTab('appearance') }, [open])
  useEffect(() => {
    if (!open) return
    returnFocus.current = document.activeElement as HTMLElement | null
    return () => {
      const target = returnFocus.current
      returnFocus.current = null
      window.requestAnimationFrame(() => target?.focus())
    }
  }, [open])
  useEffect(() => {
    if (!open) return
    const frame = window.requestAnimationFrame(() => dialog.current?.querySelector<HTMLElement>('button, input, select, textarea, [tabindex]:not([tabindex="-1"])')?.focus())
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); onClose(); return }
      if (event.key !== 'Tab' || !dialog.current) return
      const focusable = [...dialog.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])')]
      if (!focusable.length) return
      const first = focusable[0]
      const last = focusable.at(-1)!
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', key, true)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('keydown', key, true)
    }
  }, [onClose, open])
  if (!open) return null
  const setHeaderControl = (id: QuickHeaderControlID, enabled: boolean) => onQuickHeader({ ...quickHeader, [id]: enabled })
  return <div className="app-preferences-layer" role="presentation">
    <button type="button" className="app-preferences-backdrop" aria-label={copy('Close application preferences', 'بستن ترجیحات برنامه')} onClick={onClose} />
    <section ref={dialog} className="app-preferences-dialog" role="dialog" aria-modal="true" aria-label={copy('Application preferences', 'ترجیحات برنامه')}>
      <header>
        <span><Settings size={19} /><strong>{copy('Application preferences', 'ترجیحات برنامه')}</strong></span>
        <Button compact aria-label={copy('Close application preferences', 'بستن ترجیحات برنامه')} icon={X} onClick={onClose}>{copy('Close', 'بستن')}</Button>
      </header>
      <div className="app-preferences-tabs" role="tablist" aria-label={copy('Preference category', 'دستهٔ ترجیحات')}>
        <button type="button" role="tab" aria-selected={tab === 'appearance'} className={tab === 'appearance' ? 'is-active' : ''} onClick={() => setTab('appearance')}><PanelTop size={16} />{copy('Appearance', 'ظاهر')}</button>
        <button type="button" role="tab" aria-selected={tab === 'header'} className={tab === 'header' ? 'is-active' : ''} onClick={() => setTab('header')}><Settings size={16} />{copy('Quick header', 'نوار سریع')}</button>
      </div>
      {tab === 'appearance' ? <div className="app-preferences-content">
        <div className="setting-group"><label>{copy('Theme', 'پوسته')}</label><Segmented value={appearance.theme} label={copy('Theme', 'پوسته')} options={[{ value: 'system', label: copy('System', 'سیستم'), icon: PanelTop }, { value: 'dark', label: copy('Dark', 'تیره') }, { value: 'light', label: copy('Light', 'روشن') }]} onChange={(theme) => onAppearance({ ...appearance, theme })} /></div>
        <label className="app-preferences-select"><span><Languages size={16} />{copy('Language', 'زبان')}</span><select value={appearance.locale} onChange={(event) => onAppearance({ ...appearance, locale: event.target.value as Locale })}><option value="en">English</option><option value="fa">فارسی</option></select></label>
        <div className="setting-group"><label>{copy('Direction', 'جهت')}</label><Segmented value={appearance.direction} label={copy('Direction', 'جهت')} options={[{ value: 'auto', label: copy('Auto', 'خودکار') }, { value: 'ltr', label: copy('Left to right', 'چپ به راست') }, { value: 'rtl', label: copy('Right to left', 'راست به چپ') }]} onChange={(direction) => onAppearance({ ...appearance, direction })} /></div>
        <Toggle checked={appearance.reduceMotion} onChange={(reduceMotion) => onAppearance({ ...appearance, reduceMotion })} label={copy('Reduce interface motion', 'کاهش حرکت رابط')} detail={copy('Applies to this browser and is saved with application preferences.', 'در همین مرورگر اعمال و با ترجیحات برنامه ذخیره می‌شود.')} />
        <Toggle checked={!appearance.audioMuted} onChange={(enabled) => onAppearance({ ...appearance, audioMuted: !enabled })} label={<span className="setting-icon-label"><Volume2 size={16} />{copy('Interaction audio', 'صدای تعامل')}</span>} />
      </div> : <div className="app-preferences-content">
        <p>{copy('Choose which nonessential controls appear in the quick header. The Settings button stays visible so these choices are always reversible.', 'انتخاب کنید کدام کنترل‌های غیرضروری در نوار سریع نمایش داده شوند. دکمهٔ تنظیمات همیشه دیده می‌شود تا این انتخاب‌ها برگشت‌پذیر باشند.')}</p>
        <Toggle checked={quickHeader.theme} onChange={(value) => setHeaderControl('theme', value)} label={copy('Theme switch', 'تغییر پوسته')} />
        <Toggle checked={quickHeader.language} onChange={(value) => setHeaderControl('language', value)} label={copy('Language switch', 'تغییر زبان')} />
        <Toggle checked={quickHeader.audio} onChange={(value) => setHeaderControl('audio', value)} label={copy('Interaction audio', 'صدای تعامل')} />
        <Toggle checked={quickHeader.hotkeys} onChange={(value) => setHeaderControl('hotkeys', value)} label={copy('Keyboard guide', 'راهنمای صفحه‌کلید')} />
        <Toggle checked={quickHeader.notifications} onChange={(value) => setHeaderControl('notifications', value)} label={copy('Event notifications', 'اعلان رویدادها')} />
      </div>}
      <footer>{copy('These are host application preferences. Board EEPROM settings stay in the Settings dialog and are never changed here.', 'این‌ها ترجیحات برنامهٔ میزبان هستند. تنظیمات EEPROM برد در گفت‌وگوی تنظیمات باقی می‌مانند و اینجا تغییر نمی‌کنند.')}</footer>
    </section>
  </div>
}

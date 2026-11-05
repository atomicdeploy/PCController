import { useEffect, useState } from 'react'
import { FileText, FolderOpen, RotateCcw } from 'lucide-react'
import { rpc } from './api'
import { Button } from './components'
import type { HostUISettings, Locale } from './types'

export function StartupPreferences({ locale }: { locale: Locale }) {
  const [settings, setSettings] = useState<HostUISettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const text = (en: string, fa: string) => locale === 'fa' ? fa : en
  useEffect(() => {
    let active = true
    void rpc<HostUISettings>('controller.ui.config.get').then(value => {
      if (active) setSettings(value)
    }).catch(cause => { if (active) setError(String(cause)) })
    return () => { active = false }
  }, [])
  const change = async (patch: Partial<HostUISettings>) => {
    setBusy(true); setError('')
    try { setSettings(await rpc<HostUISettings>('controller.ui.config.set', patch)) }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  const open = async (target: 'file' | 'folder') => {
    setBusy(true); setError('')
    try { await rpc('controller.ui.config.open', { target }) }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  return <section className="setting-group" aria-label={text('Startup and configuration', 'راه‌اندازی و پیکربندی')}>
    {error && <p role="alert">{error}</p>}
    {settings && <>
      <label className="app-preferences-select"><span>{text('Startup introduction', 'معرفی هنگام آغاز')}</span>
        <select aria-label={text('Startup introduction', 'معرفی هنگام آغاز')} disabled={busy} value={settings.intro_mode || 'auto'} onChange={event => void change({ intro_mode: event.target.value as 'auto' | 'always' | 'skip' })}>
          <option value="auto">{text('Auto — first run', 'خودکار — اولین اجرا')}</option>
          <option value="always">{text('Always', 'همیشه')}</option>
          <option value="skip">{text('Skip', 'رد شدن')}</option>
        </select>
      </label>
      {settings.setup_complete && <Button compact icon={RotateCcw} disabled={busy} onClick={() => void change({ setup_complete: false })}>{text('Reset introduction', 'بازنشانی معرفی')}</Button>}
      {settings.config_file && <code style={{ overflowWrap: 'anywhere' }}>{settings.config_file}</code>}
      {settings.config_open_supported && <div className="button-row">
        <Button compact icon={FileText} disabled={busy} onClick={() => void open('file')}>{text('Open configuration on host', 'باز کردن پیکربندی در میزبان')}</Button>
        <Button compact icon={FolderOpen} disabled={busy} onClick={() => void open('folder')}>{text('Open folder on host', 'باز کردن پوشه در میزبان')}</Button>
      </div>}
    </>}
  </section>
}

import { useEffect, useState, type FormEvent } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { rpc } from './api'
import { Button, StatusBadge, TextField, Toggle } from './components'

type Config = {
  enabled: boolean
  broker?: string
  topic?: string
  username?: string
  password_ref?: string
  ca_file?: string
  telemetry_seconds?: number
  allow_commands: boolean
}
type Settings = { config: Config; status: { available: boolean; enabled: boolean; connected: boolean; error?: string } }

export function MQTTSettings({ locale, connectionEvent }: { locale: string; connectionEvent?: number }) {
  const t = (en: string, fa: string) => locale === 'fa' ? fa : en
  const [saved, setSaved] = useState<Settings>()
  const [draft, setDraft] = useState<Config>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    void rpc<Settings>('controller.mqtt.config.get').then(value => {
      if (active) { setSaved(value); setDraft(value.config) }
    }).catch(() => { /* The local-only surface is absent on older/remote hosts. */ })
    return () => { active = false }
  }, [])
  useEffect(() => {
    if (connectionEvent === undefined) return
    let active = true
    void rpc<Settings>('controller.mqtt.status').then(value => {
      if (active) setSaved(previous => previous && { ...previous, status: value.status })
    }).catch(() => { /* Keep the latest observed broker state. */ })
    return () => { active = false }
  }, [connectionEvent])
  if (!saved?.status.available || !draft) return null

  const update = (change: Partial<Config>) => setDraft({ ...draft, ...change })
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved.config)
  const seconds = draft.telemetry_seconds ?? 0
  const valid = !draft.enabled || Boolean(draft.broker?.trim() && draft.topic?.trim())
  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (busy || !valid || !dirty) return
    setBusy(true)
    setError('')
    try {
      const value = await rpc<Settings>('controller.mqtt.config.set', draft)
      setSaved(value)
      setDraft(value.config)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  const refresh = async () => {
    setBusy(true)
    setError('')
    try {
      const value = await rpc<Settings>('controller.mqtt.status')
      setSaved(value)
      if (!dirty) setDraft(value.config)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  const { status } = saved
  return <form className="advanced-fields" onSubmit={event => void save(event)} aria-label="MQTT">
    <div className="advanced-actions">
      <h3>MQTT</h3>
      {status.enabled && <StatusBadge tone={status.connected ? 'good' : 'warn'}>
        {status.connected ? t('Connected', 'متصل') : t('Disconnected', 'قطع')}
      </StatusBadge>}
    </div>
    <Toggle checked={draft.enabled} onChange={enabled => update({ enabled })} label={t('Enable MQTT', 'فعال‌سازی MQTT')} disabled={busy} />
    {draft.enabled && <>
      <div className="advanced-fields advanced-fields--split">
        <TextField label={t('Broker URL', 'نشانی کارگزار')} dir="ltr" placeholder="mqtts://broker.example:8883" value={draft.broker ?? ''} disabled={busy} required onChange={e => update({ broker: e.target.value.trim() })} />
        <TextField label={t('Topic prefix', 'پیشوند موضوع')} dir="ltr" placeholder="pccontroller/board-name" value={draft.topic ?? ''} disabled={busy} required onChange={e => update({ topic: e.target.value.trim() })} />
      </div>
      <div className="advanced-fields advanced-fields--split">
        <TextField label={t('Broker username', 'نام کاربری کارگزار')} dir="ltr" autoComplete="off" value={draft.username ?? ''} disabled={busy} onChange={e => update({ username: e.target.value, ...(!e.target.value ? { password_ref: '' } : {}) })} />
        {draft.username && <TextField label={t('Password reference', 'مرجع گذرواژه')} dir="ltr" placeholder="os:mqtt/board" value={draft.password_ref ?? ''} disabled={busy} onChange={e => update({ password_ref: e.target.value.trim() })} />}
        <TextField label={t('Telemetry interval · seconds', 'فاصلهٔ تله‌متری · ثانیه')} type="number" min={1} max={3600} value={seconds || ''} placeholder="5" disabled={busy} onChange={e => update({ telemetry_seconds: Number(e.target.value) })} />
        {/^(mqtts|tls):\/\//.test(draft.broker ?? '') && <TextField label={t('Additional CA bundle', 'گواهی CA اضافی')} dir="ltr" value={draft.ca_file ?? ''} disabled={busy} onChange={e => update({ ca_file: e.target.value })} />}
      </div>
      <Toggle checked={draft.allow_commands} onChange={allow_commands => update({ allow_commands })} disabled={busy} label={t('Allow board commands', 'اجازهٔ فرمان‌های برد')}
        detail={draft.allow_commands ? t('Clients with broker write access to this topic can operate outputs.', 'کلاینت‌های دارای دسترسی نوشتن به این موضوع می‌توانند خروجی‌ها را کنترل کنند.') : undefined} />
    </>}
    <div className="advanced-actions">
      {dirty && <Button type="submit" icon={Save} busy={busy} disabled={!valid}>{t('Save MQTT settings', 'ذخیرهٔ تنظیمات MQTT')}</Button>}
      {status.enabled && <Button type="button" icon={RefreshCw} disabled={busy} onClick={() => void refresh()}>{t('Refresh status', 'تازه‌سازی وضعیت')}</Button>}
    </div>
    {(error || status.enabled && status.error) && <p role="alert">{error || status.error}</p>}
  </form>
}

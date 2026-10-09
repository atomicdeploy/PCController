import { useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import {
  ArrowDownToLine,
  Braces,
  Download,
  History,
  Network,
  PackageSearch,
  Send,
  ShieldCheck,
  SquareTerminal,
  TableProperties,
} from 'lucide-react'
import {
  Button,
  Card,
  DataRow,
  EmptyState,
  Icon,
  SectionTitle,
  Segmented,
  StatusBadge,
  TextField,
} from './components'
import { downloadIntegration, integrationFetch } from './api'
import { formatCompact } from './i18n'
import { TypedCollection } from './typed-collection'
import { structuredJSONStringify } from './typed-collection-model'
import type { SharedViewProps } from './views'

type DataWorkspaceTab = 'explore' | 'table' | 'transfer'
const preferredRecordFields = ['id', 'code', 'name', 'title', 'category', 'state', 'value', 'updated_at'] as const

function recordsFromPayload(payload: unknown): Record<string, unknown>[] {
  if (Array.isArray(payload)) {
    return payload.map((value) => value && typeof value === 'object' && !Array.isArray(value)
      ? value as Record<string, unknown>
      : { value })
  }
  if (payload && typeof payload === 'object') {
    const object = payload as Record<string, unknown>
    if (Array.isArray(object.records)) return recordsFromPayload(object.records)
    if (Array.isArray(object.data)) return recordsFromPayload(object.data)
    return [object]
  }
  return []
}

export function DataWorkspaceView({ locale, t }: SharedViewProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [tab, setTab] = useState<DataWorkspaceTab>('explore')
  const [relativePath, setRelativePath] = useState('v1/status')
  const [method, setMethod] = useState<'GET' | 'POST'>('GET')
  const [requestBody, setRequestBody] = useState('{\n  "query": ""\n}')
  const [records, setRecords] = useState<Record<string, unknown>[]>([])
  const [busy, setBusy] = useState(false)
  const [response, setResponse] = useState<unknown>(null)
  const [history, setHistory] = useState<string[]>([])
  const [error, setError] = useState('')

  const runRequest = async () => {
    setBusy(true)
    try {
      let body: string | undefined
      if (method === 'POST') {
        JSON.parse(requestBody)
        body = requestBody
      }
      const value = await integrationFetch<unknown>('datahub', relativePath, { method, body })
      setResponse(value)
      setRecords(recordsFromPayload(value))
      const normalizedPath = relativePath.replace(/^\/+/, '')
      const entry = `${method} /${normalizedPath}`
      setHistory((current) => [entry, ...current.filter((item) => item !== entry)].slice(0, 6))
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : structuredJSONStringify(cause))
    } finally {
      setBusy(false)
    }
  }

  const downloadCurrent = async () => {
    setBusy(true)
    try {
      await downloadIntegration('datahub', relativePath)
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : structuredJSONStringify(cause))
    } finally {
      setBusy(false)
    }
  }

  const recall = (entry: string) => {
    const separator = entry.indexOf(' ')
    setMethod(entry.slice(0, separator) as 'GET' | 'POST')
    setRelativePath(entry.slice(separator + 2))
  }

  return (
    <>
      <SectionTitle
        eyebrow={copy('Local data and streaming', 'داده و جریان محلی')}
        title={t('data')}
        detail={copy('A loopback-only workbench for deliberate data exchange; no service-specific routes or credentials are embedded.', 'میزکاری محدود به شبکهٔ محلی برای تبادل آگاهانهٔ داده؛ هیچ مسیر اختصاصی سرویس یا اطلاعات دسترسی در رابط تعبیه نشده است.')}
        action={<StatusBadge tone={error ? 'warn' : response ? 'good' : 'info'}>{response ? copy('RESPONSE READY', 'پاسخ آماده') : copy('IDLE', 'آماده')}</StatusBadge>}
      />
      <nav className="subnav" aria-label={copy('Data workspace sections', 'بخش‌های فضای داده')}>
        {([
          ['explore', SquareTerminal, copy('Request', 'درخواست')],
          ['table', TableProperties, `${t('records')} · ${formatCompact(locale, records.length)}`],
          ['transfer', ArrowDownToLine, copy('Transfer', 'انتقال')],
        ] as const).map(([value, icon, label]) => (
          <button key={value} className={tab === value ? 'is-active' : ''} onClick={() => setTab(value)}>
            <Icon icon={icon} size={17} />{label}
          </button>
        ))}
      </nav>
      <AnimatePresence initial={false}>
        {error && <motion.div
          key="data-workspace-error"
          className="integration-banner"
          role="alert"
          initial={{ height: 0, marginBottom: 0, opacity: 0, clipPath: 'inset(0 0 100% 0)', filter: 'blur(5px)' }}
          animate={{ height: 'auto', marginBottom: 14, opacity: 1, clipPath: 'inset(0 0 0% 0)', filter: 'blur(0px)' }}
          exit={{ height: 0, marginBottom: 0, opacity: 0, clipPath: 'inset(0 0 100% 0)', filter: 'blur(4px)' }}
          transition={{ duration: .28, ease: [0.22, 1, 0.36, 1] }}
        ><Network size={19} /><div><strong>{t('unavailable')}</strong><p>{error}</p></div></motion.div>}
      </AnimatePresence>

      {tab === 'explore' && (
        <section className="operations-grid">
          <Card icon={Send} iconTone="violet" title={copy('Request composer', 'ساخت درخواست')} eyebrow={copy('Authenticated host route', 'مسیر معتبر میزبان')}>
            <div className="setting-group"><label>{copy('Method', 'روش')}</label><Segmented value={method} label={copy('HTTP method', 'روش HTTP')} options={[{ value: 'GET', label: 'GET' }, { value: 'POST', label: 'POST' }]} onChange={setMethod} /></div>
            <TextField label={copy('Relative data path', 'مسیر نسبی داده')} dir="ltr" spellCheck={false} value={relativePath} placeholder="v1/status" onChange={(event) => setRelativePath(event.target.value)} hint={copy('The upstream origin comes only from persistent host configuration.', 'مبدأ بالادستی فقط از پیکربندی ماندگار میزبان خوانده می‌شود.')} />
            {method === 'POST' && <label className="payload-field"><span>{copy('JSON body', 'بدنهٔ JSON')}</span><textarea dir="ltr" spellCheck={false} value={requestBody} onChange={(event) => setRequestBody(event.target.value)} /></label>}
            <Button tone="primary" icon={Send} busy={busy} disabled={!relativePath.trim()} onClick={() => void runRequest()}>{copy('Send request', 'ارسال درخواست')}</Button>
          </Card>
          <Card icon={Braces} iconTone="accent" title={copy('Response', 'پاسخ')} eyebrow={response === null ? copy('No response', 'بدون پاسخ') : copy(`${records.length} projected records`, `${formatCompact(locale, records.length)} رکورد استخراج‌شده`)} className="operation-result">
            <pre className="operation-output" dir="ltr">{response === null ? copy('Run a request to inspect the response.', 'برای بررسی پاسخ، یک درخواست اجرا کنید.') : typeof response === 'string' ? response : structuredJSONStringify(response, true)}</pre>
          </Card>
          <Card icon={History} iconTone="green" title={copy('Recent local requests', 'درخواست‌های محلی اخیر')} eyebrow={copy(`${history.length} in this tab`, `${formatCompact(locale, history.length)} مورد در این زبانه`)}>
            <div className="command-chips">{history.length ? history.map((entry) => <button key={entry} dir="ltr" onClick={() => recall(entry)}>{entry}</button>) : <p className="card-copy">{copy('No requests in this browser session.', 'در این نشست مرورگر هنوز درخواستی اجرا نشده است.')}</p>}</div>
          </Card>
        </section>
      )}

      {tab === 'table' && (
        <Card icon={TableProperties} iconTone="violet" className="records-card" title={`${t('records')} · ${formatCompact(locale, records.length)}`} eyebrow={copy('Typed local collection', 'مجموعهٔ محلی نوع‌دار')}>
          <TypedCollection
            records={records}
            preferredFields={preferredRecordFields}
            locale={locale}
            direction={locale === 'fa' ? 'rtl' : 'ltr'}
            ariaLabel={copy('Data workspace records', 'رکوردهای فضای داده')}
            filename="data-workspace"
            preserveViewportAnchor
            empty={<EmptyState icon={PackageSearch} title={copy('No tabular records', 'رکورد جدولی موجود نیست')} detail={copy('Run a request to inspect a structured response as a typed collection.', 'یک درخواست اجرا کنید تا پاسخ ساخت‌یافته را به‌صورت مجموعهٔ نوع‌دار بررسی کنید.')} />}
          />
          <footer className="table-footer"><span>{records.length} {copy('records', 'رکورد')}</span><span><ShieldCheck size={15} /> {copy('Credentials stripped', 'اطلاعات دسترسی حذف شده')}</span><span><ArrowDownToLine size={15} /> {t('rangeReady')}</span></footer>
        </Card>
      )}

      {tab === 'transfer' && (
        <section className="system-grid">
          <Card icon={Download} iconTone="accent" title={copy('Streaming download', 'دانلود جریانی')} eyebrow={relativePath.trim() ? `/${relativePath.replace(/^\/+/, '')}` : copy('No path selected', 'مسیری انتخاب نشده')}>{relativePath.trim() && <Button icon={Download} busy={busy} onClick={() => void downloadCurrent()}>{copy('Download current path', 'دانلود مسیر فعلی')}</Button>}</Card>
          <Card icon={ShieldCheck} iconTone="green" title={copy('Boundary status', 'وضعیت مرز امنیتی')} eyebrow={error ? copy('Request rejected', 'درخواست رد شد') : copy('Host mediated', 'با میانجی‌گری میزبان')}><div className="data-list"><DataRow label={copy('Upstream', 'بالادست')} value={copy('Configured loopback root', 'ریشهٔ محلی پیکربندی‌شده')} /><DataRow label={copy('Credentials', 'اطلاعات دسترسی')} value={copy('Removed before forwarding', 'پیش از ارسال حذف می‌شود')} tone="good" /><DataRow label={copy('Redirect selection', 'انتخاب تغییرمسیر')} value={copy('Host-owned', 'در اختیار میزبان')} /><DataRow label={copy('Range requests', 'درخواست‌های بازه‌ای')} value={copy('Preserved', 'حفظ می‌شوند')} tone="good" /></div></Card>
          <Card icon={SquareTerminal} iconTone="violet" title={copy('Current response', 'پاسخ فعلی')} eyebrow={response === null ? copy('No response', 'بدون پاسخ') : typeof response === 'string' ? copy('Text', 'متن') : copy('Structured data', 'دادهٔ ساخت‌یافته')}>{response !== null && <pre className="operation-output" dir="ltr">{typeof response === 'string' ? response : structuredJSONStringify(response, true)}</pre>}</Card>
        </section>
      )}
    </>
  )
}

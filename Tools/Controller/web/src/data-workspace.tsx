import { useMemo, useState } from 'react'
import {
  ArrowDownToLine,
  Download,
  Network,
  PackageSearch,
  Search,
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
import { downloadURL, integrationFetch } from './api'
import { formatCompact } from './i18n'
import type { SharedViewProps } from './views'

type DataWorkspaceTab = 'explore' | 'table' | 'transfer'

function recordsFromPayload(payload: unknown): Record<string, unknown>[] {
  if (Array.isArray(payload)) {
    return payload.filter((value): value is Record<string, unknown> =>
      Boolean(value && typeof value === 'object' && !Array.isArray(value)))
  }
  if (payload && typeof payload === 'object') {
    const object = payload as Record<string, unknown>
    if (Array.isArray(object.records)) return recordsFromPayload(object.records)
    if (Array.isArray(object.data)) return recordsFromPayload(object.data)
  }
  return []
}

export function DataWorkspaceView({ locale, t }: SharedViewProps) {
  const [tab, setTab] = useState<DataWorkspaceTab>('explore')
  const [relativePath, setRelativePath] = useState('v1/status')
  const [method, setMethod] = useState<'GET' | 'POST'>('GET')
  const [requestBody, setRequestBody] = useState('{\n  "query": ""\n}')
  const [records, setRecords] = useState<Record<string, unknown>[]>([])
  const [search, setSearch] = useState('')
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
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  const filtered = useMemo(() => {
    const term = search.trim().toLocaleLowerCase(locale === 'fa' ? 'fa-IR' : 'en-US')
    if (!term) return records.slice(0, 250)
    return records.filter((record) => Object.values(record).some((value) =>
      String(value ?? '').toLocaleLowerCase().includes(term))).slice(0, 250)
  }, [records, search, locale])

  const columns = useMemo(() => {
    const preferred = ['id', 'code', 'name', 'title', 'category', 'state', 'value', 'updated_at']
    const all = new Set<string>()
    filtered.slice(0, 30).forEach((record) => Object.keys(record).forEach((key) => all.add(key)))
    return [
      ...preferred.filter((key) => all.has(key)),
      ...[...all].filter((key) => !preferred.includes(key)),
    ].slice(0, 8)
  }, [filtered])

  const recall = (entry: string) => {
    const separator = entry.indexOf(' ')
    setMethod(entry.slice(0, separator) as 'GET' | 'POST')
    setRelativePath(entry.slice(separator + 2))
  }

  return (
    <>
      <SectionTitle
        eyebrow="LOCAL DATA / STREAMING BRIDGE"
        title={t('data')}
        detail="A loopback-only workbench for deliberate data exchange; no service-specific routes or credentials are embedded."
        action={<StatusBadge tone={error ? 'warn' : response ? 'good' : 'info'}>{response ? 'RESPONSE READY' : 'IDLE'}</StatusBadge>}
      />
      <nav className="subnav" aria-label="Data workspace sections">
        {([
          ['explore', SquareTerminal, 'Request'],
          ['table', TableProperties, `${t('records')} · ${formatCompact(locale, records.length)}`],
          ['transfer', ArrowDownToLine, 'Transfer'],
        ] as const).map(([value, icon, label]) => (
          <button key={value} className={tab === value ? 'is-active' : ''} onClick={() => setTab(value)}>
            <Icon icon={icon} size={17} />{label}
          </button>
        ))}
      </nav>
      {error && <div className="integration-banner"><Network size={19} /><div><strong>{t('unavailable')}</strong><p>{error}</p></div></div>}

      {tab === 'explore' && (
        <section className="operations-grid">
          <Card title="Request composer" eyebrow="SAME-ORIGIN / AUTHENTICATED">
            <div className="setting-group"><label>Method</label><Segmented value={method} label="HTTP method" options={[{ value: 'GET', label: 'GET' }, { value: 'POST', label: 'POST' }]} onChange={setMethod} /></div>
            <TextField label="Relative data path" dir="ltr" spellCheck={false} value={relativePath} placeholder="v1/status" onChange={(event) => setRelativePath(event.target.value)} hint="The upstream origin comes only from persistent host configuration." />
            {method === 'POST' && <label className="payload-field"><span>JSON body</span><textarea dir="ltr" spellCheck={false} value={requestBody} onChange={(event) => setRequestBody(event.target.value)} /></label>}
            <Button tone="primary" icon={Send} busy={busy} disabled={!relativePath.trim()} onClick={() => void runRequest()}>Send request</Button>
          </Card>
          <Card title="Response" eyebrow="BOUNDED JSON / TEXT" className="operation-result">
            <pre className="operation-output" dir="ltr">{response === null ? 'Run a request to inspect the response.' : typeof response === 'string' ? response : JSON.stringify(response, null, 2)}</pre>
          </Card>
          <Card title="Recent local requests" eyebrow="SESSION MEMORY">
            <div className="command-chips">{history.length ? history.map((entry) => <button key={entry} dir="ltr" onClick={() => recall(entry)}>{entry}</button>) : <p className="card-copy">No requests in this browser session.</p>}</div>
          </Card>
        </section>
      )}

      {tab === 'table' && (
        <Card className="records-card" title={`${t('records')} · ${formatCompact(locale, records.length)}`} eyebrow="SCHEMA-FREE PROJECTION" action={<label className="table-search"><Search size={16} /><input value={search} placeholder={t('search')} onChange={(event) => setSearch(event.target.value)} /></label>}>
          {!filtered.length ? <EmptyState icon={PackageSearch} title="No tabular records" detail="Run a request whose response is an array or contains a records/data array." /> : (
            <div className="data-table-wrap"><table className="data-table"><thead><tr><th>#</th>{columns.map((column) => <th key={column}>{column.replaceAll('_', ' ')}</th>)}</tr></thead><tbody>{filtered.map((record, index) => <tr key={String(record.id ?? record.code ?? index)}><td>{index + 1}</td>{columns.map((column) => <td key={column}>{String(record[column] ?? '—')}</td>)}</tr>)}</tbody></table></div>
          )}
          <footer className="table-footer"><span>{filtered.length} / {records.length}</span><span><ShieldCheck size={15} /> Credentials stripped</span><span><ArrowDownToLine size={15} /> {t('rangeReady')}</span></footer>
        </Card>
      )}

      {tab === 'transfer' && (
        <section className="system-grid">
          <Card title="Streaming download" eyebrow="HEAD / RANGE / ETAG"><p className="card-copy">Download the current relative path through the authenticated bridge. The host preserves byte ranges and validators without buffering the payload.</p><a className={`button button--secondary${relativePath.trim() ? '' : ' is-disabled'}`} href={relativePath.trim() ? downloadURL('datahub', relativePath) : undefined}><span className="button__wash" /><Download size={17} />Download current path</a></Card>
          <Card title="Boundary guarantees" eyebrow="FAIL CLOSED"><div className="data-list"><DataRow label="Upstream" value="Configured loopback root" /><DataRow label="Credentials" value="Removed before forwarding" tone="good" /><DataRow label="Redirect selection" value="Host-owned" /><DataRow label="Range requests" value="Preserved" tone="good" /></div></Card>
          <Card title="Current response" eyebrow="SOURCE-BACKED"><pre className="operation-output" dir="ltr">{response === null ? 'No response loaded.' : typeof response === 'string' ? response : JSON.stringify(response, null, 2)}</pre></Card>
        </section>
      )}
    </>
  )
}

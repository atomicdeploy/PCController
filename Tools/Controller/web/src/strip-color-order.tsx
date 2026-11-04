import { useEffect, useId, useRef, useState } from 'react'
import { execute } from './api'
import type { Locale } from './types'

interface StripOrder { color_order: string; firmware_order: string; orders: string[] }

export function StripOrderSelect({ value, busy, title, onChange }: { value: StripOrder | null; busy: boolean; title: string; onChange: (order: string) => void }) {
  const id = useId()
  return <div className="text-field"><label htmlFor={id}>{title}</label>
    <select id={id} aria-label={title} dir="ltr" disabled={busy || !value?.orders} value={value?.color_order || ''} onChange={event => onChange(event.target.value)}>
      {!value?.orders && <option value="">—</option>}
      {value?.orders?.map(order => <option key={order} value={order}>{order}</option>)}
    </select>
  </div>
}

export function StripColorOrder({ locale, revision = 0 }: { locale: Locale; revision?: number }) {
  const [value, setValue] = useState<StripOrder | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)
  const mounted = useRef(false)
  const title = locale === 'fa' ? 'ترتیب رنگ پیکسل' : 'Pixel color order'
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++ } }, [])
  useEffect(() => {
    if (busy) return
    const current = ++generation.current
    const abort = new AbortController()
    void execute('strip order', abort.signal).then(result => {
      if (mounted.current && current === generation.current) {
        setValue(JSON.parse(result.output || '{}') as StripOrder); setError('')
      }
    }).catch(cause => { if (!abort.signal.aborted && current === generation.current) setError(String(cause)) })
    return () => abort.abort()
  }, [revision, busy])
  const change = async (order: string) => {
    const current = ++generation.current
    setBusy(true); setError('')
    try {
      const result = await execute(`strip order ${order}`)
      if (mounted.current && current === generation.current) setValue(JSON.parse(result.output || '{}') as StripOrder)
    } catch (cause) {
      if (mounted.current && current === generation.current) setError(String(cause))
    } finally { if (mounted.current) setBusy(false) }
  }
  return <div>
    <StripOrderSelect value={value} busy={busy} title={title} onChange={order => void change(order)} />
    {error && <p role="alert">{error}</p>}
  </div>
}

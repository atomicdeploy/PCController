// Isolated visual QA only. No controller transport, mutation or hardware access.
import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { UpdateOperationPanel } from '../src/update-operation-panel'
import type { UpdateStatus } from '../src/updates-api'
import '../src/styles.css'

function Preview() {
  const [locale, setLocale] = useState<'en' | 'fa'>('en')
  const [theme, setTheme] = useState('dark')
  const [state, setState] = useState('failed')
  useEffect(() => { document.documentElement.dataset.theme = theme }, [theme])
  const operation: UpdateStatus = { id: 'visual-fixture', kind: 'firmware', state: state as UpdateStatus['state'],
    stage: state === 'failed' ? 'preflight' : state === 'writing' ? 'flash write:writing' : 'reconnect',
    progress_percent: 42, progress_known: state === 'writing', programming_method: 'urclock',
    started_at: '2026-09-28T22:46:37Z', updated_at: '2026-09-28T22:47:12Z',
    detail: state === 'failed' ? 'AVRDUDE could not be found in the configured Arduino data directory. Check programming.toolchain_config or set programming.avrdude and programming.avrdude_conf to your installed toolchain.' : state === 'writing' ? 'Writing flash' : 'Waiting for the board to reconnect and answer HELLO',
    error_code: state === 'failed' ? 'toolchain_unavailable' : undefined }
  if (state !== 'failed') operation.started_at = new Date().toISOString()
  return <main data-theme={theme} dir={locale === 'fa' ? 'rtl' : 'ltr'} style={{ minHeight: '100vh', background: 'var(--bg)', padding: '24px', fontFamily: locale === 'fa' ? 'var(--font-fa)' : 'var(--font-ui)' }}>
    <div style={{ maxWidth: 940, margin: '0 auto' }}>
      <p style={{ color: 'var(--text-muted)', marginBottom: 20 }}>VISUAL QA FIXTURE — simulated states, no hardware operation</p>
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 24 }}>
        <label>State <select aria-label="State" value={state} onChange={e => setState(e.target.value)}><option value="failed">Failed preflight</option><option value="writing">Measured write</option><option value="reconnecting">Unknown-duration reconnect</option></select></label>
        <label>Language <select aria-label="Language" value={locale} onChange={e => setLocale(e.target.value as 'en' | 'fa')}><option value="en">English</option><option value="fa">فارسی</option></select></label>
        <label>Theme <select aria-label="Theme" value={theme} onChange={e => setTheme(e.target.value)}><option value="dark">Dark</option><option value="light">Light</option></select></label>
      </div>
      <UpdateOperationPanel status={operation} locale={locale} />
    </div>
  </main>
}
createRoot(document.getElementById('root')!).render(<Preview />)

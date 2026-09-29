import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Clock3, LoaderCircle, TerminalSquare } from 'lucide-react'
import type { UpdateStatus } from './updates-api'

const terminalStates = new Set(['completed', 'failed', 'cancelled', 'downloaded', 'staged'])

export function updateIsRunning(status: UpdateStatus | null | undefined): boolean {
  return !!status?.state && !terminalStates.has(status.state)
}

export function measuredUpdateProgress(status: UpdateStatus | null | undefined): number | null {
  if (!updateIsRunning(status) || !status?.progress_known || !Number.isFinite(status.progress_percent)) return null
  return Math.max(0, Math.min(100, status.progress_percent))
}

export function updateElapsed(status: UpdateStatus, now: number): string | null {
  const start = Date.parse(status.started_at ?? '')
  const end = updateIsRunning(status) ? now : Date.parse(status.updated_at ?? '')
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return null
  const seconds = Math.floor((end - start) / 1000)
  return seconds >= 60 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${seconds}s`
}

const stageLabels: Record<string, [string, string]> = {
  queued: ['Queued', 'در صف'], preflight: ['Checking tools & target', 'بررسی ابزارها و مقصد'],
  preparing: ['Preparing board', 'آماده‌سازی برد'], 'backing-up': ['Saving board settings', 'ذخیره تنظیمات برد'],
  reading: ['Reading memory', 'خواندن حافظه'], writing: ['Writing memory', 'نوشتن حافظه'],
  programming: ['Programming board', 'پروگرام برد'], erasing: ['Erasing memory', 'پاک کردن حافظه'],
  verifying: ['Verifying memory', 'تأیید حافظه'], reconnecting: ['Reconnecting board', 'اتصال دوباره برد'],
  restoring: ['Restoring settings', 'بازیابی تنظیمات'], downloading: ['Downloading artifact', 'دریافت فایل'],
  staging: ['Preparing host replacement', 'آماده‌سازی جایگزین میزبان'], staged: ['Host replacement ready', 'جایگزین میزبان آماده است'],
  prepare: ['Preparing board', 'آماده‌سازی برد'], settings: ['Reading settings', 'خواندن تنظیمات'],
  release: ['Releasing serial port', 'آزاد کردن درگاه سریال'], backup: ['Saving board backup', 'ذخیره پشتیبان برد'],
  flash: ['Programming board', 'پروگرام برد'], reconnect: ['Reconnecting board', 'اتصال دوباره برد'],
  restore: ['Restoring settings', 'بازیابی تنظیمات'],
  'flash read': ['Reading flash', 'خواندن فلش'], 'EEPROM read': ['Reading EEPROM', 'خواندن EEPROM'],
  'flash write': ['Writing flash', 'نوشتن فلش'], 'flash verification': ['Verifying flash', 'تأیید فلش'],
}

/** Current measured stage, not an artificial whole-update progress estimate. */
export function UpdateOperationPanel({ status, locale }: { status: UpdateStatus; locale: 'en' | 'fa' }) {
  const copy = (en: string, fa: string) => locale === 'fa' ? fa : en
  const running = updateIsRunning(status)
  const percent = measuredUpdateProgress(status)
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    if (!running) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [running])
  const elapsed = updateElapsed(status, now)
  const failed = status.state === 'failed' || status.state === 'cancelled'
  const stage = status.stage || (running ? status.state : '')
  const stageName = stage.split(':')[0]
  const stageLabel = stageLabels[stageName]?.[locale === 'fa' ? 1 : 0] || stageName.replaceAll('-', ' ')
  const title = failed ? copy('Update stopped', 'به‌روزرسانی متوقف شد')
    : running ? stageLabel : copy('Operation complete', 'عملیات کامل شد')
  const Icon = failed ? AlertTriangle : running ? LoaderCircle : CheckCircle2
  return <section className={`update-operation ${failed ? 'is-failed' : running ? 'is-running' : 'is-complete'}`} aria-label={copy('Update operation', 'عملیات به‌روزرسانی')}>
    <header className="update-operation__header">
      <div className="update-operation__icon"><Icon className={running ? 'spin' : undefined} /></div>
      <div className="update-operation__heading"><span>{status.kind === 'firmware' ? copy('Board firmware', 'میان‌افزار برد') : status.kind.replaceAll('-', ' ')}</span><h2>{title}</h2></div>
      {elapsed && <span className="update-operation__elapsed"><Clock3 size={16} /><b>{elapsed}</b></span>}
    </header>
    {running && <div className="update-operation__measurement">
      <div><span>{percent === null ? copy('Waiting for this stage', 'در انتظار این مرحله') : copy('Current stage', 'مرحله جاری')}</span><strong>{percent === null ? '…' : `${percent}%`}</strong></div>
      <div key={stage} className={`update-progress${percent === null ? ' is-indeterminate' : ''}`} role="progressbar" aria-label={stageLabel} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent ?? undefined} aria-valuetext={percent === null ? copy('In progress; duration unknown', 'در حال اجرا؛ زمان نامشخص') : undefined}><i style={percent === null ? undefined : { width: `${percent}%` }} /></div>
    </div>}
    {status.detail && <div className="update-operation__detail" role={failed ? 'alert' : undefined}>{status.detail}</div>}
    <dl className="update-operation__facts">
      {failed && stageLabel && <div><dt>{copy('Stopped during', 'مرحله توقف')}</dt><dd>{stageLabel}</dd></div>}
      {status.programming_method && status.programming_method !== 'none' && <div><dt>{copy('Connection', 'اتصال')}</dt><dd>{status.programming_method === 'urclock' ? 'UART / Urclock' : 'USBasp / ISP'}</dd></div>}
      {!!status.bytes_total && <div><dt>{copy('Transferred', 'انتقال داده')}</dt><dd><bdi>{status.bytes_done ?? 0} / {status.bytes_total} B</bdi></dd></div>}
      {status.updated_at && <div><dt>{copy('Last activity', 'آخرین فعالیت')}</dt><dd>{new Date(status.updated_at).toLocaleTimeString(locale)}</dd></div>}
    </dl>
    <details className="update-operation__diagnostics"><summary><TerminalSquare size={16} />{copy('Technical details', 'جزئیات فنی')}</summary><dl>
      <div><dt>{copy('Operation', 'عملیات')}</dt><dd><code>{status.id}</code></dd></div>
      {status.error_code && <div><dt>{copy('Error', 'خطا')}</dt><dd><code>{status.error_code}</code></dd></div>}
      {status.artifact_sha256 && <div><dt>SHA-256</dt><dd><code>{status.artifact_sha256}</code></dd></div>}
    </dl></details>
  </section>
}

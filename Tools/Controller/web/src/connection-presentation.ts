import type { Locale, Snapshot } from './types'

export type ConnectionTone = 'good' | 'info' | 'warn' | 'bad' | 'neutral'

export interface ConnectionPresentation {
  phase: NonNullable<Snapshot['connection_phase']>
  title: string
  detail: string
  candidate: string
  timing: string
  attempt: string
  action: string
  retryDisabled: boolean
  tone: ConnectionTone
  animated: boolean
}

export function hardwareResetAvailable(snapshot: Pick<Snapshot, 'reset_lines_available'>): boolean {
  return snapshot.reset_lines_available === true
}

function parsedTime(value?: string): number | undefined {
  if (!value) return undefined
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

function duration(locale: Locale, milliseconds: number): string {
  const seconds = Math.max(0, milliseconds) / 1000
  return new Intl.NumberFormat(locale === 'fa' ? 'fa-IR' : 'en', {
    maximumFractionDigits: seconds < 10 ? 1 : 0,
  }).format(seconds) + (locale === 'fa' ? ' ثانیه' : 's')
}

function actualPhase(snapshot: Snapshot): NonNullable<Snapshot['connection_phase']> {
  if (snapshot.connection_phase) return snapshot.connection_phase
  if (snapshot.connected) return 'connected'
  if (snapshot.paused) return 'paused'
  if (snapshot.connection_state === 'reconnecting' || snapshot.connection_state === 'connecting') return 'queued'
  return 'disconnected'
}

export function connectionPresentation(snapshot: Snapshot, locale: Locale, now = Date.now()): ConnectionPresentation {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const phase = actualPhase(snapshot)
  const candidateInfo = snapshot.connection_candidate
  const candidate = candidateInfo?.friendly_name || candidateInfo?.product || candidateInfo?.name || snapshot.port.friendly_name || snapshot.port.product || snapshot.port.name || ''
  const attemptNumber = snapshot.connection_attempt ?? 0
  const attempt = attemptNumber > 0
    ? copy(`Attempt ${attemptNumber}`, `تلاش ${new Intl.NumberFormat('fa-IR').format(attemptNumber)}`)
    : ''
  const started = parsedTime(snapshot.connection_attempt_started)
  const retryAt = parsedTime(snapshot.connection_next_retry)
  const reason = snapshot.connection_reason?.trim() || ''

  if (phase === 'connected') return {
    phase, title: copy('Board connected', 'برد متصل است'), detail: candidate,
    candidate, timing: '', attempt: '', action: copy('Reconnect', 'اتصال دوباره'),
    retryDisabled: false, tone: 'good', animated: false,
  }
  if (phase === 'attempting') return {
    phase, title: copy('Contacting controller board', 'در حال تماس با برد کنترلر'),
    detail: candidate
      ? copy(`Authenticating the application protocol on ${candidate}.`, `در حال احراز پروتکل برنامه روی ${candidate}.`)
      : copy('Discovering a compatible controller and authenticating its application protocol.', 'در حال یافتن کنترلر سازگار و احراز پروتکل برنامهٔ آن.'),
    candidate,
    timing: started === undefined ? '' : copy(`Elapsed ${duration(locale, now - started)}`, `گذشته ${duration(locale, now - started)}`),
    attempt,
    action: copy('Attempt in progress', 'تلاش در حال انجام است'), retryDisabled: true,
    tone: 'info', animated: true,
  }
  if (phase === 'waiting_retry') return {
    phase, title: copy('Board did not answer', 'برد پاسخ نداد'),
    detail: reason || copy('The serial device is present, but the application handshake did not complete.', 'دستگاه سریال حاضر است، اما دست‌دهی برنامه کامل نشد.'),
    candidate,
    timing: retryAt === undefined
      ? ''
      : retryAt > now
        ? copy(`Next automatic attempt in ${duration(locale, retryAt - now)}`, `تلاش خودکار بعدی تا ${duration(locale, retryAt - now)}`)
        : copy('Next automatic attempt is due now', 'زمان تلاش خودکار بعدی رسیده است'),
    attempt,
    action: copy('Try now', 'اکنون تلاش کن'), retryDisabled: false,
    tone: 'warn', animated: false,
  }
  if (phase === 'queued') return {
    phase, title: copy('Reconnect queued', 'اتصال دوباره در صف است'),
    detail: reason || copy('The host is preparing a bounded connection attempt.', 'میزبان در حال آماده‌سازی یک تلاش اتصال محدود است.'),
    candidate, timing: '', attempt,
    action: copy('Try now', 'اکنون تلاش کن'), retryDisabled: false,
    tone: 'info', animated: false,
  }
  if (phase === 'paused') return {
    phase, title: copy('Board connection is closed', 'اتصال برد بسته است'),
    detail: reason || copy('Automatic reconnect is paused by the operator.', 'اتصال خودکار توسط کاربر متوقف شده است.'),
    candidate, timing: '', attempt: '',
    action: copy('Resume connection', 'ازسرگیری اتصال'), retryDisabled: false,
    tone: 'neutral', animated: false,
  }
  if (phase === 'blocked') return {
    phase, title: copy('Serial cleanup needs attention', 'پاک‌سازی درگاه سریال نیاز به بررسی دارد'),
    detail: reason || copy('The previous serial handle could not be released safely.', 'درگاه سریال قبلی با اطمینان آزاد نشد.'),
    candidate, timing: '', attempt,
    action: copy('Retry cleanup', 'تلاش دوباره برای پاک‌سازی'), retryDisabled: false,
    tone: 'bad', animated: false,
  }
  return {
    phase, title: copy('Controller board disconnected', 'برد کنترلر قطع است'),
    detail: reason || copy('No authenticated controller application is connected.', 'هیچ برنامهٔ کنترلر احرازشده‌ای متصل نیست.'),
    candidate, timing: '', attempt,
    action: copy('Connect', 'اتصال'), retryDisabled: false,
    tone: 'bad', animated: false,
  }
}

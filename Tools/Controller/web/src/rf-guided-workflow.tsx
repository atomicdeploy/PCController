import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { AnimatePresence, motion } from 'motion/react'
import {
  Antenna,
  Check,
  CheckCircle2,
  ListRestart,
  Pencil,
  Radio,
  RefreshCw,
  ScanSearch,
  Send,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react'
import { rpc } from './api'
import { Button, Card, KeyCombo, Segmented, StatusBadge } from './components'
import type { ControllerEvent, DialogState, Locale, RFLearnedEntry, Snapshot } from './types'

export const RF_GUIDE_LABELS = ['A', 'B', 'C', 'D'] as const
export type RFGuideLabel = typeof RF_GUIDE_LABELS[number]
type RFGuidePhase = 'idle' | 'capturing' | 'resolving' | 'identity' | 'mapping' | 'saving' | 'complete' | 'interrupted'
type RFMapAction = 'key' | 'menu' | 'relay' | 'side' | 'pwm' | 'none'

interface RFMapDraft {
  action: RFMapAction
  target: string
  behavior: string
}

interface RFGuidedWorkflowProps {
  snapshot: Snapshot
  events: ControllerEvent[]
  locale: Locale
  openDialog: (dialog: Omit<DialogState, 'open'>) => void
}

const captureKinds = new Set(['rf.learn.mapping-required', 'rf.learn.capture'])
const terminalLearnKinds = new Set(['rf.learn.ended', 'rf.learn.cancelled', 'rf.learn.full'])

export function isRFGuidedCaptureEvent(event: ControllerEvent, afterID = 0): boolean {
  return event.id > afterID && captureKinds.has(event.kind.trim().toLowerCase()) && Number.isInteger(event.rf_id)
}

export function stableRFIdentity(entry: Pick<RFLearnedEntry, 'code' | 'bits' | 'protocol'>): string {
  return `${entry.code}:${entry.bits}:${entry.protocol}`
}

export function rfEntryNeedsReview(entry: RFLearnedEntry, entries: readonly RFLearnedEntry[]): boolean {
  if (entry.action_kind === 0) return true
  const identity = stableRFIdentity(entry)
  const first = entries.find((candidate) => stableRFIdentity(candidate) === identity)
  return first?.id !== entry.id
}

export function defaultRFMapDraft(entry?: Pick<RFLearnedEntry, 'action_kind' | 'action_value' | 'behavior'>): RFMapDraft {
  // A fresh capture stays Unmapped. Only an authoritative board mapping is carried forward.
  return entry?.action_kind ? mappingDraftForEntry(entry) : { action: 'none', target: '', behavior: '' }
}

export function rfMapRPCParams(id: number, draft: RFMapDraft): Record<string, string | number> {
  if (draft.action === 'none') return { id, action: 'none' }
  if (draft.action === 'menu') return { id, action: 'menu', target: draft.target }
  return { id, action: draft.action, target: draft.target, behavior: draft.behavior }
}

function mappingDraftForEntry(entry: Pick<RFLearnedEntry, 'action_kind' | 'action_value' | 'behavior'>): RFMapDraft {
  const behaviors = ['press', 'toggle', 'momentary', 'up', 'down', 'stop']
  const behavior = behaviors[entry.behavior] ?? 'press'
  switch (entry.action_kind) {
    case 1: return { action: 'key', target: String(entry.action_value + 1), behavior }
    case 2: return { action: 'menu', target: ['prev', 'next', 'dec', 'inc'][entry.action_value] ?? 'next', behavior: '' }
    case 3: return { action: 'relay', target: String(entry.action_value + 1), behavior }
    case 4: return { action: 'side', target: entry.action_value === 1 ? 'right' : 'left', behavior }
    case 5: return { action: 'pwm', target: String(entry.action_value), behavior }
    default: return { action: 'none', target: '', behavior: '' }
  }
}

export function rfMapDraftIsComplete(draft: RFMapDraft): boolean {
  if (draft.action === 'none') return true
  if (!draft.target) return false
  return draft.action === 'menu' || Boolean(draft.behavior)
}

function mappingSummary(entry: RFLearnedEntry, copy: (english: string, persian: string) => string): string {
  const behavior = ['press', 'toggle', 'momentary', 'up', 'down', 'stop'][entry.behavior] ?? `#${entry.behavior}`
  switch (entry.action_kind) {
    case 1: return `${copy('Key', 'کلید')} ${entry.action_value + 1} · ${behavior}`
    case 2: return `${copy('Menu', 'منو')} · ${['prev', 'next', 'dec', 'inc'][entry.action_value] ?? `#${entry.action_value}`}`
    case 3: return `${copy('Relay', 'رله')} ${entry.action_value + 1} · ${behavior}`
    case 4: return `${copy('Motion', 'حرکت')} ${entry.action_value === 1 ? 'B' : 'A'} · ${behavior}`
    case 5: return `PWM ${entry.action_value} · ${behavior}`
    default: return copy('Unmapped', 'بدون نگاشت')
  }
}

function identityCode(entry: RFLearnedEntry): string {
  return entry.code_display || `0x${entry.code.toString(16).toUpperCase().padStart(8, '0')}`
}

function mappingTargets(action: RFMapAction, copy: (english: string, persian: string) => string): Array<{ value: string; label: string }> {
  switch (action) {
    case 'key': return [1, 2, 3, 4].map((value) => ({ value: String(value), label: `${copy('Key', 'کلید')} ${value}` }))
    case 'menu': return ['prev', 'next', 'dec', 'inc'].map((value) => ({ value, label: value.toUpperCase() }))
    case 'relay': return [5, 6, 7, 8].map((value) => ({ value: String(value), label: `${copy('Relay', 'رله')} ${value}` }))
    case 'side': return [{ value: 'left', label: copy('Motion side A', 'سمت حرکتی A') }, { value: 'right', label: copy('Motion side B', 'سمت حرکتی B') }]
    case 'pwm': return Array.from({ length: 11 }, (_, value) => ({ value: String(value), label: `PWM ${value}` }))
    default: return []
  }
}

function mappingBehaviors(action: RFMapAction, copy: (english: string, persian: string) => string): Array<{ value: string; label: string }> {
  if (action === 'side') return [
    { value: 'up', label: copy('Up', 'بالا') },
    { value: 'down', label: copy('Down', 'پایین') },
    { value: 'stop', label: copy('Stop', 'توقف') },
  ]
  if (action === 'menu' || action === 'none') return []
  return [
    { value: 'press', label: copy('Press', 'فشار') },
    { value: 'toggle', label: copy('Toggle', 'تغییر وضعیت') },
    { value: 'momentary', label: copy('Momentary', 'لحظه‌ای') },
  ]
}

export function RFGuidedWorkflow({ snapshot, events, locale, openDialog }: RFGuidedWorkflowProps) {
  const isPersian = locale === 'fa'
  const copy = useCallback((english: string, persian: string) => isPersian ? persian : english, [isPersian])
  const [phase, setPhase] = useState<RFGuidePhase>('idle')
  const [activeIndex, setActiveIndex] = useState(0)
  const [captures, setCaptures] = useState<Array<RFLearnedEntry | null>>([null, null, null, null])
  const [candidate, setCandidate] = useState<RFLearnedEntry | null>(null)
  const [candidateIsGuided, setCandidateIsGuided] = useState(false)
  const [records, setRecords] = useState<RFLearnedEntry[]>([])
  const [draft, setDraft] = useState<RFMapDraft>(() => defaultRFMapDraft())
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState(() => copy('Choose button A to begin.', 'برای شروع دکمه A را انتخاب کنید.'))
  const captureAfterID = useRef(0)
  const handledCaptureIDs = useRef(new Set<number>())
  const startedByView = useRef(false)
  const ignoreStaleActiveSnapshot = useRef(false)
  const mounted = useRef(true)
  const connected = useRef(snapshot.connected)

  const loadRecords = useCallback(async () => {
    const value = await rpc<RFLearnedEntry[]>('controller.rf.list')
    if (mounted.current) setRecords(value)
    return value
  }, [])

  useEffect(() => { connected.current = snapshot.connected }, [snapshot.connected])

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      if (startedByView.current && connected.current) void rpc('controller.rf.learn.cancel').catch(() => undefined)
    }
  }, [])

  useEffect(() => {
    if (snapshot.connected) {
      void loadRecords().catch((cause) => {
        if (mounted.current) setMessage(copy(`Inventory unavailable: ${cause instanceof Error ? cause.message : String(cause)}`, `فهرست در دسترس نیست: ${cause instanceof Error ? cause.message : String(cause)}`))
      })
    }
  }, [copy, loadRecords, snapshot.connected])

  useEffect(() => {
    if (snapshot.connected || phase === 'idle' || phase === 'complete' || phase === 'interrupted') return
    startedByView.current = false
    setBusy('')
    setPhase('interrupted')
    setMessage(copy('Capture paused because the controller disconnected. Reconnect before retrying.', 'به‌دلیل قطع ارتباط کنترلر، دریافت متوقف شد. پس از اتصال دوباره تلاش کنید.'))
  }, [copy, phase, snapshot.connected])

  const resolveCapture = useCallback(async (id: number) => {
    setPhase('resolving')
    setMessage(copy(`Entry ${id} received. Verifying its exact identity…`, `ورودی ${id} دریافت شد؛ هویت دقیق آن در حال بررسی است…`))
    if (startedByView.current) {
      try { await rpc('controller.rf.learn.cancel') } catch { /* The list read below remains authoritative. */ }
      startedByView.current = false
      ignoreStaleActiveSnapshot.current = true
    }
    try {
      let value = await loadRecords()
      let entry = value.find((record) => record.id === id)
      if (!entry) {
        await new Promise((resolve) => window.setTimeout(resolve, 160))
        value = await loadRecords()
        entry = value.find((record) => record.id === id)
      }
      if (!entry) throw new Error(copy('captured entry was not present in board readback', 'ورودی دریافت‌شده در بازخوانی برد وجود نداشت'))
      if (!mounted.current) return
      setCandidate(entry)
      setCandidateIsGuided(true)
      setPhase('identity')
      setMessage(copy(`Confirm that ${identityCode(entry)} is handset button ${RF_GUIDE_LABELS[activeIndex]}.`, `تأیید کنید ${identityCode(entry)} همان دکمه ${RF_GUIDE_LABELS[activeIndex]} فرستنده است.`))
    } catch (cause) {
      if (!mounted.current) return
      setPhase('interrupted')
      setMessage(copy(`Identity verification failed: ${cause instanceof Error ? cause.message : String(cause)}`, `بررسی هویت ناموفق بود: ${cause instanceof Error ? cause.message : String(cause)}`))
    }
  }, [activeIndex, copy, loadRecords])

  useEffect(() => {
    if (phase !== 'capturing') return
    const captured = events.find((event) => isRFGuidedCaptureEvent(event, captureAfterID.current) && !handledCaptureIDs.current.has(event.id))
    if (!captured || captured.rf_id === undefined) return
    handledCaptureIDs.current.add(captured.id)
    void resolveCapture(captured.rf_id)
  }, [events, phase, resolveCapture])

  useEffect(() => {
    if (phase !== 'capturing') return
    const ended = events.find((event) => event.id > captureAfterID.current && terminalLearnKinds.has(event.kind.trim().toLowerCase()))
    if (!ended) return
    startedByView.current = false
    ignoreStaleActiveSnapshot.current = true
    setPhase('interrupted')
    setMessage(ended.kind.toLowerCase().includes('full')
      ? copy('RF storage is full. Review stale records before capturing again.', 'حافظه RF پر است؛ پیش از دریافت دوباره، ورودی‌های قدیمی را بازبینی کنید.')
      : copy('No button was confirmed in this capture window. Retry when the handset is ready.', 'در این بازه دکمه‌ای تأیید نشد؛ وقتی فرستنده آماده است دوباره تلاش کنید.'))
  }, [copy, events, phase])

  const chooseStep = (index: number) => {
    if (phase === 'capturing' || phase === 'resolving' || phase === 'saving') return
    setActiveIndex(index)
    setCandidate(null)
    setCandidateIsGuided(false)
    setDraft(defaultRFMapDraft())
    setPhase(captures[index] ? 'complete' : 'idle')
    setMessage(captures[index]
      ? captures[index]?.action_kind === 0
        ? copy(`Button ${RF_GUIDE_LABELS[index]} is captured and remains Unmapped.`, `دکمه ${RF_GUIDE_LABELS[index]} دریافت شده و بدون نگاشت باقی مانده است.`)
        : copy(`Button ${RF_GUIDE_LABELS[index]} has an explicit mapping. You can review or remap it.`, `دکمه ${RF_GUIDE_LABELS[index]} نگاشت صریح دارد و قابل بازبینی یا نگاشت دوباره است.`)
      : copy(`Button ${RF_GUIDE_LABELS[index]} is ready to capture.`, `دکمه ${RF_GUIDE_LABELS[index]} آماده دریافت است.`))
  }

  const startCapture = async () => {
    if (!snapshot.connected || busy) return
    if (snapshot.rf_learning?.active && !startedByView.current && !ignoreStaleActiveSnapshot.current) {
      setPhase('interrupted')
      setMessage(copy('Another RF learning session is active. Cancel it explicitly before starting the guided capture.', 'یک نشست یادگیری RF دیگر فعال است؛ پیش از شروع دریافت هدایت‌شده آن را صریحاً لغو کنید.'))
      return
    }
    setBusy('capture')
    setCandidate(null)
    setCandidateIsGuided(false)
    captureAfterID.current = events.reduce((maximum, event) => Math.max(maximum, event.id), 0)
    try {
      await rpc('controller.rf.learn.start', { mode: 'timer', timeout_ms: 30_000 })
      ignoreStaleActiveSnapshot.current = false
      startedByView.current = true
      setPhase('capturing')
      setMessage(copy(`Press handset button ${RF_GUIDE_LABELS[activeIndex]} once. Other buttons are ignored until you confirm the identity.`, `دکمه ${RF_GUIDE_LABELS[activeIndex]} فرستنده را یک‌بار فشار دهید؛ تا پیش از تأیید هویت، دکمه دیگری را فشار ندهید.`))
    } catch (cause) {
      setPhase('interrupted')
      setMessage(copy(`Capture could not start: ${cause instanceof Error ? cause.message : String(cause)}`, `دریافت آغاز نشد: ${cause instanceof Error ? cause.message : String(cause)}`))
    } finally {
      setBusy('')
    }
  }

  const cancelCapture = async () => {
    if (busy) return
    setBusy('cancel')
    try {
      if (snapshot.connected && (startedByView.current || snapshot.rf_learning?.active)) await rpc('controller.rf.learn.cancel')
      startedByView.current = false
      ignoreStaleActiveSnapshot.current = true
      setCandidate(null)
      setCandidateIsGuided(false)
      setPhase('idle')
      setMessage(copy(`Capture for button ${RF_GUIDE_LABELS[activeIndex]} was cancelled. No mapping changed.`, `دریافت دکمه ${RF_GUIDE_LABELS[activeIndex]} لغو شد و هیچ نگاشتی تغییر نکرد.`))
    } catch (cause) {
      setPhase('interrupted')
      setMessage(copy(`Cancellation was not acknowledged: ${cause instanceof Error ? cause.message : String(cause)}`, `لغو تأیید نشد: ${cause instanceof Error ? cause.message : String(cause)}`))
    } finally {
      setBusy('')
    }
  }

  const confirmIdentity = () => {
    if (!candidate) return
    setDraft(defaultRFMapDraft(candidate))
    setPhase('mapping')
    setMessage(candidate.action_kind === 0
      ? copy(`Identity confirmed for button ${RF_GUIDE_LABELS[activeIndex]}. It remains Unmapped unless you explicitly choose an action.`, `هویت دکمه ${RF_GUIDE_LABELS[activeIndex]} تأیید شد؛ تا زمانی که صریحاً عملی انتخاب نکنید، بدون نگاشت می‌ماند.`)
      : copy(`Identity confirmed for button ${RF_GUIDE_LABELS[activeIndex]}. Its existing board mapping is preserved for review.`, `هویت دکمه ${RF_GUIDE_LABELS[activeIndex]} تأیید شد؛ نگاشت موجود برد برای بازبینی حفظ شده است.`))
  }

  const saveMapping = async () => {
    if (!candidate || !snapshot.connected || busy) return
    setBusy('mapping')
    setPhase('saving')
    try {
      const value = await rpc<RFLearnedEntry[]>('controller.rf.map', rfMapRPCParams(candidate.id, draft))
      const updated = value.find((entry) => entry.id === candidate.id) ?? { ...candidate }
      setRecords(value)
      if (candidateIsGuided) {
        const nextCaptures = [...captures]
        nextCaptures[activeIndex] = updated
        setCaptures(nextCaptures)
        const next = nextCaptures.findIndex((entry, index) => !entry && index > activeIndex)
        const wrapped = next >= 0 ? next : nextCaptures.findIndex((entry) => !entry)
        if (wrapped >= 0) {
          setActiveIndex(wrapped)
          setDraft(defaultRFMapDraft())
          setPhase('idle')
          setMessage(updated.action_kind === 0
            ? copy(`Button ${RF_GUIDE_LABELS[activeIndex]} was captured and kept Unmapped. Continue with button ${RF_GUIDE_LABELS[wrapped]}.`, `دکمه ${RF_GUIDE_LABELS[activeIndex]} دریافت شد و بدون نگاشت ماند؛ با دکمه ${RF_GUIDE_LABELS[wrapped]} ادامه دهید.`)
            : copy(`Button ${RF_GUIDE_LABELS[activeIndex]} mapping was saved. Continue with button ${RF_GUIDE_LABELS[wrapped]}.`, `نگاشت دکمه ${RF_GUIDE_LABELS[activeIndex]} ذخیره شد؛ با دکمه ${RF_GUIDE_LABELS[wrapped]} ادامه دهید.`))
        } else {
          setPhase('complete')
          setMessage(copy('A/B/C/D capture is complete. Review the inventory and test only the intended signal.', 'دریافت A/B/C/D کامل شد؛ فهرست را بازبینی و فقط سیگنال موردنظر را آزمایش کنید.'))
        }
      } else {
        setPhase('idle')
        setMessage(copy(`Entry ${candidate.id} was remapped and verified by board readback.`, `ورودی ${candidate.id} نگاشت و با بازخوانی برد تأیید شد.`))
      }
      setCandidate(null)
      setCandidateIsGuided(false)
    } catch (cause) {
      setPhase('mapping')
      setMessage(copy(`Mapping was not saved: ${cause instanceof Error ? cause.message : String(cause)}`, `نگاشت ذخیره نشد: ${cause instanceof Error ? cause.message : String(cause)}`))
    } finally {
      setBusy('')
    }
  }

  const removeRecord = async (entry: RFLearnedEntry) => {
    const value = await rpc<RFLearnedEntry[]>('controller.rf.remove', { id: entry.id })
    setRecords(value)
    setCaptures((current) => current.map((capture) => capture?.id === entry.id ? null : capture))
    if (candidate?.id === entry.id) {
      setCandidate(null)
      setCandidateIsGuided(false)
      setPhase('idle')
    }
    setMessage(copy(`Entry ${entry.id} was removed and the inventory was refreshed.`, `ورودی ${entry.id} حذف و فهرست تازه‌سازی شد.`))
  }

  const confirmRemove = (entry: RFLearnedEntry) => openDialog({
    tone: 'danger',
    title: copy(`Remove RF entry ${entry.id}?`, `ورودی RF شماره ${entry.id} حذف شود؟`),
    body: copy(`This removes ${identityCode(entry)} from controller storage. Other records are not changed.`, `کد ${identityCode(entry)} از حافظه کنترلر حذف می‌شود و سایر ورودی‌ها تغییر نمی‌کنند.`),
    confirmLabel: copy('Remove entry', 'حذف ورودی'),
    action: () => removeRecord(entry),
  })

  const confirmClear = () => openDialog({
    tone: 'danger',
    title: copy('Clear every learned RF record?', 'همه ورودی‌های RF پاک شوند؟'),
    body: copy(`This permanently clears ${records.length} learned record${records.length === 1 ? '' : 's'}. Export or review identities first.`, `این عمل ${records.length} ورودی آموخته‌شده را برای همیشه پاک می‌کند؛ ابتدا هویت‌ها را بازبینی کنید.`),
    confirmLabel: copy('Clear all records', 'پاک‌کردن همه'),
    action: async () => {
      await rpc('controller.rf.clear', { confirm: 'CLEAR RF' })
      setRecords([])
      setCaptures([null, null, null, null])
      setCandidate(null)
      setPhase('idle')
      setMessage(copy('All learned RF records were cleared by the controller.', 'همه ورودی‌های RF توسط کنترلر پاک شدند.'))
    },
  })

  const confirmTransmit = (entry: RFLearnedEntry) => openDialog({
    title: copy('Transmit one verification burst?', 'یک پالس آزمایشی ارسال شود؟'),
    body: copy(`The controller will transmit ${identityCode(entry)} once. Keep actuators isolated and observe only the intended receiver.`, `کنترلر کد ${identityCode(entry)} را فقط یک‌بار می‌فرستد؛ عملگرها را ایزوله و فقط گیرنده موردنظر را مشاهده کنید.`),
    confirmLabel: copy('Transmit once', 'یک‌بار ارسال'),
    action: async () => {
      await rpc('controller.rf.transmit', {
        code: entry.code, bits: entry.bits, protocol: entry.protocol,
        pulse_us: entry.pulse_us, repeats: 1,
      })
      setMessage(copy(`One verification burst for entry ${entry.id} was acknowledged.`, `یک پالس آزمایشی برای ورودی ${entry.id} تأیید شد.`))
    },
  })

  const beginRemap = (entry: RFLearnedEntry) => {
    setCandidate(entry)
    setCandidateIsGuided(false)
    setDraft(mappingDraftForEntry(entry))
    setPhase('mapping')
    setMessage(copy(`Review the action for entry ${entry.id}; nothing changes until Save mapping.`, `عمل ورودی ${entry.id} را بازبینی کنید؛ تا زمان ذخیره نگاشت تغییری اعمال نمی‌شود.`))
  }

  const discardCandidate = () => {
    if (!candidate) return
    confirmRemove(candidate)
  }

  const setAction = (action: RFMapAction) => {
    const next: RFMapDraft = action === 'none'
      ? { action, target: '', behavior: '' }
      : action === 'menu'
        ? { action, target: '', behavior: '' }
        : action === 'side'
          ? { action, target: '', behavior: 'stop' }
          : action === 'relay'
            ? { action, target: '', behavior: 'press' }
            : action === 'pwm'
              ? { action, target: '', behavior: 'press' }
              : { action, target: '', behavior: 'press' }
    setDraft(next)
  }

  const handleKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement
    if (target.matches('input, select, textarea, button, [contenteditable="true"]')) return
    if (event.key === 'Escape' && (phase === 'capturing' || phase === 'resolving')) {
      event.preventDefault()
      void cancelCapture()
      return
    }
    const labelIndex = RF_GUIDE_LABELS.indexOf(event.key.toUpperCase() as RFGuideLabel)
    if (labelIndex >= 0) {
      event.preventDefault()
      chooseStep(labelIndex)
      return
    }
    if (event.key === 'Enter') {
      event.preventDefault()
      if (phase === 'identity') confirmIdentity()
      else if (phase === 'idle' || phase === 'interrupted') void startCapture()
      return
    }
    if ((event.key === 'ArrowLeft' || event.key === 'ArrowRight') && !['capturing', 'resolving', 'saving'].includes(phase)) {
      event.preventDefault()
      const visualDelta = event.key === 'ArrowRight' ? 1 : -1
      const logicalDelta = isPersian ? -visualDelta : visualDelta
      chooseStep((activeIndex + logicalDelta + RF_GUIDE_LABELS.length) % RF_GUIDE_LABELS.length)
    }
  }

  const targets = mappingTargets(draft.action, copy)
  const behaviors = mappingBehaviors(draft.action, copy)
  const reviewRecords = useMemo(() => records.filter((entry) => rfEntryNeedsReview(entry, records)), [records])
  const activeLabel = RF_GUIDE_LABELS[activeIndex]
  const externalLearningActive = Boolean(snapshot.rf_learning?.active && !startedByView.current && !ignoreStaleActiveSnapshot.current)
  const phaseTone = !snapshot.connected || phase === 'interrupted' ? 'warn' : phase === 'complete' ? 'good' : phase === 'capturing' || phase === 'resolving' ? 'info' : 'neutral'
  const phaseLabel = !snapshot.connected
    ? copy('CONTROLLER OFFLINE', 'کنترلر قطع است')
    : phase === 'capturing' ? copy('LISTENING', 'در حال دریافت')
      : phase === 'resolving' ? copy('VERIFYING', 'در حال بررسی')
        : phase === 'identity' ? copy('CONFIRM IDENTITY', 'تأیید هویت')
          : phase === 'mapping' || phase === 'saving' ? copy('MAP ACTION', 'نگاشت عمل')
            : phase === 'complete' ? copy('READY TO REVIEW', 'آماده بازبینی')
              : copy('GUIDED CAPTURE', 'دریافت هدایت‌شده')

  return (
    <Card
      icon={Radio}
      iconTone="violet"
      className="rf-guide-card"
      title={copy('Handset capture studio', 'استودیوی دریافت فرستنده')}
      eyebrow={copy('433 MHz · guided A/B/C/D', '۴۳۳ مگاهرتز · هدایت‌شده A/B/C/D')}
      action={<StatusBadge tone={phaseTone} pulse={phase === 'capturing'}>{phaseLabel}</StatusBadge>}
      menu={snapshot.connected ? [
        { label: copy('Refresh learned inventory', 'تازه‌سازی فهرست آموخته‌ها'), icon: RefreshCw, disabled: !snapshot.connected, onSelect: () => void loadRecords() },
        { label: copy('Clear all learned records', 'پاک‌کردن همه آموخته‌ها'), icon: Trash2, tone: 'danger', disabled: !snapshot.connected || records.length === 0, onSelect: confirmClear },
      ] : []}
    >
      <div className="rf-guide" tabIndex={0} onKeyDown={handleKeyboard} aria-label={copy('Guided RF handset capture workflow', 'فرایند هدایت‌شده دریافت فرستنده RF')}>
        <ol className="rf-guide__steps" aria-label={copy('Handset buttons', 'دکمه‌های فرستنده')}>
          {RF_GUIDE_LABELS.map((label, index) => {
            const captured = captures[index]
            const active = index === activeIndex
            return <li key={label}>
              <button type="button" className={active ? 'is-active' : ''} aria-current={active ? 'step' : undefined} onClick={() => chooseStep(index)} disabled={phase === 'capturing' || phase === 'resolving' || phase === 'saving'}>
                <span>{captured ? <Check size={17} /> : index + 1}</span>
                <strong>{copy('Button', 'دکمه')} {label}</strong>
                <small>{captured ? identityCode(captured) : active ? copy('Current step', 'گام کنونی') : copy('Not captured', 'دریافت نشده')}</small>
              </button>
            </li>
          })}
        </ol>

        <motion.div layout transition={{ layout: { duration: .26, ease: [0.22, 1, 0.36, 1] } }} className={`rf-guide__message rf-guide__message--${phaseTone}`} role={phase === 'interrupted' ? 'alert' : 'status'} aria-live="polite">
          <span>{phase === 'capturing' || phase === 'resolving' ? <span className="spinner" /> : phase === 'complete' ? <CheckCircle2 size={19} /> : <Antenna size={19} />}</span>
          <div><strong>{phaseLabel}</strong><p>{message}</p></div>
        </motion.div>

        <AnimatePresence mode="wait" initial={false}>
          {(phase === 'idle' || phase === 'interrupted' || phase === 'complete') && <motion.div
            key="capture"
            className="rf-guide__capture"
            initial={{ opacity: 0, clipPath: 'inset(0 8% 0 8% round 12px)' }}
            animate={{ opacity: 1, clipPath: 'inset(0 0% 0 0% round 12px)' }}
            exit={{ opacity: 0, clipPath: 'inset(0 8% 0 8% round 12px)' }}
            transition={{ duration: .24, ease: [0.22, 1, 0.36, 1] }}
          >
            <div><span className="rf-guide__label">{activeLabel}</span><div><strong>{copy(`Capture handset button ${activeLabel}`, `دریافت دکمه ${activeLabel} فرستنده`)}</strong><p>{snapshot.connected ? copy('A 30-second bounded session starts only after you press the button below.', 'نشست محدود ۳۰ ثانیه‌ای فقط پس از فشردن دکمه زیر آغاز می‌شود.') : copy('Controls remain hidden until an authenticated controller reconnects.', 'تا اتصال دوباره کنترلر معتبر، کنترل‌ها پنهان می‌مانند.')}</p></div></div>
            {snapshot.connected && (externalLearningActive
              ? <Button tone="danger" icon={X} busy={busy === 'cancel'} onClick={() => void cancelCapture()}>{copy('Cancel active session', 'لغو نشست فعال')}</Button>
              : <Button tone="primary" icon={ScanSearch} busy={busy === 'capture'} onClick={() => void startCapture()}>{copy('Start capture', 'شروع دریافت')}</Button>)}
          </motion.div>}

          {(phase === 'capturing' || phase === 'resolving') && <motion.div key="listening" className="rf-guide__listening" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <div className="rf-guide__signal" aria-hidden="true"><i /><i /><i /><Antenna size={28} /></div>
            <Button tone="ghost" icon={X} busy={busy === 'cancel'} onClick={() => void cancelCapture()}>{copy('Cancel safely', 'لغو ایمن')}</Button>
          </motion.div>}

          {phase === 'identity' && candidate && <motion.div key="identity" className="rf-guide__identity" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }} transition={{ duration: .22, ease: [0.22, 1, 0.36, 1] }}>
            <header><ShieldCheck size={22} /><div><strong>{copy(`Is this button ${activeLabel}?`, `آیا این همان دکمه ${activeLabel} است؟`)}</strong><p>{copy('Confirm the exact waveform identity before assigning any action.', 'پیش از تعیین عمل، هویت دقیق موج را تأیید کنید.')}</p></div></header>
            <dl dir="ltr"><div><dt>ID</dt><dd>{candidate.id}</dd></div><div><dt>CODE</dt><dd>{identityCode(candidate)}</dd></div><div><dt>BITS</dt><dd>{candidate.bits}</dd></div><div><dt>PROTOCOL</dt><dd>{candidate.protocol}</dd></div><div><dt>PULSE</dt><dd>{candidate.pulse_us} µs</dd></div></dl>
            <div className="rf-guide__actions"><Button tone="primary" icon={CheckCircle2} onClick={confirmIdentity}>{copy(`Confirm button ${activeLabel}`, `تأیید دکمه ${activeLabel}`)}</Button><Button tone="danger" icon={Trash2} onClick={discardCandidate}>{copy('Discard capture', 'حذف دریافت')}</Button></div>
          </motion.div>}

          {(phase === 'mapping' || phase === 'saving') && candidate && <motion.div key="mapping" className="rf-guide__mapping" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }} transition={{ duration: .22, ease: [0.22, 1, 0.36, 1] }}>
            <header><div><strong>{candidateIsGuided ? copy(`Map button ${activeLabel}`, `نگاشت دکمه ${activeLabel}`) : copy(`Remap entry ${candidate.id}`, `نگاشت دوباره ورودی ${candidate.id}`)}</strong><p dir="ltr">{identityCode(candidate)} · {candidate.bits} bit · P{candidate.protocol} · {candidate.pulse_us} µs</p></div><StatusBadge tone="info">ID {candidate.id}</StatusBadge></header>
            <div className="rf-guide__mapping-kind"><label>{copy('Action domain', 'حوزه عمل')}</label><Segmented value={draft.action} label={copy('RF action domain', 'حوزه عمل RF')} options={[
              { value: 'key', label: copy('Key', 'کلید') }, { value: 'menu', label: copy('Menu', 'منو') },
              { value: 'relay', label: copy('Relay', 'رله') }, { value: 'side', label: copy('Motion', 'حرکت') },
              { value: 'pwm', label: 'PWM' }, { value: 'none', label: copy('None', 'بدون عمل') },
            ]} onChange={setAction} /></div>
            {draft.action !== 'none' && <div className="rf-guide__mapping-fields">
              <label><span>{copy('Target', 'مقصد')}</span><select value={draft.target} onChange={(event) => setDraft((current) => ({ ...current, target: event.target.value }))}><option value="" disabled>{copy('Choose target…', 'مقصد را انتخاب کنید…')}</option>{targets.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
              {behaviors.length > 0 && <label><span>{copy('Behavior', 'رفتار')}</span><select value={draft.behavior} onChange={(event) => setDraft((current) => ({ ...current, behavior: event.target.value }))}>{behaviors.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>}
            </div>}
            <div className="rf-guide__actions"><Button tone="primary" icon={Check} busy={busy === 'mapping'} disabled={!rfMapDraftIsComplete(draft)} onClick={() => void saveMapping()}>{draft.action === 'none' ? copy('Keep Unmapped', 'بدون نگاشت بماند') : copy('Save mapping', 'ذخیره نگاشت')}</Button><Button tone="ghost" icon={X} disabled={phase === 'saving'} onClick={() => { setCandidate(null); setCandidateIsGuided(false); setPhase('idle'); setMessage(copy('Mapping review closed without changing the board.', 'بازبینی نگاشت بدون تغییر برد بسته شد.')) }}>{copy('Close without saving', 'بستن بدون ذخیره')}</Button></div>
          </motion.div>}
        </AnimatePresence>

        <div className="rf-guide__keyboard" aria-label={copy('Keyboard shortcuts', 'میان‌برهای صفحه‌کلید')}><KeyCombo keys={[['A', 'B', 'C', 'D']]} separator="/" /><span>{copy('select button', 'انتخاب دکمه')}</span><KeyCombo keys={['Enter']} /><span>{copy('capture or confirm', 'دریافت یا تأیید')}</span><KeyCombo keys={['Esc']} /><span>{copy('cancel capture', 'لغو دریافت')}</span><KeyCombo keys={[['←', '→']]} separator="/" /><span>{copy('move between steps', 'جابجایی بین گام‌ها')}</span></div>

        <section className="rf-inventory" aria-labelledby="rf-inventory-title">
          <header><div><span className="eyebrow">{copy('BOARD READBACK', 'بازخوانی برد')}</span><h3 id="rf-inventory-title">{copy('Learned record review', 'بازبینی ورودی‌های آموخته‌شده')}</h3></div><div><StatusBadge tone={reviewRecords.length ? 'warn' : 'good'}>{reviewRecords.length} {copy('need review', 'نیازمند بررسی')}</StatusBadge>{snapshot.connected && <Button compact tone="ghost" icon={RefreshCw} onClick={() => void loadRecords()}>{copy('Refresh', 'تازه‌سازی')}</Button>}</div></header>
          {records.length === 0
            ? <div className="rf-inventory__empty"><Radio size={24} /><strong>{copy('No learned records', 'ورودی آموخته‌شده‌ای نیست')}</strong><p>{copy('The inventory will update after the first confirmed capture.', 'فهرست پس از نخستین دریافت تأییدشده به‌روز می‌شود.')}</p></div>
            : <div className="rf-inventory__list">{records.map((entry) => {
              const needsReview = rfEntryNeedsReview(entry, records)
              return <article key={entry.id} className={needsReview ? 'needs-review' : ''}>
                <span className="rf-inventory__id">{entry.id}</span>
                <div className="rf-inventory__identity"><strong dir="ltr">{entry.name || identityCode(entry)}</strong><small dir="ltr">{entry.name ? `${identityCode(entry)} · ` : ''}{entry.bits} bit · P{entry.protocol} · {entry.pulse_us} µs</small></div>
                <div className="rf-inventory__mapping"><span>{mappingSummary(entry, copy)}</span>{needsReview && <small>{entry.action_kind === 0 ? copy('Unmapped record', 'ورودی بدون نگاشت') : copy('Duplicate identity', 'هویت تکراری')}</small>}</div>
                {snapshot.connected && <div className="rf-inventory__actions"><Button compact icon={Pencil} onClick={() => beginRemap(entry)}>{copy('Remap', 'نگاشت دوباره')}</Button><Button compact icon={Send} onClick={() => confirmTransmit(entry)}>{copy('Test once', 'آزمایش یک‌بار')}</Button><Button compact tone="danger" icon={Trash2} aria-label={copy(`Remove RF entry ${entry.id}`, `حذف ورودی RF شماره ${entry.id}`)} onClick={() => confirmRemove(entry)} /></div>}
              </article>
            })}</div>}
          {records.length > 0 && <footer><span>{copy(`${records.length} of 20 slots used`, `${records.length} از ۲۰ جایگاه استفاده شده`)}</span>{snapshot.connected && <Button compact tone="danger" icon={ListRestart} onClick={confirmClear}>{copy('Clear all…', 'پاک‌کردن همه…')}</Button>}</footer>}
        </section>
      </div>
    </Card>
  )
}

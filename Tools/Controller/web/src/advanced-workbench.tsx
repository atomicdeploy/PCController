import {
  type FormEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useState,
} from 'react'
import {
  Activity,
  Antenna,
  AudioLines,
  BookOpen,
  Cable,
  ChevronDown,
  CircleDot,
  CircleStop,
  Cpu,
  Database,
  DoorOpen,
  Eraser,
  Eye,
  FolderOpen,
  Gauge,
  HardDriveDownload,
  Keyboard,
  KeyboardOff,
  LayoutDashboard,
  LayoutPanelTop,
  List,
  ListChecks,
  ListTree,
  MemoryStick,
  MessageSquareText,
  MonitorCog,
  Network,
  Palette,
  PanelTopOpen,
  Play,
  Plug,
  Radio,
  RefreshCw,
  ScanSearch,
  Save,
  Send,
  ShieldAlert,
  SlidersHorizontal,
  Sparkles,
  SquareTerminal,
  SunMedium,
  Thermometer,
  Trash2,
  Unplug,
  Usb,
  Volume2,
  VolumeX,
  Waves,
  Workflow,
  X,
  type LucideIcon,
} from 'lucide-react'
import {
  Button,
  Icon,
  RangeField,
  Segmented,
  StatusBadge,
  TextField,
  Toggle,
} from './components'
import { rpc } from './api'
import { redactSensitiveCommand, shellArgument as quoteArgument } from './command-line'
import type { FrontPanelState } from './types'
import type { SharedViewProps } from './views'

interface AdvancedWorkbenchProps extends SharedViewProps {
  run: (command: string) => Promise<string>
  busy: string
}

type ReviewRisk = 'normal' | 'caution' | 'danger'

interface ReviewState {
  command: string
  reason: string
  risk: ReviewRisk
  needsDevice: boolean
  execute?: () => Promise<unknown>
}

interface PanelProps {
  icon: LucideIcon
  eyebrow: string
  title: string
  detail: string
  children: ReactNode
  defaultOpen?: boolean
  tone?: 'good' | 'warn' | 'bad' | 'info' | 'neutral'
  status?: string
}

function AdvancedPanel({
  icon,
  eyebrow,
  title,
  detail,
  children,
  defaultOpen = false,
  tone = 'neutral',
  status,
}: PanelProps) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <details
      className="advanced-panel"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="advanced-panel__summary">
        <span className="advanced-panel__icon"><Icon icon={icon} size={18} /></span>
        <span className="advanced-panel__copy">
          <small>{eyebrow}</small>
          <strong>{title}</strong>
          <span>{detail}</span>
        </span>
        {status && <StatusBadge tone={tone}>{status}</StatusBadge>}
        <ChevronDown className="advanced-panel__chevron" aria-hidden="true" size={17} />
      </summary>
      <div className="advanced-panel__body">{children}</div>
    </details>
  )
}

function boundedInteger(value: string, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value, 10)
  if (!Number.isFinite(parsed)) return fallback
  return Math.min(maximum, Math.max(minimum, parsed))
}

function normalizeTokens(value: string): string {
  return value.trim().split(/\s+/).filter(Boolean).join(' ')
}

// Builds the same file-watched short-label update used by the CLI and IPC API.
export function hostMenuLabelCommand(reference: string, label: string): string | undefined {
  const normalizedReference = reference.trim()
  const normalizedLabel = label.trim()
  if (!normalizedReference || !/^[\x20-\x7e]{1,4}$/.test(normalizedLabel)) return undefined
  return `host-menu set ${quoteArgument(normalizedReference)} label ${quoteArgument(normalizedLabel)}`
}

const segmentLines = [
  ['a', 8, 5, 28, 5], ['b', 31, 8, 31, 27], ['c', 31, 32, 31, 51],
  ['d', 8, 54, 28, 54], ['e', 5, 32, 5, 51], ['f', 5, 8, 5, 27],
  ['g', 8, 29.5, 28, 29.5],
] as const

function SevenSegmentPreview({ panel }: { panel?: FrontPanelState }) {
  const raw = panel?.raw_segments ?? [0, 0, 0, 0]
  return (
    <div className={`live-segment-preview${panel?.segments_active ? ' is-active' : ''}`} dir="ltr" aria-label="Live four-digit display preview">
      {raw.map((mask, index) => (
        <svg key={index} viewBox="0 0 40 62" role="img" aria-label={`digit ${index + 1} raw 0x${mask.toString(16).padStart(2, '0')}`}>
          {segmentLines.map(([name, x1, y1, x2, y2], bit) => (
            <line key={name} x1={x1} y1={y1} x2={x2} y2={y2} className={(mask & (1 << bit)) !== 0 ? 'is-lit' : ''} />
          ))}
          <circle cx="36" cy="54" r="2.2" className={(mask & 0x80) !== 0 ? 'is-lit' : ''} />
        </svg>
      ))}
    </div>
  )
}

export function AdvancedWorkbench({
  snapshot,
  locale,
  run,
  busy,
}: AdvancedWorkbenchProps) {
  const isPersian = locale === 'fa'
  const copy = (english: string, persian: string) => isPersian ? persian : english
  const online = snapshot.connected
  const boardBusy = busy.length > 0

  const [port, setPort] = useState('')
  const [streamEnabled, setStreamEnabled] = useState(true)
  const [streamPeriod, setStreamPeriod] = useState(500)
  const [programMode, setProgramMode] = useState<'idle' | 'running'>('running')
  const [programModeTouched, setProgramModeTouched] = useState(false)
  const [programReason, setProgramReason] = useState('web workbench')

  const [silent, setSilent] = useState(false)
  const [doorAudio, setDoorAudio] = useState(true)
  const [relayAudio, setRelayAudio] = useState(true)

  const [menuPage, setMenuPage] = useState('status')
  const [menuMask, setMenuMask] = useState('0xFFFF')
  const [menuOrder, setMenuOrder] = useState('status voltage current temperature')
  const [hostMenuID, setHostMenuID] = useState('')
  const [hostMenuLabel, setHostMenuLabel] = useState('')
  const [frontPanel, setFrontPanel] = useState<FrontPanelState | undefined>(snapshot.front_panel)

  const [pixel, setPixel] = useState(0)
  const [pixelRed, setPixelRed] = useState(32)
  const [pixelGreen, setPixelGreen] = useState(214)
  const [pixelBlue, setPixelBlue] = useState(220)
  const [pixelBrightness, setPixelBrightness] = useState(180)
  const [effectName, setEffectName] = useState('attention')

  const [rfID, setRFID] = useState('1')
  const [rfKind, setRFKind] = useState<'none' | 'key' | 'menu' | 'relay' | 'side' | 'pwm'>('key')
  const [rfValue, setRFValue] = useState('1')
  const [rfBehavior, setRFBehavior] = useState('press')

  const [macroRef, setMacroRef] = useState('')
  const [macroName, setMacroName] = useState('')
  const [macroCategory, setMacroCategory] = useState('Web')
  const [macroColor, setMacroColor] = useState<'red' | 'blue' | 'violet' | 'green' | 'white'>('red')

  const [i2cAddress, setI2CAddress] = useState('0x27')
  const [i2cLease, setI2CLease] = useState(2)
  const [i2cReadCount, setI2CReadCount] = useState(4)
  const [i2cWrite, setI2CWrite] = useState('0x00')

  const [virtualKey, setVirtualKey] = useState('F13')
  const [virtualHold, setVirtualHold] = useState(80)
  const [powerAction, setPowerAction] = useState<'lock' | 'sleep' | 'hibernate' | 'shutdown' | 'restart' | 'logoff'>('lock')
  const [powerToken, setPowerToken] = useState('')

  const [messageTarget, setMessageTarget] = useState<'host' | 'lcd'>('host')
  const [messageType, setMessageType] = useState('ui.note')
  const [messageText, setMessageText] = useState('')
  const [messageLine1, setMessageLine1] = useState('')
  const [messageLine2, setMessageLine2] = useState('')
  const [discoverMDNS, setDiscoverMDNS] = useState(true)
  const [discoverSSDP, setDiscoverSSDP] = useState(true)
  const [discoverTimeout, setDiscoverTimeout] = useState(1500)
  const [historyMinutes, setHistoryMinutes] = useState(60)
  const [historyLimit, setHistoryLimit] = useState(100)
  const [serviceOutput, setServiceOutput] = useState(copy('No service query yet.', 'هنوز پرس‌وجوی سرویسی انجام نشده است.'))
  const [serviceBusy, setServiceBusy] = useState('')

  const [bootPath, setBootPath] = useState('backup')
  const [review, setReview] = useState<ReviewState>({
    command: '',
    reason: '',
    risk: 'normal',
    needsDevice: false,
  })
  const [dangerArm, setDangerArm] = useState('')
  const [reviewBusy, setReviewBusy] = useState(false)

  useEffect(() => {
    if (!snapshot.have_settings) return
    setSilent((snapshot.settings.flags & 0x01) !== 0)
    setDoorAudio((snapshot.settings.flags & 0x20) === 0)
    setRelayAudio((snapshot.settings.flags & 0x40) === 0)
    setStreamEnabled(snapshot.settings.stream_period_ms > 0)
    if (snapshot.settings.stream_period_ms > 0) {
      setStreamPeriod(snapshot.settings.stream_period_ms)
    }
  }, [
    snapshot.have_settings,
    snapshot.settings.flags,
    snapshot.settings.stream_period_ms,
  ])

  useEffect(() => {
    if (!port && snapshot.port.name) setPort(snapshot.port.name)
  }, [port, snapshot.port.name])

  useEffect(() => {
    if (programModeTouched || !snapshot.program_state?.mode) return
    setProgramMode(snapshot.program_state.mode.toLowerCase() === 'running' ? 'running' : 'idle')
  }, [programModeTouched, snapshot.program_state?.mode])

  useEffect(() => {
    if (!online && messageTarget === 'lcd') setMessageTarget('host')
  }, [messageTarget, online])

  useEffect(() => {
    if (snapshot.have_front_panel && snapshot.front_panel) setFrontPanel(snapshot.front_panel)
  }, [snapshot.front_panel_updated, snapshot.have_front_panel, snapshot.front_panel])

  useEffect(() => {
    setServiceOutput(copy('No service query yet.', 'هنوز پرس‌وجوی سرویسی انجام نشده است.'))
  }, [locale])

  const rfMapCommand = useMemo(() => {
    const id = rfID.trim() || '1'
    switch (rfKind) {
      case 'none': return `rf map ${id} none`
      case 'menu': return `rf map ${id} menu ${rfValue.trim() || 'next'}`
      case 'side': return `rf map ${id} side ${rfValue.trim() || 'left'} ${rfBehavior.trim() || 'stop'}`
      case 'relay': return `rf map ${id} relay ${rfValue.trim() || '5'} ${rfBehavior.trim() || 'press'}`
      case 'pwm': return `rf map ${id} pwm ${rfValue.trim() || '0'} ${rfBehavior.trim() || 'press'}`
      default: return `rf map ${id} key ${rfValue.trim() || '1'} ${rfBehavior.trim() || 'press'}`
    }
  }, [rfBehavior, rfID, rfKind, rfValue])

  const prepare = (
    command: string,
    reason: string,
    risk: ReviewRisk = 'caution',
    needsDevice = false,
    execute?: () => Promise<unknown>,
  ) => {
    if (reviewBusy) return
    setReview({ command: command.trim(), reason, risk, needsDevice, execute })
    setDangerArm('')
    window.requestAnimationFrame(() => {
      document.getElementById('advanced-command-review')?.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
      })
    })
  }

  const executeReviewed = async () => {
    if (!review.command) return
    const prepared = review
    setDangerArm('')
    setReviewBusy(true)
    try {
      if (prepared.execute) {
        const value = await prepared.execute()
        setServiceOutput(JSON.stringify(value, null, 2))
      } else {
        await run(prepared.command)
      }
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      setServiceOutput(`! ${detail}`)
    } finally {
      setDangerArm('')
      setReview({ command: '', reason: '', risk: 'normal', needsDevice: false })
      setReviewBusy(false)
    }
  }

  const performRPC = async (label: string, task: () => Promise<unknown>) => {
    setServiceBusy(label)
    try {
      const value = await task()
      setServiceOutput(JSON.stringify(value, null, 2))
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      setServiceOutput(`! ${detail}`)
    } finally {
      setServiceBusy('')
    }
  }

  const sendMessage = (event: FormEvent) => {
    event.preventDefault()
    const type = messageType.trim()
    if (!type) return
    if (messageTarget === 'lcd') {
      if (!lcdMessageValid) return
      void performRPC('message', () => rpc('controller.message.send', {
        target: 'lcd',
        type,
        text: `${messageLine1}\n${messageLine2}`,
        line1: messageLine1,
        line2: messageLine2,
      }))
      return
    }
    const text = messageText.trim()
    if (!text || messageByteLength > 4096) return
    void performRPC('message', () => rpc('controller.message.send', { target: 'host', type, text }))
  }

  const queryHistory = (kind: 'status' | 'timeline') => {
    const since = new Date(Date.now() - historyMinutes * 60_000).toISOString()
    const method = kind === 'status' ? 'controller.history.status' : 'controller.history.timeline'
    const params = kind === 'status' ? { since } : { since, limit: historyLimit }
    void performRPC(`history-${kind}`, () => rpc(method, params))
  }

  const setRFDefaults = (kind: typeof rfKind) => {
    setRFKind(kind)
    switch (kind) {
      case 'none': setRFValue(''); setRFBehavior(''); break
      case 'menu': setRFValue('next'); setRFBehavior(''); break
      case 'relay': setRFValue('5'); setRFBehavior('toggle'); break
      case 'side': setRFValue('left'); setRFBehavior('stop'); break
      case 'pwm': setRFValue('0'); setRFBehavior('press'); break
      default: setRFValue('1'); setRFBehavior('press')
    }
  }

  const serialOpen = port.trim() ? `open ${quoteArgument(port.trim())}` : 'open'
  const streamCommand = `stream ${streamEnabled ? streamPeriod : 0}`
  const programStateCommand = programMode === 'idle'
    ? 'program-state set web idle'
    : `program-state set web running ${quoteArgument(programReason.trim() || 'web workbench')}`
  const i2cBytes = normalizeTokens(i2cWrite)
  const i2cCommand = `i2c transfer ${i2cAddress.trim() || '0x27'} ${i2cLease} ${i2cReadCount}${i2cBytes ? ` ${i2cBytes}` : ''}`
  const hostMenuLabelUpdate = hostMenuLabelCommand(hostMenuID, hostMenuLabel)
  const macroRecordCommand = [
    'macro record start',
    quoteArgument(macroName.trim()),
    ...(macroCategory.trim() ? [quoteArgument(macroCategory.trim())] : []),
    ...(macroColor.trim() ? [quoteArgument(macroColor.trim())] : []),
  ].join(' ')
  const macroUpdateCommand = [
    'macro update',
    quoteArgument(macroRef.trim()),
    quoteArgument(macroName.trim()),
    quoteArgument(macroCategory.trim() || '-'),
    quoteArgument(macroColor.trim() || '-'),
  ].join(' ')
  const messageByteLength = new TextEncoder().encode(messageText.trim()).byteLength
  const lcdMessageValid = Boolean(messageLine1 || messageLine2) &&
    messageLine1.length <= 16 && messageLine2.length <= 16 &&
    /^[\x20-\x7e]*$/.test(messageLine1) && /^[\x20-\x7e]*$/.test(messageLine2)
  const messageNeedsBoard = messageTarget === 'lcd'
  const reviewBlocked = !review.command || boardBusy || reviewBusy ||
    (review.needsDevice && !online) ||
    (review.risk === 'danger' && dangerArm.trim().toUpperCase() !== 'RUN')

  return (
    <section className="advanced-surface" aria-labelledby="advanced-workbench-title">
      <header className="advanced-heading">
        <div>
          <span>{copy('DEEP CONTROL / PROGRESSIVE DISCLOSURE', 'کنترل عمیق / نمایش مرحله‌ای')}</span>
          <h2 id="advanced-workbench-title">{copy('Advanced capability deck', 'میز قابلیت‌های پیشرفته')}</h2>
          <p>{copy(
            'The common controls stay calm; every specialist path remains reachable here through the same primary dispatcher.',
            'کنترل‌های روزمره ساده می‌مانند و تمام مسیرهای تخصصی از همین بخش و هسته فرمان اصلی در دسترس هستند.',
          )}</p>
        </div>
        <StatusBadge tone={online ? 'good' : 'warn'} pulse={online}>
          {online ? copy('DEVICE ACTIONS READY', 'فرمان‌های برد آماده') : copy('HOST CONTROLS ONLY', 'فقط کنترل میزبان')}
        </StatusBadge>
      </header>

      <div className="advanced-grid">
        <AdvancedPanel
          icon={Usb}
          eyebrow={copy('SERIAL LIFECYCLE', 'چرخه اتصال سریال')}
          title={copy('Connection, stream & program state', 'اتصال، جریان وضعیت و حالت برنامه')}
          detail={copy('Port discovery, authenticated open/reconnect, telemetry cadence and shared run state.', 'کشف درگاه، اتصال امن، آهنگ تله‌متری و حالت اجرای مشترک.')}
          defaultOpen
          status={online ? snapshot.port.name || copy('connected', 'متصل') : copy('offline', 'آفلاین')}
          tone={online ? 'good' : 'warn'}
        >
          <div className="advanced-actions">
            <Button icon={ListTree} busy={busy === 'ports'} onClick={() => void run('ports')}>{copy('List ports', 'فهرست درگاه‌ها')}</Button>
            {online && <Button icon={Activity} busy={busy === 'status'} onClick={() => void run('status')}>{copy('Live status', 'وضعیت زنده')}</Button>}
            <Button icon={Workflow} busy={busy === 'program-state'} onClick={() => void run('program-state')}>{copy('Program state', 'حالت برنامه')}</Button>
          </div>
          <div className="advanced-fields">
            <TextField
              label={copy('Serial port · blank = authenticated auto-detect', 'درگاه سریال · خالی = کشف امن خودکار')}
              value={port}
              dir="ltr"
              spellCheck={false}
              onChange={(event) => setPort(event.target.value)}
              action={<>
                <Button tone="primary" icon={Plug} busy={busy === serialOpen} onClick={() => void run(serialOpen)}>{copy('Open', 'اتصال')}</Button>
                <Button icon={RefreshCw} busy={busy === 'reconnect'} onClick={() => void run('reconnect')}>{copy('Reconnect', 'اتصال مجدد')}</Button>
                {online && <Button icon={Unplug} busy={busy === 'close'} onClick={() => void run('close')}>{copy('Close & pause', 'بستن و توقف')}</Button>}
              </>}
            />
          </div>
          {online && <div className="advanced-control-row">
            <Toggle
              checked={streamEnabled}
              onChange={setStreamEnabled}
              label={copy('Status stream', 'جریان وضعیت')}
              detail={copy('Desired value; Apply sends the EEPROM-safe stream command.', 'مقدار درخواستی؛ اعمال، فرمان امن جریان را می‌فرستد.')}
            />
            {streamEnabled && <RangeField label={copy('Period', 'دوره')} value={streamPeriod} min={100} max={5000} step={100} unit="ms" onChange={setStreamPeriod} />}
            <Button icon={Waves} busy={busy === streamCommand} onClick={() => void run(streamCommand)}>{copy('Apply stream', 'اعمال جریان')}</Button>
          </div>}
          <div className="advanced-fields advanced-fields--program">
            <div className="advanced-field">
              <label>{copy('Desired host program state', 'حالت برنامه در میزبان')}</label>
              <Segmented value={programMode} label={copy('Program mode', 'حالت برنامه')} options={[{ value: 'idle', label: copy('Idle', 'آماده') }, { value: 'running', label: copy('Running', 'در حال اجرا') }]} onChange={(value) => { setProgramMode(value); setProgramModeTouched(true) }} />
            </div>
            <TextField label={copy('Reason', 'دلیل')} value={programReason} disabled={programMode === 'idle'} onChange={(event) => setProgramReason(event.target.value)} />
            <Button icon={SquareTerminal} onClick={() => prepare(programStateCommand, copy('Review the shared host/board program-state transition.', 'تغییر حالت برنامه میزبان و برد را بازبینی کنید.'), 'caution')}>
              {copy('Prepare state change', 'آماده‌سازی تغییر حالت')}
            </Button>
          </div>
        </AdvancedPanel>

        {online && <><AdvancedPanel
          icon={Thermometer}
          eyebrow="DS18B20"
          title={copy('Temperature identities', 'شناسه‌های دما')}
          detail={copy('Read assigned channels, inspect ROM identities, or explicitly rescan the OneWire bus.', 'خواندن کانال‌ها، بررسی شناسه ROM یا پویش دوباره گذرگاه OneWire.')}
          status={online ? copy('available', 'آماده') : copy('device required', 'نیازمند برد')}
          tone={online ? 'info' : 'neutral'}
        >
          <div className="advanced-actions">
            <Button icon={Thermometer} disabled={!online} busy={busy === 'temp'} onClick={() => void run('temp')}>{copy('Read values', 'خواندن دما')}</Button>
            <Button icon={ListTree} disabled={!online} busy={busy === 'temp list'} onClick={() => void run('temp list')}>{copy('List identities', 'فهرست شناسه‌ها')}</Button>
            <Button icon={ScanSearch} disabled={!online} busy={busy === 'temp scan'} onClick={() => void run('temp scan')}>{copy('Rescan bus', 'پویش دوباره')}</Button>
          </div>
          <p className="advanced-note">{copy('A rescan refreshes physical sensor assignment; it does not claim that attached probes have been validated here.', 'پویش دوباره، تخصیص حسگر فیزیکی را به‌روز می‌کند؛ این صفحه به‌تنهایی تأییدکننده سلامت سخت‌افزار متصل نیست.')}</p>
        </AdvancedPanel>

        <AdvancedPanel
          icon={AudioLines}
          eyebrow={copy('EEPROM-BACKED', 'ذخیره‌شده در EEPROM')}
          title={copy('Silent mode & firmware buzzer cues', 'حالت بی‌صدا و اعلان‌های بیزر')}
          detail={copy('Inspect the authoritative board settings, choose desired audio policy, then apply explicitly.', 'تنظیمات معتبر برد را ببینید، سیاست صوتی دلخواه را انتخاب و صریحاً اعمال کنید.')}
          status={snapshot.have_settings ? copy('settings synced', 'تنظیمات همگام') : copy('not read', 'خوانده نشده')}
          tone={snapshot.have_settings ? 'good' : 'neutral'}
        >
          <div className="advanced-actions">
            <Button icon={MemoryStick} disabled={!online} busy={busy === 'settings'} onClick={() => void run('settings')}>{copy('EEPROM status', 'وضعیت EEPROM')}</Button>
            <Button icon={VolumeX} disabled={!online} busy={busy === 'silent status'} onClick={() => void run('silent status')}>{copy('Silent status', 'وضعیت بی‌صدا')}</Button>
          </div>
          <div className="advanced-toggle-stack">
            <Toggle checked={silent} disabled={!online} onChange={setSilent} label={copy('Silent mode', 'حالت بی‌صدا')} detail={copy('Desired persisted value.', 'مقدار ذخیره‌شونده درخواستی.')} />
            <Toggle checked={doorAudio} disabled={!online} onChange={setDoorAudio} label={copy('Door cues', 'صدای باز و بسته شدن در')} detail={copy('Desired firmware cue state.', 'وضعیت درخواستی صدای میان‌افزار.')} />
            <Toggle checked={relayAudio} disabled={!online} onChange={setRelayAudio} label={copy('Relay cues', 'صدای روشن و خاموش شدن رله')} detail={copy('Desired firmware cue state.', 'وضعیت درخواستی صدای میان‌افزار.')} />
          </div>
          <div className="advanced-actions">
            <Button icon={silent ? VolumeX : Volume2} disabled={!online} busy={busy === `silent ${silent ? 'on' : 'off'}`} onClick={() => void run(`silent ${silent ? 'on' : 'off'}`)}>{copy('Apply silent', 'اعمال بی‌صدا')}</Button>
            <Button icon={DoorOpen} disabled={!online} busy={busy === `settings audio door ${doorAudio ? 'on' : 'off'}`} onClick={() => void run(`settings audio door ${doorAudio ? 'on' : 'off'}`)}>{copy('Apply door cues', 'اعمال صدای در')}</Button>
            <Button icon={AudioLines} disabled={!online} busy={busy === `settings audio relay ${relayAudio ? 'on' : 'off'}`} onClick={() => void run(`settings audio relay ${relayAudio ? 'on' : 'off'}`)}>{copy('Apply relay cues', 'اعمال صدای رله')}</Button>
          </div>
        </AdvancedPanel>

        <AdvancedPanel
          icon={LayoutPanelTop}
          eyebrow={copy('FRONT PANEL', 'پنل جلویی')}
          title={copy('Catalog, layout & navigation', 'کاتالوگ، چیدمان و پیمایش')}
          detail={copy('Firmware page IDs and HOST-supplied menu overlays remain separately inspectable.', 'شناسه صفحات میان‌افزار و منوهای میزبان به‌صورت مستقل قابل بررسی هستند.')}
          defaultOpen
        >
          <div className="front-panel-live">
            <SevenSegmentPreview panel={frontPanel} />
            <div>
              <strong>{copy('Live physical display', 'نمایش زنده پنل')}</strong>
              <span>{frontPanel ? `${copy('page', 'صفحه')} ${frontPanel.menu_page} · ${copy('brightness', 'روشنایی')} ${frontPanel.brightness}/7` : copy('Awaiting exact front-panel state', 'در انتظار وضعیت دقیق پنل')}</span>
			  <small>{copy('Changed-only board opcodes update this preview immediately; refresh is explicit.', 'اپ‌کدهای تغییرمحور برد این پیش‌نمایش را فوری به‌روز می‌کنند؛ تازه‌سازی صریح است.')}</small>
            </div>
          </div>
          <div className="advanced-actions">
			<Button icon={RefreshCw} disabled={!online} onClick={() => void rpc<FrontPanelState>('controller.front_panel').then(setFrontPanel).catch(() => undefined)}>{copy('Refresh physical state', 'تازه‌سازی وضعیت فیزیکی')}</Button>
            <Button icon={BookOpen} disabled={!online} busy={busy === 'menu list'} onClick={() => void run('menu list')}>{copy('Firmware catalog', 'کاتالوگ میان‌افزار')}</Button>
            <Button icon={LayoutDashboard} disabled={!online} busy={busy === 'menu current'} onClick={() => void run('menu current')}>{copy('Current page', 'صفحه فعلی')}</Button>
            <Button icon={LayoutPanelTop} disabled={!online} busy={busy === 'menu layout'} onClick={() => void run('menu layout')}>{copy('Stored layout', 'چیدمان ذخیره‌شده')}</Button>
            <Button icon={ListTree} busy={busy === 'host-menu list'} onClick={() => void run('host-menu list')}>{copy('Host catalog', 'کاتالوگ میزبان')}</Button>
            <Button icon={FolderOpen} busy={busy === 'host-menu directory'} onClick={() => void run('host-menu directory')}>{copy('Host directory', 'دایرکتوری میزبان')}</Button>
            <Button icon={Eye} busy={busy === 'host-menu status'} onClick={() => void run('host-menu status')}>{copy('Overlay status', 'وضعیت لایه میزبان')}</Button>
            <Button icon={Workflow} disabled={!online} busy={busy === 'host-menu open macro-library'} onClick={() => void run('host-menu open macro-library')}>{copy('Macro front panel', 'پنل ماکرو')}</Button>
          </div>
          <div className="advanced-nav-pad" aria-label={copy('Firmware menu navigation', 'پیمایش منوی میان‌افزار')}>
            {(['prev', 'next', 'dec', 'inc'] as const).map((action) => (
              <Button key={action} compact disabled={!online} busy={busy === `menu ${action}`} onClick={() => void run(`menu ${action}`)}>{action}</Button>
            ))}
          </div>
          <div className="advanced-nav-pad" aria-label={copy('HOST menu navigation', 'پیمایش منوی میزبان')}>
            {([['K1', 'Previous'], ['K2', 'Next'], ['K3', 'Decrease'], ['K4', 'Select']] as const).map(([key, label]) => (
              <Button key={key} compact disabled={!online} busy={busy === `host-menu key ${key} press`} onClick={() => void run(`host-menu key ${key} press`)}>{copy(`${key} · ${label}`, `${key} · ${label}`)}</Button>
            ))}
          </div>
          <p className="advanced-note advanced-note--safe">{copy('The Macro front-panel page lists the file-watched library, records MCU acknowledgement deltas, shows playback progress, and offers safe cancel or guarded keep-output cancel.', 'صفحه ماکروی پنل، فهرست تحت پایش فایل، ضبط زمان‌بندی MCU، پیشرفت اجرا و لغو امن یا لغو با حفظ خروجی را ارائه می‌کند.')}</p>
          <div className="advanced-fields">
            <TextField
              label={copy('Firmware page ID or key', 'شناسه یا کلید صفحه میان‌افزار')}
              value={menuPage}
              dir="ltr"
              spellCheck={false}
              onChange={(event) => setMenuPage(event.target.value)}
              action={<Button icon={LayoutDashboard} disabled={!online || !menuPage.trim()} busy={busy === `menu page ${menuPage.trim()}`} onClick={() => void run(`menu page ${quoteArgument(menuPage.trim())}`)}>{copy('Go to page', 'رفتن به صفحه')}</Button>}
            />
            <TextField
              label={copy('Host menu ID · optional', 'شناسه منوی میزبان · اختیاری')}
              value={hostMenuID}
              dir="ltr"
              spellCheck={false}
              onChange={(event) => setHostMenuID(event.target.value)}
              action={<>
                <Button icon={PanelTopOpen} disabled={!online} busy={busy === `host-menu open${hostMenuID.trim() ? ` ${quoteArgument(hostMenuID.trim())}` : ''}`} onClick={() => void run(`host-menu open${hostMenuID.trim() ? ` ${quoteArgument(hostMenuID.trim())}` : ''}`)}>{copy('Open overlay', 'باز کردن لایه')}</Button>
                <Button icon={RefreshCw} disabled={!online} busy={busy === 'host-menu refresh'} onClick={() => void run('host-menu refresh')}>{copy('Refresh', 'تازه‌سازی')}</Button>
                <Button icon={X} busy={busy === 'host-menu close'} onClick={() => void run('host-menu close')}>{copy('Close', 'بستن')}</Button>
              </>}
            />
            <TextField
              label={copy('Short 7-segment label', 'برچسب کوتاه نمایشگر')}
              hint={copy('Select a HOST menu ID above, then enter 1–4 printable ASCII characters.', 'شناسه منوی میزبان را در بالا انتخاب کنید و ۱ تا ۴ نویسه ASCII وارد کنید.')}
              value={hostMenuLabel}
              dir="ltr"
              maxLength={4}
              spellCheck={false}
              onChange={(event) => setHostMenuLabel(event.target.value)}
              action={<Button icon={Save} disabled={!hostMenuLabelUpdate} busy={busy === hostMenuLabelUpdate} onClick={() => hostMenuLabelUpdate && void run(hostMenuLabelUpdate)}>{copy('Save label', 'ذخیره برچسب')}</Button>}
            />
          </div>
          <div className="advanced-fields advanced-fields--layout">
            <TextField label={copy('Visibility mask', 'ماسک نمایش')} value={menuMask} dir="ltr" spellCheck={false} onChange={(event) => setMenuMask(event.target.value)} />
            <TextField label={copy('Ordered page references', 'ترتیب شناسه یا کلید صفحات')} value={menuOrder} dir="ltr" spellCheck={false} onChange={(event) => setMenuOrder(event.target.value)} />
            <Button icon={ShieldAlert} disabled={!menuMask.trim() || !normalizeTokens(menuOrder)} onClick={() => prepare(`menu layout set ${menuMask.trim()} ${normalizeTokens(menuOrder)}`, copy('Layout writes are schema-sensitive and persistent on supported firmware.', 'نوشتن چیدمان وابسته به ساختار و در میان‌افزارهای پشتیبانی‌شده ماندگار است.'), 'caution', true)}>{copy('Prepare layout write', 'آماده‌سازی نوشتن چیدمان')}</Button>
          </div>
        </AdvancedPanel>

        <AdvancedPanel
          icon={SlidersHorizontal}
          eyebrow="WS281X + STATUS RGB"
          title={copy('Per-pixel light & effect engine', 'نور هر پیکسل و موتور افکت')}
          detail={copy('Address one of eleven pixels or inspect and control configured status effects.', 'یکی از یازده پیکسل را کنترل یا افکت‌های وضعیت تعریف‌شده را بررسی کنید.')}
        >
          <div className="advanced-fields advanced-fields--pixel">
            <TextField label={copy('Pixel 0..10', 'پیکسل ۰ تا ۱۰')} type="number" min={0} max={10} value={pixel} onChange={(event) => setPixel(boundedInteger(event.target.value, 0, 0, 10))} />
            <TextField label="R" type="number" min={0} max={255} value={pixelRed} onChange={(event) => setPixelRed(boundedInteger(event.target.value, 0, 0, 255))} />
            <TextField label="G" type="number" min={0} max={255} value={pixelGreen} onChange={(event) => setPixelGreen(boundedInteger(event.target.value, 0, 0, 255))} />
            <TextField label="B" type="number" min={0} max={255} value={pixelBlue} onChange={(event) => setPixelBlue(boundedInteger(event.target.value, 0, 0, 255))} />
          </div>
          <RangeField label={copy('Pixel brightness', 'روشنایی پیکسل')} value={pixelBrightness} min={0} max={255} disabled={!online} onChange={setPixelBrightness} />
          <Button tone="primary" icon={Palette} disabled={!online} busy={busy === `strip pixel ${pixel} ${pixelRed} ${pixelGreen} ${pixelBlue} ${pixelBrightness}`} onClick={() => void run(`strip pixel ${pixel} ${pixelRed} ${pixelGreen} ${pixelBlue} ${pixelBrightness}`)}>{copy('Apply pixel', 'اعمال پیکسل')}</Button>
          <div className="advanced-divider" />
          <TextField label={copy('Configured effect name', 'نام افکت تعریف‌شده')} value={effectName} dir="ltr" spellCheck={false} onChange={(event) => setEffectName(event.target.value)} />
          <div className="advanced-actions">
            <Button icon={Sparkles} busy={busy === 'rgb effect list'} onClick={() => void run('rgb effect list')}>{copy('List effects', 'فهرست افکت‌ها')}</Button>
            <Button icon={Activity} busy={busy === 'rgb effect status'} onClick={() => void run('rgb effect status')}>{copy('Effect status', 'وضعیت افکت')}</Button>
            <Button icon={Play} disabled={!online || !effectName.trim()} busy={busy === `rgb effect play ${quoteArgument(effectName.trim())}`} onClick={() => void run(`rgb effect play ${quoteArgument(effectName.trim())}`)}>{copy('Play effect', 'اجرای افکت')}</Button>
            <Button icon={CircleStop} busy={busy === 'rgb effect stop'} onClick={() => void run('rgb effect stop')}>{copy('Stop effect', 'توقف افکت')}</Button>
          </div>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Radio}
          eyebrow="433 MHz"
          title={copy('RF inspection, removal & mapping', 'بررسی، حذف و نگاشت RF')}
          detail={copy('Reads execute directly; persistent removal and mapping always enter the review dock first.', 'خواندن مستقیم انجام می‌شود؛ حذف و نگاشت ماندگار ابتدا وارد بخش بازبینی می‌شوند.')}
          status={online ? copy('receiver ready', 'گیرنده آماده') : copy('device required', 'نیازمند برد')}
          tone={online ? 'info' : 'neutral'}
        >
          <div className="advanced-actions">
            <Button icon={Antenna} busy={busy === 'rf status'} onClick={() => void run('rf status')}>{copy('Learn status', 'وضعیت یادگیری')}</Button>
            <Button icon={ListTree} disabled={!online} busy={busy === 'rf list'} onClick={() => void run('rf list')}>{copy('Inspect learned entries', 'بررسی ورودی‌های آموخته‌شده')}</Button>
          </div>
          <TextField label={copy('Learned entry ID', 'شناسه ورودی آموخته‌شده')} value={rfID} dir="ltr" inputMode="numeric" onChange={(event) => setRFID(event.target.value)} />
          <div className="advanced-actions">
            <Button tone="danger" icon={Trash2} disabled={!rfID.trim()} onClick={() => prepare(`rf remove ${rfID.trim()}`, copy('This permanently removes one learned RF entry from the board.', 'این فرمان یک ورودی RF آموخته‌شده را برای همیشه از برد حذف می‌کند.'), 'danger', true)}>{copy('Prepare removal', 'آماده‌سازی حذف')}</Button>
          </div>
          <div className="advanced-field">
            <label>{copy('Mapping kind', 'نوع نگاشت')}</label>
            <Segmented value={rfKind} label={copy('RF mapping kind', 'نوع نگاشت RF')} options={[
              { value: 'key', label: copy('Key', 'کلید') },
              { value: 'menu', label: copy('Menu', 'منو') },
              { value: 'relay', label: copy('Relay', 'رله') },
              { value: 'side', label: copy('Motion', 'حرکت') },
              { value: 'pwm', label: 'PWM' },
              { value: 'none', label: copy('None', 'بدون نگاشت') },
            ]} onChange={setRFDefaults} />
          </div>
          {rfKind !== 'none' && (
            <div className="advanced-fields advanced-fields--split">
              <TextField label={copy('Target / action', 'مقصد / عمل')} hint={copy('Key 1..4 · menu prev/next/dec/inc · relay 5..8 · side left/right · PWM 0..10', 'کلید ۱..۴ · منو prev/next/dec/inc · رله ۵..۸ · سمت left/right · PWM ۰..۱۰')} value={rfValue} dir="ltr" spellCheck={false} onChange={(event) => setRFValue(event.target.value)} />
              {rfKind !== 'menu' && <TextField label={copy('Behavior', 'رفتار')} hint={rfKind === 'side' ? 'up | down | stop' : 'press | toggle | momentary'} value={rfBehavior} dir="ltr" spellCheck={false} onChange={(event) => setRFBehavior(event.target.value)} />}
            </div>
          )}
          <div className="advanced-command-inline" dir="ltr"><code>{rfMapCommand}</code></div>
          <Button icon={ShieldAlert} disabled={!rfID.trim()} onClick={() => prepare(rfMapCommand, copy('RF mappings affect physical outputs and must match the registered action schema.', 'نگاشت RF بر خروجی فیزیکی اثر می‌گذارد و باید دقیقاً با ساختار فرمان سازگار باشد.'), 'caution', true)}>{copy('Review mapping', 'بازبینی نگاشت')}</Button>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Workflow}
          eyebrow={copy('MCU-TIMED', 'زمان‌بندی‌شده روی MCU')}
          title={copy('Macro inspection & recording', 'بررسی و ضبط ماکرو')}
          detail={copy('Inspect compiled timing; record exact host, front-panel, and RF action deltas; then name, categorize, save, or discard deliberately.', 'زمان‌بندی کامپایل‌شده را ببینید؛ اختلاف زمانی دقیق کنش‌های میزبان، پنل و RF را ضبط و سپس آگاهانه نام‌گذاری، دسته‌بندی، ذخیره یا دور بریزید.')}
        >
          <div className="advanced-actions">
            <Button icon={List} busy={busy === 'macro list'} onClick={() => void run('macro list')}>{copy('List', 'فهرست')}</Button>
            <Button icon={Play} busy={busy === 'macro status'} onClick={() => void run('macro status')}>{copy('Playback status', 'وضعیت اجرا')}</Button>
            <Button icon={CircleDot} busy={busy === 'macro record status'} onClick={() => void run('macro record status')}>{copy('Recording status', 'وضعیت ضبط')}</Button>
          </div>
          <div className="advanced-fields">
            <TextField
              label={copy('Macro name or ID', 'نام یا شناسه ماکرو')}
              value={macroRef}
              dir="ltr"
              spellCheck={false}
              onChange={(event) => setMacroRef(event.target.value)}
              action={<Button icon={ListChecks} disabled={!macroRef.trim()} busy={busy === `macro show ${quoteArgument(macroRef.trim())}`} onClick={() => void run(`macro show ${quoteArgument(macroRef.trim())}`)}>{copy('Inspect exact steps', 'بررسی گام‌های دقیق')}</Button>}
            />
          </div>
          <div className="advanced-fields advanced-fields--record">
            <TextField label={copy('New or updated name', 'نام جدید یا ویرایش‌شده')} value={macroName} dir="ltr" spellCheck={false} onChange={(event) => setMacroName(event.target.value)} />
            <TextField label={copy('Category', 'دسته‌بندی')} value={macroCategory} dir="ltr" spellCheck={false} onChange={(event) => setMacroCategory(event.target.value)} />
            <div className="advanced-field">
              <label>{copy('Color', 'رنگ')}</label>
              <Segmented value={macroColor} label={copy('Macro color', 'رنگ ماکرو')} options={[
                { value: 'red', label: copy('Red', 'قرمز') },
                { value: 'blue', label: copy('Blue', 'آبی') },
                { value: 'violet', label: copy('Violet', 'بنفش') },
                { value: 'green', label: copy('Green', 'سبز') },
                { value: 'white', label: copy('White', 'سفید') },
              ]} onChange={setMacroColor} />
            </div>
            <Button tone="primary" icon={CircleDot} disabled={!online || !macroName.trim() || (!!macroColor.trim() && !macroCategory.trim())} busy={busy === macroRecordCommand} onClick={() => void run(macroRecordCommand)}>{copy('Start recording', 'شروع ضبط')}</Button>
            <Button icon={Save} disabled={!macroRef.trim() || !macroName.trim()} busy={busy === macroUpdateCommand} onClick={() => void run(macroUpdateCommand)}>{copy('Save name/category', 'ذخیره نام/دسته')}</Button>
          </div>
          <div className="advanced-actions">
            <Button icon={Save} busy={busy === 'macro record save'} onClick={() => void run('macro record save')}>{copy('Save recording', 'ذخیره ضبط')}</Button>
            <Button icon={Trash2} busy={busy === 'macro record discard'} onClick={() => void run('macro record discard')}>{copy('Discard recording', 'حذف ضبط')}</Button>
            <Button icon={CircleStop} disabled={!online} busy={busy === 'macro cancel'} onClick={() => void run('macro cancel')}>{copy('Cancel safely', 'لغو امن')}</Button>
            <Button tone="danger" icon={ShieldAlert} disabled={!online} onClick={() => prepare('macro cancel keep', copy('Cancelling with keep deliberately leaves current physical outputs unchanged.', 'لغو با حفظ خروجی، وضعیت فعلی خروجی‌های فیزیکی را عمداً نگه می‌دارد.'), 'danger', true)}>{copy('Prepare cancel + keep', 'آماده‌سازی لغو با حفظ خروجی')}</Button>
          </div>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Cable}
          eyebrow="I²C + LCD"
          title={copy('Cooperative bus & raw transfer', 'گذرگاه اشتراکی و انتقال خام')}
          detail={copy('Inspect LCD ownership and scan safely; raw schemas are reviewed before the host lease is acquired.', 'مالکیت LCD را ببینید و امن پویش کنید؛ انتقال خام پیش از گرفتن دسترسی میزبان بازبینی می‌شود.')}
          status={online ? `LCD 0x${snapshot.status.lcd_address.toString(16).padStart(2, '0').toUpperCase()}` : copy('offline', 'آفلاین')}
          tone={online ? 'info' : 'neutral'}
        >
          <div className="advanced-actions">
            <Button icon={ScanSearch} disabled={!online} busy={busy === 'i2c scan'} onClick={() => void run('i2c scan')}>{copy('Scan bus', 'پویش گذرگاه')}</Button>
            <Button icon={MonitorCog} busy={busy === 'i2c lcd status'} onClick={() => void run('i2c lcd status')}>{copy('LCD status', 'وضعیت LCD')}</Button>
            <Button icon={RefreshCw} disabled={!online} busy={busy === 'i2c lcd rescan'} onClick={() => void run('i2c lcd rescan')}>{copy('Rescan LCD', 'پویش دوباره LCD')}</Button>
            <Button icon={Unplug} disabled={!online} busy={busy === 'i2c release'} onClick={() => void run('i2c release')}>{copy('Release lease', 'آزادسازی دسترسی')}</Button>
          </div>
          <div className="advanced-fields advanced-fields--i2c">
            <TextField label={copy('7-bit address', 'نشانی ۷ بیتی')} value={i2cAddress} dir="ltr" spellCheck={false} onChange={(event) => setI2CAddress(event.target.value)} />
            <TextField label={copy('Lease seconds 0..10', 'مدت دسترسی ۰..۱۰ ثانیه')} type="number" min={0} max={10} value={i2cLease} onChange={(event) => setI2CLease(boundedInteger(event.target.value, 2, 0, 10))} />
            <TextField label={copy('Read count', 'تعداد بایت خواندن')} type="number" min={0} max={16} value={i2cReadCount} onChange={(event) => setI2CReadCount(boundedInteger(event.target.value, 0, 0, 16))} />
            <TextField label={copy('Write bytes · numeric tokens', 'بایت‌های نوشتن · توکن عددی')} hint="0x00 0x01 255" value={i2cWrite} dir="ltr" spellCheck={false} onChange={(event) => setI2CWrite(event.target.value)} />
          </div>
          <div className="advanced-command-inline" dir="ltr"><code>{i2cCommand}</code></div>
          <Button icon={ShieldAlert} onClick={() => prepare(i2cCommand, copy('Raw I²C transfer parameters are sent exactly as reviewed and acquire a bounded cooperative lease.', 'پارامترهای انتقال خام I²C دقیقاً پس از بازبینی ارسال و دسترسی اشتراکی محدود دریافت می‌شود.'), 'caution', true)}>{copy('Review raw transfer', 'بازبینی انتقال خام')}</Button>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Keyboard}
          eyebrow={copy('GLOBAL INPUT', 'ورودی سراسری')}
          title={copy('Keyboard lifecycle & emergency release', 'چرخه صفحه‌کلید و آزادسازی اضطراری')}
          detail={copy('Inspect bindings, control the primary Windows hook, or release every held and latched output.', 'نگاشت‌ها را ببینید، هوک اصلی ویندوز را کنترل یا همه خروجی‌های نگه‌داشته‌شده را آزاد کنید.')}
        >
          <div className="advanced-actions">
            <Button icon={Activity} busy={busy === 'keyboard status'} onClick={() => void run('keyboard status')}>{copy('Hook status', 'وضعیت هوک')}</Button>
            <Button icon={ListTree} busy={busy === 'keyboard list'} onClick={() => void run('keyboard list')}>{copy('Binding catalog', 'فهرست نگاشت‌ها')}</Button>
            <Button icon={Keyboard} onClick={() => prepare('keyboard enable', copy('Enabling the global hook activates the configured bindings; review them first with Keyboard list.', 'فعال‌سازی هوک سراسری، نگاشت‌های پیکربندی‌شده را فعال می‌کند؛ ابتدا فهرست صفحه‌کلید را بازبینی کنید.'), 'caution')}>{copy('Review enable', 'بازبینی فعال‌سازی')}</Button>
            <Button icon={KeyboardOff} busy={busy === 'keyboard disable'} onClick={() => void run('keyboard disable')}>{copy('Disable & release', 'غیرفعال و آزادسازی')}</Button>
            <Button tone="danger" icon={CircleStop} busy={busy === 'keyboard stop'} onClick={() => void run('keyboard stop')}>{copy('Emergency output release', 'آزادسازی اضطراری خروجی‌ها')}</Button>
          </div>
          <p className="advanced-note advanced-note--safe">{copy('The stop command releases keyboard-held and latched outputs without shutting down the host.', 'فرمان توقف، خروجی‌های نگه‌داشته‌شده توسط صفحه‌کلید را بدون خاموش‌کردن میزبان آزاد می‌کند.')}</p>
        </AdvancedPanel></>}

        <AdvancedPanel
          icon={MonitorCog}
          eyebrow={copy('POLICY-GATED OS', 'سیستم‌عامل تحت سیاست امنیتی')}
          title={copy('Brightness, virtual key & power preparation', 'روشنایی، کلید مجازی و آماده‌سازی توان')}
          detail={copy('Read host state directly. Virtual-key and power requests are composed visibly before submission.', 'وضعیت میزبان را مستقیم بخوانید؛ کلید مجازی و فرمان توان پیش از ارسال به‌وضوح ساخته می‌شوند.')}
        >
          <div className="advanced-actions">
            <Button icon={Gauge} busy={busy === 'os status'} onClick={() => void run('os status')}>{copy('Host status', 'وضعیت میزبان')}</Button>
            <Button icon={ShieldAlert} busy={busy === 'os policy'} onClick={() => void run('os policy')}>{copy('Action policy', 'سیاست فرمان‌ها')}</Button>
            <Button icon={SunMedium} busy={busy === 'os brightness get'} onClick={() => void run('os brightness get')}>{copy('Read brightness', 'خواندن روشنایی')}</Button>
          </div>
          <div className="advanced-fields advanced-fields--split">
            <TextField label={copy('Virtual key', 'کلید مجازی')} hint="F13 · MEDIA_PLAY_PAUSE · VOLUME_UP" value={virtualKey} dir="ltr" spellCheck={false} onChange={(event) => setVirtualKey(event.target.value)} />
            <TextField label={copy('Hold ms', 'مدت نگه‌داری ms')} type="number" min={0} max={5000} value={virtualHold} onChange={(event) => setVirtualHold(boundedInteger(event.target.value, 80, 0, 5000))} />
          </div>
          <Button icon={ShieldAlert} disabled={!virtualKey.trim()} onClick={() => prepare(`os key ${quoteArgument(virtualKey.trim())} ${virtualHold}`, copy('The configured OS policy still authorizes or rejects this reviewed key request.', 'سیاست سیستم‌عامل همچنان این درخواست کلید بازبینی‌شده را می‌پذیرد یا رد می‌کند.'), 'caution')}>
            {copy('Prepare virtual key', 'آماده‌سازی کلید مجازی')}
          </Button>
          <div className="advanced-divider" />
          <div className="advanced-field">
            <label>{copy('Power action', 'فرمان توان')}</label>
            <Segmented value={powerAction} label={copy('Power action', 'فرمان توان')} options={[
              { value: 'lock', label: copy('Lock', 'قفل') },
              { value: 'sleep', label: copy('Sleep', 'خواب') },
              { value: 'hibernate', label: copy('Hibernate', 'هایبرنیت') },
              { value: 'shutdown', label: copy('Shutdown', 'خاموشی') },
              { value: 'restart', label: copy('Restart', 'راه‌اندازی') },
              { value: 'logoff', label: copy('Log off', 'خروج') },
            ]} onChange={setPowerAction} />
          </div>
          <TextField label={copy('Configured confirmation token', 'توکن تأیید تنظیم‌شده')} type="password" autoComplete="off" value={powerToken} dir="ltr" onChange={(event) => setPowerToken(event.target.value)} />
          <Button tone="danger" icon={ShieldAlert} disabled={!powerToken.trim()} onClick={() => prepare(`os power ${powerAction} [REDACTED]`, copy('This can change the host power session. The secret is sent only through typed RPC, and the review dock requires a second typed RUN confirmation.', 'این فرمان می‌تواند وضعیت توان میزبان را تغییر دهد. توکن محرمانه فقط از RPC تایپ‌شده ارسال می‌شود و بخش بازبینی به تأیید تایپی RUN نیاز دارد.'), 'danger', false, () => rpc('controller.os.power', { action: powerAction, confirmation: powerToken }))}>
            {copy('Prepare only — do not execute', 'فقط آماده‌سازی — بدون اجرا')}
          </Button>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Network}
          eyebrow={copy('TYPED HOST SERVICES', 'سرویس‌های تایپ‌شده میزبان')}
          title={copy('Messages, discovery & history', 'پیام‌ها، کشف شبکه و تاریخچه')}
          detail={copy('Typed RPC is used where the shell intentionally has no alias; results remain visible as protocol data.', 'در عملیات بدون فرمان پوسته، RPC تایپ‌شده استفاده می‌شود و نتیجه به‌صورت داده پروتکل دیده می‌شود.')}
        >
          <form className="advanced-form" onSubmit={sendMessage}>
            <div className="advanced-field">
              <label>{copy('Message target', 'مقصد پیام')}</label>
              <Segmented value={messageTarget} label={copy('Message target', 'مقصد پیام')} options={online ? [
                { value: 'host', label: copy('Event stream', 'جریان رویداد') },
                { value: 'lcd', label: copy('Physical LCD', 'LCD فیزیکی') },
              ] : [{ value: 'host', label: copy('Event stream', 'جریان رویداد') }]} onChange={setMessageTarget} />
            </div>
            <div className="advanced-fields advanced-fields--message">
              <TextField label={copy('Message type', 'نوع پیام')} hint="a-z · 0-9 · dot · dash · underscore" value={messageType} dir="ltr" spellCheck={false} maxLength={32} onChange={(event) => setMessageType(event.target.value)} />
              {messageTarget === 'host' ? (
                <TextField label={copy('Event text', 'متن رویداد')} value={messageText} maxLength={4096} onChange={(event) => setMessageText(event.target.value)} hint={`${messageByteLength} / 4096 UTF-8 bytes`} aria-invalid={messageByteLength > 4096} />
              ) : (
                <div className="advanced-fields advanced-fields--split">
                  <TextField label={copy('LCD line 1', 'خط اول LCD')} value={messageLine1} maxLength={16} dir="ltr" spellCheck={false} onChange={(event) => setMessageLine1(event.target.value)} />
                  <TextField label={copy('LCD line 2', 'خط دوم LCD')} value={messageLine2} maxLength={16} dir="ltr" spellCheck={false} onChange={(event) => setMessageLine2(event.target.value)} />
                </div>
              )}
              <Button type="submit" tone="primary" icon={Send} busy={serviceBusy === 'message'} disabled={!messageType.trim() || (messageTarget === 'host' ? !messageText.trim() || messageByteLength > 4096 : !lcdMessageValid) || (messageNeedsBoard && !online)}>{copy('Send typed message', 'ارسال پیام تایپ‌شده')}</Button>
            </div>
            {messageTarget === 'lcd' && <p className={`advanced-note${lcdMessageValid ? ' advanced-note--safe' : ''}`}>{copy('Up to two 16-character lines; printable ASCII only. Hardware LCD text is intentionally LTR.', 'حداکثر دو خط ۱۶ نویسه‌ای و فقط ASCII قابل چاپ؛ متن LCD سخت‌افزاری عمداً چپ‌به‌راست است.')}</p>}
          </form>
          <div className="advanced-divider" />
          <div className="advanced-toggle-stack advanced-toggle-stack--compact">
            <Toggle checked={discoverMDNS} onChange={setDiscoverMDNS} label="mDNS" />
            <Toggle checked={discoverSSDP} onChange={setDiscoverSSDP} label="SSDP" />
          </div>
          <RangeField label={copy('Discovery timeout', 'مهلت کشف')} value={discoverTimeout} min={100} max={5000} step={100} unit="ms" onChange={setDiscoverTimeout} />
          <Button icon={ScanSearch} busy={serviceBusy === 'discovery'} disabled={!discoverMDNS && !discoverSSDP} onClick={() => void performRPC('discovery', () => rpc('controller.discovery.scan', { timeout_ms: discoverTimeout, mdns: discoverMDNS, ssdp: discoverSSDP }))}>{copy('Discover trusted hosts', 'کشف میزبان‌ها')}</Button>
          <div className="advanced-divider" />
          <div className="advanced-fields advanced-fields--history">
            <TextField label={copy('Look back · minutes', 'بازه گذشته · دقیقه')} type="number" min={1} max={43200} value={historyMinutes} onChange={(event) => setHistoryMinutes(boundedInteger(event.target.value, 60, 1, 43200))} />
            <TextField label={copy('Timeline limit', 'حد رویدادها')} type="number" min={1} max={5000} value={historyLimit} onChange={(event) => setHistoryLimit(boundedInteger(event.target.value, 100, 1, 5000))} />
            <Button icon={Database} busy={serviceBusy === 'history-status'} onClick={() => queryHistory('status')}>{copy('Status history', 'تاریخچه وضعیت')}</Button>
            <Button icon={Activity} busy={serviceBusy === 'history-timeline'} onClick={() => queryHistory('timeline')}>{copy('Event timeline', 'خط زمانی رویداد')}</Button>
          </div>
          <pre className="advanced-service-output" dir="ltr" aria-live="polite">{serviceOutput}</pre>
        </AdvancedPanel>

        <AdvancedPanel
          icon={Cpu}
          eyebrow={copy('READ-FIRST RECOVERY', 'بازیابی با اولویت خواندن')}
          title={copy('Firmware, toolchain & boot preparation', 'میان‌افزار، زنجیره‌ابزار و آماده‌سازی بوت')}
          detail={copy('Identity and toolchain reads are immediate; bootloader probe/read/backup commands are prepared only.', 'خواندن شناسه و زنجیره‌ابزار مستقیم است؛ فرمان‌های پروب، خواندن و پشتیبان بوت فقط آماده می‌شوند.')}
        >
          <div className="advanced-actions">
            {online && <Button icon={Cpu} busy={busy === 'hello'} onClick={() => void run('hello')}>{copy('Firmware identity', 'شناسه میان‌افزار')}</Button>}
            <Button icon={MemoryStick} busy={busy === 'toolchain profile'} onClick={() => void run('toolchain profile')}>{copy('Resolved profile', 'پروفایل حل‌شده')}</Button>
            <Button icon={Cpu} busy={busy === 'toolchain core-info'} onClick={() => void run('toolchain core-info')}>{copy('Core info', 'اطلاعات هسته')}</Button>
          </div>
          {online && <><TextField label={copy('Backup directory or read output file', 'پوشه پشتیبان یا فایل خروجی خواندن')} value={bootPath} dir="ltr" spellCheck={false} onChange={(event) => setBootPath(event.target.value)} />
          <div className="advanced-actions">
            <Button icon={ScanSearch} onClick={() => prepare('boot probe', copy('Prepare a non-write bootloader probe; execution temporarily hands over the serial port.', 'پروب غیرنوشتنی بوت آماده می‌شود؛ اجرا موقتاً درگاه سریال را تحویل می‌دهد.'), 'caution', true)}>{copy('Prepare probe', 'آماده‌سازی پروب')}</Button>
            <Button icon={SquareTerminal} onClick={() => prepare('boot info', copy('Prepare a metadata read through the guarded programming lifecycle.', 'خواندن فراداده از مسیر امن پروگرام آماده می‌شود.'), 'caution', true)}>{copy('Prepare metadata', 'آماده‌سازی فراداده')}</Button>
            <Button icon={HardDriveDownload} disabled={!bootPath.trim()} onClick={() => prepare(`boot backup ${quoteArgument(bootPath.trim())}`, copy('Prepare a backup-only operation to the reviewed destination.', 'عملیات فقط پشتیبان‌گیری در مقصد بازبینی‌شده آماده می‌شود.'), 'caution', true)}>{copy('Prepare backup', 'آماده‌سازی پشتیبان')}</Button>
            <Button icon={HardDriveDownload} disabled={!bootPath.trim()} onClick={() => prepare(`boot read ${quoteArgument(bootPath.trim())}`, copy('Prepare a flash read to the reviewed output file; no flash write command is exposed here.', 'خواندن فلش در فایل بازبینی‌شده آماده می‌شود؛ هیچ فرمان نوشتن فلش در این بخش وجود ندارد.'), 'caution', true)}>{copy('Prepare flash read', 'آماده‌سازی خواندن فلش')}</Button>
          </div></>}
        </AdvancedPanel>
      </div>

      <section id="advanced-command-review" className={`advanced-review advanced-review--${review.risk}`} aria-live="polite">
        <header>
          <span className="advanced-review__icon"><ShieldAlert size={19} aria-hidden="true" /></span>
          <div>
            <small>{copy('EXPLICIT COMMAND REVIEW', 'بازبینی صریح فرمان')}</small>
            <h3>{review.command ? copy('Prepared, never implicit', 'آماده، هرگز ضمنی') : copy('No command prepared', 'فرمانی آماده نشده')}</h3>
          </div>
          {review.command && <StatusBadge tone={review.risk === 'danger' ? 'bad' : 'warn'}>{review.risk.toUpperCase()}</StatusBadge>}
        </header>
        <div className="advanced-review__command" dir="ltr">
          <span>pc›</span>
          <code>{review.command ? redactSensitiveCommand(review.command) : copy('Choose a prepare action above.', 'یک فرمان آماده‌سازی را از بالا انتخاب کنید.')}</code>
        </div>
        {review.reason && <p>{review.reason}</p>}
        {review.risk === 'danger' && (
          <TextField
            label={copy('Type RUN to arm this reviewed high-impact command', 'برای فعال‌سازی فرمان پراثر، RUN را تایپ کنید')}
            value={dangerArm}
            dir="ltr"
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => setDangerArm(event.target.value)}
          />
        )}
        <div className="advanced-review__actions">
          <Button tone={review.risk === 'danger' ? 'danger' : 'primary'} icon={SquareTerminal} disabled={reviewBlocked} busy={reviewBusy || busy === review.command} onClick={() => void executeReviewed()}>{copy('Run reviewed command', 'اجرای فرمان بازبینی‌شده')}</Button>
          <Button icon={Eraser} disabled={!review.command || reviewBusy} onClick={() => { setReview({ command: '', reason: '', risk: 'normal', needsDevice: false }); setDangerArm('') }}>{copy('Clear', 'پاک‌کردن')}</Button>
          {review.needsDevice && !online && <span className="advanced-review__blocked">{copy('Connect an authenticated board before execution.', 'پیش از اجرا یک برد معتبر متصل کنید.')}</span>}
        </div>
      </section>

      <footer className="advanced-footnote">
        <ShieldAlert size={15} aria-hidden="true" />
        <span>{copy(
          'Controls reflect implemented protocol paths. Physical peripherals and external services still require live-device acceptance testing.',
          'کنترل‌ها بر اساس مسیرهای پیاده‌سازی‌شده پروتکل هستند؛ تجهیزات فیزیکی و سرویس‌های بیرونی همچنان به آزمون پذیرش زنده نیاز دارند.',
        )}</span>
      </footer>
    </section>
  )
}

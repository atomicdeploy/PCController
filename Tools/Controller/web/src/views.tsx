import {
  type FormEvent,
  type ReactNode,
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  Activity,
  AlertOctagon,
  ArrowDownToLine,
  Binary,
  Bluetooth,
  Boxes,
  Braces,
  Cable,
  ChartNoAxesCombined,
  CheckCircle2,
  ChevronDown,
  CirclePower,
  CircuitBoard,
  CloudDownload,
  Code2,
  Cpu,
  Database,
  DoorOpen,
  Download,
  Eye,
  EyeOff,
  ExternalLink,
  Fan,
  FileJson2,
  FileSpreadsheet,
  Gauge,
  GripVertical,
  HardDrive,
  HeartPulse,
  Info,
  Languages,
  Lightbulb,
  ListFilter,
  MemoryStick,
  MessageSquareText,
  Moon,
  MoreHorizontal,
  Network,
  PackageSearch,
  PanelTop,
  PlugZap,
  Power,
  Radio,
  RefreshCw,
  Send,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  SquareTerminal,
  Sun,
  TableProperties,
  Thermometer,
  TimerReset,
  ToggleRight,
  Unplug,
  Upload,
  Usb,
  Volume2,
  Waves,
  Wifi,
  Zap,
} from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import {
  Button,
  Card,
  DataRow,
  EmptyState,
  HoldActionButton,
  Icon,
  MetricCard,
  RangeField,
  SectionTitle,
  Segmented,
  Sparkline,
  StatusBadge,
  TextField,
  Toggle,
} from './components'
import { execute, rpc, type StreamState } from './api'
import { settingsSetCommand } from './command-line'
import { EventList } from './event-collection'
import { HotkeyEditor } from './hotkey-settings-editor'
import { PeripheralNamesEditor } from './peripheral-names-editor'
import {
  normalizePWMValues,
  pwmPercent,
  pwmValue,
  PWMMutationScheduler,
  PWMOperationQueue,
  PWMReconciler,
  USER_PWM_CHANNELS,
} from './pwm-authority'
import { formatClock, formatCompact, formatConnectionState, formatDuration, formatNumber, type MessageKey } from './i18n'
const TelemetryChart = lazy(() => import('./telemetry-chart').then((module) => ({ default: module.TelemetryChart })))
import {
  integrationSettingsEqual,
  normalizeAppTitle,
  normalizeRootURLInput,
  normalizeSegmentPagesInput,
  normalizeSegmentTextInput,
  normalizeSessionToken,
  segmentScrollSettingsEqual,
  validateAppTitle,
  validateDataHubURL,
  validateLocalDeviceURL,
  validateSegmentPages,
  validateSegmentText,
} from './settings-validation'
import type { TerminalEntry as TabTerminalEntry } from './tab-channel'
import type {
  Appearance,
  BoardSettingsReadState,
  ControllerEvent,
  DialogState,
  HostUISettings,
  LifecycleSafetyAction,
  LocalIntegrationSettings,
  Locale,
  LocalDeviceSnapshot,
  MetricSample,
  PWMValues,
  Snapshot,
  SegmentScrollSettings,
  UIConfig,
} from './types'
import { buzzerPathFromState, type BuzzerPath } from './buzzer-routing'
import { type DashboardCardID, loadDashboardLayout, moveDashboardCard, saveDashboardLayout, toggleDashboardCard } from './dashboard-layout'

export interface SharedViewProps {
  appTitle: string
  snapshot: Snapshot
  samples: MetricSample[]
  events: ControllerEvent[]
  locale: Locale
  t: (key: MessageKey) => string
  command: (command: string, success?: string) => Promise<string>
  relayToggle: (relay: number, active: boolean) => Promise<void>
  relayPending: ReadonlySet<number>
  refresh: () => Promise<void>
  openDialog: (dialog: Omit<DialogState, 'open'>) => void
  transport: {
    streamState: StreamState
    detail: string
    tabBusSupported: boolean
    tabPeers: number
    requestAuthentication: () => void
  }
  relayedTerminal: Array<TabTerminalEntry & { id: string; tabId: string }>
  broadcastTerminal: (entry: TabTerminalEntry) => void
  boardSettingsReadState: BoardSettingsReadState
  openAppPreferences: () => void
}

function pageDetail(snapshot: Snapshot, appTitle: string, locale: Locale): string {
  if (snapshot.connected) return snapshot.port.friendly_name || snapshot.port.product || snapshot.port.name || appTitle
  return snapshot.connection_reason || (locale === 'fa' ? 'برای دریافت تله‌متری زنده، یک کنترلر معتبر متصل کنید.' : 'Connect an authenticated controller to receive live telemetry.')
}

function values(samples: MetricSample[], field: keyof Omit<MetricSample, 'at'>): number[] {
  return samples.map((sample) => sample[field])
}

function eventTone(event: ControllerEvent): 'good' | 'warn' | 'bad' | 'info' {
  const kind = event.kind.toLowerCase()
  if (kind.includes('error') || kind.includes('fault') || kind.includes('hot')) return 'bad'
  if (kind.includes('warning') || kind.includes('door') || kind.includes('disconnect')) return 'warn'
  if (kind.includes('connect') || kind.includes('ready') || kind.includes('complete')) return 'good'
  return 'info'
}

export function dashboardSocketIsFresh(
  snapshot: Pick<Snapshot, 'connected' | 'status_updated'>,
  streamState: SharedViewProps['transport']['streamState'],
  now = Date.now(),
): boolean {
  if (!snapshot.connected || streamState !== 'open' || !snapshot.status_updated) return false
  const updated = new Date(snapshot.status_updated).getTime()
  return Number.isFinite(updated) && Math.max(0, now - updated) < 1000
}

export function dashboardRelayToggleCommand(relay: number, active: boolean): string {
  return `relay ${relay} ${active ? 'off' : 'on'}`
}

export function dashboardDeviceSummary(snapshot: Pick<Snapshot, 'connected' | 'connection_reason' | 'port' | 'hello'>, locale: Locale): { device: string; firmware: string } {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const version = [snapshot.hello.firmware_major, snapshot.hello.firmware_minor, snapshot.hello.firmware_patch]
  const versionText = version.every((value) => typeof value === 'number') ? `v${version.join('.')}` : ''
  const hash = typeof snapshot.hello.build_hash === 'number' ? snapshot.hello.build_hash.toString(16).toUpperCase().padStart(8, '0') : ''
  const build = snapshot.hello.build_timestamp?.trim() || [snapshot.hello.build_date, snapshot.hello.build_time].filter(Boolean).join(' ').trim()
  return {
    device: snapshot.hello.name?.trim() || snapshot.port.friendly_name?.trim() || snapshot.port.product?.trim() || snapshot.port.name?.trim() || (snapshot.connected ? copy('Connected controller', 'کنترلر متصل') : snapshot.connection_reason?.trim() || copy('Awaiting controller', 'در انتظار کنترلر')),
    firmware: [build, versionText, hash ? `#${hash}` : ''].filter(Boolean).join(' · ') || (snapshot.connected ? copy('Firmware identity pending', 'شناسهٔ میان‌افزار در انتظار است') : copy('No controller connected', 'کنترلری متصل نیست')),
  }
}

function DashboardCardFrame({
  id,
  title,
  order,
  collapsed,
  hidden,
  onToggleCollapsed,
  onToggleHidden,
  onDragStart,
  onDrop,
  children,
}: {
  id: DashboardCardID
  title: string
  order: number
  collapsed: boolean
  hidden: boolean
  onToggleCollapsed: () => void
  onToggleHidden: () => void
  onDragStart: (id: DashboardCardID) => void
  onDrop: (id: DashboardCardID) => void
  children: ReactNode
}) {
  if (hidden) return null
  return <section
    className={`dashboard-card-frame${collapsed ? ' is-collapsed' : ''}`}
    style={{ order }}
    draggable
    onDragStart={() => onDragStart(id)}
    onDragOver={(event) => event.preventDefault()}
    onDrop={() => onDrop(id)}
  >
    <div className="dashboard-card-frame__actions">
      <span title="Drag to reorder"><GripVertical size={15} /></span><strong>{title}</strong>
      <button type="button" title={collapsed ? 'Expand card' : 'Collapse card'} aria-label={collapsed ? `Expand ${title}` : `Collapse ${title}`} onClick={onToggleCollapsed}><ChevronDown size={15} /></button>
      <button type="button" title="Hide card" aria-label={`Hide ${title}`} onClick={onToggleHidden}><EyeOff size={15} /></button>
    </div>
    {!collapsed && children}
  </section>
}

// A real popover rather than <details>: native disclosure controls inherit
// browser colours and do not reliably close when a controller disappears.
function HeroContextMenu({ label, connected, children }: { label: string; connected: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const close = (restoreFocus = false) => {
    setOpen(false)
    if (restoreFocus) requestAnimationFrame(() => trigger.current?.focus())
  }
  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent | FocusEvent) => {
      if (root.current && !root.current.contains(event.target as Node)) close()
    }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); close(true) } }
    const blur = () => close()
    window.addEventListener('pointerdown', outside)
    window.addEventListener('focusin', outside)
    window.addEventListener('keydown', escape)
    window.addEventListener('blur', blur)
    window.addEventListener('hashchange', blur)
    return () => {
      window.removeEventListener('pointerdown', outside)
      window.removeEventListener('focusin', outside)
      window.removeEventListener('keydown', escape)
      window.removeEventListener('blur', blur)
      window.removeEventListener('hashchange', blur)
    }
  }, [open])
  useEffect(() => { if (!connected) close() }, [connected])
  return <div className="hero-context-menu" ref={root}>
    <button ref={trigger} type="button" className="hero-context-menu__trigger" aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((value) => !value)}><MoreHorizontal size={18} /></button>
    {open && <div className="hero-context-menu__panel" role="menu" onClickCapture={() => close()}>{children}</div>}
  </div>
}

export function DashboardView(props: SharedViewProps) {
  const { appTitle, snapshot, samples, events, locale, t, command, relayToggle, relayPending, refresh, openDialog, transport } = props
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const status = snapshot.status
  const authenticationRequired = props.transport.streamState === 'authentication-required'
  const connectedTone = snapshot.connected ? 'good' : authenticationRequired || snapshot.paused ? 'warn' : 'bad'
  const connectionLabel = authenticationRequired ? t('authenticationRequired') : formatConnectionState(locale, snapshot.connection_state, snapshot.connected, snapshot.paused)
  const hash = snapshot.hello.build_hash ? snapshot.hello.build_hash.toString(16).toUpperCase().padStart(8, '0') : '—'
  const activeRelayCount = Array.from({ length: 8 }, (_, index) => Boolean(status.active_relays & (1 << index))).filter(Boolean).length
  const configurationEventID = events.find((event) => event.kind === 'config')?.id ?? 0
  const [hostUI, setHostUI] = useState<HostUISettings | null>(null)
  const [layout, setLayout] = useState(loadDashboardLayout)
  const [draggedCard, setDraggedCard] = useState<DashboardCardID | null>(null)
  const [temperatureTab, setTemperatureTab] = useState<'led' | 'audio'>('led')
  const [telemetryMode, setTelemetryMode] = useState<'electrical' | 'power' | 'thermal'>('electrical')
  const [clock, setClock] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 250)
    return () => window.clearInterval(timer)
  }, [])
  useEffect(() => {
    let active = true
    void rpc<HostUISettings>('controller.ui.config.get')
      .then((value) => { if (active) setHostUI(value) })
      .catch(() => { if (active) setHostUI(null) })
    return () => { active = false }
  }, [configurationEventID])
  const peripheralName = (key: string, fallback: string) => hostUI?.peripheral_names?.[key]?.trim() || fallback
  const relayDefaults = [
    copy('Side A direction', 'جهت سمت A'), copy('Side A output', 'خروجی سمت A'),
    copy('Side B direction', 'جهت سمت B'), copy('Side B output', 'خروجی سمت B'),
    copy('User relay 5', 'رلهٔ کاربر ۵'), copy('User relay 6', 'رلهٔ کاربر ۶'),
    copy('User relay 7', 'رلهٔ کاربر ۷'), copy('User relay 8', 'رلهٔ کاربر ۸'),
  ]
  const socketFresh = dashboardSocketIsFresh(snapshot, transport.streamState, clock)
  const deviceSummary = dashboardDeviceSummary(snapshot, locale)
  const updateLayout = (change: (current: ReturnType<typeof loadDashboardLayout>) => ReturnType<typeof loadDashboardLayout>) => {
    setLayout((current) => {
      const next = change(current)
      saveDashboardLayout(next)
      return next
    })
  }
  const frame = (id: DashboardCardID, title: string, child: ReactNode) => <DashboardCardFrame
    id={id}
    title={title}
    order={layout.order.indexOf(id)}
    collapsed={layout.collapsed.includes(id)}
    hidden={layout.hidden.includes(id)}
    onToggleCollapsed={() => updateLayout((current) => ({ ...current, collapsed: toggleDashboardCard(current.collapsed, id) }))}
    onToggleHidden={() => updateLayout((current) => ({ ...current, hidden: toggleDashboardCard(current.hidden, id) }))}
    onDragStart={setDraggedCard}
    onDrop={(target) => { if (draggedCard) updateLayout((current) => moveDashboardCard(current, draggedCard, target)); setDraggedCard(null) }}
  >{child}</DashboardCardFrame>
  return (
    <>
      <SectionTitle
        eyebrow={snapshot.connected ? t('liveTelemetry') : connectionLabel}
        title={t('dashboard')}
        detail={authenticationRequired ? props.transport.detail : pageDetail(snapshot, appTitle, locale)}
        action={
          <div className="header-actions">
            <StatusBadge tone={connectedTone} pulse={snapshot.connection_state === 'connecting'}>
              {authenticationRequired
                ? connectionLabel
                : socketFresh
                  ? <><Wifi size={14} /> {t('online')}</>
                  : snapshot.connected
                    ? <><Wifi size={14} /> {copy('STALE', 'قدیمی')}</>
                    : snapshot.connection_state === 'connecting'
                      ? t('connecting')
                      : t('offline')}
            </StatusBadge>
            {authenticationRequired
              ? <Button icon={ShieldCheck} compact tone="primary" onClick={props.transport.requestAuthentication}>{t('enterSessionToken')}</Button>
              : <>
                  {!snapshot.connected && <Button icon={Cable} compact onClick={() => void command('reconnect', t('reconnect'))}>{t('reconnect')}</Button>}
                  {!socketFresh && <Button icon={RefreshCw} compact onClick={() => void refresh()}>{t('refresh')}</Button>}
                </>}
          </div>
        }
      />

      <section className={`hero-panel${snapshot.connected ? ' is-online' : ''}`}>
        <div className="hero-panel__identity">
          <div className="eyebrow">{copy('Controller', 'کنترلر')} · {snapshot.connection_state}</div>
          <h2><a href="#/dashboard">{snapshot.connected ? snapshot.hello.name || appTitle : connectionLabel}</a></h2>
          {snapshot.connected
            ? <p>{`USB ${snapshot.port.vid || '—'}:${snapshot.port.pid || '—'} · ${snapshot.port.name || copy('automatic port', 'درگاه خودکار')}`}</p>
            : snapshot.connection_updated
              ? <p>{copy(`State updated ${formatClock(locale, snapshot.connection_updated)}`, `آخرین تغییر وضعیت ${formatClock(locale, snapshot.connection_updated)}`)}</p>
              : null}
        </div>
        <div className={`hero-panel__readout${snapshot.have_status ? '' : ' is-empty'}`} dir="ltr">
          <span>{copy('BUILD', 'ساخت')}</span><strong>{hash}</strong>
          <span>{copy('UPTIME', 'زمان کارکرد')}</span><strong>{formatDuration(locale, status.uptime_ms)}</strong>
        </div>
        <HeroContextMenu label={copy('Connection actions', 'عملیات اتصال')} connected={snapshot.connected}>
            <Button compact icon={snapshot.connected ? Unplug : Cable} onClick={() => void command(snapshot.connected ? 'close' : 'open', snapshot.connected ? copy('Connection closed', 'اتصال بسته شد') : copy('Connection opened', 'اتصال باز شد'))}>{snapshot.connected ? copy('Close', 'بستن') : copy('Open', 'بازکردن')}</Button>
            <Button compact icon={RefreshCw} onClick={() => void command('reconnect', t('reconnect'))}>{t('reconnect')}</Button>
            <Button compact icon={Usb} onClick={() => void command('ports', copy('USB device list requested', 'فهرست دستگاه‌های USB درخواست شد'))}>{copy('Change USB device', 'تغییر دستگاه USB')}</Button>
            {layout.hidden.map((id) => <Button key={id} compact icon={Eye} onClick={() => updateLayout((current) => ({ ...current, hidden: toggleDashboardCard(current.hidden, id) }))}>{copy(`Show ${id}`, `نمایش ${id}`)}</Button>)}
        </HeroContextMenu>
      </section>

      {snapshot.connected && snapshot.have_status && <section className="metric-grid">
        <MetricCard icon={Zap} label={peripheralName('sensor.supply-voltage', t('voltage'))} value={formatNumber(locale, status.supply_mv / 1000, 2)} unit="V" values={values(samples, 'supply')} tone="accent" detail={`${peripheralName('sensor.bus-voltage', copy('Bus voltage', 'ولتاژ باس'))} · ${formatNumber(locale, status.bus_mv / 1000, 2)} V`} />
        <MetricCard icon={Waves} label={peripheralName('sensor.current', t('current'))} value={formatNumber(locale, status.current_ma, 0)} unit="mA" values={values(samples, 'current')} tone="green" detail={`${peripheralName('sensor.power', copy('Load power', 'توان بار'))} · ${formatNumber(locale, status.power_mw / 1000, 2)} W`} />
        <Card icon={Thermometer} iconTone="amber" title={copy('Temperature', 'دما')} eyebrow={temperatureTab === 'led' ? peripheralName('sensor.temperature-led', 'LED') : peripheralName('sensor.temperature-audio', copy('Audio', 'صوت'))} className="temperature-card">
          <Segmented value={temperatureTab} label={copy('Temperature sensor', 'حسگر دما')} options={[{ value: 'led', label: 'LED' }, { value: 'audio', label: copy('Audio', 'صوت') }]} onChange={(value) => { setTemperatureTab(value as 'led' | 'audio'); setTelemetryMode('thermal') }} />
          <strong className="temperature-card__value">{formatNumber(locale, (temperatureTab === 'led' ? status.temperature_led_centi_c : status.temperature_bt_audio_centi_c) / 100, 1)}<small>°C</small></strong>
          <span>{temperatureTab === 'led' ? copy('Lighting thermal sensor', 'حسگر حرارتی نور') : copy('Bluetooth audio thermal sensor', 'حسگر حرارتی صوت بلوتوث')}</span>
          <Sparkline values={values(samples, temperatureTab === 'led' ? 'ledTemp' : 'btTemp')} tone="amber" label={temperatureTab === 'led' ? copy('LED temperature trend', 'روند دمای LED') : copy('Audio temperature trend', 'روند دمای صوت')} />
        </Card>
        <MetricCard icon={PlugZap} label="PWM" value={status.pwm_available ? formatNumber(locale, status.pwm_value * 100 / 4095, 1) : '—'} unit={status.pwm_available ? '%' : ''} values={values(samples, 'power')} tone="violet" detail={status.pwm_available ? `${copy('CH', 'کانال')} ${status.pwm_channel + 1}` : copy('Unavailable on this controller', 'در این کنترلر در دسترس نیست')} />
      </section>}

      {snapshot.connected && frame('telemetry', copy('Telemetry', 'تله‌متری'), <Card
        icon={ChartNoAxesCombined}
        iconTone="violet"
        title={snapshot.connected ? t('liveTelemetry') : copy('Telemetry history', 'تاریخچهٔ تله‌متری')}
        eyebrow={snapshot.connected ? copy('REAL-TIME', 'هم‌زمان') : copy('LAST KNOWN', 'آخرین داده')}
        className="telemetry-chart-card"
        action={<StatusBadge tone={snapshot.connected ? 'good' : 'warn'}>{samples.length} {copy('samples', 'نمونه')}</StatusBadge>}
        menu={[
          ...(!socketFresh ? [{ label: copy('Refresh telemetry', 'تازه‌سازی تله‌متری'), icon: RefreshCw, onSelect: () => { void refresh() } }] : []),
          { label: copy('Open Workbench', 'بازکردن میزکار'), icon: SquareTerminal, onSelect: () => { window.location.hash = '#/workbench' } },
        ]}
      >
        <Suspense fallback={<div className="telemetry-chart__empty" role="status"><Activity size={22} /><span>{locale === 'fa' ? 'در حال آماده‌سازی نمودار…' : 'Preparing chart…'}</span></div>}>
          <TelemetryChart connected={snapshot.connected} locale={locale} samples={samples} mode={telemetryMode} onModeChange={setTelemetryMode} thermalSeries={telemetryMode === 'thermal' ? temperatureTab : undefined} />
        </Suspense>
      </Card>)}

      <section className="dashboard-grid">
        {snapshot.connected && frame('outputs', copy('Outputs', 'خروجی‌ها'), <Card icon={ToggleRight} iconTone={activeRelayCount ? 'amber' : 'green'} title={t('outputs')} eyebrow="R1—R8" className="outputs-card" action={<StatusBadge tone={status.active_relays ? 'warn' : 'neutral'}>{status.active_relays ? `${activeRelayCount} ${copy('ACTIVE', 'فعال')}` : copy('SAFE', 'ایمن')}</StatusBadge>} menu={[
          { label: copy('Read controller status', 'خواندن وضعیت کنترلر'), icon: Gauge, onSelect: () => { void command('status') } },
          { label: copy('Edit relay, MOSFET, and side labels', 'ویرایش برچسب رله، ماسفت و سمت‌ها'), icon: PanelTop, onSelect: () => { window.location.hash = '#/settings' } },
          { label: copy('Release every output', 'آزادسازی همهٔ خروجی‌ها'), icon: Unplug, tone: 'danger', onSelect: () => openDialog({ tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'), action: async () => { await command('relay off'); await command('pwm off') } }) },
        ]}>
          <div className="output-matrix">
            {Array.from({ length: 8 }, (_, index) => {
              const active = Boolean(status.active_relays & (1 << index))
              return (
                <button
                  key={index}
                  type="button"
                  className={`output-cell${active ? ' is-active' : ''}${relayPending.has(index + 1) ? ' is-pending' : ''}`}
                  onClick={() => void relayToggle(index + 1, active)}
                  disabled={relayPending.has(index + 1)}
                  aria-pressed={active}
                  aria-label={copy(`Turn relay ${index + 1} ${active ? 'off' : 'on'}`, `رلهٔ ${index + 1} را ${active ? 'خاموش' : 'روشن'} کن`)}
                  title={copy(`Turn relay ${index + 1} ${active ? 'off' : 'on'}`, `رلهٔ ${index + 1} را ${active ? 'خاموش' : 'روشن'} کن`)}
                >
                  <span>R{index + 1} · {peripheralName(`relay.${index + 1}`, relayDefaults[index])}</span><strong>{active ? t('on') : t('off')}</strong><i aria-hidden="true" />
                </button>
              )
            })}
          </div>
          <div className="safety-strip"><ShieldCheck size={17} /><span>{activeRelayCount ? copy(`${activeRelayCount} outputs active · confirmation required for emergency release`, `${activeRelayCount} خروجی فعال است · آزادسازی اضطراری به تأیید نیاز دارد`) : copy('All physical outputs are released', 'همهٔ خروجی‌های فیزیکی آزاد هستند')}</span></div>
        </Card>)}

        {snapshot.connected && frame('overview', copy('Controller overview', 'نمای کلی کنترلر'), <Card icon={Gauge} iconTone="green" title={t('status')} eyebrow={t('device')} className="device-card" menu={[
          { label: copy('Read identity', 'خواندن شناسه'), icon: Cpu, onSelect: () => { void command('hello') } },
          { label: copy('Open controller controls', 'بازکردن کنترل‌های برد'), icon: CircuitBoard, onSelect: () => { window.location.hash = '#/controls' } },
          { label: copy('Configure seven-segment and LCD', 'پیکربندی نمایشگر هفت‌بخشی و LCD'), icon: Binary, onSelect: () => { window.location.hash = '#/settings' } },
          { label: copy('Configure buzzer routing', 'پیکربندی مسیر بیزر'), icon: Volume2, onSelect: () => { window.location.hash = '#/settings' } },
          { label: copy('Open connection settings', 'باز کردن تنظیمات اتصال'), icon: Cable, onSelect: () => { window.location.hash = '#/settings' } },
        ]}>
          <div className="data-list">
            <DataRow label={t('device')} value={deviceSummary.device} />
            <DataRow label={t('firmware')} value={deviceSummary.firmware} mono />
            <DataRow label={t('door')} value={status.door_open ? t('open') : t('closed')} tone={status.door_open ? 'warn' : 'good'} />
            <DataRow label={t('bluetooth')} value={status.bluetooth_audio_state === 2 ? t('online') : status.bluetooth_audio_state === 1 ? t('connecting') : t('offline')} />
            <DataRow label="LCD" value={status.lcd_address ? `0x${status.lcd_address.toString(16).toUpperCase().padStart(2, '0')}` : copy('not detected', 'شناسایی نشد')} mono />
            <DataRow label={copy('Seven-segment', 'هفت‌بخشی')} value={snapshot.have_front_panel && snapshot.front_panel ? snapshot.front_panel.raw_segments.map((value) => value.toString(16).toUpperCase().padStart(2, '0')).join(' ') : copy('awaiting event', 'در انتظار رویداد')} mono tone={snapshot.have_front_panel ? 'good' : 'warn'} />
            <DataRow label={copy('Buzzer', 'بیزر')} value={(snapshot.settings.flags & 0x01) !== 0 ? copy('silent', 'بی‌صدا') : copy('available', 'آماده')} tone={(snapshot.settings.flags & 0x01) !== 0 ? 'warn' : 'good'} />
            <DataRow label={copy('Socket', 'سوکت')} value={socketFresh ? copy('live (<1 s)', 'زنده (کمتر از ۱ ثانیه)') : copy('stale', 'قدیمی')} tone={socketFresh ? 'good' : 'warn'} />
            <DataRow label={copy('UART CRC / framing', 'CRC / قاب‌بندی UART')} value={`${status.crc_errors} / ${status.framing_errors}`} mono tone={status.crc_errors || status.framing_errors ? 'warn' : 'good'} />
            <DataRow label={copy('Reset count', 'تعداد بازنشانی')} value={status.reset_count} mono />
          </div>
        </Card>)}

        {snapshot.connected && frame('actions', copy('Quick actions', 'عملیات سریع'), <Card icon={Zap} iconTone="amber" title={t('quickActions')} eyebrow={copy('Confirmation protected', 'محافظت‌شده با تأیید')} className="actions-card">
          <div className="action-grid">
            <Button icon={Unplug} tone="danger" onClick={() => openDialog({
              tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'),
              action: async () => { await command('relay off', t('emergencyOff')); await command('pwm off') },
            })}>{t('emergencyOff')}</Button>
            <Button icon={Gauge} onClick={() => void command('status')}>{copy('Read status', 'خواندن وضعیت')}</Button>
            <Button icon={Lightbulb} onClick={() => void command('rgb effect play attention')}>{copy('Attention cue', 'اعلان توجه')}</Button>
          </div>
        </Card>)}

        {frame('events', copy('Events', 'رویدادها'), <Card icon={Activity} iconTone="violet" title={t('events')} eyebrow={events.length ? `${formatClock(locale, events[0].time)} · ${events[0].kind}` : t('eventStream')} className="activity-card" action={<span className="count-chip">{events.length}</span>} menu={[
          { label: copy('Open full timeline', 'بازکردن خط زمانی کامل'), icon: Activity, onSelect: () => { window.location.hash = '#/events' } },
          ...(!socketFresh ? [{ label: copy('Refresh snapshot', 'تازه‌سازی وضعیت'), icon: RefreshCw, onSelect: () => { void refresh() } }] : []),
        ]}>
          <EventList events={events} locale={locale} t={t} />
        </Card>)}
      </section>
    </>
  )
}

export function ControlsView(props: SharedViewProps) {
  const { snapshot, locale, t, command, relayToggle, relayPending, openDialog } = props
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const socketFresh = dashboardSocketIsFresh(snapshot, props.transport.streamState)
  const initialPWM = () => Array.from({ length: 16 }, (_, index) => index === snapshot.status.pwm_channel ? snapshot.status.pwm_value : 0)
  const [pwmReported, setPWMReported] = useState<number[]>(initialPWM)
  const [pwmDraft, setPWMDraft] = useState<number[]>(initialPWM)
  const [pwmLoaded, setPWMLoaded] = useState(false)
  const [pwmPending, setPWMPending] = useState<number[]>([])
  const [pwmError, setPWMError] = useState('')
  const [pwmAllBusy, setPWMAllBusy] = useState(false)
  const [hostUI, setHostUI] = useState<HostUISettings | null>(null)
  const [red, setRed] = useState(25)
  const [green, setGreen] = useState(130)
  const [blue, setBlue] = useState(220)
	const [statusBrightness, setStatusBrightness] = useState(190)
	const [alternateColor, setAlternateColor] = useState('#000000')
	const [statusEffect, setStatusEffect] = useState<'color' | 'breathe' | 'flash' | 'cycle' | 'transition'>('color')
	const [statusPeriod, setStatusPeriod] = useState(1600)
	const [statusMinimum, setStatusMinimum] = useState(8)
	const [statusRepeat, setStatusRepeat] = useState<'once' | 'loop'>('loop')
	const [statusProfile, setStatusProfile] = useState('ready')
  const configurationEventID = props.events.find((event) => event.kind === 'config')?.id ?? 0
  const pwmReportedRef = useRef(pwmReported)
  const pwmAllBusyRef = useRef(pwmAllBusy)
  const pwmOperationsRef = useRef<PWMOperationQueue | null>(null)
  const pwmSchedulerRef = useRef<PWMMutationScheduler | null>(null)
	const chosenHex = `#${[red, green, blue].map((value) => value.toString(16).padStart(2, '0')).join('')}`.toUpperCase()
	const liveLED = snapshot.have_status_led ? snapshot.status_led : undefined
	const liveHex = liveLED
		? `#${[liveLED.red, liveLED.green, liveLED.blue].map((value) => value.toString(16).padStart(2, '0')).join('')}`.toUpperCase()
		: '#000000'
	const effectOptions = `${statusEffect === 'color' ? '' : ` --period ${statusPeriod} --minimum ${statusMinimum} --repeat ${statusRepeat}`}${statusEffect === 'flash' || statusEffect === 'cycle' || statusEffect === 'transition' ? ` --to "${alternateColor}"` : ''}`
	const statusCommand = statusEffect === 'color'
		? `rgb color "${chosenHex}" ${statusBrightness}`
		: `rgb effect ${statusEffect} "${chosenHex}" --brightness ${statusBrightness}${effectOptions}`
	const profileCommand = statusEffect === 'color'
		? `rgb profile set ${statusProfile} color "${chosenHex}" ${statusBrightness}`
		: `rgb profile set ${statusProfile} ${statusEffect} "${chosenHex}" --brightness ${statusBrightness}${effectOptions}`

  if (!pwmOperationsRef.current) pwmOperationsRef.current = new PWMOperationQueue()

  const applyAuthoritativePWM = (response: PWMValues, acknowledgedChannel: number | null = null) => {
    const authoritative = normalizePWMValues(response)
    pwmReportedRef.current = authoritative.values
    setPWMReported(authoritative.values)
    setPWMDraft((current) => current.map((value, channel) => {
      if (channel === acknowledgedChannel) return authoritative.values[channel]
      if (pwmSchedulerRef.current?.pendingChannels().includes(channel)) return value
      return authoritative.values[channel]
    }))
    setPWMError('')
    setPWMLoaded(true)
  }

  if (!pwmSchedulerRef.current) {
    pwmSchedulerRef.current = new PWMMutationScheduler({
      operations: pwmOperationsRef.current,
      commit: (channel, value) => rpc<PWMValues>('controller.pwm.set', { channel, value }),
      onDraft: (channel, value) => {
        setPWMError('')
        setPWMDraft((current) => current.map((candidate, index) => index === channel ? value : candidate))
      },
      onAuthoritative: applyAuthoritativePWM,
      onPending: (channels) => setPWMPending([...channels]),
      onError: (channel, cause) => {
        setPWMError(cause.message)
        setPWMDraft((current) => current.map((value, index) => index === channel ? pwmReportedRef.current[channel] : value))
      },
    })
  }

  useEffect(() => () => pwmSchedulerRef.current?.dispose(), [])

  useEffect(() => {
    let active = true
    void rpc<HostUISettings>('controller.ui.config.get')
      .then((value) => { if (active) setHostUI(value) })
      .catch(() => { if (active) setHostUI(null) })
    return () => { active = false }
  }, [configurationEventID])

  useEffect(() => {
    if (!snapshot.connected || !snapshot.status.pwm_available) {
      setPWMLoaded(false)
      return
    }
    setPWMError('')
    const reconciler = new PWMReconciler({
      operations: pwmOperationsRef.current!,
      canRead: () => !pwmAllBusyRef.current && (pwmSchedulerRef.current?.pendingChannels().length ?? 0) === 0,
      read: (signal) => rpc<PWMValues>('controller.pwm.values', {}, signal),
      onAuthoritative: (value) => applyAuthoritativePWM(value),
      onError: (cause) => setPWMError(cause.message),
    })
    reconciler.start()
    return () => reconciler.stop()
  }, [snapshot.connected, snapshot.status.pwm_available])

  useEffect(() => {
    if (!snapshot.have_status || snapshot.status.pwm_channel < 0 || snapshot.status.pwm_channel > 15) return
    const channel = snapshot.status.pwm_channel
    const value = snapshot.status.pwm_value
    pwmReportedRef.current = pwmReportedRef.current.map((candidate, index) => index === channel ? value : candidate)
    setPWMReported(pwmReportedRef.current)
    if (!pwmSchedulerRef.current?.pendingChannels().includes(channel)) {
      setPWMDraft((current) => current.map((candidate, index) => index === channel ? value : candidate))
    }
  }, [snapshot.have_status, snapshot.status.pwm_channel, snapshot.status.pwm_value])

  const pwmDefaults = [
    'MOSFET 1', 'MOSFET 2', 'MOSFET 3', 'MOSFET 4',
    'MOSFET 5', 'MOSFET 6', 'MOSFET 7', 'MOSFET 8',
    copy('User PWM 9', 'PWM کاربر ۹'), copy('User PWM 10', 'PWM کاربر ۱۰'), copy('User PWM 11', 'PWM کاربر ۱۱'), copy('Enclosure light', 'نور محفظه'),
    copy('Power indicator', 'نشانگر توان'), copy('Status red', 'وضعیت قرمز'), copy('Status green', 'وضعیت سبز'), copy('Status blue', 'وضعیت آبی'),
  ]
  const relayDefaults = [
    copy('Side A direction', 'جهت سمت A'), copy('Side A output', 'خروجی سمت A'),
    copy('Side B direction', 'جهت سمت B'), copy('Side B output', 'خروجی سمت B'),
    copy('User relay 5', 'رلهٔ کاربر ۵'), copy('User relay 6', 'رلهٔ کاربر ۶'),
    copy('User relay 7', 'رلهٔ کاربر ۷'), copy('User relay 8', 'رلهٔ کاربر ۸'),
  ]
  const peripheralName = (key: string, fallback: string) => hostUI?.peripheral_names?.[key]?.trim() || fallback
  const pwmName = (channel: number) => peripheralName(`pwm.${channel}`, pwmDefaults[channel])
  const setPWMPercent = (channel: number, percent: number, immediate = false) => {
    pwmSchedulerRef.current?.schedule(channel, pwmValue(percent), immediate)
  }
  const clearPWM = async () => {
    if (pwmPending.length) return
    pwmAllBusyRef.current = true
    setPWMAllBusy(true)
    setPWMError('')
    try {
      applyAuthoritativePWM(await pwmOperationsRef.current!.run(() => rpc<PWMValues>('controller.pwm.off')))
    } catch (cause) {
      setPWMError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      pwmAllBusyRef.current = false
      setPWMAllBusy(false)
    }
  }

  if (!snapshot.connected) {
    const authenticationRequired = props.transport.streamState === 'authentication-required'
    return (
      <>
        <SectionTitle eyebrow={copy('Controller controls', 'کنترل‌های برد')} title={t('controls')} detail={authenticationRequired ? props.transport.detail : snapshot.connection_reason || copy('Controller offline', 'کنترلر آفلاین است')} />
        <Card icon={authenticationRequired ? ShieldCheck : CircuitBoard} iconTone="amber" title={authenticationRequired ? t('authenticationRequired') : copy('Controller controls are unavailable', 'کنترل‌های برد در دسترس نیست')} eyebrow={authenticationRequired ? copy('Session access required', 'دسترسی نشست لازم است') : copy('No authenticated controller', 'کنترلر معتبری متصل نیست')}>
          <EmptyState
            icon={authenticationRequired ? ShieldCheck : Cable}
            title={authenticationRequired ? t('authenticationRequired') : copy('Connect the controller to reveal its controls', 'برای نمایش کنترل‌ها، برد را متصل کنید')}
            detail={authenticationRequired ? props.transport.detail : copy('Host tools and the full terminal remain available in Workbench.', 'ابزارهای میزبان و ترمینال کامل همچنان در میزکار در دسترس‌اند.')}
            action={authenticationRequired
              ? <Button tone="primary" icon={ShieldCheck} onClick={props.transport.requestAuthentication}>{t('enterSessionToken')}</Button>
              : <Button tone="primary" icon={Cable} onClick={() => void command('reconnect')}>{t('reconnect')}</Button>}
          />
        </Card>
      </>
    )
  }
  return (
    <>
      <SectionTitle eyebrow={copy('Connected controller', 'کنترلر متصل')} title={t('controls')} detail={pageDetail(snapshot, props.appTitle, locale)} />
      <section className="control-layout">
        <Card icon={CircuitBoard} iconTone={snapshot.status.active_relays ? 'amber' : 'green'} eyebrow={copy('Eight protected outputs', 'هشت خروجی محافظت‌شده')} title={copy('Relays & motion', 'رله‌ها و حرکت')} className="relay-control-card" action={<StatusBadge tone={snapshot.status.active_relays ? 'warn' : 'good'}>{snapshot.status.active_relays ? copy('OUTPUTS ON', 'خروجی‌ها روشن') : copy('ALL OFF', 'همه خاموش')}</StatusBadge>} menu={[
          ...(!socketFresh ? [{ label: copy('Refresh output state', 'تازه‌سازی وضعیت خروجی‌ها'), icon: RefreshCw, onSelect: () => { void command('status') } }] : []),
          { label: copy('Release every relay', 'آزادسازی همهٔ رله‌ها'), icon: Unplug, tone: 'danger', onSelect: () => openDialog({ tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'), action: async () => { await command('relay off') } }) },
        ]}>
          <div className="relay-bank">
            {Array.from({ length: 8 }, (_, index) => {
              const active = Boolean(snapshot.status.active_relays & (1 << index))
              return (
                <article key={index} className={`relay-switch${active ? ' is-active' : ''}${relayPending.has(index + 1) ? ' is-pending' : ''}`}>
                  <button type="button" className="relay-switch__toggle" data-relay={index + 1} aria-pressed={active} aria-label={copy(`Turn relay ${index + 1} ${active ? 'off' : 'on'}`, `رلهٔ ${index + 1} را ${active ? 'خاموش' : 'روشن'} کن`)} disabled={relayPending.has(index + 1)} onClick={(event) => void relayToggle(Number(event.currentTarget.dataset.relay), active)}>
                    <span>R{index + 1}</span><i aria-hidden="true"><b /></i><small>{peripheralName(`relay.${index + 1}`, relayDefaults[index])}</small>
                  </button>
                  <div className="relay-switch__actions"><Button compact disabled={active || relayPending.has(index + 1)} onClick={() => void relayToggle(index + 1, false)}>{t('on')}</Button><Button compact disabled={!active || relayPending.has(index + 1)} onClick={() => void relayToggle(index + 1, true)}>{t('off')}</Button></div>
                </article>
              )
            })}
          </div>
          <div className="motion-actions">
            {(['left', 'right'] as const).map((side) => (
              <div key={side} className="motion-side">
                <strong>{side === 'left' ? peripheralName('motion.a', copy('Side A motion', 'حرکت سمت A')) : peripheralName('motion.b', copy('Side B motion', 'حرکت سمت B'))}</strong>
                <HoldActionButton compact onHoldStart={() => command(`relay side ${side} up`)} onHoldStop={() => command(`relay side ${side} stop`)}>{copy('Hold Up', 'بالا نگه‌دار')}</HoldActionButton>
                <Button compact onClick={() => void command(`relay side ${side} stop`)}>{copy('Stop', 'توقف')}</Button>
                <HoldActionButton compact onHoldStart={() => command(`relay side ${side} down`)} onHoldStop={() => command(`relay side ${side} stop`)}>{copy('Hold Down', 'پایین نگه‌دار')}</HoldActionButton>
              </div>
            ))}
          </div>
          <Button icon={AlertOctagon} tone="danger" onClick={() => openDialog({ tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'), action: async () => { await command('relay off') } })}>{t('emergencyOff')}</Button>
        </Card>

        {snapshot.status.pwm_available && <Card icon={SlidersHorizontal} iconTone="violet" eyebrow={copy('11 user channels · saved changes', '۱۱ کانال کاربر · تغییرات ذخیره‌شده')} title={copy('PWM mixer', 'میکسر PWM')} className="pwm-card" action={<StatusBadge tone={pwmError ? 'bad' : pwmPending.length ? 'warn' : pwmLoaded ? 'good' : 'neutral'}>{pwmError ? copy('ERROR', 'خطا') : pwmPending.length ? copy('SYNCING', 'همگام‌سازی') : pwmLoaded ? copy('READY', 'آماده') : copy('READING', 'در حال خواندن')}</StatusBadge>}>
          <div className="pwm-mixer">
            {USER_PWM_CHANNELS.map((channel) => {
              const reported = pwmReported[channel]
              const draft = pwmDraft[channel]
              const pending = pwmPending.includes(channel)
              const name = pwmName(channel)
              return <div key={channel} className={`pwm-mixer__row${snapshot.status.pwm_channel === channel && !pending ? ' is-live' : ''}${pending ? ' is-pending' : ''}`}>
                <div><strong>{name}</strong><small>{pending
                  ? copy(`Board ${reported} · draft ${draft}`, `برد ${reported} · پیش‌نویس ${draft}`)
                  : copy(`Board reported ${reported} / 4095`, `گزارش برد ${reported} از ۴۰۹۵`)}</small></div>
                <RangeField label={copy(`${name} duty`, `دیوتی ${name}`)} value={pwmPercent(draft)} min={0} max={100} unit="%" disabled={!pwmLoaded || pwmAllBusy} onChange={(percent) => setPWMPercent(channel, percent)} />
                <div className="pwm-mixer__actions"><Button compact disabled={!pwmLoaded || pwmAllBusy || draft === 0} onClick={() => setPWMPercent(channel, 0, true)}>{t('off')}</Button><Button compact disabled={!pwmLoaded || pwmAllBusy || draft === 4095} onClick={() => setPWMPercent(channel, 100, true)}>{copy('FULL', 'کامل')}</Button></div>
              </div>
            })}
          </div>
          <div className="system-output-summary" aria-label={copy('Role-specific system PWM channels', 'کانال‌های PWM سیستمی با کنترل اختصاصی')}>
            {[11, 12, 13, 14, 15].map((channel) => <span key={channel}><strong>{pwmName(channel)}</strong><small>{pwmReported[channel]} / 4095</small></span>)}
          </div>
          <div className={`pwm-authority-status${pwmError ? ' is-error' : ''}`} role="status" aria-live="polite">{pwmError || (pwmPending.length
            ? copy(`${pwmPending.length} channel${pwmPending.length === 1 ? '' : 's'} waiting for controller acknowledgement.`, `${pwmPending.length} کانال در انتظار تأیید کنترلر است.`)
            : pwmLoaded
              ? copy('Every displayed board value is authoritative; sliders are separate local drafts until acknowledged.', 'همهٔ مقادیر برد معتبرند؛ لغزنده‌ها تا زمان تأیید، پیش‌نویس محلی جداگانه‌اند.')
              : copy('Reading the complete 16-channel state from the controller…', 'در حال خواندن وضعیت کامل ۱۶ کانال از کنترلر…'))}</div>
          <Button icon={Power} busy={pwmAllBusy} disabled={!pwmLoaded || pwmAllBusy || pwmPending.length > 0} onClick={() => void clearPWM()}>{copy('Clear all PWM channels', 'پاک‌کردن همهٔ کانال‌های PWM')}</Button>
        </Card>}

        <Card icon={Lightbulb} iconTone="amber" eyebrow={copy('Color and effects', 'رنگ و افکت‌ها')} title={copy('Status lighting', 'نور وضعیت')} className="rgb-card">
          <div className="status-led-previews">
			<div className="color-preview" style={{ '--preview': chosenHex } as React.CSSProperties}>
				<span />
				<strong>{copy('Selected', 'انتخاب‌شده')} · {chosenHex} · RGB {red} {green} {blue}</strong>
			</div>
			<div className="status-led-live" style={{ '--preview': liveHex } as React.CSSProperties}>
				<i aria-hidden="true" />
				<div><strong>{copy('Physical LED mirror', 'بازتاب LED فیزیکی')}</strong><small dir="ltr">{liveLED ? `${liveHex} · effect ${liveLED.effect} · condition ${liveLED.condition}` : copy('Awaiting pushed board state', 'در انتظار وضعیت ارسالی برد')}</small></div>
			</div>
		  </div>
          <label className="native-color-field">
			<span>{copy('Color picker', 'انتخاب‌گر رنگ')}</span>
			<input type="color" value={chosenHex} aria-label={copy('Status LED color', 'رنگ LED وضعیت')} onChange={(event) => {
				const value = event.target.value
				setRed(Number.parseInt(value.slice(1, 3), 16)); setGreen(Number.parseInt(value.slice(3, 5), 16)); setBlue(Number.parseInt(value.slice(5, 7), 16))
			}} />
			<code>{chosenHex}</code>
		  </label>
          {pwmLoaded && <div className="rgb-board-state"><span>{copy('Board-reported PWM', 'PWM گزارش‌شدهٔ برد')}</span><strong dir="ltr">{pwmReported[13]} · {pwmReported[14]} · {pwmReported[15]}</strong></div>}
          <RangeField label={copy('Red', 'قرمز')} value={red} min={0} max={255} onChange={setRed} />
          <RangeField label={copy('Green', 'سبز')} value={green} min={0} max={255} onChange={setGreen} />
          <RangeField label={copy('Blue', 'آبی')} value={blue} min={0} max={255} onChange={setBlue} />
		  <RangeField label={copy('Brightness', 'روشنایی')} value={statusBrightness} min={0} max={255} onChange={(value) => { setStatusBrightness(value); setStatusMinimum((current) => Math.min(current, value)) }} />
		  <div className="rgb-effect-fields">
			<label><span>{copy('Presentation', 'نحوه نمایش')}</span><select value={statusEffect} onChange={(event) => setStatusEffect(event.target.value as typeof statusEffect)}><option value="color">{copy('Steady color', 'رنگ ثابت')}</option><option value="breathe">{copy('Breathe', 'تنفس')}</option><option value="flash">{copy('Flash', 'چشمک')}</option><option value="cycle">{copy('Two-color cycle', 'چرخه دو رنگ')}</option><option value="transition">{copy('Transition', 'گذار')}</option></select></label>
			{statusEffect !== 'color' && <><label><span>{copy('Period', 'دوره')}</span><input type="number" min={640} max={60000} value={statusPeriod} onChange={(event) => setStatusPeriod(Math.max(640, Math.min(60000, Number(event.target.value))))} /><small>ms</small></label><label><span>{copy('Repeat', 'تکرار')}</span><select value={statusRepeat} onChange={(event) => setStatusRepeat(event.target.value as typeof statusRepeat)}><option value="once">{copy('Once', 'یک‌بار')}</option><option value="loop">{copy('Loop', 'پیوسته')}</option></select></label></>}
			{statusEffect === 'breathe' && <label><span>{copy('Minimum brightness', 'حداقل روشنایی')}</span><input type="number" min={0} max={statusBrightness} value={statusMinimum} onChange={(event) => setStatusMinimum(Math.max(0, Math.min(statusBrightness, Number(event.target.value))))} /></label>}
			{(statusEffect === 'flash' || statusEffect === 'cycle' || statusEffect === 'transition') && <label className="native-color-field native-color-field--compact"><span>{copy('Second color', 'رنگ دوم')}</span><input type="color" value={alternateColor} onChange={(event) => setAlternateColor(event.target.value.toUpperCase())} /><code>{alternateColor}</code></label>}
		  </div>
		  <div className="inline-actions"><Button tone="primary" icon={Sparkles} disabled={!snapshot.connected} onClick={() => void command(statusCommand)}>{copy('Apply live', 'اعمال زنده')}</Button><Button disabled={!snapshot.connected} onClick={() => void command('rgb effect stop')}>{copy('Stop effect', 'توقف افکت')}</Button></div>
		  <div className="rgb-profile-row"><label><span>{copy('EEPROM condition', 'شرط EEPROM')}</span><select value={statusProfile} onChange={(event) => setStatusProfile(event.target.value)}>{['off', 'boot', 'ready', 'learning', 'hot', 'fault', 'custom', 'bluetooth-connected', 'bluetooth-off', 'bluetooth-waiting', 'running', 'door-open', 'door-closed', 'bluetooth', 'menu', 'radio', 'save', 'discard', 'reset'].map((condition) => <option key={condition} value={condition}>{condition}</option>)}</select></label><Button icon={MemoryStick} disabled={!snapshot.connected} onClick={() => void command(profileCommand)}>{copy('Save profile (live when active)', 'ذخیره پروفایل (هنگام فعال‌شدن زنده)')}</Button></div>
        </Card>

      </section>
    </>
  )
}

const deviceResources = ['capabilities', 'snapshot'] as const

export function localDeviceControlsAvailable(snapshot: Pick<LocalDeviceSnapshot, 'http_reachable'>): boolean {
  return snapshot.http_reachable === true
}

export function localDeviceReconnectAvailable(snapshot: Pick<LocalDeviceSnapshot, 'configured' | 'http_reachable'>): boolean {
  return snapshot.configured === true && !localDeviceControlsAvailable(snapshot)
}

export function LocalDeviceView({ locale, t }: SharedViewProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [snapshot, setSnapshot] = useState<LocalDeviceSnapshot>({ power: 'UNKNOWN', phase: 'idle' })
  const [busy, setBusy] = useState('')
  const [message, setMessage] = useState('')
  const [inspection, setInspection] = useState<unknown>(null)
  const [resource, setResource] = useState<(typeof deviceResources)[number]>('capabilities')
  const [error, setError] = useState('')

  const load = async () => {
    try {
      setSnapshot(await rpc<LocalDeviceSnapshot>('controller.device.status'))
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }
  useEffect(() => { void load(); const timer = window.setInterval(() => void load(), 3000); return () => window.clearInterval(timer) }, [])

  const action = async (kind: string, text = '', count = 0) => {
    if (kind !== 'passive.refresh' && !localDeviceControlsAvailable(snapshot)) {
      setError(locale === 'fa' ? 'ارتباط HTTP وسیله در دسترس نیست.' : 'The device HTTP command transport is unavailable.')
      return
    }
    setBusy(kind)
    try {
      setSnapshot(await rpc<LocalDeviceSnapshot>('controller.device.action', { action: kind, text, count }))
      setError('')
      if (kind === 'display.message') setMessage('')
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const inspect = async () => {
    if (!localDeviceControlsAvailable(snapshot)) {
      setError(locale === 'fa' ? 'برای بازرسی، ارتباط HTTP لازم است.' : 'Inspection requires the device HTTP transport.')
      return
    }
    setBusy('inspect')
    try { setInspection(await rpc('controller.device.inspect', { resource })); setError('') }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const powered = snapshot.power === 'ON'
  const controlsAvailable = localDeviceControlsAvailable(snapshot)
  const reconnectAvailable = localDeviceReconnectAvailable(snapshot)
  const unavailableTitle = snapshot.configured === false
    ? (locale === 'fa' ? 'یکپارچه‌سازی وسیله غیرفعال است' : 'Local device integration is disabled')
    : snapshot.websocket_online
      ? (locale === 'fa' ? 'مسیر فرمان HTTP در دسترس نیست' : 'HTTP command transport is unavailable')
      : snapshot.configured
        ? (locale === 'fa' ? 'وسیلهٔ محلی در دسترس نیست' : 'Local companion is unreachable')
        : error
          ? (locale === 'fa' ? 'وضعیت وسیله دریافت نشد' : 'Companion status is unavailable')
          : (locale === 'fa' ? 'در حال بررسی وسیلهٔ محلی' : 'Checking local companion availability')
  const unavailableDetail = snapshot.last_error || error || (snapshot.configured === false
    ? (locale === 'fa' ? 'نشانی و فعال‌سازی را در تنظیمات خدمات محلی بررسی کنید.' : 'Choose a private-network endpoint and enable it in local-service settings.')
    : (locale === 'fa' ? `وضعیت فعلی: ${snapshot.phase || 'نامشخص'}` : `Current phase: ${snapshot.phase || 'unknown'}`))
  const deviceMenu = [
    { label: locale === 'fa' ? 'تازه‌سازی وضعیت' : 'Refresh device status', icon: RefreshCw, onSelect: () => { void load() } },
    ...(reconnectAvailable ? [{ label: locale === 'fa' ? 'تلاش دوباره برای اتصال' : 'Retry companion connection', icon: PlugZap, onSelect: () => { void action('passive.refresh') } }] : []),
    ...(controlsAvailable ? [{ label: powered ? (locale === 'fa' ? 'خاموش‌کردن وسیله' : 'Turn device off') : (locale === 'fa' ? 'روشن‌کردن وسیله' : 'Turn device on'), icon: CirclePower, onSelect: () => { void action(powered ? 'power.off' : 'power.on') } }] : []),
  ]
  return (
    <>
      <SectionTitle eyebrow={`${copy('Local companion', 'وسیلهٔ محلی')} · ${snapshot.phase || 'idle'}`} title={t('device')} detail={snapshot.base_url || copy('Typed local-network companion through the primary host', 'وسیلهٔ شبکهٔ محلی با قرارداد مشخص، از طریق میزبان اصلی')} action={<StatusBadge tone={snapshot.websocket_online ? 'good' : snapshot.http_reachable ? 'warn' : 'bad'} pulse={snapshot.phase === 'connecting'}>{snapshot.websocket_online ? copy('EVENT STREAM', 'جریان رویداد') : snapshot.http_reachable ? 'HTTP' : t('offline')}</StatusBadge>} />
      <section className="device-layout">
        <Card
          icon={Cpu}
          iconTone={powered ? 'green' : 'amber'}
          title={copy('Local device', 'وسیلهٔ محلی')}
          eyebrow={snapshot.websocket_online ? copy('Events connected', 'رویدادها متصل‌اند') : snapshot.http_reachable ? copy('HTTP reachable', 'HTTP در دسترس') : copy('Unavailable', 'دردسترس نیست')}
          className={`device-stage${powered ? ' is-on' : ''}`}
          menu={deviceMenu}
        >
          <div className="device-overview" aria-label={copy(`Device power ${snapshot.power ?? 'unknown'}`, `توان وسیله ${snapshot.power ?? 'نامشخص'}`)}>
            <div className="device-overview__mark"><Cpu size={38} /><span aria-hidden="true" /></div>
            <div className="device-overview__copy">
              <span>{snapshot.base_url || copy('Host-mediated local connection', 'اتصال محلی با میانجی‌گری میزبان')}</span>
              <strong>{powered ? copy('Device is powered on', 'وسیله روشن است') : snapshot.power === 'OFF' ? copy('Device is powered off', 'وسیله خاموش است') : copy('Power state is unknown', 'وضعیت توان نامشخص است')}</strong>
              <small>{snapshot.last_event || snapshot.last_error || (snapshot.phase ? `${copy('Phase', 'مرحله')}: ${snapshot.phase}` : copy('No device event received', 'رویدادی از وسیله دریافت نشده است'))}</small>
            </div>
          </div>
          <div className="device-facts">
            <div><span>{t('status')}</span><strong>{snapshot.power ?? t('unknown')}</strong></div>
            <div><span>{copy('Transport', 'رسانهٔ ارتباطی')}</span><strong>{snapshot.websocket_online ? copy('Events', 'رویدادها') : snapshot.http_reachable ? 'HTTP' : t('offline')}</strong></div>
            <div><span>{copy('Updated', 'آخرین تغییر')}</span><strong>{snapshot.updated_at ? formatClock(locale, snapshot.updated_at) : '—'}</strong></div>
          </div>
          {controlsAvailable
            ? <div className="device-actions"><Button tone="success" icon={Sun} busy={busy === 'power.on'} onClick={() => void action('power.on')}>{t('on')}</Button><Button tone="secondary" icon={Moon} busy={busy === 'power.off'} onClick={() => void action('power.off')}>{t('off')}</Button><Button tone="primary" icon={CirclePower} busy={busy === 'power.toggle'} onClick={() => void action('power.toggle')}>{t('toggle')}</Button></div>
            : <EmptyState
                icon={snapshot.configured === false ? Settings2 : Unplug}
                title={unavailableTitle}
                detail={unavailableDetail}
                action={<div className="device-unavailable__actions">
                  {reconnectAvailable && <Button compact tone="primary" icon={PlugZap} busy={busy === 'passive.refresh'} onClick={() => void action('passive.refresh')}>{locale === 'fa' ? 'تلاش دوباره' : 'Retry connection'}</Button>}
                  <Button compact icon={RefreshCw} busy={busy === 'status'} onClick={() => void load()}>{locale === 'fa' ? 'تازه‌سازی وضعیت' : 'Refresh status'}</Button>
                  {snapshot.configured !== true && <Button compact icon={Settings2} onClick={() => { window.location.hash = '#/settings' }}>{locale === 'fa' ? 'تنظیم یکپارچه‌سازی' : 'Open integration settings'}</Button>}
                </div>}
              />}
          <AnimatePresence initial={false}>
            {error && <motion.div
              key="local-device-error"
              className="inline-error-presence"
              initial={{ height: 0, opacity: 0, clipPath: 'inset(0 0 100% 0)', filter: 'blur(4px)' }}
              animate={{ height: 'auto', opacity: 1, clipPath: 'inset(0 0 0% 0)', filter: 'blur(0px)' }}
              exit={{ height: 0, opacity: 0, clipPath: 'inset(0 0 100% 0)', filter: 'blur(3px)' }}
              transition={{ duration: .28, ease: [0.22, 1, 0.36, 1] }}
            ><div className="inline-error" role="alert"><AlertOctagon size={17} /><span>{error}</span></div></motion.div>}
          </AnimatePresence>
        </Card>

        <div className="device-side">
          {controlsAvailable && <Card icon={MessageSquareText} iconTone="violet" title={t('message')} eyebrow={message.length ? copy(`${message.length} characters`, `${message.length} نویسه`) : copy('No message entered', 'پیامی وارد نشده')} action={<StatusBadge tone="info">{copy('LOCAL DEVICE', 'وسیلهٔ محلی')}</StatusBadge>}>
            <div className="message-compose"><input value={message} maxLength={256} placeholder={copy('A bounded display message', 'پیام کوتاه برای نمایش')} onChange={(event) => setMessage(event.target.value)} /><Button icon={Send} tone="primary" busy={busy === 'display.message'} disabled={!message.trim()} onClick={() => void action('display.message', message.trim())}>{t('send')}</Button></div>
            <div className="beep-row"><div><Volume2 size={18} /><span>{t('beep')} · {copy('explicit alert pulse', 'پالس هشدار صریح')}</span></div>{[1, 2, 3].map((count) => <Button key={count} compact busy={busy === 'alert.pulse'} onClick={() => void action('alert.pulse', '', count)}>{count}×</Button>)}</div>
          </Card>}

          {controlsAvailable && <Card icon={Binary} iconTone="accent" title={t('diagnostics')} eyebrow={inspection ? copy('Inspection loaded', 'بازرسی بارگیری شد') : copy('Not inspected', 'بازرسی نشده')} action={<Button icon={RefreshCw} compact busy={busy === 'inspect'} onClick={() => void inspect()}>{t('fetch')}</Button>}>
            <div className="diagnostic-picker">{deviceResources.map((value) => <button key={value} className={resource === value ? 'is-active' : ''} onClick={() => setResource(value)} dir="ltr">{value}</button>)}</div>
            {inspection !== null && <pre className="diagnostic-output" dir="ltr">{JSON.stringify(inspection, null, 2)}</pre>}
          </Card>}

          <Card icon={HeartPulse} iconTone={snapshot.websocket_online ? 'green' : 'amber'} title={copy('Connection health', 'سلامت اتصال')} eyebrow={snapshot.websocket_online ? copy('Event stream connected', 'جریان رویداد متصل است') : snapshot.http_reachable ? copy('HTTP reachable', 'HTTP در دسترس') : copy('Unavailable', 'دردسترس نیست')} action={<Button icon={RefreshCw} compact onClick={() => void load()}>{locale === 'fa' ? 'تازه‌سازی' : 'Refresh'}</Button>}>
            <div className="data-list"><DataRow label={copy('Phase', 'مرحله')} value={snapshot.phase || 'idle'} /><DataRow label="HTTP" value={snapshot.http_reachable ? t('online') : t('offline')} tone={snapshot.http_reachable ? 'good' : 'bad'} /><DataRow label={copy('Event stream', 'جریان رویداد')} value={snapshot.websocket_online ? t('online') : t('offline')} tone={snapshot.websocket_online ? 'good' : 'warn'} /><DataRow label={copy('Last event', 'آخرین رویداد')} value={snapshot.last_event || '—'} /><DataRow label={copy('Capabilities', 'قابلیت‌ها')} value={snapshot.capabilities?.join(', ') || '—'} /></div>
          </Card>
        </div>
      </section>
    </>
  )
}

export function EventsView({ events, locale, t }: SharedViewProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [level, setLevel] = useState<'all' | 'good' | 'warn' | 'bad' | 'info'>('all')
  const [kind, setKind] = useState('all')
  const [showDebugNoise, setShowDebugNoise] = useState(false)
  const kinds = useMemo(() => [...new Set(events.map((event) => event.kind.trim()).filter(Boolean))].sort((left, right) => left.localeCompare(right)), [events])
  const isDebugNoise = (event: ControllerEvent) => /^(status[_ .-]?rgb|status_led\.changed|pwm\.changed|segment\.changed)/i.test(event.kind.trim())
  const visible = events.filter((event) =>
    (level === 'all' || eventTone(event) === level) &&
    (kind === 'all' || event.kind === kind) &&
    (showDebugNoise || !isDebugNoise(event)))
  return (
    <>
      <SectionTitle eyebrow={copy('Unified history', 'تاریخچهٔ یکپارچه')} title={t('events')} detail={copy('One timeline for controller lifecycle, RF, doors, macros, automations and integrated services.', 'یک خط زمانی برای چرخهٔ کنترلر، RF، درها، ماکروها، خودکارسازی‌ها و سرویس‌های یکپارچه.')} action={<StatusBadge tone="info">{events.length} {copy('retained', 'نگه‌داری‌شده')}</StatusBadge>} />
      <Card icon={Activity} iconTone="violet" className="events-page-card" title={copy('Event timeline', 'خط زمانی رویدادها')} eyebrow={copy(`${visible.length} visible`, `${visible.length} مورد نمایان`)}>
        <div className="event-toolbar"><Segmented value={level} label={copy('Event severity', 'شدت رویداد')} options={[{ value: 'all', label: copy('All', 'همه') }, { value: 'good', label: copy('Good', 'عادی') }, { value: 'warn', label: copy('Warn', 'هشدار') }, { value: 'bad', label: copy('Fault', 'خطا') }, { value: 'info', label: copy('Info', 'اطلاعات') }]} onChange={setLevel} /><label className="event-toolbar__kind"><span>{copy('Event type', 'نوع رویداد')}</span><select value={kind} onChange={(event) => setKind(event.target.value)}><option value="all">{copy('All types', 'همه نوع‌ها')}</option>{kinds.map((value) => <option key={value} value={value}>{value}</option>)}</select></label><Toggle checked={showDebugNoise} onChange={setShowDebugNoise} label={copy('Show STATUS_RGB and high-rate debug events', 'نمایش STATUS_RGB و رویدادهای پرتکرار اشکال‌زدایی')} detail={copy('Off by default to keep the activity timeline actionable.', 'برای خوانایی خط زمانی به‌صورت پیش‌فرض خاموش است.')} /></div>
        <EventList events={visible} locale={locale} t={t} limit={visible.length} toolbar />
      </Card>
    </>
  )
}

export function SettingsView({ appTitle, snapshot, locale, t, command, appearance, token, onToken, onAppTitle, boardSettingsReadState, uiConfig, onBuzzerPath, openAppPreferences, transport, sessionAccessRequest = 0 }: SharedViewProps & { appearance: Appearance; onAppearance: (value: Appearance) => void; token: string; onToken: (value: string) => void; onAppTitle: (value: string) => Promise<string>; uiConfig: UIConfig | null; onBuzzerPath: (value: BuzzerPath) => Promise<void>; sessionAccessRequest?: number }) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const validationMessage = (message: string) => locale !== 'fa' ? message : ({
    'Application title is required.': 'عنوان برنامه الزامی است.',
    'Application title must be 64 characters or fewer.': 'عنوان برنامه باید حداکثر ۶۴ نویسه باشد.',
    'Application title must include a letter or number.': 'عنوان برنامه باید دست‌کم یک حرف یا عدد داشته باشد.',
    'A root URL is required while this integration is enabled.': 'وقتی یکپارچه‌سازی فعال است، نشانی ریشه الزامی است.',
    'Enter a valid HTTP or HTTPS root URL.': 'یک نشانی ریشهٔ معتبر HTTP یا HTTPS وارد کنید.',
    'Only HTTP and HTTPS are supported.': 'فقط HTTP و HTTPS پشتیبانی می‌شوند.',
    'Credentials are not allowed in the URL.': 'اطلاعات دسترسی نباید داخل نشانی باشد.',
    'Query strings and fragments are not allowed.': 'پارامتر پرس‌وجو و قطعه در نشانی مجاز نیست.',
    'Use the service root without a path.': 'ریشهٔ سرویس را بدون مسیر وارد کنید.',
    'Use a private, loopback, link-local, or local-DNS host.': 'از میزبان خصوصی، loopback، link-local یا DNS محلی استفاده کنید.',
    'The data service must use localhost or a loopback address.': 'سرویس داده باید از localhost یا نشانی loopback استفاده کند.',
    'Use no more than 14 page keys or IDs.': 'حداکثر ۱۴ کلید یا شناسهٔ صفحه وارد کنید.',
    'Page keys or IDs must use printable ASCII characters.': 'کلیدها یا شناسه‌های صفحه باید فقط از نویسه‌های قابل چاپ ASCII باشند.',
    'Page keys or IDs must be unique.': 'کلیدها یا شناسه‌های صفحه باید یکتا باشند.',
    'Text must use printable ASCII characters.': 'متن باید فقط از نویسه‌های قابل چاپ ASCII باشد.',
    'Text must contain at least 5 printable ASCII bytes.': 'متن باید دست‌کم ۵ بایت ASCII قابل چاپ داشته باشد.',
    'Text and repeat gap must fit within 40 bytes.': 'متن همراه با فاصلهٔ تکرار باید در ۴۰ بایت جا شود.',
  } as Record<string, string>)[message] ?? message
  const [draftToken, setDraftToken] = useState(token)
  const [draftAppTitle, setDraftAppTitle] = useState(appTitle)
  const [titleBusy, setTitleBusy] = useState(false)
  const [titleNotice, setTitleNotice] = useState('')
	const [buzzerPathBusy, setBuzzerPathBusy] = useState(false)
	const [buzzerPathNotice, setBuzzerPathNotice] = useState('')
  const [displayBrightness, setDisplayBrightness] = useState(snapshot.settings.display_brightness)
  const [displayClosedBrightness, setDisplayClosedBrightness] = useState(snapshot.settings.display_closed_brightness)
  const [motionExitHoldSeconds, setMotionExitHoldSeconds] = useState(snapshot.settings.motion_exit_hold_seconds || 2)
  const [statusBrightness, setStatusBrightness] = useState(snapshot.settings.status_brightness)
  const [streamPeriod, setStreamPeriod] = useState(snapshot.settings.stream_period_ms || 200)
  const [outputPersistence, setOutputPersistence] = useState(snapshot.settings.output_persistence)
  const [relayRestoreMask, setRelayRestoreMask] = useState(snapshot.settings.relay_restore_mask)
  const [segmentScroll, setSegmentScroll] = useState<SegmentScrollSettings>({
    enabled: true,
    pages: ['door'],
    door_open_text: 'door is open',
    door_closed_text: 'door is closed',
    speed_ms: 220,
    gap_cells: 3,
  })
  const [segmentPages, setSegmentPages] = useState('door')
  const [savedSegmentScroll, setSavedSegmentScroll] = useState<SegmentScrollSettings | null>(null)
  const [segmentScrollBusy, setSegmentScrollBusy] = useState(true)
  const [segmentScrollNotice, setSegmentScrollNotice] = useState('')
  const [segmentScrollError, setSegmentScrollError] = useState(false)
  const [localIntegrations, setLocalIntegrations] = useState<LocalIntegrationSettings>({
    local_device: { enabled: false, base_url: '' },
    data_hub: { enabled: false, base_url: 'http://127.0.0.1:8080' },
    lifecycle_safety: { session_lock: 'stop-motion', suspend: 'stop-motion', refresh_on_resume: true },
  })
  const [savedIntegrations, setSavedIntegrations] = useState<LocalIntegrationSettings>({
    local_device: { enabled: false, base_url: '' },
    data_hub: { enabled: false, base_url: 'http://127.0.0.1:8080' },
    lifecycle_safety: { session_lock: 'stop-motion', suspend: 'stop-motion', refresh_on_resume: true },
  })
  const [integrationsBusy, setIntegrationsBusy] = useState(true)
  const [integrationsLoaded, setIntegrationsLoaded] = useState(false)
  const [integrationsNotice, setIntegrationsNotice] = useState('')
  const titleValidation = useMemo(() => validateAppTitle(draftAppTitle), [draftAppTitle])
  const normalizedToken = useMemo(() => normalizeSessionToken(draftToken), [draftToken])
	const boardSilent = (snapshot.settings.flags & 0x01) !== 0
	const hostSilent = !(uiConfig?.integrations?.buzzer_host_enabled ?? false)
  useEffect(() => {
    if (sessionAccessRequest <= 0) return
    const frame = window.requestAnimationFrame(() => {
      const field = document.getElementById('session-access-token')
      if (!(field instanceof HTMLInputElement)) return
      field.focus({ preventScroll: true })
      field.scrollIntoView({ block: 'center', behavior: appearance.reduceMotion ? 'auto' : 'smooth' })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [appearance.reduceMotion, sessionAccessRequest])
	const buzzerPath = buzzerPathFromState(boardSilent, hostSilent)
	const applyBuzzerPath = async (value: BuzzerPath) => {
		setBuzzerPathBusy(true)
		setBuzzerPathNotice('')
		try {
			await onBuzzerPath(value)
			setBuzzerPathNotice(copy('Applied immediately to board and host.', 'فوراً روی برد و میزبان اعمال شد.'))
		} catch (cause) {
			setBuzzerPathNotice(cause instanceof Error ? cause.message : String(cause))
		} finally {
			setBuzzerPathBusy(false)
		}
	}
  const localDeviceValidation = useMemo(
    () => validateLocalDeviceURL(localIntegrations.local_device.base_url ?? '', localIntegrations.local_device.enabled),
    [localIntegrations.local_device.base_url, localIntegrations.local_device.enabled],
  )
  const dataHubValidation = useMemo(
    () => validateDataHubURL(localIntegrations.data_hub.base_url ?? '', localIntegrations.data_hub.enabled),
    [localIntegrations.data_hub.base_url, localIntegrations.data_hub.enabled],
  )
  const integrationsValid = localDeviceValidation.valid && dataHubValidation.valid
  const integrationsDirty = integrationsLoaded && !integrationSettingsEqual(localIntegrations, savedIntegrations)
  const titleDirty = titleValidation.normalized !== appTitle
  const tokenDirty = normalizedToken !== token
  const segmentPagesValidation = useMemo(() => validateSegmentPages(segmentPages), [segmentPages])
  const segmentOpenValidation = useMemo(
    () => validateSegmentText(segmentScroll.door_open_text, segmentScroll.gap_cells),
    [segmentScroll.door_open_text, segmentScroll.gap_cells],
  )
  const segmentClosedValidation = useMemo(
    () => validateSegmentText(segmentScroll.door_closed_text, segmentScroll.gap_cells),
    [segmentScroll.door_closed_text, segmentScroll.gap_cells],
  )
  const segmentScrollDraft = useMemo<SegmentScrollSettings>(() => ({
    ...segmentScroll,
    pages: segmentPagesValidation.pages,
    door_open_text: segmentOpenValidation.normalized,
    door_closed_text: segmentClosedValidation.normalized,
  }), [segmentClosedValidation.normalized, segmentOpenValidation.normalized, segmentPagesValidation.pages, segmentScroll])
  const segmentScrollValid = segmentPagesValidation.valid && segmentOpenValidation.valid && segmentClosedValidation.valid
  const segmentScrollDirty = savedSegmentScroll !== null && !segmentScrollSettingsEqual(segmentScrollDraft, savedSegmentScroll)
  const lifecycleOptions: { value: LifecycleSafetyAction; label: string }[] = [
    { value: 'leave', label: copy('Release keys', 'رهاسازی کلیدها') },
    { value: 'stop-motion', label: copy('Stop motion', 'توقف حرکت') },
    { value: 'all-off', label: copy('All outputs off', 'خاموشی همه خروجی‌ها') },
  ]
  const segmentScrollFeedback = segmentScrollNotice || (segmentScrollBusy
    ? savedSegmentScroll
      ? copy('Saving the validated scrolling configuration…', 'در حال ذخیرهٔ تنظیمات معتبر پیمایش…')
      : copy('Loading the persisted scrolling configuration…', 'در حال بارگذاری تنظیمات ذخیره‌شدهٔ پیمایش…')
    : !segmentScrollValid
      ? copy('Correct the highlighted fields before saving.', 'پیش از ذخیره، فیلدهای مشخص‌شده را اصلاح کنید.')
      : segmentScrollDirty
        ? copy('Validated draft · ready to save.', 'پیش‌نویس معتبر است و آمادهٔ ذخیره است.')
        : savedSegmentScroll
          ? copy('Saved configuration is up to date.', 'تنظیمات ذخیره‌شده به‌روز است.')
          : copy('Persistent scrolling configuration is unavailable.', 'تنظیمات ماندگار پیمایش در دسترس نیست.'))

  useEffect(() => setDraftAppTitle(appTitle), [appTitle])
  useEffect(() => {
    let active = true
    void rpc<HostUISettings>('controller.ui.config.get')
      .then((value) => {
        if (!active) return
        const pages = validateSegmentPages(value.segment_scroll.pages.join(' ')).pages
        const canonical = {
          ...value.segment_scroll,
          pages,
          door_open_text: normalizeSegmentTextInput(value.segment_scroll.door_open_text),
          door_closed_text: normalizeSegmentTextInput(value.segment_scroll.door_closed_text),
        }
        setSegmentScroll(canonical)
        setSavedSegmentScroll(canonical)
        setSegmentPages(pages.join(' '))
        setSegmentScrollNotice('')
        setSegmentScrollError(false)
      })
      .catch((cause) => {
        if (!active) return
        setSegmentScrollNotice(cause instanceof Error ? cause.message : String(cause))
        setSegmentScrollError(true)
      })
      .finally(() => { if (active) setSegmentScrollBusy(false) })
    return () => { active = false }
  }, [])
  useEffect(() => {
    setDisplayBrightness(snapshot.settings.display_brightness)
    setDisplayClosedBrightness(snapshot.settings.display_closed_brightness)
    setMotionExitHoldSeconds(snapshot.settings.motion_exit_hold_seconds || 2)
    setStatusBrightness(snapshot.settings.status_brightness)
    setStreamPeriod(snapshot.settings.stream_period_ms || 200)
    setOutputPersistence(snapshot.settings.output_persistence)
    setRelayRestoreMask(snapshot.settings.relay_restore_mask)
  }, [
    snapshot.settings.display_brightness,
    snapshot.settings.display_closed_brightness,
    snapshot.settings.motion_exit_hold_seconds,
    snapshot.settings.status_brightness,
    snapshot.settings.stream_period_ms,
    snapshot.settings.output_persistence,
    snapshot.settings.relay_restore_mask,
  ])

  const setPersistenceBit = (bit: number, enabled: boolean) => {
    setOutputPersistence((current) => enabled ? current | bit : current & ~bit)
  }

  const updateSegmentScroll = (update: (current: SegmentScrollSettings) => SegmentScrollSettings) => {
    setSegmentScrollNotice('')
    setSegmentScrollError(false)
    setSegmentScroll(update)
  }

  const updateSegmentPages = (value: string) => {
    setSegmentScrollNotice('')
    setSegmentScrollError(false)
    setSegmentPages(normalizeSegmentPagesInput(value))
  }

  const saveSegmentScroll = async (event: FormEvent) => {
    event.preventDefault()
    if (!segmentScrollValid) {
      setSegmentScrollNotice(copy('Correct the highlighted scrolling fields before saving.', 'پیش از ذخیره، فیلدهای پیمایش مشخص‌شده را اصلاح کنید.'))
      setSegmentScrollError(true)
      return
    }
    if (!segmentScrollDirty) return
    setSegmentScrollBusy(true)
    setSegmentScrollNotice('')
    setSegmentScrollError(false)
    try {
      const saved = await rpc<HostUISettings>('controller.ui.config.set', {
        segment_scroll: segmentScrollDraft,
      })
      const pages = validateSegmentPages(saved.segment_scroll.pages.join(' ')).pages
      const canonical = {
        ...saved.segment_scroll,
        pages,
        door_open_text: normalizeSegmentTextInput(saved.segment_scroll.door_open_text),
        door_closed_text: normalizeSegmentTextInput(saved.segment_scroll.door_closed_text),
      }
      setSegmentScroll(canonical)
      setSavedSegmentScroll(canonical)
      setSegmentPages(pages.join(' '))
      setSegmentScrollNotice(copy('Saved and applied by the file-watched HOST service.', 'ذخیره شد و سرویس میزبان آن را اعمال کرد.'))
    } catch (cause) {
      setSegmentScrollNotice(cause instanceof Error ? cause.message : String(cause))
      setSegmentScrollError(true)
    } finally {
      setSegmentScrollBusy(false)
    }
  }

  const saveAppTitle = async (event: FormEvent) => {
    event.preventDefault()
    setTitleBusy(true)
    setTitleNotice('')
    try {
      if (!titleValidation.valid) {
        setTitleNotice(validationMessage(titleValidation.error))
        return
      }
      const saved = await onAppTitle(titleValidation.normalized)
      setDraftAppTitle(saved)
      setTitleNotice('Saved')
    } catch (cause) {
      setTitleNotice(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setTitleBusy(false)
    }
  }

  useEffect(() => {
    let active = true
    void rpc<LocalIntegrationSettings>('controller.integrations.local.get')
      .then((value) => {
        if (!active) return
        const normalized: LocalIntegrationSettings = {
          local_device: { ...value.local_device, base_url: normalizeRootURLInput(value.local_device.base_url ?? '') },
          data_hub: { ...value.data_hub, base_url: normalizeRootURLInput(value.data_hub.base_url ?? '') },
          lifecycle_safety: value.lifecycle_safety ?? {
            session_lock: 'stop-motion', suspend: 'stop-motion', refresh_on_resume: true,
          },
        }
        setLocalIntegrations(normalized)
        setSavedIntegrations(normalized)
        setIntegrationsLoaded(true)
        setIntegrationsNotice('')
      })
      .catch((cause) => {
        if (active) setIntegrationsNotice(cause instanceof Error ? cause.message : String(cause))
      })
      .finally(() => {
        if (active) setIntegrationsBusy(false)
      })
    return () => { active = false }
  }, [])

  const saveLocalIntegrations = async (event: FormEvent) => {
    event.preventDefault()
    if (!integrationsValid) {
      setIntegrationsNotice(copy('Correct the highlighted field before saving.', 'پیش از ذخیره، فیلد مشخص‌شده را اصلاح کنید.'))
      return
    }
    setIntegrationsBusy(true)
    setIntegrationsNotice('')
    try {
      const normalized: LocalIntegrationSettings = {
        local_device: { ...localIntegrations.local_device, base_url: localDeviceValidation.normalized },
        data_hub: { ...localIntegrations.data_hub, base_url: dataHubValidation.normalized },
        lifecycle_safety: localIntegrations.lifecycle_safety,
      }
      const saved = await rpc<LocalIntegrationSettings>(
        'controller.integrations.local.set',
        normalized,
      )
      const canonical: LocalIntegrationSettings = {
        local_device: { ...saved.local_device, base_url: normalizeRootURLInput(saved.local_device.base_url ?? '') },
        data_hub: { ...saved.data_hub, base_url: normalizeRootURLInput(saved.data_hub.base_url ?? '') },
        lifecycle_safety: saved.lifecycle_safety,
      }
      setLocalIntegrations(canonical)
      setSavedIntegrations(canonical)
      setIntegrationsNotice('Saved')
    } catch (cause) {
      setIntegrationsNotice(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setIntegrationsBusy(false)
    }
  }

  const appearanceThemeLabel = t(appearance.theme)
  const appearanceLocaleLabel = t(appearance.locale === 'fa' ? 'persian' : 'english')
  const appearanceDirectionLabel = t(appearance.direction === 'auto' ? 'auto' : appearance.direction === 'ltr' ? 'leftToRight' : 'rightToLeft')

  return (
    <>
      <SectionTitle
        eyebrow={snapshot.connected ? copy('Host and controller settings', 'تنظیمات میزبان و کنترلر') : copy('Host settings', 'تنظیمات میزبان')}
        title={t('settings')}
        detail={`${copy('Theme', 'پوسته')}: ${appearanceThemeLabel} · ${copy('Language', 'زبان')}: ${appearanceLocaleLabel} · ${copy('Direction', 'جهت')}: ${appearanceDirectionLabel}${snapshot.connected ? copy(' · controller available', ' · کنترلر در دسترس') : copy(' · controller offline', ' · کنترلر آفلاین')}`}
      />
      <section className="settings-grid">
        <Card icon={HardDrive} iconTone="violet" title={copy('PC host identity', 'هویت میزبان رایانه')} eyebrow={titleBusy ? copy('Saving', 'در حال ذخیره') : titleDirty ? copy('Unsaved change', 'تغییر ذخیره‌نشده') : copy('Saved', 'ذخیره‌شده')} className="settings-card">
          <form onSubmit={(event) => void saveAppTitle(event)}>
            <TextField
              label={copy('Application title', 'عنوان برنامه')}
              value={draftAppTitle}
              minLength={1}
              maxLength={64}
              onInput={(event) => { setTitleNotice(''); setDraftAppTitle(normalizeAppTitle(event.currentTarget.value)) }}
              onChange={(event) => { setTitleNotice(''); setDraftAppTitle(normalizeAppTitle(event.currentTarget.value)) }}
              error={titleDirty && !titleValidation.valid ? validationMessage(titleValidation.error) : undefined}
              success={titleNotice === 'Saved' ? copy(`Saved as “${appTitle}”.`, `با عنوان «${appTitle}» ذخیره شد.`) : undefined}
              hint={!titleDirty ? copy(`Current title: ${appTitle}`, `عنوان فعلی: ${appTitle}`) : copy(`${titleValidation.normalized.length}/64 characters · ready to save`, `${titleValidation.normalized.length} از ۶۴ نویسه · آمادهٔ ذخیره`)}
              action={<Button type="submit" tone="primary" icon={ShieldCheck} busy={titleBusy} disabled={!titleDirty || !titleValidation.valid}>{copy('Save', 'ذخیره')}</Button>}
            />
          </form>
        </Card>

        <PeripheralNamesEditor locale={locale} />

        <Card icon={Binary} iconTone="accent" title={copy('HOST display scrolling', 'پیمایش نمایشگر میزبان')} eyebrow={segmentScroll.enabled ? copy('Enabled', 'فعال') : copy('Disabled', 'غیرفعال')} className="settings-card settings-card--wide">
          <form className="segment-scroll-settings" onSubmit={(event) => void saveSegmentScroll(event)}>
            <Toggle checked={segmentScroll.enabled} onChange={(enabled) => updateSegmentScroll((value) => ({ ...value, enabled }))} label={copy('Supply scrolling text to selected pages', 'ارسال متن پیمایشی به صفحات انتخابی')} detail={copy('The board keeps OPEN/CLSD as its offline fallback; warnings, edits, programming, and other critical overlays remain higher priority.', 'برد در حالت آفلاین OPEN/CLSD را نمایش می‌دهد و هشدارها و لایه‌های مهم اولویت بالاتری دارند.')} />
            <div className="advanced-fields advanced-fields--split">
              <TextField
                label={copy('Selected page keys or IDs', 'کلید یا شناسه صفحات')}
                value={segmentPages}
                dir="ltr"
                spellCheck={false}
                onInput={(event) => updateSegmentPages(event.currentTarget.value)}
                onChange={(event) => updateSegmentPages(event.currentTarget.value)}
                error={!segmentPagesValidation.valid ? validationMessage(segmentPagesValidation.error) : undefined}
                success={segmentScrollDirty && segmentPagesValidation.valid ? copy(`${segmentPagesValidation.pages.length} canonical target${segmentPagesValidation.pages.length === 1 ? '' : 's'}.`, `${segmentPagesValidation.pages.length} مقصد معتبر.`) : undefined}
                hint={copy('Spaces and commas normalize automatically; maximum 14 unique ASCII IDs.', 'فاصله و ویرگول خودکار یکسان می‌شوند؛ حداکثر ۱۴ شناسهٔ یکتای ASCII.')}
              />
              <TextField
                label={copy('Door open text', 'متن درب باز')}
                value={segmentScroll.door_open_text}
                dir="ltr"
                maxLength={40}
                spellCheck={false}
                onInput={(event) => updateSegmentScroll((value) => ({ ...value, door_open_text: normalizeSegmentTextInput(event.currentTarget.value) }))}
                onChange={(event) => updateSegmentScroll((value) => ({ ...value, door_open_text: normalizeSegmentTextInput(event.currentTarget.value) }))}
                error={!segmentOpenValidation.valid ? validationMessage(segmentOpenValidation.error) : undefined}
                success={segmentScrollDirty && segmentOpenValidation.valid ? copy(`${segmentOpenValidation.byteLength} + ${segmentScroll.gap_cells} / 40 bytes.`, `${segmentOpenValidation.byteLength} + ${segmentScroll.gap_cells} از ۴۰ بایت.`) : undefined}
                hint={copy(`${segmentOpenValidation.availableBytes} ASCII bytes available before the repeat gap.`, `${segmentOpenValidation.availableBytes} بایت ASCII پیش از فاصلهٔ تکرار در دسترس است.`)}
              />
              <TextField
                label={copy('Door closed text', 'متن درب بسته')}
                value={segmentScroll.door_closed_text}
                dir="ltr"
                maxLength={40}
                spellCheck={false}
                onInput={(event) => updateSegmentScroll((value) => ({ ...value, door_closed_text: normalizeSegmentTextInput(event.currentTarget.value) }))}
                onChange={(event) => updateSegmentScroll((value) => ({ ...value, door_closed_text: normalizeSegmentTextInput(event.currentTarget.value) }))}
                error={!segmentClosedValidation.valid ? validationMessage(segmentClosedValidation.error) : undefined}
                success={segmentScrollDirty && segmentClosedValidation.valid ? copy(`${segmentClosedValidation.byteLength} + ${segmentScroll.gap_cells} / 40 bytes.`, `${segmentClosedValidation.byteLength} + ${segmentScroll.gap_cells} از ۴۰ بایت.`) : undefined}
                hint={copy(`${segmentClosedValidation.availableBytes} ASCII bytes available before the repeat gap.`, `${segmentClosedValidation.availableBytes} بایت ASCII پیش از فاصلهٔ تکرار در دسترس است.`)}
              />
            </div>
            <RangeField label={copy('Scroll window dwell', 'مکث هر پنجره پیمایش')} value={segmentScroll.speed_ms} min={60} max={5000} step={20} unit="ms" onChange={(speed_ms) => updateSegmentScroll((value) => ({ ...value, speed_ms }))} />
            <RangeField label={copy('Gap between repeats', 'فاصله بین تکرارها')} value={segmentScroll.gap_cells} min={0} max={12} unit={copy(' cells', ' خانه')} onChange={(gap_cells) => updateSegmentScroll((value) => ({ ...value, gap_cells }))} />
            <div className="local-integrations-form__footer">
              <span className={`segment-scroll-settings__notice${segmentScrollError || !segmentScrollValid ? ' is-error' : ''}`} role="status" aria-live="polite">{segmentScrollFeedback}</span>
              <Button type="submit" tone="primary" icon={ShieldCheck} busy={segmentScrollBusy} disabled={!segmentScrollDirty || !segmentScrollValid}>{copy('Save scrolling', 'ذخیره پیمایش')}</Button>
            </div>
          </form>
        </Card>

		<Card icon={Volume2} iconTone="green" title={copy('Buzzer routing', 'مسیر بیزر')} eyebrow={buzzerPath.toUpperCase()} className="settings-card settings-card--wide">
			<div className="setting-group">
				<label>{copy('Playback path', 'مسیر پخش')}</label>
				<Segmented value={buzzerPath} label={copy('Buzzer playback path', 'مسیر پخش بیزر')} options={[
					{ value: 'board', label: copy('Board', 'برد') },
					{ value: 'host', label: copy('PC', 'رایانه') },
					{ value: 'both', label: copy('Both', 'هر دو') },
					{ value: 'none', label: copy('None', 'هیچ‌کدام') },
				]} onChange={(value) => void applyBuzzerPath(value as BuzzerPath)} />
			</div>
			<div className="settings-inline-status">
				<StatusBadge tone={boardSilent ? 'neutral' : 'good'}>{copy(`Board ${boardSilent ? 'silent' : 'active'}`, `برد ${boardSilent ? 'بی‌صدا' : 'فعال'}`)}</StatusBadge>
				<StatusBadge tone={hostSilent ? 'neutral' : 'good'}>{copy(`PC ${hostSilent ? 'silent' : 'active'}`, `رایانه ${hostSilent ? 'بی‌صدا' : 'فعال'}`)}</StatusBadge>
				{buzzerPathBusy && <StatusBadge tone="warn">{copy('Applying…', 'در حال اعمال…')}</StatusBadge>}
			</div>
			{buzzerPathNotice && <p className="settings-action-feedback" role="status">{buzzerPathNotice}</p>}
		</Card>

        <Card icon={PanelTop} iconTone="accent" title={copy('Application preferences', 'ترجیحات برنامه')} eyebrow={`${appearanceThemeLabel} · ${appearanceLocaleLabel} · ${appearanceDirectionLabel}`} className="settings-card settings-card--wide" action={<Button compact icon={Settings2} onClick={openAppPreferences}>{copy('Open preferences', 'باز کردن ترجیحات')}</Button>}>
          <p className="card-copy">{copy('Theme, language, direction, interaction feedback, and quick-header visibility belong to the host application. They are edited in the tabbed preferences dialog and never write to the controller.', 'پوسته، زبان، جهت، بازخورد تعامل و نمایش نوار سریع متعلق به برنامهٔ میزبان هستند. این موارد در گفت‌وگوی زبانه‌دار ترجیحات ویرایش می‌شوند و هرگز روی کنترلر نوشته نمی‌شوند.')}</p>
        </Card>

        <HotkeyEditor locale={appearance.locale} />

        <Card icon={ShieldCheck} iconTone="green" title={t('security')} eyebrow={tokenDirty ? copy('Token change pending', 'تغییر توکن در انتظار اعمال') : token ? copy('Session authenticated', 'نشست معتبر است') : copy('No session token', 'بدون توکن نشست')} className="settings-card">
          <TextField
            label={t('authToken')}
            type="password"
            value={draftToken}
            id="session-access-token"
            autoComplete="off"
            spellCheck={false}
            onInput={(event) => setDraftToken(normalizeSessionToken(event.currentTarget.value))}
            onChange={(event) => setDraftToken(normalizeSessionToken(event.currentTarget.value))}
            hint={tokenDirty ? copy(`${normalizedToken.length} normalized characters · not applied`, `${normalizedToken.length} نویسهٔ نرمال‌شده · اعمال نشده`) : token ? copy('Applied to this browser tab.', 'در این زبانهٔ مرورگر اعمال شده است.') : copy('Enter a token if the host requires one.', 'اگر میزبان توکن می‌خواهد، آن را وارد کنید.')}
            action={<Button tone="primary" icon={ShieldCheck} disabled={!tokenDirty} onClick={() => { onToken(normalizedToken); setDraftToken(normalizedToken) }}>{t('apply')}</Button>}
          />
        </Card>
        {snapshot.connected && <Card icon={CircuitBoard} iconTone="amber" title={copy('Board EEPROM settings', 'تنظیمات EEPROM برد')} eyebrow={snapshot.have_settings ? copy('Live draft · explicit write', 'پیش‌نویس زنده · نوشتن صریح') : boardSettingsReadState === 'loading' ? copy('Reading board settings', 'در حال خواندن تنظیمات برد') : copy('Settings unavailable', 'تنظیمات در دسترس نیست')} className="settings-card">
          {!snapshot.have_settings ? <EmptyState
            icon={MemoryStick}
            title={boardSettingsReadState === 'loading' ? copy('Reading board settings', 'در حال خواندن تنظیمات برد') : copy('Board settings are unavailable', 'تنظیمات برد در دسترس نیست')}
            detail={boardSettingsReadState === 'loading'
              ? copy('Waiting for the controller to return its live EEPROM settings.', 'در انتظار دریافت تنظیمات زندهٔ EEPROM از برد.')
              : copy('No EEPROM values are shown until the controller returns an authoritative settings report.', 'تا دریافت گزارش معتبر تنظیمات از برد، هیچ مقدار EEPROM نمایش داده نمی‌شود.')}
          /> : <>
          <table className="settings-value-table">
            <thead><tr><th>{copy('Setting', 'تنظیم')}</th><th>{copy('EEPROM report', 'گزارش EEPROM')}</th><th>{copy('Live draft', 'پیش‌نویس زنده')}</th></tr></thead>
            <tbody>
              <tr><td>TM1637 · {copy('door open', 'درب باز')}</td><td>{snapshot.settings.display_brightness}</td><td>{displayBrightness}</td></tr>
              <tr><td>TM1637 · {copy('door closed', 'درب بسته')}</td><td>{snapshot.settings.display_closed_brightness}</td><td>{displayClosedBrightness}</td></tr>
              <tr><td>{copy('Motion-menu exit hold', 'مکث خروج از منوی حرکت')}</td><td>{snapshot.settings.motion_exit_hold_seconds} s</td><td>{motionExitHoldSeconds} s</td></tr>
              <tr><td>{copy('Status brightness', 'روشنایی وضعیت')}</td><td>{snapshot.settings.status_brightness}</td><td>{statusBrightness}</td></tr>
              <tr><td>{copy('Telemetry period', 'دورهٔ تله‌متری')}</td><td>{snapshot.settings.stream_period_ms} ms</td><td>{streamPeriod} ms</td></tr>
              <tr><td>{copy('Remember motion command', 'به‌خاطرسپاری فرمان حرکت')}</td><td>{snapshot.settings.output_persistence & 1 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td><td>{outputPersistence & 1 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td></tr>
              <tr><td>{copy('Remember user relays', 'به‌خاطرسپاری رله‌های کاربر')}</td><td>{snapshot.settings.output_persistence & 2 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td><td>{outputPersistence & 2 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td></tr>
              <tr><td>{copy('Remember user PWM', 'به‌خاطرسپاری PWM کاربر')}</td><td>{snapshot.settings.output_persistence & 4 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td><td>{outputPersistence & 4 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td></tr>
              <tr><td>{copy('Stop retains direction', 'حفظ جهت هنگام توقف')}</td><td>{snapshot.settings.output_persistence & 8 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td><td>{outputPersistence & 8 ? copy('Yes', 'بله') : copy('No', 'خیر')}</td></tr>
              <tr><td>{copy('Stored relay restore mask', 'ماسک ذخیره‌شدهٔ بازیابی رله')}</td><td colSpan={2}>0x{relayRestoreMask.toString(16).toUpperCase().padStart(2, '0')}</td></tr>
            </tbody>
          </table>
          <RangeField label={copy('TM1637 · door-open brightness', 'TM1637 · روشنایی درب باز')} value={displayBrightness} min={0} max={7} onChange={setDisplayBrightness} />
          <RangeField label={copy('TM1637 · door-closed brightness', 'TM1637 · روشنایی درب بسته')} value={displayClosedBrightness} min={0} max={7} onChange={setDisplayClosedBrightness} />
          <RangeField label={copy('Motion-menu exit hold', 'مکث خروج از منوی حرکت')} value={motionExitHoldSeconds} min={1} max={31} unit="s" onChange={setMotionExitHoldSeconds} />
          <RangeField label={copy('Status brightness', 'روشنایی وضعیت')} value={statusBrightness} min={0} max={255} onChange={setStatusBrightness} />
          <RangeField label={copy('Telemetry period', 'دورهٔ تله‌متری')} value={streamPeriod} min={50} max={5000} step={50} unit="ms" onChange={setStreamPeriod} />
          <Toggle checked={Boolean(outputPersistence & 1)} onChange={(enabled) => setPersistenceBit(1, enabled)} label={copy('Remember motion command', 'به‌خاطرسپاری فرمان حرکت')} detail={copy('Disabled by default; motion starts released after a reboot.', 'به‌طور پیش‌فرض غیرفعال است؛ حرکت پس از راه‌اندازی دوباره در حالت آزاد آغاز می‌شود.')} />
          <Toggle checked={Boolean(outputPersistence & 2)} onChange={(enabled) => setPersistenceBit(2, enabled)} label={copy('Remember user relays R5–R8', 'به‌خاطرسپاری رله‌های کاربر R5 تا R8')} detail={copy('Restore the user-output relay mask kept in EEPROM.', 'ماسک رله‌های خروجی کاربر ذخیره‌شده در EEPROM بازیابی می‌شود.')} />
          <Toggle checked={Boolean(outputPersistence & 4)} onChange={(enabled) => setPersistenceBit(4, enabled)} label={copy('Remember user PWM values', 'به‌خاطرسپاری مقادیر PWM کاربر')} detail={copy('Restore MOSFET/user PWM channel values after boot.', 'مقادیر کانال‌های MOSFET/PWM کاربر پس از راه‌اندازی بازیابی می‌شوند.')} />
          <Toggle checked={Boolean(outputPersistence & 8)} onChange={(enabled) => setPersistenceBit(8, enabled)} label={copy('Stop keeps direction relay', 'توقف، رلهٔ جهت را حفظ کند')} detail={outputPersistence & 8 ? copy('Motion stop releases only the output/enable relay.', 'توقف حرکت فقط رلهٔ خروجی/فعال‌سازی را آزاد می‌کند.') : copy('Motion stop releases both direction and output relays.', 'توقف حرکت هر دو رلهٔ جهت و خروجی را آزاد می‌کند.')} />
          <Button tone="primary" icon={MemoryStick} onClick={() => void command(settingsSetCommand(snapshot.settings, displayBrightness, displayClosedBrightness, statusBrightness, streamPeriod, motionExitHoldSeconds, outputPersistence, relayRestoreMask))}>{copy('Write board settings', 'نوشتن تنظیمات برد')}</Button>
          </>}
        </Card>}

        <Card icon={snapshot.connected ? Usb : transport.streamState === 'authentication-required' ? ShieldCheck : Cable} iconTone={snapshot.connected ? 'green' : 'amber'} title={t('connection')} eyebrow={snapshot.connected ? copy('Controller connected', 'کنترلر متصل است') : transport.streamState === 'authentication-required' ? t('authenticationRequired') : copy('Controller offline', 'کنترلر آفلاین است')} className="settings-card">
          <div className="data-list"><DataRow label={copy('State', 'وضعیت')} value={snapshot.connection_state} tone={snapshot.connected ? 'good' : 'warn'} /><DataRow label={copy('Port', 'درگاه')} value={snapshot.port.name || copy('automatic', 'خودکار')} mono /><DataRow label="VID:PID" value={`${snapshot.port.vid || '—'}:${snapshot.port.pid || '—'}`} mono /><DataRow label={copy('Serial', 'سریال')} value={snapshot.port.serial_number || '—'} mono /><DataRow label={copy('Baud', 'نرخ باد')} value="115200 8N1" mono /></div>
          <div className="inline-actions">{snapshot.connected
            ? <Button icon={Unplug} onClick={() => void command('close')}>{copy('Pause controller', 'توقف کنترلر')}</Button>
            : transport.streamState === 'authentication-required'
              ? <Button icon={ShieldCheck} tone="primary" onClick={transport.requestAuthentication}>{t('enterSessionToken')}</Button>
              : <Button icon={Cable} tone="primary" onClick={() => void command('reconnect')}>{t('reconnect')}</Button>}
          </div>
        </Card>

        <Card icon={Network} iconTone="violet" title={copy('Host services and lifecycle', 'سرویس‌ها و چرخهٔ میزبان')} eyebrow={integrationsBusy ? copy('Synchronizing', 'در حال همگام‌سازی') : integrationsDirty ? copy('Unsaved changes', 'تغییرات ذخیره‌نشده') : integrationsNotice === 'Saved' ? copy('Saved', 'ذخیره‌شده') : copy('Up to date', 'به‌روز')} className="settings-card settings-card--wide">
          <form className="local-integrations-form" onSubmit={(event) => void saveLocalIntegrations(event)}>
            <section className="integration-config-panel">
              <header><Network size={19} /><div><strong>{copy('Local device', 'وسیلهٔ محلی')}</strong><small>{localIntegrations.local_device.enabled ? copy('Enabled', 'فعال') : copy('Disabled', 'غیرفعال')}</small></div></header>
              <Toggle
                checked={localIntegrations.local_device.enabled}
                onChange={(enabled) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, enabled } })) }}
                label={copy('Enable local device', 'فعال‌سازی وسیلهٔ محلی')}
                detail={localIntegrations.local_device.enabled ? copy('The host will mediate local commands and events.', 'میزبان میانجی فرمان‌ها و رویدادهای محلی خواهد بود.') : copy('No local-device requests will be sent.', 'هیچ درخواستی برای وسیلهٔ محلی ارسال نمی‌شود.')}
              />
              {localIntegrations.local_device.enabled && <TextField
                label={copy('Device root URL', 'نشانی ریشهٔ وسیله')}
                type="url"
                dir="ltr"
                spellCheck={false}
                placeholder="http://controller-device.local"
                value={localIntegrations.local_device.base_url ?? ''}
                onInput={(event) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, base_url: normalizeRootURLInput(event.currentTarget.value) } })) }}
                onChange={(event) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, base_url: normalizeRootURLInput(event.currentTarget.value) } })) }}
                onBlur={() => localDeviceValidation.valid && setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, base_url: localDeviceValidation.normalized } }))}
                error={!localDeviceValidation.valid ? validationMessage(localDeviceValidation.error) : undefined}
                success={localDeviceValidation.valid && localDeviceValidation.normalized ? copy(`Validated: ${localDeviceValidation.normalized}`, `تأیید شد: ${localDeviceValidation.normalized}`) : undefined}
              />}
            </section>
            <section className="integration-config-panel">
              <header><FileSpreadsheet size={19} /><div><strong>{copy('Data hub', 'مرکز داده')}</strong><small>{localIntegrations.data_hub.enabled ? copy('Enabled', 'فعال') : copy('Disabled', 'غیرفعال')}</small></div></header>
              <Toggle
                checked={localIntegrations.data_hub.enabled}
                onChange={(enabled) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, enabled } })) }}
                label={copy('Enable data hub', 'فعال‌سازی مرکز داده')}
                detail={localIntegrations.data_hub.enabled ? copy('Authenticated data operations are routed through the host.', 'عملیات معتبر داده از طریق میزبان مسیریابی می‌شود.') : copy('No data-hub requests will be sent.', 'هیچ درخواستی برای مرکز داده ارسال نمی‌شود.')}
              />
              {localIntegrations.data_hub.enabled && <TextField
                label={copy('Data-hub root URL', 'نشانی ریشهٔ مرکز داده')}
                type="url"
                dir="ltr"
                spellCheck={false}
                placeholder="http://127.0.0.1:8080"
                value={localIntegrations.data_hub.base_url ?? ''}
                onInput={(event) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, base_url: normalizeRootURLInput(event.currentTarget.value) } })) }}
                onChange={(event) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, base_url: normalizeRootURLInput(event.currentTarget.value) } })) }}
                onBlur={() => dataHubValidation.valid && setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, base_url: dataHubValidation.normalized } }))}
                error={!dataHubValidation.valid ? validationMessage(dataHubValidation.error) : undefined}
                success={dataHubValidation.valid && dataHubValidation.normalized ? copy(`Validated: ${dataHubValidation.normalized}`, `تأیید شد: ${dataHubValidation.normalized}`) : undefined}
              />}
            </section>
            <section className="integration-config-panel integration-config-panel--wide">
              <header><ShieldCheck size={19} /><div><strong>{copy('Session and power safety', 'ایمنی نشست و توان')}</strong><small>{localIntegrations.lifecycle_safety.refresh_on_resume ? copy('Automatic reconciliation', 'همگام‌سازی خودکار') : copy('Manual reconciliation', 'همگام‌سازی دستی')}</small></div></header>
              <div className="setting-group">
                <label>{copy('When Windows locks', 'هنگام قفل‌شدن ویندوز')}</label>
                <Segmented
                  value={localIntegrations.lifecycle_safety.session_lock}
                  label={copy('Session-lock safety action', 'عمل ایمنی هنگام قفل نشست')}
                  options={lifecycleOptions}
                  onChange={(session_lock) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, lifecycle_safety: { ...current.lifecycle_safety, session_lock } })) }}
                />
              </div>
              <div className="setting-group">
                <label>{copy('Before system suspend', 'پیش از تعلیق سیستم')}</label>
                <Segmented
                  value={localIntegrations.lifecycle_safety.suspend}
                  label={copy('Suspend safety action', 'عمل ایمنی هنگام تعلیق')}
                  options={lifecycleOptions}
                  onChange={(suspend) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, lifecycle_safety: { ...current.lifecycle_safety, suspend } })) }}
                />
              </div>
              <Toggle
                checked={localIntegrations.lifecycle_safety.refresh_on_resume}
                onChange={(refresh_on_resume) => { setIntegrationsNotice(''); setLocalIntegrations((current) => ({ ...current, lifecycle_safety: { ...current.lifecycle_safety, refresh_on_resume } })) }}
                label={copy('Reconcile after unlock, resume, and network changes', 'همگام‌سازی پس از بازشدن قفل، بازگشت و تغییر شبکه')}
                detail={localIntegrations.lifecycle_safety.refresh_on_resume
                  ? copy(snapshot.connected ? 'The host will refresh live telemetry without forcing a reconnect.' : 'The host will re-arm discovery while respecting a manually paused connection.', snapshot.connected ? 'میزبان بدون اتصال مجدد اجباری، تله‌متری زنده را تازه می‌کند.' : 'میزبان با حفظ توقف دستی اتصال، کشف دستگاه را دوباره فعال می‌کند.')
                  : copy('The current connection state will be preserved until you act.', 'وضعیت فعلی اتصال تا اقدام شما حفظ می‌شود.')}
              />
            </section>
            <footer className="local-integrations-form__footer">
              <span className={integrationsNotice === 'Saved' ? 'text-good' : integrationsNotice ? 'text-warn' : ''}>{integrationsNotice === 'Saved' ? copy('Host service and lifecycle settings saved.', 'تنظیمات سرویس و چرخهٔ میزبان ذخیره شد.') : integrationsNotice || (integrationsDirty ? copy('Review and save the current changes.', 'تغییرات فعلی را بازبینی و ذخیره کنید.') : copy('No pending changes.', 'تغییری در انتظار ذخیره نیست.'))}</span>
              <Button type="submit" tone="primary" icon={ShieldCheck} busy={integrationsBusy} disabled={!integrationsDirty || !integrationsValid}>{copy('Save host settings', 'ذخیرهٔ تنظیمات میزبان')}</Button>
            </footer>
          </form>
        </Card>
      </section>
    </>
  )
}

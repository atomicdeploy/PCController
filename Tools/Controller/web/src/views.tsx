import {
  type FormEvent,
  type ReactNode,
  useEffect,
  useMemo,
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
  CloudDownload,
  Code2,
  Cpu,
  Database,
  DoorOpen,
  Download,
  ExternalLink,
  Fan,
  FileJson2,
  FileSpreadsheet,
  Gauge,
  HardDrive,
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
  RotateCcw,
  Search,
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
  Unplug,
  Upload,
  Usb,
  Volume2,
  Waves,
  Zap,
} from 'lucide-react'
import { motion } from 'motion/react'
import {
  Button,
  Card,
  DataRow,
  EmptyState,
  Icon,
  MetricCard,
  RangeField,
  SectionTitle,
  Segmented,
  StatusBadge,
  TextField,
  Toggle,
} from './components'
import { execute, rpc } from './api'
import { settingsSetCommand } from './command-line'
import { formatClock, formatCompact, formatDuration, formatNumber, type MessageKey } from './i18n'
import type {
  Appearance,
  ControllerEvent,
  DialogState,
  LocalIntegrationSettings,
  Locale,
  LocalDeviceSnapshot,
  MetricSample,
  Snapshot,
} from './types'

export interface SharedViewProps {
  appTitle: string
  snapshot: Snapshot
  samples: MetricSample[]
  events: ControllerEvent[]
  locale: Locale
  t: (key: MessageKey) => string
  command: (command: string, success?: string) => Promise<string>
  refresh: () => Promise<void>
  openDialog: (dialog: Omit<DialogState, 'open'>) => void
}

function pageDetail(snapshot: Snapshot, appTitle: string): string {
  if (snapshot.connected) return snapshot.port.friendly_name || snapshot.port.product || snapshot.port.name || appTitle
  return snapshot.connection_reason || 'Waiting for an authenticated controller'
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

function EventList({ events, locale, t, limit = 6 }: { events: ControllerEvent[]; locale: Locale; t: SharedViewProps['t']; limit?: number }) {
  const visible = events.slice(0, limit)
  if (!visible.length) return <EmptyState icon={Activity} title={t('noEvents')} detail={t('eventStream')} />
  return (
    <div className="event-list">
      {visible.map((event) => (
        <article key={`${event.id}-${event.time}`} className="event-row">
          <span className={`event-row__rail event-row__rail--${eventTone(event)}`} aria-hidden="true" />
          <time dateTime={event.time}>{formatClock(locale, event.time)}</time>
          <div><strong>{event.kind}</strong><p>{event.text || event.reason || event.state || '—'}</p></div>
          {event.source && <span className="event-row__source">{event.source}</span>}
        </article>
      ))}
    </div>
  )
}

export function DashboardView(props: SharedViewProps) {
  const { appTitle, snapshot, samples, events, locale, t, command, refresh, openDialog } = props
  const status = snapshot.status
  const connectedTone = snapshot.connected ? 'good' : snapshot.paused ? 'warn' : 'bad'
  const hash = snapshot.hello.build_hash ? snapshot.hello.build_hash.toString(16).toUpperCase().padStart(8, '0') : '—'
  return (
    <>
      <SectionTitle
        eyebrow={t('liveTelemetry')}
        title={t('dashboard')}
        detail={pageDetail(snapshot, appTitle)}
        action={
          <div className="header-actions">
            <StatusBadge tone={connectedTone} pulse={snapshot.connection_state === 'connecting'}>
              {snapshot.connected ? t('online') : snapshot.connection_state === 'connecting' ? t('connecting') : t('offline')}
            </StatusBadge>
            <Button icon={RefreshCw} compact onClick={() => void refresh()}>{t('refresh')}</Button>
          </div>
        }
      />

      <section className={`hero-panel${snapshot.connected ? ' is-online' : ''}`}>
        <div className="hero-panel__scan" aria-hidden="true" />
        <div className="hero-panel__identity">
          <div className="eyebrow">CONTROLLER / {snapshot.connection_state.toUpperCase()}</div>
          <h2>{snapshot.connected ? snapshot.hello.name || appTitle : t('noHardware')}</h2>
          <p>{snapshot.connected ? `USB ${snapshot.port.vid || '—'}:${snapshot.port.pid || '—'} · ${snapshot.port.name || 'automatic port'}` : snapshot.connection_reason || t('noHardware')}</p>
        </div>
        <div className="hero-panel__readout" dir="ltr">
          <span>BUILD</span><strong>{hash}</strong>
          <span>UPTIME</span><strong>{formatDuration(locale, status.uptime_ms)}</strong>
        </div>
        <div className="hero-panel__signal" aria-hidden="true">
          {Array.from({ length: 42 }, (_, index) => (
            <motion.i
              key={index}
              animate={{ scaleY: snapshot.connected ? [.18, .45 + ((index * 17) % 55) / 100, .2] : .08, opacity: snapshot.connected ? [.28, .9, .32] : .14 }}
              transition={{ duration: 1.6 + (index % 7) * .11, delay: index * .018, repeat: Infinity, ease: 'easeInOut' }}
            />
          ))}
        </div>
      </section>

      <section className="metric-grid">
        <MetricCard icon={Zap} label={t('voltage')} value={formatNumber(locale, status.supply_mv / 1000, 2)} unit="V" values={values(samples, 'supply')} tone="cyan" detail={`Bus ${formatNumber(locale, status.bus_mv / 1000, 2)} V`} />
        <MetricCard icon={Waves} label={t('current')} value={formatNumber(locale, status.current_ma, 0)} unit="mA" values={values(samples, 'current')} tone="green" detail={`${formatNumber(locale, status.power_mw / 1000, 2)} W`} />
        <MetricCard icon={Thermometer} label={`${t('temperature')} · LED`} value={formatNumber(locale, status.temperature_led_centi_c / 100, 1)} unit="°C" values={values(samples, 'ledTemp')} tone="amber" detail={`BT ${formatNumber(locale, status.temperature_bt_audio_centi_c / 100, 1)} °C`} />
        <MetricCard icon={PlugZap} label="PWM" value={formatNumber(locale, status.pwm_value, 0)} unit="/4095" values={values(samples, 'power')} tone="violet" detail={`CH ${status.pwm_channel + 1} · mode ${status.pwm_mode}`} />
      </section>

      <section className="dashboard-grid">
        <Card title={t('outputs')} eyebrow="R1—R8" className="outputs-card" action={<StatusBadge tone={status.active_relays ? 'warn' : 'neutral'}>{status.active_relays ? 'ACTIVE' : 'SAFE'}</StatusBadge>}>
          <div className="output-matrix">
            {Array.from({ length: 8 }, (_, index) => {
              const active = Boolean(status.active_relays & (1 << index))
              return (
                <button
                  key={index}
                  className={`output-cell${active ? ' is-active' : ''}`}
                  onClick={() => void command(`relay ${index + 1} toggle`, `R${index + 1} updated`)}
                  disabled={!snapshot.connected}
                  aria-pressed={active}
                >
                  <span>R{index + 1}</span><strong>{active ? t('on') : t('off')}</strong><i aria-hidden="true" />
                </button>
              )
            })}
          </div>
          <div className="safety-strip"><ShieldCheck size={17} /><span>{t('outputSafety')}</span></div>
        </Card>

        <Card title={t('status')} eyebrow={t('device')} className="device-card">
          <div className="data-list">
            <DataRow label={t('device')} value={snapshot.port.friendly_name || snapshot.port.name || '—'} />
            <DataRow label={t('firmware')} value={snapshot.hello.build_timestamp || hash} mono />
            <DataRow label={t('door')} value={status.door_open ? t('open') : t('closed')} tone={status.door_open ? 'warn' : 'good'} />
            <DataRow label={t('bluetooth')} value={status.bluetooth_audio_state === 2 ? t('online') : status.bluetooth_audio_state === 1 ? t('connecting') : t('offline')} />
            <DataRow label="UART CRC / framing" value={`${status.crc_errors} / ${status.framing_errors}`} mono tone={status.crc_errors || status.framing_errors ? 'warn' : 'good'} />
            <DataRow label="Reset count" value={status.reset_count} mono />
          </div>
        </Card>

        <Card title={t('quickActions')} eyebrow="GUARDED" className="actions-card">
          <div className="action-grid">
            <Button icon={Unplug} tone="danger" disabled={!snapshot.connected} onClick={() => openDialog({
              tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'),
              action: async () => { await command('relay off', t('emergencyOff')); await command('pwm off') },
            })}>{t('emergencyOff')}</Button>
            <Button icon={Cable} disabled={snapshot.connected} onClick={() => void command('reconnect', t('reconnect'))}>{t('reconnect')}</Button>
            <Button icon={Gauge} disabled={!snapshot.connected} onClick={() => void command('status')}>Read status</Button>
            <Button icon={Lightbulb} disabled={!snapshot.connected} onClick={() => void command('rgb effect play attention')}>Attention cue</Button>
          </div>
        </Card>

        <Card title={t('events')} eyebrow={t('eventStream')} className="activity-card" action={<span className="count-chip">{events.length}</span>}>
          <EventList events={events} locale={locale} t={t} />
        </Card>
      </section>
    </>
  )
}

export function ControlsView(props: SharedViewProps) {
  const { snapshot, t, command, openDialog } = props
  const [selectedPWM, setSelectedPWM] = useState(snapshot.status.pwm_channel || 0)
  const [pwmValue, setPWMValue] = useState(snapshot.status.pwm_value || 0)
  const [red, setRed] = useState(25)
  const [green, setGreen] = useState(130)
  const [blue, setBlue] = useState(220)
  const [consoleCommand, setConsoleCommand] = useState('status')
  const [consoleOutput, setConsoleOutput] = useState('')
  const [busy, setBusy] = useState(false)

  const runConsole = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try { setConsoleOutput(await command(consoleCommand)) } finally { setBusy(false) }
  }
  return (
    <>
      <SectionTitle eyebrow="DIRECT / SAFE" title={t('controls')} detail="Relays, motion, PWM, lighting, menus, macros and the canonical command surface." />
      <section className="control-layout">
        <Card eyebrow="74HC595" title="Relays & motion" className="relay-control-card" action={<StatusBadge tone={snapshot.status.active_relays ? 'warn' : 'good'}>{snapshot.status.active_relays ? 'ENERGIZED' : 'RELEASED'}</StatusBadge>}>
          <div className="relay-bank">
            {Array.from({ length: 8 }, (_, index) => {
              const active = Boolean(snapshot.status.active_relays & (1 << index))
              return (
                <button key={index} className={`relay-switch${active ? ' is-active' : ''}`} disabled={!snapshot.connected} onClick={() => void command(`relay ${index + 1} toggle`)}>
                  <span>R{index + 1}</span><i aria-hidden="true"><b /></i><small>{index < 4 ? index % 2 ? 'Enable' : 'Direction' : 'User output'}</small>
                </button>
              )
            })}
          </div>
          <div className="motion-actions">
            {(['left', 'right'] as const).map((side) => (
              <div key={side} className="motion-side"><strong>{side.toUpperCase()} SIDE</strong><Button compact disabled={!snapshot.connected} onClick={() => void command(`relay side ${side} up`)}>Up</Button><Button compact disabled={!snapshot.connected} onClick={() => void command(`relay side ${side} stop`)}>Stop</Button><Button compact disabled={!snapshot.connected} onClick={() => void command(`relay side ${side} down`)}>Down</Button></div>
            ))}
          </div>
          <Button icon={AlertOctagon} tone="danger" disabled={!snapshot.connected} onClick={() => openDialog({ tone: 'danger', title: t('confirmEmergencyTitle'), body: t('confirmEmergencyBody'), confirmLabel: t('emergencyOff'), action: async () => { await command('relay off') } })}>{t('emergencyOff')}</Button>
        </Card>

        <Card eyebrow="PCA9685 / 12-BIT" title="PWM matrix" className="pwm-card">
          <div className="channel-selector">
            {Array.from({ length: 16 }, (_, index) => <button key={index} className={selectedPWM === index ? 'is-active' : ''} onClick={() => setSelectedPWM(index)}>{index + 1}</button>)}
          </div>
          <div className="pwm-readout"><span>CH {selectedPWM + 1}</span><strong dir="ltr">{pwmValue}</strong><small>/ 4095</small></div>
          <RangeField label="Duty value" value={pwmValue} min={0} max={4095} onChange={setPWMValue} disabled={!snapshot.connected} />
          <div className="inline-actions"><Button tone="primary" icon={SlidersHorizontal} disabled={!snapshot.connected} onClick={() => void command(`pwm set ${selectedPWM} ${pwmValue}`)}>Apply channel</Button><Button icon={Power} disabled={!snapshot.connected} onClick={() => void command('pwm off')}>Clear all</Button></div>
        </Card>

        <Card eyebrow="SEMANTIC LIGHT" title="Status lighting" className="rgb-card">
          <div className="color-preview" style={{ '--preview': `rgb(${red} ${green} ${blue})` } as React.CSSProperties}><span /><strong>RGB {red} · {green} · {blue}</strong></div>
          <RangeField label="Red" value={red} min={0} max={255} onChange={setRed} disabled={!snapshot.connected} />
          <RangeField label="Green" value={green} min={0} max={255} onChange={setGreen} disabled={!snapshot.connected} />
          <RangeField label="Blue" value={blue} min={0} max={255} onChange={setBlue} disabled={!snapshot.connected} />
          <div className="inline-actions"><Button tone="primary" icon={Sparkles} disabled={!snapshot.connected} onClick={() => void command(`rgb ${red} ${green} ${blue} 190`)}>Apply color</Button><Button disabled={!snapshot.connected} onClick={() => void command('rgb effect play breathe')}>Breathe</Button></div>
        </Card>

        <Card eyebrow="ONE DISPATCHER" title="Command console" className="console-card">
          <form className="command-form" onSubmit={(event) => void runConsole(event)}>
            <span className="command-prompt">pc›</span>
            <input aria-label="Controller command" value={consoleCommand} onChange={(event) => setConsoleCommand(event.target.value)} spellCheck={false} dir="ltr" />
            <Button type="submit" tone="primary" busy={busy} icon={Send}>{t('run')}</Button>
          </form>
          <pre className="command-output" dir="ltr">{consoleOutput || 'Commands are correlated through the primary host. Try: status, settings, rf list, macro list.'}</pre>
          <div className="command-chips">{['status', 'settings', 'rf list', 'macro list', 'i2c scan'].map((value) => <button key={value} onClick={() => setConsoleCommand(value)}>{value}</button>)}</div>
        </Card>
      </section>
    </>
  )
}

const deviceResources = ['capabilities', 'snapshot'] as const

export function LocalDeviceView({ locale, t }: SharedViewProps) {
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
    setBusy(kind)
    try {
      setSnapshot(await rpc<LocalDeviceSnapshot>('controller.device.action', { action: kind, text, count }))
      setError('')
      if (kind === 'display.message') setMessage('')
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const inspect = async () => {
    setBusy('inspect')
    try { setInspection(await rpc('controller.device.inspect', { resource })); setError('') }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const powered = snapshot.power === 'ON'
  return (
    <>
      <SectionTitle eyebrow="LOCAL DEVICE / PCC V1" title={t('device')} detail={snapshot.base_url || 'Typed local-network companion through the primary host'} action={<StatusBadge tone={snapshot.websocket_online ? 'good' : snapshot.http_reachable ? 'warn' : 'bad'} pulse={snapshot.phase === 'connecting'}>{snapshot.websocket_online ? 'EVENT STREAM' : snapshot.http_reachable ? 'HTTP' : t('offline')}</StatusBadge>} />
      <section className="device-layout">
        <Card className={`device-stage${powered ? ' is-on' : ''}`}>
          <div className="device-stage__rails" aria-hidden="true"><i /><i /><i /><i /></div>
          <div className="device-node" aria-label={`Device power ${snapshot.power ?? 'unknown'}`}>
            <div className="device-node__top"><Cpu size={20} /><span>PCC / LOCAL DEVICE V1</span></div>
            <motion.div className="device-node__core" animate={{ opacity: powered ? 1 : .34, filter: powered ? 'brightness(1.18) saturate(1.12)' : 'brightness(.7) saturate(.42)' }} transition={{ duration: .55, ease: [0.22, 1, 0.36, 1] }}>
              <span className="device-node__signal" /><strong>CONTROL NODE</strong><small>{snapshot.websocket_online ? 'EVENT LINK' : snapshot.http_reachable ? 'HTTP LINK' : 'STANDBY'}</small><i /><i /><i />
            </motion.div>
            <div className="device-node__bus"><span>01</span><span>10</span><span>11</span><span>00</span></div>
          </div>
          <div className="device-state"><span>{t('status')}</span><strong>{snapshot.power ?? t('unknown')}</strong><small>{snapshot.updated_at ? formatClock(locale, snapshot.updated_at) : '—'}</small></div>
          <div className="device-actions"><Button tone="success" icon={Sun} busy={busy === 'power.on'} onClick={() => void action('power.on')}>{t('on')}</Button><Button tone="secondary" icon={Moon} busy={busy === 'power.off'} onClick={() => void action('power.off')}>{t('off')}</Button><Button tone="primary" icon={CirclePower} busy={busy === 'power.toggle'} onClick={() => void action('power.toggle')}>{t('toggle')}</Button></div>
          {error && <div className="inline-error"><AlertOctagon size={17} /><span>{error}</span></div>}
        </Card>

        <div className="device-side">
          <Card title={t('message')} eyebrow="BOUNDED ACTION" action={<StatusBadge tone="info">PCC DEVICE V1</StatusBadge>}>
            <div className="message-compose"><input value={message} maxLength={256} placeholder="A bounded display message" onChange={(event) => setMessage(event.target.value)} /><Button icon={Send} tone="primary" busy={busy === 'display.message'} disabled={!message.trim()} onClick={() => void action('display.message', message.trim())}>{t('send')}</Button></div>
            <div className="beep-row"><div><Volume2 size={18} /><span>{t('beep')} · explicit alert pulse</span></div>{[1, 2, 3].map((count) => <Button key={count} compact busy={busy === 'alert.pulse'} onClick={() => void action('alert.pulse', '', count)}>{count}×</Button>)}</div>
          </Card>

          <Card title={t('diagnostics')} eyebrow="SAFE TYPED INSPECTION" action={<Button icon={RefreshCw} compact busy={busy === 'inspect'} onClick={() => void inspect()}>{t('fetch')}</Button>}>
            <div className="diagnostic-picker">{deviceResources.map((value) => <button key={value} className={resource === value ? 'is-active' : ''} onClick={() => setResource(value)} dir="ltr">{value}</button>)}</div>
            <pre className="diagnostic-output" dir="ltr">{inspection ? JSON.stringify(inspection, null, 2) : 'Only the controller capability document and sanitized snapshot are inspectable.'}</pre>
          </Card>

          <Card title="Connection health" eyebrow="PASSIVE EVENT STREAM">
            <div className="data-list"><DataRow label="Phase" value={snapshot.phase || 'idle'} /><DataRow label="HTTP" value={snapshot.http_reachable ? t('online') : t('offline')} tone={snapshot.http_reachable ? 'good' : 'bad'} /><DataRow label="Event stream" value={snapshot.websocket_online ? t('online') : t('offline')} tone={snapshot.websocket_online ? 'good' : 'warn'} /><DataRow label="Last event" value={snapshot.last_event || '—'} /><DataRow label="Capabilities" value={snapshot.capabilities?.join(', ') || '—'} /></div>
          </Card>
        </div>
      </section>
    </>
  )
}

export function EventsView({ events, locale, t }: SharedViewProps) {
  const [query, setQuery] = useState('')
  const [level, setLevel] = useState<'all' | 'good' | 'warn' | 'bad' | 'info'>('all')
  const visible = events.filter((event) => {
    if (level !== 'all' && eventTone(event) !== level) return false
    const term = query.trim().toLowerCase()
    return !term || `${event.kind} ${event.text} ${event.source ?? ''}`.toLowerCase().includes(term)
  })
  return (
    <>
      <SectionTitle eyebrow="ORDERED / CORRELATED" title={t('events')} detail="One timeline for controller lifecycle, RF, doors, macros, automations and integrated services." action={<StatusBadge tone="info">{events.length} retained</StatusBadge>} />
      <Card className="events-page-card">
        <div className="event-toolbar"><label className="table-search"><Search size={17} /><input value={query} placeholder={t('search')} onChange={(event) => setQuery(event.target.value)} /></label><Segmented value={level} label="Event severity" options={[{ value: 'all', label: 'All' }, { value: 'good', label: 'Good' }, { value: 'warn', label: 'Warn' }, { value: 'bad', label: 'Fault' }, { value: 'info', label: 'Info' }]} onChange={setLevel} /></div>
        <EventList events={visible} locale={locale} t={t} limit={500} />
      </Card>
    </>
  )
}

export function SettingsView({ snapshot, t, command, appearance, onAppearance, token, onToken }: SharedViewProps & { appearance: Appearance; onAppearance: (value: Appearance) => void; token: string; onToken: (value: string) => void }) {
  const [draftToken, setDraftToken] = useState(token)
  const [displayBrightness, setDisplayBrightness] = useState(snapshot.settings.display_brightness)
  const [statusBrightness, setStatusBrightness] = useState(snapshot.settings.status_brightness)
  const [streamPeriod, setStreamPeriod] = useState(snapshot.settings.stream_period_ms || 200)
  const [localIntegrations, setLocalIntegrations] = useState<LocalIntegrationSettings>({
    local_device: { enabled: false, base_url: '' },
    data_hub: { enabled: false, base_url: 'http://127.0.0.1:8080' },
  })
  const [integrationsBusy, setIntegrationsBusy] = useState(true)
  const [integrationsNotice, setIntegrationsNotice] = useState('')
  const updateAppearance = <K extends keyof Appearance>(key: K, value: Appearance[K]) => onAppearance({ ...appearance, [key]: value })

  useEffect(() => {
    let active = true
    void rpc<LocalIntegrationSettings>('controller.integrations.local.get')
      .then((value) => {
        if (!active) return
        setLocalIntegrations(value)
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
    setIntegrationsBusy(true)
    setIntegrationsNotice('')
    try {
      const saved = await rpc<LocalIntegrationSettings>(
        'controller.integrations.local.set',
        localIntegrations,
      )
      setLocalIntegrations(saved)
      setIntegrationsNotice('Saved. The primary host is applying the new local routes.')
    } catch (cause) {
      setIntegrationsNotice(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setIntegrationsBusy(false)
    }
  }

  return (
    <>
      <SectionTitle eyebrow="PERSISTENT / ACCESSIBLE" title={t('settings')} detail="Global appearance is shared across controller, device, and data-workspace views; hardware settings remain board-owned." />
      <section className="settings-grid">
        <Card title={t('appearance')} eyebrow="RTL / LTR / THEME" className="settings-card settings-card--wide">
          <div className="setting-group"><label>{t('theme')}</label><Segmented value={appearance.theme} label={t('theme')} options={[{ value: 'system', label: t('system'), icon: PanelTop }, { value: 'dark', label: t('dark'), icon: Moon }, { value: 'light', label: t('light'), icon: Sun }]} onChange={(value) => updateAppearance('theme', value)} /></div>
          <div className="setting-group"><label>{t('language')}</label><Segmented value={appearance.locale} label={t('language')} options={[{ value: 'en', label: t('english'), icon: Languages }, { value: 'fa', label: t('persian'), icon: Languages }]} onChange={(value) => updateAppearance('locale', value)} /></div>
          <div className="setting-group"><label>{t('direction')}</label><Segmented value={appearance.direction} label={t('direction')} options={[{ value: 'auto', label: t('auto') }, { value: 'ltr', label: t('leftToRight') }, { value: 'rtl', label: t('rightToLeft') }]} onChange={(value) => updateAppearance('direction', value)} /></div>
          <Toggle checked={appearance.reduceMotion} onChange={(value) => updateAppearance('reduceMotion', value)} label={t('reduceMotion')} detail="Honors operating-system reduced-motion preferences and removes nonessential spring/sweep effects." />
          <Toggle checked={appearance.compactNumbers} onChange={(value) => updateAppearance('compactNumbers', value)} label={t('compactNumbers')} detail="Use concise notation for large product and telemetry counts." />
          <Toggle checked={!appearance.audioMuted} onChange={(value) => updateAppearance('audioMuted', !value)} label={<span className="setting-icon-label"><Volume2 size={16} />Interaction audio</span>} detail="Gesture-gated procedural focus, navigation and confirmation cues; no downloaded audio, music, or continuous sound." />
          <RangeField label="Cue volume" value={Math.round(appearance.audioVolume * 100)} min={0} max={100} unit="%" disabled={appearance.audioMuted} onChange={(value) => updateAppearance('audioVolume', value / 100)} />
        </Card>

        <Card title={t('security')} eyebrow="SESSION-ONLY TOKEN" className="settings-card">
          <TextField label={t('authToken')} type="password" value={draftToken} autoComplete="off" onChange={(event) => setDraftToken(event.target.value)} hint="Stored only in this browser tab's session storage; never included in bundled assets." />
          <Button tone="primary" icon={ShieldCheck} onClick={() => onToken(draftToken)}>{t('apply')}</Button>
        </Card>

        <Card title="Board-owned display" eyebrow="EEPROM / EXPLICIT WRITE" className="settings-card">
          <RangeField label="TM1637 brightness" value={displayBrightness} min={0} max={7} onChange={setDisplayBrightness} disabled={!snapshot.connected} />
          <RangeField label="Status brightness" value={statusBrightness} min={0} max={255} onChange={setStatusBrightness} disabled={!snapshot.connected} />
          <RangeField label="Telemetry period" value={streamPeriod} min={50} max={5000} step={50} unit="ms" onChange={setStreamPeriod} disabled={!snapshot.connected} />
          <Button tone="primary" icon={MemoryStick} disabled={!snapshot.connected} onClick={() => void command(settingsSetCommand(snapshot.settings, displayBrightness, statusBrightness, streamPeriod))}>Write board settings</Button>
        </Card>

        <Card title={t('connection')} eyebrow="PRIMARY OWNER" className="settings-card">
          <div className="data-list"><DataRow label="State" value={snapshot.connection_state} tone={snapshot.connected ? 'good' : 'warn'} /><DataRow label="Port" value={snapshot.port.name || 'automatic'} mono /><DataRow label="VID:PID" value={`${snapshot.port.vid || '—'}:${snapshot.port.pid || '—'}`} mono /><DataRow label="Serial" value={snapshot.port.serial_number || '—'} mono /><DataRow label="Baud" value="115200 8N1" mono /></div>
          <div className="inline-actions"><Button icon={Cable} disabled={snapshot.connected} onClick={() => void command('reconnect')}>{t('reconnect')}</Button><Button icon={Unplug} disabled={!snapshot.connected} onClick={() => void command('close')}>Pause</Button></div>
        </Card>

        <Card title="Local companions" eyebrow="PERSISTENT / HOST-OWNED" className="settings-card settings-card--wide">
          <form className="local-integrations-form" onSubmit={(event) => void saveLocalIntegrations(event)}>
            <section className="integration-config-panel">
              <header><Network size={19} /><div><strong>Local Device</strong><small>Controller capability contract</small></div></header>
              <Toggle
                checked={localIntegrations.local_device.enabled}
                onChange={(enabled) => setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, enabled } }))}
                label="Enable Local Device"
                detail="Commands, live state, message, beep and scrubbed diagnostics remain host-mediated."
              />
              <TextField
                label="Device root URL"
                type="url"
                dir="ltr"
                spellCheck={false}
                placeholder="http://controller-device.local"
                value={localIntegrations.local_device.base_url ?? ''}
                onChange={(event) => setLocalIntegrations((current) => ({ ...current, local_device: { ...current.local_device, base_url: event.target.value } }))}
                hint="HTTP(S), canonical root only; private, loopback, link-local or explicitly local DNS."
              />
            </section>
            <section className="integration-config-panel">
              <header><FileSpreadsheet size={19} /><div><strong>Data Hub</strong><small>Loopback data service</small></div></header>
              <Toggle
                checked={localIntegrations.data_hub.enabled}
                onChange={(enabled) => setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, enabled } }))}
                label="Enable Data Hub"
                detail="Records, downloads and guarded operations use the authenticated same-origin bridge."
              />
              <TextField
                label="Data-hub root URL"
                type="url"
                dir="ltr"
                spellCheck={false}
                placeholder="http://127.0.0.1:8080"
                value={localIntegrations.data_hub.base_url ?? ''}
                onChange={(event) => setLocalIntegrations((current) => ({ ...current, data_hub: { ...current.data_hub, base_url: event.target.value } }))}
                hint="Loopback HTTP(S) root only. Host-controller credentials are stripped before forwarding."
              />
            </section>
            <footer className="local-integrations-form__footer">
              <span className={integrationsNotice.startsWith('Saved.') ? 'text-good' : integrationsNotice ? 'text-warn' : ''}>{integrationsNotice || 'Secrets and upstream sessions never enter browser storage.'}</span>
              <Button type="submit" tone="primary" icon={ShieldCheck} busy={integrationsBusy}>Save integrations</Button>
            </footer>
          </form>
        </Card>

        <Card title={t('integrations')} eyebrow="ONE AUTHORITY / TYPED IPC" className="settings-card settings-card--wide">
          <div className="integration-principles"><article><Radio /><strong>Demand-driven telemetry</strong><p>Status polling runs only while a UI or automation subscribes.</p></article><article><ShieldCheck /><strong>Fail-closed writes</strong><p>Every relay, device, and data mutation crosses an authenticated typed boundary.</p></article><article><Binary /><strong>Exact byte serving</strong><p>Hashed assets and downloads preserve HEAD, ETag, Range, If-Range and 416 semantics.</p></article><article><Languages /><strong>One locale system</strong><p>Vazirmatn, Persian digits and RTL flow apply without reversing protocol text.</p></article></div>
        </Card>
      </section>
    </>
  )
}

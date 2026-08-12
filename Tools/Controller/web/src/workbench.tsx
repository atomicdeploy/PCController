import { type CSSProperties, type FormEvent, useEffect, useRef, useState } from 'react'
import {
  Activity,
  AudioLines,
  Binary,
  Bot,
  Cable,
  Cpu,
  Eraser,
  Gauge,
  Keyboard,
  LayoutDashboard,
  Lightbulb,
  List,
  ListRestart,
  MemoryStick,
  MonitorCog,
  Network,
  Palette,
  Play,
  ScanSearch,
  Send,
  Settings2,
  ShieldCheck,
  SquareTerminal,
  StopCircle,
  SunMedium,
  Unplug,
  Volume2,
  Workflow,
} from 'lucide-react'
import {
  Button,
  Card,
  RangeField,
  SectionTitle,
  Segmented,
  StatusBadge,
  TextField,
} from './components'
import { formatClock } from './i18n'
import { redactSensitiveCommand, shellArgument } from './command-line'
import {
  BrowserConsoleModel,
  consoleTokensText,
  consoleTokensToNativeArgs,
  parseConsoleInvocation,
  type ConsoleEntry,
  type ConsoleToken,
} from './console-format'
import { AdvancedWorkbench } from './advanced-workbench'
import {
  canonicalPeripheralHash,
  peripheralDestinationByID,
  peripheralDestinationDetail,
  peripheralDestinationFromHash,
  peripheralDestinationLabel,
  peripheralDestinationState,
  peripheralDestinations,
  peripheralGroups,
  peripheralStateLabel,
  type PeripheralDestinationID,
} from './peripheral-navigation'
import { RFGuidedWorkflow } from './rf-guided-workflow'
import { MacroLibraryPanel } from './macro-library'
import type { SharedViewProps } from './views'

interface TextTerminalRow {
  id: string
  kind: 'text'
  text: string
  level?: 'log' | 'info' | 'warn' | 'error' | 'debug'
}

interface ConsoleTerminalRow {
  id: string
  kind: 'console'
  entry: ConsoleEntry
}

type TerminalRow = TextTerminalRow | ConsoleTerminalRow

export type RFLearnMode = 'indefinite' | 'timer'

export function rfLearnCommand(mode: RFLearnMode, seconds: number): string {
  if (mode === 'indefinite') return 'rf learn indefinite'
  const bounded = Math.max(1, Math.min(120, Math.round(seconds)))
  return `rf learn timer ${bounded}s`
}

let terminalRowID = 0

function textRow(text: string, level: TextTerminalRow['level'] = 'log', id?: string): TextTerminalRow {
  return { id: id ?? `terminal-${++terminalRowID}`, kind: 'text', text, level }
}

function ConsoleTokens({ tokens }: { tokens: readonly ConsoleToken[] }) {
  return <>{tokens.map((token, index) => <span key={`${index}-${token.text}`} className={`console-token console-token--${token.kind === 'value' ? token.valueType : 'text'}`} style={token.style as CSSProperties}>{token.text}</span>)}</>
}

function ConsoleRecord({ entry, locale }: { entry: ConsoleEntry; locale: SharedViewProps['locale'] }) {
  if (entry.hidden) return null
  const style = { '--console-indent': `${entry.depth * 16}px` } as CSSProperties
  if (entry.kind === 'table') {
    return (
      <div className="console-record console-record--table" style={style} data-level="log">
        <table><thead><tr>{entry.columns.map((column) => <th key={column}>{column}</th>)}</tr></thead><tbody>{entry.rows.map((row) => <tr key={row.key}>{entry.columns.map((column) => <td key={column}><ConsoleTokens tokens={row.cells[column] ?? []} /></td>)}</tr>)}</tbody></table>
        {(entry.omittedRows > 0 || entry.omittedColumns > 0) && <small>{locale === 'fa' ? `${entry.omittedRows} ردیف و ${entry.omittedColumns} ستون نمایش داده نشد` : `${entry.omittedRows} rows and ${entry.omittedColumns} columns omitted`}</small>}
      </div>
    )
  }
  return <div className={`console-record console-record--${entry.kind}`} style={style} data-level={entry.level}>{entry.kind === 'group' && <span className="console-group-mark">{entry.collapsed ? '▸' : '▾'}</span>}<ConsoleTokens tokens={entry.tokens} /></div>
}

function mirrorConsoleEntry(entry: ConsoleEntry): void {
  if (entry.kind === 'table') {
    console.table(entry.rows.map((row) => Object.fromEntries(entry.columns.map((column) => [column, consoleTokensText(row.cells[column] ?? [])]))))
    return
  }
  if (entry.kind === 'group') {
    console[entry.collapsed ? 'groupCollapsed' : 'group'](...consoleTokensToNativeArgs(entry.tokens))
    return
  }
  const method = entry.level === 'error' ? 'error' : entry.level === 'warn' ? 'warn' : entry.level === 'debug' ? 'debug' : entry.level === 'info' ? 'info' : 'log'
  console[method](...consoleTokensToNativeArgs(entry.tokens))
}

function consoleEntryText(entry: ConsoleEntry): string {
  if (entry.kind === 'table') return `console.table: ${entry.rows.length} rows × ${entry.columns.length} columns`
  return consoleTokensText(entry.tokens)
}

export function WorkbenchView(props: SharedViewProps) {
  const { events, locale, t, command, snapshot, transport, relayedTerminal } = props
  const isPersian = locale === 'fa'
  const copy = (english: string, persian: string) => isPersian ? persian : english
  const [line, setLine] = useState('status')
  const [transcript, setTranscript] = useState<TerminalRow[]>([])
  const [consoleHelpOpen, setConsoleHelpOpen] = useState(false)
  const [busy, setBusy] = useState('')
  const [displayTarget, setDisplayTarget] = useState<'segments' | 'lcd' | 'both'>('both')
  const [displayDuration, setDisplayDuration] = useState(5000)
  const [displayText, setDisplayText] = useState('READY')
  const [frequency, setFrequency] = useState(880)
  const [toneDuration, setToneDuration] = useState(120)
  const [melody, setMelody] = useState('welcome')
  const [red, setRed] = useState(38)
  const [green, setGreen] = useState(210)
  const [blue, setBlue] = useState(220)
  const [stripBrightness, setStripBrightness] = useState(180)
  const [automation, setAutomation] = useState('')
  const [hostBrightness, setHostBrightness] = useState(60)
  const [peripheralDestination, setPeripheralDestination] = useState<PeripheralDestinationID>(() => peripheralDestinationFromHash(location.hash) ?? 'overview')
  const [openPeripheralGroups, setOpenPeripheralGroups] = useState<ReadonlySet<string>>(() => {
    const initial = peripheralDestinationFromHash(location.hash) ?? 'overview'
    const group = peripheralDestinations.find((destination) => destination.id === initial)?.group
    return new Set(group ? [group] : [])
  })
  const latestStreamEventID = useRef(events.reduce((latest, event) => Math.max(latest, event.id), 0))
  const relayedTerminalIDs = useRef(new Set<string>())
  const consoleModel = useRef(new BrowserConsoleModel({ maxEntries: 240 }))
  const displayTextIsValid = displayText.length <= 32 && /^[\x20-\x7e]*$/.test(displayText)

  useEffect(() => {
    const syncPeripheralDestination = () => setPeripheralDestination(peripheralDestinationFromHash(location.hash) ?? 'overview')
    window.addEventListener('hashchange', syncPeripheralDestination)
    return () => window.removeEventListener('hashchange', syncPeripheralDestination)
  }, [])

  const selectPeripheralDestination = (destination: PeripheralDestinationID) => {
    const hash = canonicalPeripheralHash(destination)
    setPeripheralDestination(destination)
    if (location.hash !== hash) location.hash = hash
  }
  const selectedPeripheral = peripheralDestinationByID(peripheralDestination)
  const selectedPeripheralState = peripheralDestinationState(selectedPeripheral, snapshot)
  const peripheralTone = (state: ReturnType<typeof peripheralDestinationState>) => state === 'available' ? 'good' : state === 'disconnected' ? 'warn' : state === 'unsupported' ? 'bad' : 'neutral'

  useEffect(() => {
    setOpenPeripheralGroups((current) => {
      if (current.has(selectedPeripheral.group)) return current
      return new Set([...current, selectedPeripheral.group])
    })
  }, [selectedPeripheral.group])

  useEffect(() => {
    const incoming = events
      .filter((event) => event.id > latestStreamEventID.current)
      .sort((left, right) => left.id - right.id)
    if (!incoming.length) return
    latestStreamEventID.current = Math.max(latestStreamEventID.current, ...incoming.map((event) => event.id))
    const lines = incoming.flatMap((event) => [
      textRow(`[${formatClock(locale, event.time)}] event/${event.source || 'host'} ${event.kind}`, /error|fault|hot/i.test(event.kind) ? 'error' : /warn|door|disconnect/i.test(event.kind) ? 'warn' : 'info'),
      textRow(event.text),
    ])
    setTranscript((current) => [...current, ...lines].slice(-120))
  }, [events, locale])

  useEffect(() => {
    const incoming = relayedTerminal.filter((entry) => !relayedTerminalIDs.current.has(entry.id))
    if (!incoming.length) return
    for (const entry of incoming) relayedTerminalIDs.current.add(entry.id)
    const lines = incoming.map((entry) => {
      const peer = entry.tabId.slice(0, 7)
      const prefix = entry.kind === 'error' ? '!' : entry.kind === 'command' ? 'pc›' : '↳'
      return textRow(`[tab/${peer}] ${prefix} ${entry.text.replace(/^pc›\s*/, '')}`, entry.kind === 'error' ? 'error' : entry.kind === 'system' ? 'info' : 'log', `tab-${entry.id}`)
    })
    setTranscript((current) => [...current, ...lines].slice(-120))
  }, [relayedTerminal])

  const run = async (value: string) => {
    const normalized = value.trim()
    if (!normalized) return ''
    if (/^console\s*\./i.test(normalized)) {
      const invocation = parseConsoleInvocation(normalized)
      const result = consoleModel.current.dispatch(invocation ?? normalized)
      if (result.cleared) { console.clear(); setTranscript([]) }
      else if (result.emitted.length) {
        const rows = result.emitted.map((entry): ConsoleTerminalRow => ({ id: `console-${entry.id}`, kind: 'console', entry }))
        setTranscript((current) => [...current, ...rows].slice(-120))
        for (const entry of result.emitted) {
          mirrorConsoleEntry(entry)
          props.broadcastTerminal({ kind: entry.level === 'error' ? 'error' : 'system', text: consoleEntryText(entry), at: entry.timestamp })
        }
      }
      if (invocation?.method === 'groupEnd') console.groupEnd()
      return result.emitted.map(consoleEntryText).join('\n')
    }
    setBusy(normalized)
    setTranscript((current) => [...current.slice(-119), textRow(`pc› ${redactSensitiveCommand(normalized)}`)])
    try {
      const output = await command(normalized)
      setTranscript((current) => [...current.slice(-119), textRow(output || copy('✓ accepted', '✓ پذیرفته شد'), 'info')])
      return output
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      setTranscript((current) => [...current.slice(-119), textRow(`! ${detail}`, 'error')])
      return ''
    } finally {
      setBusy('')
    }
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (redactSensitiveCommand(line.trim()) !== line.trim()) setLine('')
    void run(line)
  }

  const inspectEvent = (event: (typeof events)[number]) => {
    setTranscript((current) => [
      ...current.slice(-118),
      textRow(`${copy('Event', 'رویداد')} #${event.id}`, 'info'),
      textRow(JSON.stringify(event, null, 2)),
    ])
  }

  return (
    <>
      <SectionTitle
        eyebrow={snapshot.connected ? copy('Controller and host tools', 'ابزارهای برد و میزبان') : copy('Host tools', 'ابزارهای میزبان')}
        title={t('workbench')}
        detail={`${transport.streamState.toUpperCase()} · ${events.length} ${copy('events', 'رویداد')} · ${transport.tabPeers + 1} ${copy('tabs', 'تب')}`}
        action={<StatusBadge tone={snapshot.connected ? 'good' : 'warn'}>{snapshot.connected ? copy('BOARD + HOST', 'برد + میزبان') : copy('HOST ONLY', 'فقط میزبان')}</StatusBadge>}
      />

      <section className="peripheral-workbench" aria-labelledby="peripheral-workbench-title">
        <header className="peripheral-workbench__header">
          <div><span>{copy('PERIPHERAL WORKBENCH', 'میزکار تجهیزات جانبی')}</span><h2 id="peripheral-workbench-title">{copy('Discoverable device destinations', 'مقصدهای قابل‌کشف دستگاه')}</h2><p>{copy('Known routes stay visible while the host reports whether they are ready, pending, unsupported, or disconnected.', 'مسیرهای شناخته‌شده همیشه دیده می‌شوند و میزبان وضعیت آماده، در انتظار، پشتیبانی‌نشده یا قطع را گزارش می‌کند.')}</p></div>
          <StatusBadge tone={peripheralTone(selectedPeripheralState)}>{peripheralStateLabel(selectedPeripheralState, locale)}</StatusBadge>
        </header>
        <nav className="peripheral-workbench__tree" aria-label={copy('Peripheral destinations', 'مقصدهای تجهیزات جانبی')}>
          {peripheralGroups.map((group) => {
            const destinations = peripheralDestinations.filter((destination) => destination.group === group.id)
            const groupOpen = openPeripheralGroups.has(group.id)
            return <details key={group.id} open={groupOpen} className="peripheral-workbench__group" onToggle={(event) => {
              const open = event.currentTarget.open
              setOpenPeripheralGroups((current) => {
                const next = new Set(current)
                if (open) next.add(group.id)
                else next.delete(group.id)
                return next
              })
            }}>
              <summary>{group.label[locale]}<small>{destinations.length}</small></summary>
              <div>{destinations.map((destination) => {
                const state = peripheralDestinationState(destination, snapshot)
                return <button key={destination.id} type="button" className={destination.id === peripheralDestination ? 'is-active' : ''} aria-current={destination.id === peripheralDestination ? 'page' : undefined} onClick={() => selectPeripheralDestination(destination.id)}>
                  <span><strong>{peripheralDestinationLabel(destination, locale)}</strong><small dir="ltr">{canonicalPeripheralHash(destination.id)}</small></span>
                  <StatusBadge tone={peripheralTone(state)}>{peripheralStateLabel(state, locale)}</StatusBadge>
                </button>
              })}</div>
            </details>
          })}
        </nav>
        <footer className={`peripheral-workbench__selection is-${selectedPeripheralState}`}>
          <div><strong>{peripheralDestinationLabel(selectedPeripheral, locale)}</strong><span>{peripheralDestinationDetail(selectedPeripheral, locale)}</span></div>
          {selectedPeripheralState === 'disconnected' && <Button compact icon={Cable} onClick={() => { setLine('reconnect'); window.requestAnimationFrame(() => document.getElementById('workbench-command')?.focus()) }}>{copy('Prepare reconnect', 'آماده‌سازی اتصال مجدد')}</Button>}
        </footer>
      </section>

      <section className="workbench-grid">
        <Card icon={SquareTerminal} iconTone="accent" className="workbench-terminal" title={copy('Bridge terminal', 'ترمینال پل')} eyebrow={copy('Full duplex', 'ارتباط دوطرفه')} action={<div className="terminal-transport"><StatusBadge tone={transport.streamState === 'open' ? 'good' : 'warn'}>WS {transport.streamState.toUpperCase()}</StatusBadge><StatusBadge tone={transport.tabBusSupported ? transport.tabPeers ? 'good' : 'info' : 'warn'}>TAB {transport.tabBusSupported ? transport.tabPeers + 1 : '—'}</StatusBadge><StatusBadge tone="info"><Activity size={13} /> {events.length}</StatusBadge></div>} menu={[
          { label: copy('Clear terminal', 'پاک‌کردن ترمینال'), icon: ListRestart, onSelect: () => { console.clear(); setTranscript([]) } },
          { label: consoleHelpOpen ? copy('Hide console syntax', 'بستن راهنما') : copy('Show console syntax', 'نمایش راهنمای کنسول'), icon: SquareTerminal, onSelect: () => setConsoleHelpOpen((open) => !open) },
          { label: copy('Open event timeline', 'بازکردن خط زمانی'), icon: Activity, onSelect: () => { window.location.hash = '#/events' } },
        ]}>
          <div className="terminal-transcript" role="log" aria-live="polite" dir="ltr">
            {transcript.map((row) => row.kind === 'console'
              ? <ConsoleRecord key={row.id} entry={row.entry} locale={locale} />
              : <pre key={row.id} className="terminal-line" data-level={row.level}>{row.text}</pre>)}
          </div>
          <form className="command-form" onSubmit={submit}>
            <span className="command-prompt">pc›</span>
            <input id="workbench-command" aria-label={copy('Primary bridge command', 'فرمان پل اصلی')} value={line} onChange={(event) => setLine(event.target.value)} spellCheck={false} dir="ltr" />
            <Button type="submit" tone="primary" icon={Send} busy={busy === line.trim()}>{t('run')}</Button>
          </form>
          <div className="terminal-console-help">
            <Button compact tone="ghost" onClick={() => setConsoleHelpOpen((open) => !open)} aria-expanded={consoleHelpOpen}>{consoleHelpOpen ? copy('Hide console syntax', 'بستن راهنما') : copy('Console syntax', 'راهنمای کنسول')}</Button>
            {consoleHelpOpen && <code dir="ltr">console.*() · %s %d %i %f %o %O %c %% · table · groups · counters · timers</code>}
          </div>
          <div className="terminal-event-strip">
            {events.slice(0, 5).map((event) => <button key={event.id} onClick={() => inspectEvent(event)}><time>{formatClock(locale, event.time)}</time><strong>{event.kind}</strong><span>{event.text}</span></button>)}
          </div>
        </Card>

        {snapshot.connected && <Card icon={Binary} iconTone="violet" title={copy('Displays', 'نمایشگرها')} eyebrow="TM1637 + LCD">
          <div className="setting-group"><label>{copy('Target', 'مقصد')}</label><Segmented value={displayTarget} label={copy('Display target', 'مقصد نمایش')} options={[{ value: 'segments', label: copy('Segments', 'سون‌سگمنت') }, { value: 'lcd', label: 'LCD' }, { value: 'both', label: copy('Both', 'هر دو') }]} onChange={setDisplayTarget} /></div>
          <TextField
            label={copy('Bounded display text', 'متن نمایشگر، حداکثر ۳۲ نویسه')}
            hint={displayTextIsValid
              ? copy('Printable ASCII only · 32 characters maximum.', 'فقط نویسه‌های قابل چاپ ASCII؛ حداکثر ۳۲ نویسه.')
              : copy('Use printable ASCII characters only.', 'فقط از نویسه‌های قابل چاپ ASCII استفاده کنید.')}
            value={displayText}
            maxLength={32}
            pattern="[ -~]*"
            aria-invalid={!displayTextIsValid}
            onChange={(event) => setDisplayText(event.target.value)}
          />
          <RangeField label={copy('Override duration', 'مدت نمایش')} value={displayDuration} min={250} max={60000} step={250} unit="ms" onChange={setDisplayDuration} />
          <div className="inline-actions"><Button tone="primary" icon={Lightbulb} disabled={!displayText.trim() || !displayTextIsValid} onClick={() => void run(`display ${displayTarget} ${displayDuration} ${shellArgument(displayText.trim())}`)}>{copy('Show text', 'نمایش متن')}</Button><Button icon={Eraser} onClick={() => void run(`display ${displayTarget} 0`)}>{copy('Clear', 'پاک‌کردن')}</Button></div>
        </Card>}

        {snapshot.connected && <Card icon={Lightbulb} iconTone="amber" title={copy('Addressable strip', 'نوار LED آدرس‌پذیر')} eyebrow={copy('11 pixels · status light', '۱۱ پیکسل · نور وضعیت')}>
          <RangeField label={copy('Red', 'قرمز')} value={red} min={0} max={255} onChange={setRed} />
          <RangeField label={copy('Green', 'سبز')} value={green} min={0} max={255} onChange={setGreen} />
          <RangeField label={copy('Blue', 'آبی')} value={blue} min={0} max={255} onChange={setBlue} />
          <RangeField label={copy('Brightness', 'روشنایی')} value={stripBrightness} min={0} max={255} onChange={setStripBrightness} />
          <div className="inline-actions"><Button tone="primary" icon={Palette} onClick={() => void run(`strip fill ${red} ${green} ${blue} ${stripBrightness}`)}>{copy('Fill strip', 'اعمال رنگ')}</Button><Button icon={Eraser} onClick={() => void run('strip clear')}>{copy('Clear', 'پاک‌کردن')}</Button></div>
        </Card>}

        {snapshot.connected && <Card icon={AudioLines} iconTone="green" title={copy('Buzzer & melody', 'بیزر و ملودی')} eyebrow={copy('Timed audio', 'صدای زمان‌بندی‌شده')}>
          <RangeField label={copy('Frequency', 'فرکانس')} value={frequency} min={20} max={20000} step={10} unit="Hz" onChange={setFrequency} />
          <RangeField label={copy('Duration', 'مدت')} value={toneDuration} min={20} max={5000} step={20} unit="ms" onChange={setToneDuration} />
		  <Button icon={Volume2} onClick={() => void run(`beep ${frequency} ${toneDuration}`)}>{copy('Play tone', 'پخش صدا')}</Button>
          <TextField label={copy('Configured melody', 'ملودی ذخیره‌شده')} value={melody} spellCheck={false} onChange={(event) => setMelody(event.target.value)} />
          <div className="inline-actions"><Button icon={Play} disabled={!melody.trim()} onClick={() => void run(`melody play ${shellArgument(melody.trim())}`)}>{copy('Play', 'پخش')}</Button><Button icon={StopCircle} onClick={() => void run('melody stop')}>{copy('Stop', 'توقف')}</Button><Button icon={List} onClick={() => void run('melody list')}>{copy('List', 'فهرست')}</Button></div>
        </Card>}

        <RFGuidedWorkflow snapshot={snapshot} events={events} locale={locale} openDialog={props.openDialog} />

        <Card className="macro-card" icon={Workflow} iconTone="green" title={copy('Macro library', 'کتابخانه ماکرو')} eyebrow={copy('Exact MCU timing · live shared state', 'زمان‌بندی دقیق MCU · وضعیت زنده مشترک')}>
          <MacroLibraryPanel online={snapshot.connected} locale={locale} events={props.macroEvents} legacyCommand={run} />
        </Card>

        <Card icon={Bot} iconTone="violet" title={copy('Host automations', 'خودکارسازی میزبان')} eyebrow={copy('Event-driven host rules', 'قواعد رویدادمحور میزبان')}>
          <TextField label={copy('Host automation name', 'نام خودکارسازی میزبان')} value={automation} onChange={(event) => setAutomation(event.target.value)} />
          <div className="inline-actions"><Button icon={Bot} disabled={!automation.trim()} onClick={() => void run(`automation run ${shellArgument(automation.trim())}`)}>{copy('Run automation', 'اجرای خودکارسازی')}</Button><Button icon={List} onClick={() => void run('automation list')}>{copy('List', 'فهرست')}</Button></div>
        </Card>

        {snapshot.connected && <Card icon={Cable} iconTone="accent" title={copy('I²C & peripherals', 'I²C و تجهیزات جانبی')} eyebrow={copy('Cooperative host lease', 'دسترسی هماهنگ میزبان')}>
          <div className="operation-buttons"><Button icon={ScanSearch} onClick={() => void run('i2c scan')}>{copy('Scan bus', 'پویش گذرگاه')}</Button><Button icon={Unplug} onClick={() => void run('i2c release')}>{copy('Stop bus access', 'پایان دسترسی به گذرگاه')}</Button><Button icon={Settings2} onClick={() => void run('settings')}>{copy('Board settings', 'تنظیمات برد')}</Button><Button icon={LayoutDashboard} onClick={() => void run('menu current')}>{copy('Menu state', 'وضعیت منو')}</Button></div>
        </Card>}

        <Card icon={MonitorCog} iconTone="violet" title={copy('Host control', 'کنترل میزبان')} eyebrow={copy('Policy-gated', 'تحت کنترل سیاست‌ها')} menu={[
          { label: copy('Read host status', 'خواندن وضعیت میزبان'), icon: MonitorCog, onSelect: () => { void run('os status') } },
          { label: copy('Inspect global hotkeys', 'بررسی میان‌برهای سراسری'), icon: Workflow, onSelect: () => { void run('hotkeys status') } },
          { label: copy('Inspect keyboard hook', 'بررسی اتصال صفحه‌کلید'), icon: Cpu, onSelect: () => { void run('keyboard status') } },
        ]}>
          <RangeField label={copy('Monitor brightness', 'روشنایی نمایشگر')} value={hostBrightness} min={0} max={100} unit="%" onChange={setHostBrightness} />
          <div className="inline-actions"><Button tone="primary" icon={SunMedium} onClick={() => void run(`os brightness set ${hostBrightness}`)}>{copy('Apply brightness', 'اعمال روشنایی')}</Button><Button icon={Gauge} onClick={() => void run('os status')}>{copy('Host status', 'وضعیت میزبان')}</Button><Button icon={ShieldCheck} onClick={() => void run('os policy')}>{copy('Policies', 'سیاست‌ها')}</Button></div>
          <div className="operation-buttons"><Button icon={Keyboard} onClick={() => void run('hotkeys status')}>{copy('Global hotkeys', 'میان‌برهای سراسری')}</Button><Button icon={MonitorCog} onClick={() => void run('keyboard status')}>{copy('Keyboard hook', 'اتصال صفحه‌کلید')}</Button><Button icon={Network} onClick={() => void run('bridge list')}>{copy('Remote bridges', 'پل‌های راه‌دور')}</Button></div>
        </Card>

        <Card icon={Cpu} iconTone="amber" title={copy('Firmware & recovery', 'میان‌افزار و بازیابی')} eyebrow={copy('Read-only first', 'ابتدا فقط خواندنی')}>
          <div className="operation-buttons">{snapshot.connected && <><Button icon={Cpu} onClick={() => void run('hello')}>{copy('Identity', 'شناسه')}</Button><Button icon={ListRestart} onClick={() => void run('reset lines')}>{copy('Reconnect pulse', 'پالس اتصال مجدد')}</Button></>}<Button icon={MemoryStick} onClick={() => void run('toolchain profile')}>{copy('Toolchain profile', 'مشخصات زنجیره‌ابزار')}</Button><Button icon={SquareTerminal} onClick={() => setLine('boot info')}>{copy('Prepare boot info', 'آماده‌سازی اطلاعات راه‌اندازی')}</Button></div>
        </Card>
      </section>
      <AdvancedWorkbench {...props} run={run} busy={busy} />
    </>
  )
}

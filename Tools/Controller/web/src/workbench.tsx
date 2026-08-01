import { type FormEvent, useEffect, useRef, useState } from 'react'
import {
  Activity,
  Antenna,
  AudioLines,
  Binary,
  Bot,
  Cable,
  Cpu,
  Lightbulb,
  ListRestart,
  MemoryStick,
  MonitorCog,
  Play,
  Radio,
  ScanSearch,
  Send,
  SquareTerminal,
  StopCircle,
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
import { AdvancedWorkbench } from './advanced-workbench'
import type { SharedViewProps } from './views'

export function WorkbenchView(props: SharedViewProps) {
  const { appTitle, events, locale, t, command, snapshot } = props
  const isPersian = locale === 'fa'
  const copy = (english: string, persian: string) => isPersian ? persian : english
  const [line, setLine] = useState('status')
  const [transcript, setTranscript] = useState<string[]>([
    copy(`${appTitle} primary dispatcher ready.`, `هسته فرمان اصلی ${appTitle} آماده است.`),
    copy(
      'Commands and important events share one authenticated host runtime.',
      'فرمان‌ها و رویدادهای مهم در یک میزبان امن و یکپارچه جریان دارند.',
    ),
  ])
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
  const [rfSeconds, setRFSeconds] = useState(15)
  const [rfCode, setRFCode] = useState('0x123456')
  const [rfBits, setRFBits] = useState(24)
  const [rfProtocol, setRFProtocol] = useState(1)
  const [rfPulse, setRFPulse] = useState(350)
  const [rfRepeats, setRFRepeats] = useState(5)
  const [macro, setMacro] = useState('')
  const [automation, setAutomation] = useState('')
  const [hostBrightness, setHostBrightness] = useState(60)
  const latestStreamEventID = useRef(events.reduce((latest, event) => Math.max(latest, event.id), 0))
  const displayTextIsValid = displayText.length <= 32 && /^[\x20-\x7e]*$/.test(displayText)

  useEffect(() => {
    setTranscript((current) => [
      copy(`${appTitle} primary dispatcher ready.`, `هسته فرمان اصلی ${appTitle} آماده است.`),
      copy(
        'Commands and important events share one authenticated host runtime.',
        'فرمان‌ها و رویدادهای مهم در یک میزبان امن و یکپارچه جریان دارند.',
      ),
      ...current.slice(2),
    ])
  }, [appTitle, locale])

  useEffect(() => {
    const incoming = events
      .filter((event) => event.id > latestStreamEventID.current)
      .sort((left, right) => left.id - right.id)
    if (!incoming.length) return
    latestStreamEventID.current = Math.max(latestStreamEventID.current, ...incoming.map((event) => event.id))
    const lines = incoming.flatMap((event) => [
      `[${formatClock(locale, event.time)}] event/${event.source || 'host'} ${event.kind}`,
      event.text,
    ])
    setTranscript((current) => [...current, ...lines].slice(-64))
  }, [events, locale])

  const run = async (value: string) => {
    const normalized = value.trim()
    if (!normalized) return ''
    setBusy(normalized)
    setTranscript((current) => [...current.slice(-60), `pc› ${redactSensitiveCommand(normalized)}`])
    try {
      const output = await command(normalized)
      setTranscript((current) => [...current.slice(-60), output || copy('✓ accepted', '✓ پذیرفته شد')])
      return output
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      setTranscript((current) => [...current.slice(-60), `! ${detail}`])
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
      ...current.slice(-60),
      `${copy('Event', 'رویداد')} #${event.id}`,
      JSON.stringify(event, null, 2),
    ])
  }

  return (
    <>
      <SectionTitle
        eyebrow={copy('EVERY CAPABILITY / ONE DISPATCHER', 'همه قابلیت‌ها / یک هسته فرمان')}
        title={t('workbench')}
        detail={copy(
          'Purpose-built controls for displays, sound, RF, macros, host actions and diagnostics—with the full terminal always available.',
          'کنترل‌های تخصصی برای نمایشگرها، صدا، رادیو، ماکروها، میزبان و عیب‌یابی؛ با ترمینل کامل و همیشه در دسترس.',
        )}
        action={<StatusBadge tone={snapshot.connected ? 'good' : 'warn'}>{snapshot.connected ? copy('BOARD + HOST', 'برد + میزبان') : copy('HOST ONLY', 'فقط میزبان')}</StatusBadge>}
      />

      <section className="workbench-grid">
        <Card className="workbench-terminal" title={copy('Bridge terminal', 'ترمینال پل')} eyebrow={copy('FULL DUPLEX', 'ارتباط دوطرفه')} action={<StatusBadge tone="info"><Activity size={13} /> {events.length} {copy('events', 'رویداد')}</StatusBadge>}>
          <div className="terminal-transcript" role="log" aria-live="polite" dir="ltr">
            {transcript.map((entry, index) => <pre key={`${index}-${entry}`}>{entry}</pre>)}
          </div>
          <form className="command-form" onSubmit={submit}>
            <span className="command-prompt">pc›</span>
            <input aria-label={copy('Primary bridge command', 'فرمان پل اصلی')} value={line} onChange={(event) => setLine(event.target.value)} spellCheck={false} dir="ltr" />
            <Button type="submit" tone="primary" icon={Send} busy={busy === line.trim()}>{t('run')}</Button>
          </form>
          <div className="terminal-event-strip">
            {events.slice(0, 5).map((event) => <button key={event.id} onClick={() => inspectEvent(event)}><time>{formatClock(locale, event.time)}</time><strong>{event.kind}</strong><span>{event.text}</span></button>)}
          </div>
        </Card>

        <Card title={copy('Displays', 'نمایشگرها')} eyebrow="TM1637 + LCD" action={<Binary size={18} />}>
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
          <div className="inline-actions"><Button tone="primary" icon={Lightbulb} disabled={!snapshot.connected || !displayText.trim() || !displayTextIsValid} onClick={() => void run(`display ${displayTarget} ${displayDuration} ${shellArgument(displayText.trim())}`)}>{copy('Show text', 'نمایش متن')}</Button><Button disabled={!snapshot.connected} onClick={() => void run(`display ${displayTarget} 0`)}>{copy('Clear', 'پاک‌کردن')}</Button></div>
        </Card>

        <Card title={copy('Addressable strip', 'نوار LED آدرس‌پذیر')} eyebrow={copy('11 PIXELS / SEMANTIC LIGHT', '۱۱ پیکسل / نور وضعیت هوشمند')} action={<Lightbulb size={18} />}>
          <RangeField label={copy('Red', 'قرمز')} value={red} min={0} max={255} onChange={setRed} />
          <RangeField label={copy('Green', 'سبز')} value={green} min={0} max={255} onChange={setGreen} />
          <RangeField label={copy('Blue', 'آبی')} value={blue} min={0} max={255} onChange={setBlue} />
          <RangeField label={copy('Brightness', 'روشنایی')} value={stripBrightness} min={0} max={255} onChange={setStripBrightness} />
          <div className="inline-actions"><Button tone="primary" disabled={!snapshot.connected} onClick={() => void run(`strip fill ${red} ${green} ${blue} ${stripBrightness}`)}>{copy('Fill strip', 'اعمال رنگ')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('strip clear')}>{copy('Clear', 'پاک‌کردن')}</Button></div>
        </Card>

        <Card title={copy('Buzzer & melody', 'بیزر و ملودی')} eyebrow={copy('TIMER-SAFE AUDIO', 'صدای هماهنگ با تایمر')} action={<AudioLines size={18} />}>
          <RangeField label={copy('Frequency', 'فرکانس')} value={frequency} min={20} max={20000} step={10} unit="Hz" onChange={setFrequency} />
          <RangeField label={copy('Duration', 'مدت')} value={toneDuration} min={20} max={5000} step={20} unit="ms" onChange={setToneDuration} />
          <Button disabled={!snapshot.connected} onClick={() => void run(`buzzer ${frequency} ${toneDuration}`)}>{copy('Play tone', 'پخش صدا')}</Button>
          <TextField label={copy('Configured melody', 'ملودی ذخیره‌شده')} value={melody} spellCheck={false} onChange={(event) => setMelody(event.target.value)} />
          <div className="inline-actions"><Button icon={Play} disabled={!snapshot.connected || !melody.trim()} onClick={() => void run(`melody play ${shellArgument(melody.trim())}`)}>{copy('Play', 'پخش')}</Button><Button icon={StopCircle} disabled={!snapshot.connected} onClick={() => void run('melody stop')}>{copy('Stop', 'توقف')}</Button><Button onClick={() => void run('melody list')}>{copy('List', 'فهرست')}</Button></div>
        </Card>

        <Card title={copy('433 MHz radio', 'رادیوی ۴۳۳ مگاهرتز')} eyebrow={copy('LEARN / INSPECT / TRANSMIT', 'یادگیری / بررسی / ارسال')} action={<Radio size={18} />}>
          <RangeField label={copy('Learn window', 'بازه یادگیری')} value={rfSeconds} min={1} max={120} unit="s" onChange={setRFSeconds} />
          <div className="inline-actions"><Button icon={ScanSearch} disabled={!snapshot.connected} onClick={() => void run(`rf learn ${rfSeconds} single`)}>{copy('Learn', 'یادگیری')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('rf cancel')}>{copy('Cancel', 'لغو')}</Button><Button onClick={() => void run('rf list')}>{copy('List', 'فهرست')}</Button></div>
          <div className="compact-fields"><TextField label={copy('Code', 'کد')} dir="ltr" value={rfCode} onChange={(event) => setRFCode(event.target.value)} /><TextField label={copy('Bits', 'بیت‌ها')} type="number" min={1} max={32} value={rfBits} onChange={(event) => setRFBits(Number(event.target.value))} /><TextField label={copy('Protocol', 'پروتکل')} type="number" min={1} max={12} value={rfProtocol} onChange={(event) => setRFProtocol(Number(event.target.value))} /><TextField label={copy('Pulse µs', 'پالس µs')} type="number" min={0} max={65535} value={rfPulse} onChange={(event) => setRFPulse(Number(event.target.value))} /><TextField label={copy('Repeats', 'تکرار')} type="number" min={1} max={20} value={rfRepeats} onChange={(event) => setRFRepeats(Number(event.target.value))} /></div>
          <Button tone="primary" icon={Antenna} disabled={!snapshot.connected || !rfCode.trim()} onClick={() => void run(`rf send ${rfCode.trim()} ${rfBits} ${rfProtocol} ${rfPulse} ${rfRepeats}`)}>{copy('Transmit explicitly', 'ارسال با تأیید کاربر')}</Button>
        </Card>

        <Card title={copy('Macros & automations', 'ماکروها و خودکارسازی')} eyebrow={copy('BOARD TIMING + HOST RULES', 'زمان‌بندی برد + قواعد میزبان')} action={<Workflow size={18} />}>
          <TextField label={copy('Macro name or ID', 'نام یا شناسه ماکرو')} value={macro} onChange={(event) => setMacro(event.target.value)} />
          <div className="inline-actions"><Button icon={Play} disabled={!snapshot.connected || !macro.trim()} onClick={() => void run(`macro play ${shellArgument(macro.trim())}`)}>{copy('Run macro', 'اجرای ماکرو')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('macro cancel')}>{copy('Cancel', 'لغو')}</Button><Button onClick={() => void run('macro list')}>{copy('List', 'فهرست')}</Button></div>
          <TextField label={copy('Host automation name', 'نام خودکارسازی میزبان')} value={automation} onChange={(event) => setAutomation(event.target.value)} />
          <div className="inline-actions"><Button icon={Bot} disabled={!automation.trim()} onClick={() => void run(`automation run ${shellArgument(automation.trim())}`)}>{copy('Run automation', 'اجرای خودکارسازی')}</Button><Button onClick={() => void run('automation list')}>{copy('List', 'فهرست')}</Button></div>
        </Card>

        <Card title={copy('I²C & peripherals', 'I²C و تجهیزات جانبی')} eyebrow={copy('COOPERATIVE HOST LEASE', 'دسترسی هماهنگ میزبان')} action={<Cable size={18} />}>
          <p className="card-copy">{copy('Scan without taking permanent ownership; release the lease explicitly after raw peripheral work.', 'گذرگاه را بدون تصاحب دائمی پویش کنید و پس از کار مستقیم با تجهیزات، دسترسی را صریحاً آزاد کنید.')}</p>
          <div className="operation-buttons"><Button icon={ScanSearch} disabled={!snapshot.connected} onClick={() => void run('i2c scan')}>{copy('Scan bus', 'پویش گذرگاه')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('i2c release')}>{copy('Release lease', 'آزادسازی دسترسی')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('settings')}>{copy('Board settings', 'تنظیمات برد')}</Button><Button disabled={!snapshot.connected} onClick={() => void run('menu current')}>{copy('Menu state', 'وضعیت منو')}</Button></div>
        </Card>

        <Card title={copy('Host control', 'کنترل میزبان')} eyebrow={copy('POLICY-GATED', 'تحت کنترل سیاست‌ها')} action={<MonitorCog size={18} />}>
          <RangeField label={copy('Monitor brightness', 'روشنایی نمایشگر')} value={hostBrightness} min={0} max={100} unit="%" onChange={setHostBrightness} />
          <div className="inline-actions"><Button tone="primary" onClick={() => void run(`os brightness set ${hostBrightness}`)}>{copy('Apply brightness', 'اعمال روشنایی')}</Button><Button onClick={() => void run('os status')}>{copy('Host status', 'وضعیت میزبان')}</Button><Button onClick={() => void run('os policy')}>{copy('Policies', 'سیاست‌ها')}</Button></div>
          <div className="operation-buttons"><Button onClick={() => void run('hotkeys status')}>{copy('Global hotkeys', 'میان‌برهای سراسری')}</Button><Button onClick={() => void run('keyboard status')}>{copy('Keyboard hook', 'اتصال صفحه‌کلید')}</Button><Button onClick={() => void run('bridge list')}>{copy('Remote bridges', 'پل‌های راه‌دور')}</Button></div>
        </Card>

        <Card title={copy('Firmware & recovery', 'میان‌افزار و بازیابی')} eyebrow={copy('READ-ONLY FIRST', 'ابتدا فقط خواندنی')} action={<Cpu size={18} />}>
          <p className="card-copy">{copy('This surface exposes identity and diagnostic reads directly. Any flash write still crosses the backup-first programmer guard and explicit confirmation path.', 'این بخش شناسه و خوانش‌های عیب‌یابی را مستقیماً ارائه می‌کند. هر نوشتن روی میان‌افزار همچنان فقط پس از پشتیبان‌گیری و تأیید صریح از مسیر امن پروگرامر عبور می‌کند.')}</p>
          <div className="operation-buttons"><Button icon={Cpu} disabled={!snapshot.connected} onClick={() => void run('hello')}>{copy('Identity', 'شناسه')}</Button><Button icon={ListRestart} disabled={!snapshot.connected} onClick={() => void run('reset lines')}>{copy('Reconnect pulse', 'پالس اتصال مجدد')}</Button><Button icon={MemoryStick} onClick={() => void run('toolchain profile')}>{copy('Toolchain profile', 'مشخصات زنجیره‌ابزار')}</Button><Button icon={SquareTerminal} onClick={() => setLine('boot info')}>{copy('Prepare boot info', 'آماده‌سازی اطلاعات راه‌اندازی')}</Button></div>
        </Card>
      </section>
      <AdvancedWorkbench {...props} run={run} busy={busy} />
    </>
  )
}

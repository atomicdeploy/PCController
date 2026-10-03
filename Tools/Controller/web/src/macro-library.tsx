import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  CircleDot,
  CircleStop,
  Database,
  ListChecks,
  Play,
  Plus,
  RadioTower,
  Save,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { rpc } from './api'
import { Button, Segmented, StatusBadge, TextField } from './components'
import { shellArgument } from './command-line'
import {
  applyMacroEventToSnapshot,
  macroEventNeedsSnapshot,
} from './macro-live'
import type { ControllerEvent, ControllerMacro, Locale, MacroSnapshot, StripEffectDescriptor } from './types'

type MacroColor = 'red' | 'blue' | 'violet' | 'green' | 'white'

interface MacroLibraryPanelProps {
  online: boolean
  locale: Locale
  events: ControllerEvent[]
  initialSnapshot?: MacroSnapshot
  stripEffects?: StripEffectDescriptor[]
  commandSurface: (command: string) => Promise<string>
}

interface MacroCatalogProps {
  macros: ControllerMacro[]
  selectedReference: string
  locale: Locale
  onSelect: (macro: ControllerMacro) => void
}

function normalizedColor(value?: string): MacroColor {
  switch (value?.trim().toLowerCase()) {
    case 'blue': return 'blue'
    case 'violet': return 'violet'
    case 'green': return 'green'
    case 'white': return 'white'
    default: return 'red'
  }
}

function macroDurationUS(macro: ControllerMacro): number {
  return (macro.steps ?? []).reduce((maximum, step) => Math.max(maximum, step.at_us ?? 0), 0)
}

function formatMicroseconds(value: number | undefined, locale: Locale): string {
  value ??= 0
  const amount = Math.abs(value)
  if (amount >= 1_000_000) return `${(value / 1_000_000).toLocaleString(locale, { maximumFractionDigits: 3 })} s`
  if (amount >= 1_000) return `${(value / 1_000).toLocaleString(locale, { maximumFractionDigits: 3 })} ms`
  return `${value.toLocaleString(locale)} µs`
}

/** Stateless typed catalog, exported so the wire-to-DOM contract remains testable. */
export function MacroCatalog({ macros, selectedReference, locale, onSelect }: MacroCatalogProps) {
  const persian = locale === 'fa'
  if (!macros.length) {
    return <div className="macro-library__empty">{persian ? 'هنوز جلوهٔ ضبط‌شده‌ای وجود ندارد.' : 'No recorded effects yet.'}</div>
  }
  return (
    <div className="macro-library__catalog" role="listbox" aria-label={persian ? 'کتابخانه جلوه‌ها' : 'Effect library'}>
      {macros.map((macro) => {
        const selected = selectedReference === String(macro.id)
        return (
          <button
            key={macro.id}
            type="button"
            role="option"
            aria-selected={selected}
            className={selected ? 'is-selected' : ''}
            onClick={() => onSelect(macro)}
          >
            <span className={`macro-library__color is-${normalizedColor(macro.color)}`} aria-hidden="true" />
            <span className="macro-library__identity">
              <strong>{macro.name}</strong>
              <small>#{macro.id} · {macro.category || (persian ? 'بدون دسته' : 'Uncategorized')} · {macro.mode || 'auto'}</small>
            </span>
            <span className="macro-library__measure">
              <strong>{(macro.steps?.length ?? 0).toLocaleString(locale)}</strong>
              <small>{persian ? 'گام' : 'steps'}</small>
            </span>
            <span className="macro-library__measure">
              <strong>{formatMicroseconds(macroDurationUS(macro), locale)}</strong>
              <small>{persian ? 'مدت' : 'duration'}</small>
            </span>
          </button>
        )
      })}
    </div>
  )
}

export function MacroLibraryPanel({ online, locale, events, initialSnapshot, stripEffects = [], commandSurface }: MacroLibraryPanelProps) {
  const persian = locale === 'fa'
  const copy = (english: string, farsi: string) => persian ? farsi : english
  const [snapshot, setSnapshot] = useState<MacroSnapshot | null>(initialSnapshot ?? null)
  const [snapshotState, setSnapshotState] = useState<'loading' | 'ready' | 'error'>(initialSnapshot ? 'ready' : 'loading')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [selectedReference, setSelectedReference] = useState('')
  const [draftID, setDraftID] = useState(0)
  const [name, setName] = useState('')
  const [category, setCategory] = useState('Web')
  const [color, setColor] = useState<MacroColor>('green')
  const [playMode, setPlayMode] = useState<'auto' | 'host' | 'mcu'>('auto')
  const [selectedStripID, setSelectedStripID] = useState('')
  const [stripDraft, setStripDraft] = useState<StripEffectDescriptor | null>(null)
  const latestAppliedEventID = useRef(initialSnapshot?.latest_event_id ?? 0)

  const loadSnapshot = useCallback(async (quiet = false) => {
    if (!quiet) setBusy('controller.macro.snapshot')
    try {
      const response = await rpc<{ macros: MacroSnapshot }>('controller.snapshot')
      const value = response.macros
      setSnapshot(value)
      setSnapshotState('ready')
      setError('')
      latestAppliedEventID.current = Math.max(latestAppliedEventID.current, value.latest_event_id || 0)
      return value
    } catch (cause) {
      setSnapshotState('error')
      setError(cause instanceof Error ? cause.message : String(cause))
      return null
    } finally {
      if (!quiet) setBusy('')
    }
  }, [locale])

  useEffect(() => { void loadSnapshot() }, [loadSnapshot])

  useEffect(() => {
    const incoming = events
      .filter((event) => event.id > latestAppliedEventID.current)
      .sort((left, right) => left.id - right.id)
    if (!incoming.length) return
    latestAppliedEventID.current = incoming[incoming.length - 1].id
    setSnapshot((current) => incoming.reduce(
      (value, event) => value ? applyMacroEventToSnapshot(value, event) : value,
      current,
    ))
    if (!incoming.some(macroEventNeedsSnapshot)) return
    const timer = window.setTimeout(() => { void loadSnapshot(true) }, 60)
    return () => window.clearTimeout(timer)
  }, [events, loadSnapshot])

  const selected = useMemo(
    () => snapshot?.library.find((macro) => String(macro.id) === selectedReference),
    [selectedReference, snapshot?.library],
  )
  const selectedStrip = useMemo(
    () => stripEffects.find((effect) => effect.id === selectedStripID),
    [selectedStripID, stripEffects],
  )

  useEffect(() => {
    if (!snapshot) return
    if (!selectedReference && snapshot.library.length) setSelectedReference(String(snapshot.library[0].id))
    const used = new Set(snapshot.library.map((macro) => macro.id))
    let available = 0
    while (available < 255 && used.has(available)) available += 1
    setDraftID((current) => used.has(current) ? available : current)
  }, [selectedReference, snapshot])

  useEffect(() => {
    if (!selected) return
    setName(selected.name)
    setCategory(selected.category || '')
    setColor(normalizedColor(selected.color))
  }, [selected?.category, selected?.color, selected?.id, selected?.name])

  useEffect(() => {
    if (!selectedStripID && stripEffects.length) setSelectedStripID(stripEffects[0].id)
  }, [selectedStripID, stripEffects])

  useEffect(() => {
    if (selectedStrip) setStripDraft({ ...selectedStrip })
  }, [selectedStrip])

  const selectMacro = (macro: ControllerMacro) => setSelectedReference(String(macro.id))

  const perform = async (method: string, _params: unknown, command: string) => {
    setBusy(method)
    setError('')
    try {
      await commandSurface(command)
      await loadSnapshot(true)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const reference = selectedReference || name.trim()
  const recording = snapshot?.recording
  const playback = snapshot?.playback
  const latestEvents = events.slice(0, 7)

  return (
    <div className="macro-library">
      <div className="macro-library__monitor" aria-live="polite">
        <div className={recording?.active ? 'is-active' : ''}>
          <CircleDot aria-hidden="true" size={17} />
          <span>
            <small>{copy('RECORDING', 'ضبط')}</small>
            <strong>{recording?.active ? recording.name || `#${recording.id}` : copy('Idle', 'آماده')}</strong>
            <em>{recording?.active
              ? `${recording.device_retained ? copy('Device-retained take', 'برداشت نگه‌داری‌شده در دستگاه') : copy('Live capture', 'ضبط زنده')} · ${recording.steps} ${copy('steps', 'گام')} · Δ ${formatMicroseconds(recording.last_delta_us, locale)}`
              : recording?.name || ''}</em>
          </span>
        </div>
        <div className={playback?.running ? 'is-active' : ''}>
          <Play aria-hidden="true" size={17} />
          <span>
            <small>{copy('PLAYBACK', 'اجرا')}</small>
            <strong>{playback?.name || copy('Idle', 'آماده')}</strong>
            <em>{playback?.name
              ? `${playback.lifecycle || 'idle'} · ${playback.policy || 'auto'} → ${playback.mode || 'pending'} · ${playback.step}/${playback.step_count} · Δ ${formatMicroseconds(playback.last_timing_delta_us, locale)}`
              : copy('No playback in this session', 'هنوز اجرایی در این نشست انجام نشده')}</em>
          </span>
        </div>
        <StatusBadge tone={snapshotState === 'error' ? 'warn' : snapshotState === 'ready' ? 'good' : 'info'}>
          {snapshotState === 'error' ? copy('UNAVAILABLE', 'در دسترس نیست') : snapshotState === 'ready' ? copy('LIVE', 'زنده') : copy('LOADING', 'در حال بارگذاری')}
        </StatusBadge>
      </div>

      {error && <div className="macro-library__error" role="alert">{error}</div>}
      {recording?.last_error && <div className="macro-library__error" role="alert">{recording.last_error}</div>}
      {!!recording?.overwritten && <div className="macro-library__error" role="status">{copy(`Ring wrapped: ${recording.overwritten} earlier snapshots overwritten. Saved profile contains the retained tail.`, `حافظه حلقوی پر شد: ${recording.overwritten} وضعیت پیشین جایگزین شد. پروفایل شامل بخش پایانی باقی‌مانده است.`)}</div>}
      <div className="macro-library__workspace">
        <section>
          <header><span><Database size={15} /> {copy('Library', 'کتابخانه')}</span><small>{snapshot?.library.length ?? 0}</small></header>
          <MacroCatalog macros={snapshot?.library ?? []} selectedReference={selectedReference} locale={locale} onSelect={selectMacro} />
        </section>
        <section className="macro-library__editor">
          <header><span><ListChecks size={15} /> {selected ? copy('Selected timed effect', 'جلوهٔ زمان‌بندی‌شدهٔ انتخابی') : copy('New timed effect', 'جلوهٔ زمان‌بندی‌شدهٔ جدید')}</span></header>
          <div className="macro-library__fields">
            <TextField label={copy('ID 0..255', 'شناسه ۰ تا ۲۵۵')} type="number" min={0} max={255} value={selected ? selected.id : draftID} disabled={Boolean(selected)} onChange={(event) => setDraftID(Math.max(0, Math.min(255, Number(event.target.value) || 0)))} />
            <TextField label={copy('Name', 'نام')} value={name} maxLength={64} onChange={(event) => setName(event.target.value)} />
            <TextField label={copy('Category', 'دسته‌بندی')} value={category} maxLength={64} onChange={(event) => setCategory(event.target.value)} />
          </div>
          <div className="macro-library__color-picker">
            <label>{copy('Color', 'رنگ')}</label>
            <Segmented value={color} label={copy('Effect color', 'رنگ جلوه')} options={[
              { value: 'red', label: copy('Red', 'قرمز') },
              { value: 'blue', label: copy('Blue', 'آبی') },
              { value: 'violet', label: copy('Violet', 'بنفش') },
              { value: 'green', label: copy('Green', 'سبز') },
              { value: 'white', label: copy('White', 'سفید') },
            ]} onChange={setColor} />
          </div>
          <div className="macro-library__actions">
            {!selected && <Button icon={Plus} disabled={!name.trim()} busy={busy === 'controller.macro.create'} onClick={() => void perform(
              'controller.macro.create',
              { id: draftID, name: name.trim(), category: category.trim(), color },
              `effect create sequence ${draftID} ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Create draft', 'ساخت پیش‌نویس')}</Button>}
            {selected && <Button icon={Save} disabled={!name.trim()} busy={busy === 'controller.macro.update'} onClick={() => void perform(
              'controller.macro.update',
              { reference, name: name.trim(), category: category.trim(), color },
              `effect update ${shellArgument(reference)} ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Save metadata', 'ذخیره مشخصات')}</Button>}
            {selected && <Button tone="danger" icon={Trash2} busy={busy === 'controller.macro.delete'} onClick={() => void perform(
              'controller.effect.delete', { reference }, `effect delete ${shellArgument(reference)}`,
            )}>{copy('Delete', 'حذف')}</Button>}
            {selected && <Button tone="ghost" onClick={() => { setSelectedReference(''); setName(''); setCategory('Web'); setColor('green') }}>{copy('New', 'جدید')}</Button>}
          </div>
        </section>
      </div>

      <section className="macro-library__strip-effects">
        <header><span><Sparkles size={15} /> {copy('Lighting effects', 'جلوه‌های نورپردازی')}</span><small>{stripEffects.length}</small></header>
        <div className="macro-library__workspace">
          <div className="macro-library__catalog" role="listbox" aria-label={copy('Lighting effects', 'جلوه‌های نورپردازی')}>
            {stripEffects.map((effect) => (
              <button key={effect.id} type="button" role="option" aria-selected={effect.id === selectedStripID}
                className={effect.id === selectedStripID ? 'is-selected' : ''} onClick={() => setSelectedStripID(effect.id)}>
                <span className="macro-library__color is-violet" aria-hidden="true" />
                <span className="macro-library__identity"><strong>{effect.name}</strong><small>{effect.category || copy('Uncategorized', 'بدون دسته')} · {effect.engine}</small></span>
                <span className="macro-library__measure"><strong>{effect.default_fps}</strong><small>FPS</small></span>
                <span className="macro-library__measure"><strong>{effect.default_pixels}</strong><small>{copy('LEDs', 'LED')}</small></span>
              </button>
            ))}
            {!stripEffects.length && <div className="macro-library__empty">{copy('No lighting effects.', 'جلوهٔ نورپردازی وجود ندارد.')}</div>}
          </div>
          <div className="macro-library__editor">
            <header><span><ListChecks size={15} /> {stripDraft?.id ? copy('Effect properties', 'ویژگی‌های جلوه') : copy('New lighting effect', 'جلوه نور جدید')}</span></header>
            {stripDraft && <>
              <div className="macro-library__fields">
                <TextField label={copy('Stable ID', 'شناسه پایدار')} value={stripDraft.id} disabled={Boolean(selectedStrip)} onChange={(event) => setStripDraft({ ...stripDraft, id: event.target.value })} />
                <TextField label={copy('Name', 'نام')} value={stripDraft.name} maxLength={64} onChange={(event) => setStripDraft({ ...stripDraft, name: event.target.value })} />
                <TextField label={copy('Category', 'دسته‌بندی')} value={stripDraft.category || ''} maxLength={64} onChange={(event) => setStripDraft({ ...stripDraft, category: event.target.value })} />
                <TextField label={copy('Description', 'توضیح')} value={stripDraft.description || ''} maxLength={64} onChange={(event) => setStripDraft({ ...stripDraft, description: event.target.value })} />
                <TextField label={copy('Frames / second', 'فریم در ثانیه')} type="number" min={1} max={30} value={stripDraft.default_fps} onChange={(event) => setStripDraft({ ...stripDraft, default_fps: Number(event.target.value) })} />
                <TextField label={copy('Duration (ms)', 'مدت (میلی‌ثانیه)')} type="number" min={100} max={3600000} value={stripDraft.default_duration_ms} onChange={(event) => setStripDraft({ ...stripDraft, default_duration_ms: Number(event.target.value) })} />
                <TextField label={copy('LED count', 'تعداد LED')} type="number" min={1} max={100} value={stripDraft.default_pixels} onChange={(event) => setStripDraft({ ...stripDraft, default_pixels: Number(event.target.value) })} />
              </div>
              <Segmented value={stripDraft.program.primitive} label={copy('Program', 'برنامه')} options={[
                { value: 'alternating-zones', label: copy('Alternating zones', 'ناحیه‌های متناوب') },
                { value: 'envelope', label: copy('Brightness envelope', 'پوش شدت نور') },
                { value: 'converging-points', label: copy('Converging points', 'نقاط همگرا') },
              ]} onChange={(primitive) => setStripDraft({ ...stripDraft, program: { ...stripDraft.program, primitive } })} />
              <div className="macro-library__actions">
                <Button tone="primary" icon={Play} disabled={!online || !selectedStrip} onClick={() => void perform('controller.effect.play', {}, `effect play ${shellArgument(stripDraft.id)}`)}>{copy('Run', 'اجرا')}</Button>
                {selectedStrip ? <Button icon={Save} disabled={!stripDraft.name.trim()} onClick={() => void perform(
                  'controller.effect.update', {},
                  `effect update ${shellArgument(stripDraft.id)} ${shellArgument(stripDraft.name.trim())} ${shellArgument((stripDraft.category || '').trim())} ${shellArgument((stripDraft.description || '').trim() || '-')} ${shellArgument(stripDraft.program.primitive)} ${stripDraft.default_fps} ${stripDraft.default_duration_ms} ${stripDraft.default_pixels}`,
                )}>{copy('Save to PCController', 'ذخیره در PCController')}</Button> : <Button icon={Plus} disabled={!stripDraft.id.trim() || !stripDraft.name.trim()} onClick={() => void perform(
                  'controller.effect.create', {},
                  `effect create strip ${shellArgument(stripDraft.id.trim())} ${shellArgument(stripDraft.name.trim())} ${shellArgument(stripDraft.program.primitive)} ${shellArgument((stripDraft.category || '').trim())} ${stripDraft.default_fps} ${stripDraft.default_duration_ms} ${stripDraft.default_pixels}`,
                )}>{copy('Create in PCController', 'ساخت در PCController')}</Button>}
                {selectedStrip && <Button tone="danger" icon={Trash2} onClick={() => void perform('controller.effect.delete', {}, `effect delete ${shellArgument(stripDraft.id)}`)}>{copy('Delete', 'حذف')}</Button>}
                <Button tone="ghost" onClick={() => {
                  setSelectedStripID('')
                  setStripDraft({ id: '', name: '', category: 'Lighting', description: '', program: { primitive: 'alternating-zones', primary: { red: 255, green: 0, blue: 0 }, secondary: { red: 0, green: 0, blue: 255 }, period_ms: 800, step_ms: 100, swap_after_steps: 4, dim_intensity: 36 }, engine: 'host-stream', editable: true, default_fps: 20, default_duration_ms: 5000, default_pixels: 100, min_pixels: 1, max_pixels: 100, min_fps: 1, max_fps: 30 })
                }}>{copy('New', 'جدید')}</Button>
              </div>
            </>}
          </div>
        </div>
      </section>

      {selected && (selected.steps?.length ?? 0) > 0 && (
        <details className="macro-library__steps">
          <summary>{copy('Exact step timing', 'زمان‌بندی دقیق گام‌ها')} · {selected.steps?.length ?? 0}</summary>
          <div role="table">
            {(selected.steps ?? []).map((step, index, steps) => {
              const previous = index ? steps[index - 1].at_us ?? 0 : 0
              const at = step.at_us ?? 0
              return <div role="row" key={`${index}-${at}-${step.kind}`}><span>#{index + 1}</span><strong>{step.kind}</strong><span>{formatMicroseconds(at, locale)}</span><span>Δ {formatMicroseconds(at - previous, locale)}</span><code>{step.text || `target=${step.target ?? 0} value=${step.value ?? 0}`}</code></div>
            })}
          </div>
        </details>
      )}

      <div className="macro-library__control-grid">
        <section>
          <header>{copy('Host recording', 'ضبط میزبان')}</header>
          <div className="macro-library__actions">
            <Button tone="primary" icon={CircleDot} disabled={!online || !name.trim() || Boolean(recording?.active)} busy={busy === 'controller.macro.record.start'} onClick={() => void perform(
              'controller.macro.record.start',
              { name: name.trim(), category: category.trim(), color },
              `effect record start ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Start', 'شروع')}</Button>
            <Button icon={Save} disabled={!recording?.active || Boolean(recording.device_retained)} busy={busy === 'controller.macro.record.stop'} onClick={() => void perform(
              'controller.macro.record.stop', { save: true }, 'effect record save',
            )}>{copy('Stop + save', 'توقف و ذخیره')}</Button>
            <Button icon={Trash2} disabled={!recording?.active || Boolean(recording.device_retained)} onClick={() => void perform(
              'controller.macro.record.stop', { save: false }, 'effect record discard',
            )}>{copy('Discard', 'دور انداختن')}</Button>
          </div>
        </section>
        <section>
          <header>{copy('Device-retained recording', 'ضبط نگه‌داری‌شده در دستگاه')}</header>
          <p>{copy('Keeps the latest 25 relay snapshots in RAM. Save before resetting the board. Stop and release retained RAM before strip streaming.', '۲۵ وضعیت آخر رله در حافظه نگهداری می‌شود. پیش از ریست ذخیره کنید. قبل از پخش نوار، ضبط را متوقف و حافظه را آزاد کنید.')}</p>
          <div className="macro-library__actions">
            <Button icon={RadioTower} disabled={!online || !name.trim() || Boolean(recording?.active)} busy={busy === 'controller.macro.board_record.start'} onClick={() => void perform(
              'controller.macro.board_record.start', {}, `effect record start-board ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Start retained take', 'شروع برداشت نگه‌داری‌شده')}</Button>
            <Button icon={Database} disabled={!online || !name.trim() || Boolean(recording?.active)} onClick={() => void perform(
              'controller.macro.board_record.import', {}, `effect record import-board ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Import retained capture', 'وارد کردن ضبط باقی‌مانده')}</Button>
            <Button icon={Save} disabled={!recording?.active || !recording.device_retained} busy={busy === 'controller.macro.board_record.stop'} onClick={() => void perform(
              'controller.macro.board_record.stop', {}, 'effect record save',
            )}>{copy('Stop + import', 'توقف و واردکردن')}</Button>
            <Button tone="danger" icon={Trash2} disabled={!recording?.active || !recording.device_retained} onClick={() => void perform(
              'controller.macro.board_record.discard', {}, 'effect record discard',
            )}>{copy('Discard', 'دور انداختن')}</Button>
            <Button icon={Trash2} disabled={!online || Boolean(recording?.active) || Boolean(playback?.running)} onClick={() => void perform(
              'controller.macro.buffer.clear', {}, 'effect buffer clear',
            )}>{copy('Release board RAM for strip', 'آزاد کردن حافظه برد برای نوار')}</Button>
          </div>
        </section>
      </div>

      <div className="macro-library__playback-actions">
        <Segmented value={playMode} label={copy('Execution', 'اجرا')} options={[{value:'auto',label:copy('Automatic', 'خودکار')},{value:'host',label:copy('Host (forced)', 'میزبان (اجباری)')},{value:'mcu',label:copy('Device (forced)', 'دستگاه (اجباری)')}]} onChange={setPlayMode} />
        <Button tone="primary" icon={Play} disabled={!online || !reference} busy={busy === 'controller.macro.play'} onClick={() => void perform(
          'controller.effect.play', { reference }, `effect play ${shellArgument(reference)} ${playMode}`,
        )}>{copy('Play selected', 'اجرای انتخاب‌شده')}</Button>
        <Button icon={CircleStop} disabled={!online || !playback?.running} busy={busy === 'controller.macro.cancel'} onClick={() => void perform(
          'controller.macro.cancel', { keep_outputs: false }, 'effect cancel',
        )}>{copy('Cancel + turn outputs off', 'لغو و خاموش‌کردن خروجی‌ها')}</Button>
        <Button tone="danger" icon={CircleStop} disabled={!online || !playback?.running} onClick={() => void perform(
          'controller.macro.cancel', { keep_outputs: true }, 'effect cancel keep',
        )}>{copy('Cancel, keep outputs', 'لغو با حفظ خروجی‌ها')}</Button>
      </div>

      <section className="macro-library__events" aria-label={copy('Live effect monitor', 'پایش زنده جلوه')}>
        <header>{copy('Live structured monitor', 'پایش زنده ساختاریافته')}</header>
        {latestEvents.length ? latestEvents.map((event) => (
          <div key={event.id}>
            <time>{new Date(event.time).toLocaleTimeString(locale)}</time>
            <strong>{event.kind}</strong>
            <span>{event.lifecycle || event.state || event.text}</span>
            {event.metadata?.delta_us && <code>Δ {event.metadata.delta_us} µs</code>}
            {event.metadata?.mcu_delta_us && <code>Δ {event.metadata.mcu_delta_us} µs</code>}
          </div>
        )) : <p>{copy('Waiting for effect events.', 'در انتظار رویدادهای جلوه.')}</p>}
      </section>
    </div>
  )
}

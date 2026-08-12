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
  Trash2,
} from 'lucide-react'
import { rpc } from './api'
import { Button, Segmented, StatusBadge, TextField } from './components'
import { shellArgument } from './command-line'
import {
  applyMacroEventToSnapshot,
  macroEventNeedsSnapshot,
  shouldUseLegacyMacroFallback,
} from './macro-live'
import type { ControllerEvent, ControllerMacro, Locale, MacroSnapshot } from './types'

type MacroColor = 'red' | 'blue' | 'violet' | 'green' | 'white'

interface MacroLibraryPanelProps {
  online: boolean
  locale: Locale
  events: ControllerEvent[]
  legacyCommand: (command: string) => Promise<string>
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
  return macro.steps.reduce((maximum, step) => Math.max(maximum, step.at_us ?? 0), 0)
}

function formatMicroseconds(value: number, locale: Locale): string {
  const amount = Math.abs(value)
  if (amount >= 1_000_000) return `${(value / 1_000_000).toLocaleString(locale, { maximumFractionDigits: 3 })} s`
  if (amount >= 1_000) return `${(value / 1_000).toLocaleString(locale, { maximumFractionDigits: 3 })} ms`
  return `${value.toLocaleString(locale)} µs`
}

/** Stateless typed catalog, exported so the wire-to-DOM contract remains testable. */
export function MacroCatalog({ macros, selectedReference, locale, onSelect }: MacroCatalogProps) {
  const persian = locale === 'fa'
  if (!macros.length) {
    return <div className="macro-library__empty">{persian ? 'هنوز ماکروی ذخیره‌شده‌ای وجود ندارد.' : 'No saved macros yet.'}</div>
  }
  return (
    <div className="macro-library__catalog" role="listbox" aria-label={persian ? 'کتابخانه ماکرو' : 'Macro library'}>
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
              <small>#{macro.id} · {macro.category || (persian ? 'بدون دسته' : 'Uncategorized')}</small>
            </span>
            <span className="macro-library__measure">
              <strong>{macro.steps.length.toLocaleString(locale)}</strong>
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

export function MacroLibraryPanel({ online, locale, events, legacyCommand }: MacroLibraryPanelProps) {
  const persian = locale === 'fa'
  const copy = (english: string, farsi: string) => persian ? farsi : english
  const [snapshot, setSnapshot] = useState<MacroSnapshot | null>(null)
  const [typedAvailable, setTypedAvailable] = useState<boolean | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [selectedReference, setSelectedReference] = useState('')
  const [draftID, setDraftID] = useState(0)
  const [name, setName] = useState('')
  const [category, setCategory] = useState('Web')
  const [color, setColor] = useState<MacroColor>('green')
  const [boardID, setBoardID] = useState(0)
  const latestAppliedEventID = useRef(0)

  const loadSnapshot = useCallback(async (quiet = false) => {
    if (!quiet) setBusy('controller.macro.snapshot')
    try {
      const value = await rpc<MacroSnapshot>('controller.macro.snapshot')
      setSnapshot(value)
      setTypedAvailable(true)
      setError('')
      latestAppliedEventID.current = Math.max(latestAppliedEventID.current, value.latest_event_id || 0)
      return value
    } catch (cause) {
      if (shouldUseLegacyMacroFallback(cause)) {
        setTypedAvailable(false)
        setError(copy('This host only provides the legacy macro command path.', 'این میزبان فقط مسیر فرمان قدیمی ماکرو را ارائه می‌دهد.'))
        return null
      }
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

  const selectMacro = (macro: ControllerMacro) => setSelectedReference(String(macro.id))

  const perform = async (method: string, params: unknown, fallback: string) => {
    setBusy(method)
    setError('')
    try {
      const value = await rpc<MacroSnapshot>(method, params)
      setSnapshot(value)
      setTypedAvailable(true)
      latestAppliedEventID.current = Math.max(latestAppliedEventID.current, value.latest_event_id || 0)
    } catch (cause) {
      if (!shouldUseLegacyMacroFallback(cause)) {
        setError(cause instanceof Error ? cause.message : String(cause))
        return
      }
      await legacyCommand(fallback)
      const refreshed = await loadSnapshot(true)
      if (!refreshed) setTypedAvailable(false)
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
              ? `${recording.board_owned ? copy('Board ring', 'حافظه حلقوی برد') : copy('Host capture', 'ضبط میزبان')} · ${recording.steps} ${copy('steps', 'گام')} · Δ ${formatMicroseconds(recording.last_delta_us, locale)}`
              : copy('Ready for exact MCU acknowledgement deltas', 'آماده برای اختلاف زمانی دقیق تأیید MCU')}</em>
          </span>
        </div>
        <div className={playback?.running ? 'is-active' : ''}>
          <Play aria-hidden="true" size={17} />
          <span>
            <small>{copy('PLAYBACK', 'اجرا')}</small>
            <strong>{playback?.name || copy('Idle', 'آماده')}</strong>
            <em>{playback?.name
              ? `${playback.lifecycle || 'idle'} · ${playback.step}/${playback.step_count} · Δ ${formatMicroseconds(playback.last_timing_delta_us, locale)}`
              : copy('No playback in this session', 'هنوز اجرایی در این نشست انجام نشده')}</em>
          </span>
        </div>
        <StatusBadge tone={typedAvailable === false ? 'warn' : typedAvailable ? 'good' : 'info'}>
          {typedAvailable === false ? copy('LEGACY', 'قدیمی') : typedAvailable ? copy('TYPED LIVE', 'زنده ساختاریافته') : copy('CONNECTING', 'در حال اتصال')}
        </StatusBadge>
      </div>

      {error && <div className="macro-library__error" role="alert">{error}</div>}
      {typedAvailable === false && (
        <div className="macro-library__legacy">
          <span>{copy('Enter a macro name or ID in the Name field; actions below will use the compatible command path.', 'نام یا شناسه ماکرو را در فیلد نام وارد کنید؛ عملیات زیر از مسیر فرمان سازگار استفاده می‌کنند.')}</span>
          <Button compact icon={Database} onClick={() => void legacyCommand('macro list')}>{copy('List in terminal', 'فهرست در ترمینال')}</Button>
        </div>
      )}

      <div className="macro-library__workspace">
        <section>
          <header><span><Database size={15} /> {copy('Library', 'کتابخانه')}</span><small>{snapshot?.library.length ?? 0}</small></header>
          <MacroCatalog macros={snapshot?.library ?? []} selectedReference={selectedReference} locale={locale} onSelect={selectMacro} />
        </section>
        <section className="macro-library__editor">
          <header><span><ListChecks size={15} /> {selected ? copy('Selected macro', 'ماکروی انتخاب‌شده') : copy('New macro', 'ماکروی جدید')}</span></header>
          <div className="macro-library__fields">
            <TextField label={copy('ID 0..255', 'شناسه ۰ تا ۲۵۵')} type="number" min={0} max={255} value={selected ? selected.id : draftID} disabled={Boolean(selected)} onChange={(event) => setDraftID(Math.max(0, Math.min(255, Number(event.target.value) || 0)))} />
            <TextField label={copy('Name', 'نام')} value={name} maxLength={64} onChange={(event) => setName(event.target.value)} />
            <TextField label={copy('Category', 'دسته‌بندی')} value={category} maxLength={64} onChange={(event) => setCategory(event.target.value)} />
          </div>
          <div className="macro-library__color-picker">
            <label>{copy('Color', 'رنگ')}</label>
            <Segmented value={color} label={copy('Macro color', 'رنگ ماکرو')} options={[
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
              `macro create ${draftID} ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Create draft', 'ساخت پیش‌نویس')}</Button>}
            {selected && <Button icon={Save} disabled={!name.trim()} busy={busy === 'controller.macro.update'} onClick={() => void perform(
              'controller.macro.update',
              { reference, name: name.trim(), category: category.trim(), color },
              `macro update ${shellArgument(reference)} ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Save metadata', 'ذخیره مشخصات')}</Button>}
            {selected && <Button tone="danger" icon={Trash2} busy={busy === 'controller.macro.delete'} onClick={() => void perform(
              'controller.macro.delete', { reference }, `macro delete ${shellArgument(reference)}`,
            )}>{copy('Delete', 'حذف')}</Button>}
            {selected && <Button tone="ghost" onClick={() => { setSelectedReference(''); setName(''); setCategory('Web'); setColor('green') }}>{copy('New', 'جدید')}</Button>}
          </div>
        </section>
      </div>

      {selected && selected.steps.length > 0 && (
        <details className="macro-library__steps">
          <summary>{copy('Exact step timing', 'زمان‌بندی دقیق گام‌ها')} · {selected.steps.length}</summary>
          <div role="table">
            {selected.steps.map((step, index) => {
              const previous = index ? selected.steps[index - 1].at_us ?? 0 : 0
              const at = step.at_us ?? 0
              return <div role="row" key={`${index}-${at}-${step.kind}`}><span>#{index + 1}</span><strong>{step.kind}</strong><span>{formatMicroseconds(at, locale)}</span><span>Δ {formatMicroseconds(at - previous, locale)}</span><code>{step.text || `target=${step.target ?? 0} value=${step.value ?? 0}`}</code></div>
            })}
          </div>
        </details>
      )}

      <div className="macro-library__control-grid">
        <section>
          <header>{copy('Host recording', 'ضبط میزبان')}</header>
          <p>{copy('Relay, motion, MOSFET, beep, display, RF, and front-panel actions are captured through the shared opcode path.', 'رله، حرکت، ماسفت، بوق، نمایشگر، RF و پنل از مسیر مشترک opcode ضبط می‌شوند.')}</p>
          <div className="macro-library__actions">
            <Button tone="primary" icon={CircleDot} disabled={!online || !name.trim() || Boolean(recording?.active)} busy={busy === 'controller.macro.record.start'} onClick={() => void perform(
              'controller.macro.record.start',
              { name: name.trim(), category: category.trim(), color },
              `macro record start ${shellArgument(name.trim())} ${shellArgument(category.trim())} ${shellArgument(color)}`,
            )}>{copy('Start', 'شروع')}</Button>
            <Button icon={Save} disabled={!recording?.active || Boolean(recording.board_owned)} busy={busy === 'controller.macro.record.stop'} onClick={() => void perform(
              'controller.macro.record.stop', { save: true }, 'macro record save',
            )}>{copy('Stop + save', 'توقف و ذخیره')}</Button>
            <Button icon={Trash2} disabled={!recording?.active || Boolean(recording.board_owned)} onClick={() => void perform(
              'controller.macro.record.stop', { save: false }, 'macro record discard',
            )}>{copy('Discard', 'دور انداختن')}</Button>
          </div>
        </section>
        <section>
          <header>{copy('Board circular capture', 'ضبط حلقوی برد')}</header>
          <p>{copy('Retains front-panel/RF timing on the board, then imports and names the captured sequence on the host.', 'زمان‌بندی پنل و RF را روی برد نگه می‌دارد و سپس توالی ضبط‌شده را روی میزبان وارد و نام‌گذاری می‌کند.')}</p>
          <div className="macro-library__board-id">
            <TextField label={copy('Capture ID', 'شناسه ضبط')} type="number" min={0} max={255} value={boardID} onChange={(event) => setBoardID(Math.max(0, Math.min(255, Number(event.target.value) || 0)))} />
          </div>
          <div className="macro-library__actions">
            <Button icon={RadioTower} disabled={!online || Boolean(recording?.active)} busy={busy === 'controller.macro.board_record.start'} onClick={() => void perform(
              'controller.macro.board_record.start', { id: boardID }, `macro record board start ${boardID}`,
            )}>{copy('Start on board', 'شروع روی برد')}</Button>
            <Button icon={Save} disabled={!recording?.active || !recording.board_owned} busy={busy === 'controller.macro.board_record.stop'} onClick={() => void perform(
              'controller.macro.board_record.stop', {}, 'macro record board stop',
            )}>{copy('Stop + import', 'توقف و واردکردن')}</Button>
            <Button tone="danger" icon={Trash2} disabled={!online || Boolean(recording?.active)} busy={busy === 'controller.macro.board_record.clear'} onClick={() => void perform(
              'controller.macro.board_record.clear', { force: false }, 'macro record board clear',
            )}>{copy('Clear retained', 'پاک‌کردن حافظه')}</Button>
          </div>
        </section>
      </div>

      <div className="macro-library__playback-actions">
        <Button tone="primary" icon={Play} disabled={!online || !reference} busy={busy === 'controller.macro.play'} onClick={() => void perform(
          'controller.macro.play', { reference }, `macro play ${shellArgument(reference)}`,
        )}>{copy('Play selected', 'اجرای انتخاب‌شده')}</Button>
        <Button icon={CircleStop} disabled={!online || (typedAvailable !== false && !playback?.running)} busy={busy === 'controller.macro.cancel'} onClick={() => void perform(
          'controller.macro.cancel', { keep_outputs: false }, 'macro cancel',
        )}>{copy('Cancel + turn outputs off', 'لغو و خاموش‌کردن خروجی‌ها')}</Button>
        <Button tone="danger" icon={CircleStop} disabled={!online || (typedAvailable !== false && !playback?.running)} onClick={() => void perform(
          'controller.macro.cancel', { keep_outputs: true }, 'macro cancel keep',
        )}>{copy('Cancel, keep outputs', 'لغو با حفظ خروجی‌ها')}</Button>
      </div>

      <section className="macro-library__events" aria-label={copy('Live macro monitor', 'پایش زنده ماکرو')}>
        <header>{copy('Live structured monitor', 'پایش زنده ساختاریافته')}</header>
        {latestEvents.length ? latestEvents.map((event) => (
          <div key={event.id}>
            <time>{new Date(event.time).toLocaleTimeString(locale)}</time>
            <strong>{event.kind}</strong>
            <span>{event.lifecycle || event.state || event.text}</span>
            {event.metadata?.delta_us && <code>Δ {event.metadata.delta_us} µs</code>}
            {event.metadata?.mcu_delta_us && <code>Δ {event.metadata.mcu_delta_us} µs</code>}
          </div>
        )) : <p>{copy('Waiting for macro events.', 'در انتظار رویدادهای ماکرو.')}</p>}
      </section>
    </div>
  )
}

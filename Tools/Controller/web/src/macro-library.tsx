import { useEffect, useState } from 'react'
import { CircleDot, CircleStop, List, Play, Save, Trash2 } from 'lucide-react'
import { rpc } from './api'
import { Button, StatusBadge, TextField } from './components'
import { shellArgument } from './command-line'
import type { Locale } from './types'

interface MacroEntry {
  id: number
  name: string
  category?: string
  color?: string
  steps: unknown[]
  recording_source?: string
  capture_dropped_steps?: number
  capture_missing_steps?: number
}

interface MacroLibraryState {
  running: boolean
  id: number
  name: string
  lifecycle?: string
  step: number
  step_count: number
  faithful: boolean
  last_error?: string
}

interface MacroRecordingState {
  active: boolean
  id: number
  name: string
  category?: string
  board_owned?: boolean
  steps: number
  host_steps: number
  panel_steps: number
  rf_steps: number
  last_error?: string
}

interface MacroLibraryResponse {
  library: MacroEntry[]
  playback: MacroLibraryState
  recording: MacroRecordingState
}

export function macroLifecycleLabel(state: Pick<MacroLibraryState, 'running' | 'lifecycle' | 'last_error'>): string {
  if (state.last_error) return 'error'
  if (state.running) return 'playing'
  return state.lifecycle || 'idle'
}

export function MacroLibrary({ locale, connected, run }: { locale: Locale; connected: boolean; run: (command: string) => Promise<string> }) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [state, setState] = useState<MacroLibraryResponse>({ library: [], playback: { running: false, id: 0, name: '', step: 0, step_count: 0, faithful: false }, recording: { active: false, id: 0, name: '', steps: 0, host_steps: 0, panel_steps: 0, rf_steps: 0 } })
  const [name, setName] = useState('')
  const [category, setCategory] = useState('Web')
  const [boardID, setBoardID] = useState(0)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')

  const refresh = async () => {
    try { setState(await rpc<MacroLibraryResponse>('controller.macro.library')); setNotice('') }
    catch (cause) { setNotice(cause instanceof Error ? cause.message : String(cause)) }
  }
  useEffect(() => {
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, 800)
    return () => window.clearInterval(timer)
  }, [])
  const invoke = async (label: string, task: () => Promise<unknown>) => {
    setBusy(label)
    try { await task(); await refresh() } catch (cause) { setNotice(cause instanceof Error ? cause.message : String(cause)) } finally { setBusy('') }
  }
  const recording = state.recording
  const playback = state.playback
  return <section className="macro-library" aria-label={copy('Macro library', 'کتابخانهٔ ماکرو')}>
    <header><div><span>{copy('LIVE LIBRARY', 'کتابخانهٔ زنده')}</span><strong>{copy('Named macro library', 'کتابخانهٔ ماکروهای نام‌دار')}</strong></div><StatusBadge tone={recording.active ? 'warn' : playback.running ? 'good' : 'neutral'}>{recording.active ? copy('RECORDING', 'در حال ضبط') : macroLifecycleLabel(playback).toUpperCase()}</StatusBadge></header>
    <p>{recording.active ? `${recording.board_owned ? copy('Board', 'برد') : copy('Host', 'میزبان')} · ${recording.name} · ${recording.steps} ${copy('steps', 'گام')} · H${recording.host_steps}/P${recording.panel_steps}/RF${recording.rf_steps}` : playback.running ? `${playback.name} · ${playback.step}/${playback.step_count}` : copy('Live state comes from the shared host macro runner.', 'وضعیت زنده از اجراکنندهٔ مشترک ماکرو در میزبان می‌آید.')}</p>
    {notice && <p className="macro-library__notice" role="status">{notice}</p>}
    <div className="macro-library__record">
      <TextField label={copy('Recording name', 'نام ضبط')} value={name} onChange={(event) => setName(event.target.value)} />
      <TextField label={copy('Category', 'دسته')} value={category} onChange={(event) => setCategory(event.target.value)} />
      <TextField label={copy('Board capture ID', 'شناسهٔ ضبط برد')} type="number" min={0} max={255} value={boardID} onChange={(event) => setBoardID(Math.max(0, Math.min(255, Number(event.target.value) || 0)))} />
      {!recording.active ? <><Button icon={CircleDot} disabled={!name.trim()} busy={busy === 'host'} onClick={() => void invoke('host', () => rpc('controller.macro.record.start', { source: 'host', name: name.trim(), category }))}>{copy('Record host', 'ضبط میزبان')}</Button><Button icon={CircleDot} disabled={!connected} busy={busy === 'board'} onClick={() => void invoke('board', () => rpc('controller.macro.record.start', { source: 'board', id: boardID }))}>{copy('Record board', 'ضبط برد')}</Button></> : <><Button icon={Save} busy={busy === 'stop'} onClick={() => void invoke('stop', () => recording.board_owned ? rpc('controller.macro.record.stop', { source: 'board' }) : rpc('controller.macro.record.stop', { source: 'host', save: true }))}>{recording.board_owned ? copy('Stop & recover', 'توقف و بازیابی') : copy('Save recording', 'ذخیره ضبط')}</Button><Button icon={Trash2} disabled={recording.board_owned} busy={busy === 'discard'} onClick={() => void invoke('discard', () => rpc('controller.macro.record.stop', { source: 'host', save: false }))}>{copy('Discard', 'حذف')}</Button></>}
    </div>
    <div className="macro-library__rows">
      {state.library.length === 0 ? <p>{copy('No saved macros yet.', 'هنوز ماکروی ذخیره‌شده‌ای نیست.')}</p> : state.library.map((macro) => <article key={macro.id}>
        <div><strong>{macro.name}</strong><small>#{macro.id} · {macro.category || copy('Uncategorized', 'بدون دسته')} · {macro.steps.length} {copy('steps', 'گام')}{macro.recording_source ? ` · ${macro.recording_source}` : ''}</small>{(macro.capture_dropped_steps || macro.capture_missing_steps) ? <em>{copy(`Capture truncated: ${macro.capture_dropped_steps || 0} dropped, ${macro.capture_missing_steps || 0} missing`, `ضبط ناقص: ${macro.capture_dropped_steps || 0} حذف، ${macro.capture_missing_steps || 0} مفقود`)}</em> : null}</div>
        <div><Button compact icon={Play} disabled={!connected || playback.running} onClick={() => void invoke(`play-${macro.id}`, () => rpc('controller.macro.play', { reference: String(macro.id) }))}>{copy('Play', 'اجرا')}</Button><Button compact icon={CircleStop} disabled={!playback.running} onClick={() => void invoke('cancel', () => rpc('controller.macro.cancel', {}))}>{copy('Cancel', 'لغو')}</Button><Button compact icon={List} onClick={() => void run(`macro show ${shellArgument(String(macro.id))}`)}>{copy('Steps', 'گام‌ها')}</Button><Button compact icon={Trash2} disabled={playback.running && playback.id === macro.id} onClick={() => void invoke(`delete-${macro.id}`, () => run(`macro delete ${shellArgument(String(macro.id))}`))}>{copy('Delete', 'حذف')}</Button></div>
      </article>)}
    </div>
  </section>
}

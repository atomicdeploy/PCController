import {
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Keyboard,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { rpc } from './api'
import { Button, Card, KeyCombo, StatusBadge, TextField, Toggle } from './components'
import { localizeDigits } from './i18n'
import {
  displayChordKeys,
  hotkeyConfigurationMatches,
  isSafeHotkeyCommand,
  normalizeHotkeyName,
  recordShortcut,
  safeHotkeyCommands,
} from './hotkey-editor'
import type { HostHotkeyBinding, HotkeySettingsResponse, Locale } from './types'

const modifierOnlyKeys = new Set(['Alt', 'AltGraph', 'Control', 'Meta', 'OS', 'Shift', 'Super', 'Win', 'Windows'])

function localized(locale: Locale, english: string, persian: string): string {
  return locale === 'fa' ? persian : english
}

function reconciliationDetail(locale: Locale, settings: HotkeySettingsResponse, state: ReturnType<typeof hotkeyConfigurationMatches>): string {
  if (state.state === 'active') {
    const count = settings.bindings.filter((binding) => binding.enabled).length
    return locale === 'fa'
      ? `${localizeDigits('fa', count)} میانبر سراسری فعال است.`
      : `${count} global ${count === 1 ? 'shortcut is' : 'shortcuts are'} active.`
  }
  if (state.state === 'idle') {
    return localized(locale, 'No global shortcuts are enabled.', 'هیچ میانبر سراسری فعالی وجود ندارد.')
  }
  return state.detail
}

function delay(milliseconds: number, signal: AbortSignal): Promise<boolean> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve(false)
      return
    }
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', cancel)
      resolve(true)
    }, milliseconds)
    const cancel = () => {
      window.clearTimeout(timer)
      resolve(false)
    }
    signal.addEventListener('abort', cancel, { once: true })
  })
}

export function HotkeyChord({ chord }: { chord: string }) {
  return <KeyCombo keys={displayChordKeys(chord)} />
}

export function HotkeyEditor({ locale }: { locale: Locale }) {
  const [settings, setSettings] = useState<HotkeySettingsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [selectedName, setSelectedName] = useState<string | null>(null)
  const [draftName, setDraftName] = useState('')
  const [draftChord, setDraftChord] = useState('')
  const [draftCommand, setDraftCommand] = useState(safeHotkeyCommands[0].value)
  const [draftEnabled, setDraftEnabled] = useState(true)
  const [recording, setRecording] = useState(false)
  const [busy, setBusy] = useState(false)
  const [reconciling, setReconciling] = useState(false)
  const [notice, setNotice] = useState('')
  const [noticeTone, setNoticeTone] = useState<'info' | 'success' | 'warning' | 'danger'>('info')
  const [deleteArmed, setDeleteArmed] = useState(false)
  const recorderRef = useRef<HTMLButtonElement>(null)
  const newBindingRef = useRef<HTMLButtonElement>(null)
  const pollRef = useRef<AbortController | null>(null)

  const selectBinding = (binding: HostHotkeyBinding) => {
    setSelectedName(binding.name)
    setDraftName(binding.name)
    setDraftChord(binding.chord)
    setDraftCommand(binding.command)
    setDraftEnabled(binding.enabled)
    setRecording(false)
    setDeleteArmed(false)
    setNotice('')
  }

  const startNewBinding = (focus = false) => {
    setSelectedName(null)
    setDraftName('')
    setDraftChord('')
    setDraftCommand(safeHotkeyCommands[0].value)
    setDraftEnabled(true)
    setRecording(false)
    setDeleteArmed(false)
    setNotice(localized(locale, 'Name the shortcut, record its keys, then choose an action.', 'میانبر را نام‌گذاری کنید، کلیدها را ضبط و سپس یک کنش انتخاب کنید.'))
    setNoticeTone('info')
    if (focus) window.requestAnimationFrame(() => newBindingRef.current?.focus())
  }

  const load = async (signal?: AbortSignal, chooseInitial = false) => {
    setLoadError('')
    try {
      const value = await rpc<HotkeySettingsResponse>('controller.hotkeys.get', {}, signal)
      setSettings(value)
      if (chooseInitial) {
        const first = value.bindings[0]
        if (first) selectBinding(first)
        else startNewBinding()
      }
      return value
    } catch (cause) {
      if (signal?.aborted) return null
      const detail = cause instanceof Error ? cause.message : String(cause)
      setLoadError(detail)
      return null
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal, true)
    return () => {
      controller.abort()
      pollRef.current?.abort()
    }
    // The initial synchronization is intentionally one-shot; later refreshes
    // preserve the user's current draft instead of resetting the editor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const selected = useMemo(
    () => settings?.bindings.find((binding) => binding.name.toLocaleLowerCase('en-US') === selectedName?.toLocaleLowerCase('en-US')),
    [selectedName, settings],
  )
  const canonicalName = normalizeHotkeyName(draftName).trim()
  const duplicateName = settings?.bindings.some((binding) => (
    binding.name.toLocaleLowerCase('en-US') === canonicalName.toLocaleLowerCase('en-US') &&
    binding.name.toLocaleLowerCase('en-US') !== selectedName?.toLocaleLowerCase('en-US')
  )) ?? false
  const duplicateChord = settings?.bindings.some((binding) => (
    binding.chord === draftChord &&
    binding.name.toLocaleLowerCase('en-US') !== selectedName?.toLocaleLowerCase('en-US')
  )) ?? false
  const nameError = !canonicalName
    ? localized(locale, 'A binding name is required.', 'نام میانبر الزامی است.')
    : duplicateName
      ? localized(locale, 'That name is already in use.', 'این نام قبلاً استفاده شده است.')
      : ''
  const chordError = !draftChord
    ? localized(locale, 'Record a supported key combination.', 'یک ترکیب کلید پشتیبانی‌شده ضبط کنید.')
    : duplicateChord
      ? localized(locale, 'That key combination is already assigned.', 'این ترکیب کلید قبلاً اختصاص یافته است.')
      : ''
  const commandError = !isSafeHotkeyCommand(draftCommand)
    ? localized(locale, 'Choose an approved action before saving.', 'پیش از ذخیره، یک کنش تأییدشده انتخاب کنید.')
    : ''
  const visibleNameError = draftName ? nameError : ''
  const visibleChordError = draftChord ? chordError : ''
  const draftValid = !nameError && !chordError && !commandError
  const draftDirty = !selected || (
    selected.enabled !== draftEnabled || selected.chord !== draftChord ||
    selected.command !== draftCommand
  )
  const reconciliation = settings ? hotkeyConfigurationMatches(settings) : null
  const applyPending = Boolean(settings?.apply_pending || reconciling)
  const registrarError = !applyPending && reconciliation?.state === 'error' ? reconciliation.detail : ''
  const effectiveNoticeTone = registrarError ? 'danger' : noticeTone
  const badge = loading
    ? { tone: 'neutral' as const, label: localized(locale, 'Synchronizing', 'همگام‌سازی') }
    : loadError
      ? { tone: 'bad' as const, label: localized(locale, 'Unavailable', 'دردسترس نیست') }
      : applyPending
        ? { tone: 'warn' as const, label: localized(locale, 'Apply pending', 'در انتظار اعمال') }
        : reconciliation?.state === 'active'
          ? { tone: 'good' as const, label: localized(locale, 'Active', 'فعال') }
          : reconciliation?.state === 'error'
            ? { tone: 'bad' as const, label: localized(locale, 'Registration failed', 'ثبت ناموفق') }
            : reconciliation?.state === 'idle'
              ? { tone: 'neutral' as const, label: localized(locale, 'No active bindings', 'بدون میانبر فعال') }
              : { tone: 'warn' as const, label: localized(locale, 'Not confirmed', 'تأیید نشده') }

  const reconcile = async (initial: HotkeySettingsResponse) => {
    pollRef.current?.abort()
    const controller = new AbortController()
    pollRef.current = controller
    setReconciling(true)
    setSettings(initial)
    setNotice(localized(locale, 'Saved. Waiting for the host registrar to confirm the change…', 'ذخیره شد؛ در انتظار تأیید تغییر توسط میزبان…'))
    setNoticeTone('info')

    for (let attempt = 0; attempt < 16; attempt += 1) {
      if (!await delay(attempt === 0 ? 100 : 250, controller.signal)) return
      let current: HotkeySettingsResponse
      try {
        current = await rpc<HotkeySettingsResponse>('controller.hotkeys.get', {}, controller.signal)
      } catch (cause) {
        if (controller.signal.aborted) return
        setReconciling(false)
        setSettings((current) => current ? { ...current, apply_pending: false } : current)
        setNotice(cause instanceof Error ? cause.message : String(cause))
        setNoticeTone('danger')
        return
      }
      setSettings(current)
      const state = hotkeyConfigurationMatches(current)
      if (state.state === 'active' || state.state === 'idle') {
        setReconciling(false)
        setNotice(reconciliationDetail(locale, current, state))
        setNoticeTone('success')
        return
      }
      if (state.state === 'error' || state.state === 'unavailable') {
        setReconciling(false)
        setNotice(state.detail)
        setNoticeTone(state.state === 'error' ? 'danger' : 'warning')
        return
      }
    }
    setReconciling(false)
    setNotice(localized(locale, 'Saved, but the registrar did not confirm the change in time.', 'ذخیره شد، اما ثبت‌کننده در زمان مقرر تغییر را تأیید نکرد.'))
    setNoticeTone('warning')
  }

  const saveBinding = async (event: FormEvent) => {
    event.preventDefault()
    if (!draftValid || !draftDirty) return
    setBusy(true)
    setDeleteArmed(false)
    setNotice('')
    try {
      const value = await rpc<HotkeySettingsResponse>('controller.hotkeys.set', {
        operation: 'upsert',
        name: selected?.name ?? canonicalName,
        enabled: draftEnabled,
        chord: draftChord,
        command: draftCommand,
      })
      const saved = value.bindings.find((binding) => binding.name.toLocaleLowerCase('en-US') === (selected?.name ?? canonicalName).toLocaleLowerCase('en-US'))
      if (saved) selectBinding(saved)
      void reconcile(value)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : String(cause))
      setNoticeTone('danger')
    } finally {
      setBusy(false)
    }
  }

  const removeBinding = async () => {
    if (!selected) return
    if (!deleteArmed) {
      setDeleteArmed(true)
      setNotice(localized(locale, `Confirm removal of “${selected.name}”.`, `حذف «${selected.name}» را تأیید کنید.`))
      setNoticeTone('warning')
      return
    }
    setBusy(true)
    try {
      const value = await rpc<HotkeySettingsResponse>('controller.hotkeys.set', {
        operation: 'remove',
        name: selected.name,
      })
      startNewBinding(true)
      void reconcile(value)
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : String(cause))
      setNoticeTone('danger')
    } finally {
      setBusy(false)
    }
  }

  const beginRecording = () => {
    setRecording(true)
    setDeleteArmed(false)
    setNotice(localized(locale, 'Listening now. Press a combination, or Escape to cancel.', 'در حال شنیدن؛ ترکیب کلید را بزنید یا با Escape لغو کنید.'))
    setNoticeTone('info')
    window.requestAnimationFrame(() => recorderRef.current?.focus())
  }

  const captureShortcut = (event: ReactKeyboardEvent<HTMLButtonElement>) => {
    if (!recording || event.nativeEvent.isComposing || event.keyCode === 229) return
    event.stopPropagation()
    if (event.key === 'Escape' && !event.ctrlKey && !event.altKey && !event.shiftKey && !event.metaKey) {
      event.preventDefault()
      setRecording(false)
      setNotice(localized(locale, 'Shortcut recording cancelled.', 'ضبط میانبر لغو شد.'))
      setNoticeTone('info')
      return
    }
    if (modifierOnlyKeys.has(event.key) || event.repeat) return
    event.preventDefault()
    const captured = recordShortcut(event.nativeEvent)
    if (!captured) {
      setNotice(localized(locale, 'Use Ctrl, Alt, Shift or Win with a supported key; F1–F24 may be used alone.', 'از Ctrl، Alt، Shift یا Win همراه یک کلید پشتیبانی‌شده استفاده کنید؛ F1 تا F24 می‌توانند تنها باشند.'))
      setNoticeTone('warning')
      return
    }
    setDraftChord(captured.canonical)
    setRecording(false)
    setDeleteArmed(false)
    setNotice(localized(locale, `Recorded ${captured.canonical}.`, `${captured.canonical} ضبط شد.`))
    setNoticeTone('success')
  }

  const groupedCommands = (['navigation', 'diagnostics', 'safety'] as const).map((group) => ({
    group,
    commands: safeHotkeyCommands.filter((command) => command.group === group),
  }))

  return (
    <Card
      icon={Keyboard}
      iconTone={badge.tone === 'bad' ? 'red' : badge.tone === 'good' ? 'green' : badge.tone === 'warn' ? 'amber' : 'violet'}
      title={localized(locale, 'Global shortcuts', 'میانبرهای سراسری')}
      eyebrow={badge.label}
      className="settings-card settings-card--wide hotkey-card"
      action={<StatusBadge tone={badge.tone} pulse={applyPending}>{badge.label}</StatusBadge>}
    >
      {loadError && !settings ? (
        <div className="hotkey-editor__load-error" role="alert">
          <AlertTriangle aria-hidden="true" size={20} />
          <div><strong>{localized(locale, 'Hotkey settings could not be loaded', 'تنظیمات میانبر بارگیری نشد')}</strong><span>{loadError}</span></div>
          <Button icon={RefreshCw} busy={loading} onClick={() => { setLoading(true); void load(undefined, true) }}>{localized(locale, 'Retry', 'تلاش دوباره')}</Button>
        </div>
      ) : (
        <div className="hotkey-editor">
          <section className="hotkey-editor__library" aria-label={localized(locale, 'Configured global shortcuts', 'میانبرهای سراسری پیکربندی‌شده')}>
            <header>
              <div>
                <strong>{localized(locale, 'Bindings', 'میانبرها')}</strong>
                <small>{settings ? localized(locale, `${settings.bindings.length} configured`, `${settings.bindings.length} مورد پیکربندی شده`) : localized(locale, 'Loading…', 'در حال بارگیری…')}</small>
              </div>
              <Button ref={newBindingRef} compact icon={Plus} onClick={() => startNewBinding()}>{localized(locale, 'New', 'جدید')}</Button>
            </header>
            <div className="hotkey-binding-list">
              {settings?.bindings.map((binding) => (
                <button
                  key={binding.name}
                  type="button"
                  className={binding.name.toLocaleLowerCase('en-US') === selectedName?.toLocaleLowerCase('en-US') ? 'is-selected' : ''}
                  aria-pressed={binding.name.toLocaleLowerCase('en-US') === selectedName?.toLocaleLowerCase('en-US')}
                  onClick={() => selectBinding(binding)}
                >
                  <span className="hotkey-binding-list__icon"><Keyboard aria-hidden="true" size={17} /></span>
                  <span className="hotkey-binding-list__copy">
                    <strong>{binding.name}</strong>
                    <HotkeyChord chord={binding.chord} />
                  </span>
                  <span className={`hotkey-binding-list__state${binding.enabled ? ' is-enabled' : ''}`}>
                    {binding.enabled ? localized(locale, 'Enabled', 'فعال') : localized(locale, 'Disabled', 'غیرفعال')}
                  </span>
                </button>
              ))}
              {settings?.bindings.length === 0 && (
                <div className="hotkey-binding-list__empty">
                  <Keyboard aria-hidden="true" size={22} />
                  <span>{localized(locale, 'No shortcuts configured yet.', 'هنوز میانبری پیکربندی نشده است.')}</span>
                </div>
              )}
            </div>
          </section>

          <form className="hotkey-editor__form" onSubmit={(event) => void saveBinding(event)}>
            <header className="hotkey-editor__form-heading">
              <span className="hotkey-editor__form-icon">{selected ? <ShieldCheck aria-hidden="true" size={19} /> : <Plus aria-hidden="true" size={19} />}</span>
              <div>
                <small>{selected ? localized(locale, 'EDIT BINDING', 'ویرایش میانبر') : localized(locale, 'NEW BINDING', 'میانبر جدید')}</small>
                <strong>{selected?.name || localized(locale, 'Create a global shortcut', 'ایجاد میانبر سراسری')}</strong>
              </div>
            </header>

            <div className="hotkey-editor__fields">
              <TextField
                label={localized(locale, 'Binding name', 'نام میانبر')}
                value={draftName}
                disabled={Boolean(selected)}
                autoComplete="off"
                spellCheck={false}
                onInput={(event) => { setDraftName(normalizeHotkeyName(event.currentTarget.value)); setNotice(''); setDeleteArmed(false) }}
                onChange={(event) => { setDraftName(normalizeHotkeyName(event.currentTarget.value)); setNotice(''); setDeleteArmed(false) }}
                onBlur={() => setDraftName((current) => current.trim())}
                error={visibleNameError || undefined}
                hint={selected ? localized(locale, 'Names stay fixed so an edit cannot create a duplicate binding.', 'نام ثابت می‌ماند تا ویرایش، میانبر تکراری نسازد.') : undefined}
              />

              <label className={`hotkey-command-field${commandError ? ' is-invalid' : ''}`}>
                <span>{localized(locale, 'Action', 'کنش')}</span>
                <select
                  value={draftCommand}
                  aria-invalid={commandError ? true : undefined}
                  onChange={(event) => { setDraftCommand(event.currentTarget.value); setNotice(''); setDeleteArmed(false) }}
                >
                  {!isSafeHotkeyCommand(draftCommand) && <option value={draftCommand} disabled>{localized(locale, 'Existing custom action — choose a safe replacement', 'کنش سفارشی فعلی — یک جایگزین امن انتخاب کنید')}</option>}
                  {groupedCommands.map(({ group, commands }) => (
                    <optgroup key={group} label={{
                      navigation: localized(locale, 'Navigation', 'پیمایش'),
                      diagnostics: localized(locale, 'Diagnostics', 'عیب‌یابی'),
                      safety: localized(locale, 'Safety', 'ایمنی'),
                    }[group]}>
                      {commands.map((command) => <option key={command.value} value={command.value}>{command.label[locale]}</option>)}
                    </optgroup>
                  ))}
                </select>
                <small className={commandError ? 'text-bad' : ''} dir="ltr">{commandError || draftCommand}</small>
              </label>

              <div className={`hotkey-recorder-field${visibleChordError ? ' is-invalid' : ''}`}>
                <span>{localized(locale, 'Key combination', 'ترکیب کلید')}</span>
                <button
                  ref={recorderRef}
                  type="button"
                  className={`hotkey-recorder${recording ? ' is-recording' : ''}`}
                  aria-pressed={recording}
                  aria-describedby="hotkey-recorder-message"
                  onClick={beginRecording}
                  onKeyDown={captureShortcut}
                  onBlur={() => {
                    if (!recording) return
                    setRecording(false)
                    setNotice(localized(locale, 'Shortcut recording stopped.', 'ضبط میانبر متوقف شد.'))
                    setNoticeTone('info')
                  }}
                >
                  <span className="hotkey-recorder__indicator" aria-hidden="true" />
                  {draftChord ? <HotkeyChord chord={draftChord} /> : <span>{localized(locale, 'Record shortcut', 'ضبط میانبر')}</span>}
                  <small>{recording ? localized(locale, 'Listening…', 'در حال شنیدن…') : localized(locale, 'Press to record', 'برای ضبط فشار دهید')}</small>
                </button>
                <small id="hotkey-recorder-message" className={visibleChordError ? 'text-bad' : ''} role={visibleChordError ? 'alert' : 'status'}>
                  {visibleChordError || (recording
                    ? localized(locale, 'Waiting for a non-modifier key.', 'در انتظار یک کلید غیرترکیبی.')
                    : draftChord
                      ? localized(locale, 'The recorded combination is valid.', 'ترکیب ضبط‌شده معتبر است.')
                      : localized(locale, 'No key combination recorded.', 'هنوز ترکیب کلیدی ضبط نشده است.'))}
                </small>
              </div>
            </div>

            <Toggle
              checked={draftEnabled}
              onChange={(enabled) => { setDraftEnabled(enabled); setNotice(''); setDeleteArmed(false) }}
              label={localized(locale, 'Enable this global shortcut', 'فعال‌سازی این میانبر سراسری')}
              detail={draftEnabled
                ? localized(locale, 'The host will register it after saving.', 'میزبان پس از ذخیره آن را ثبت می‌کند.')
                : localized(locale, 'The binding is saved but not registered.', 'میانبر ذخیره می‌شود اما ثبت نخواهد شد.')}
            />

            <div className={`hotkey-editor__notice is-${effectiveNoticeTone}`} role={effectiveNoticeTone === 'danger' ? 'alert' : 'status'} aria-live="polite">
              {effectiveNoticeTone === 'danger'
                ? <AlertTriangle aria-hidden="true" size={17} />
                : effectiveNoticeTone === 'success'
                  ? <CheckCircle2 aria-hidden="true" size={17} />
                  : <span className="hotkey-editor__notice-line" aria-hidden="true" />}
              <span>{registrarError || notice || (settings && reconciliation ? reconciliationDetail(locale, settings, reconciliation) : '') || localized(locale, 'Choose a binding to edit or create a new one.', 'یک میانبر را برای ویرایش انتخاب یا میانبر تازه‌ای ایجاد کنید.')}</span>
            </div>

            <footer className="hotkey-editor__actions">
              <Button type="submit" tone="primary" icon={Save} busy={busy} disabled={!draftValid || !draftDirty || deleteArmed || applyPending}>
                {selected ? localized(locale, 'Save binding', 'ذخیره میانبر') : localized(locale, 'Add binding', 'افزودن میانبر')}
              </Button>
              {selected && <Button type="button" tone="danger" icon={Trash2} busy={busy} disabled={applyPending} onClick={() => void removeBinding()}>
                {deleteArmed ? localized(locale, 'Confirm remove', 'تأیید حذف') : localized(locale, 'Remove', 'حذف')}
              </Button>}
              {deleteArmed && <Button type="button" tone="ghost" onClick={() => { setDeleteArmed(false); setNotice(''); }}>{localized(locale, 'Cancel', 'لغو')}</Button>}
            </footer>
          </form>
        </div>
      )}
    </Card>
  )
}

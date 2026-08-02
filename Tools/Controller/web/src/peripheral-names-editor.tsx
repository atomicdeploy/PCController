import { useEffect, useMemo, useState } from 'react'
import { PanelTop, RotateCcw, ShieldCheck } from 'lucide-react'
import { rpc } from './api'
import { Button, Card, TextField } from './components'
import { normalizePeripheralNameInput, validatePeripheralName } from './settings-validation'
import type { Locale, PeripheralDescriptor, PeripheralSettings } from './types'

interface PeripheralNamesEditorProps {
  locale: Locale
}

const kindOrder = ['relay', 'motion', 'pwm', 'display', 'sensor'] as const

function canonicalNames(
  names: Record<string, string>,
  catalog: PeripheralDescriptor[],
): Record<string, string> {
  const defaults = new Map(catalog.map((descriptor) => [descriptor.key, descriptor.default_name]))
  return Object.fromEntries(Object.entries(names)
    .map(([key, value]) => [key, validatePeripheralName(value).normalized] as const)
    .filter(([key, value]) => value && value !== defaults.get(key))
    .sort(([left], [right]) => left.localeCompare(right)))
}

function namesEqual(left: Record<string, string>, right: Record<string, string>): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

export function PeripheralNamesEditor({ locale }: PeripheralNamesEditorProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [catalog, setCatalog] = useState<PeripheralDescriptor[]>([])
  const [saved, setSaved] = useState<Record<string, string>>({})
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(true)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState(false)

  useEffect(() => {
    let active = true
    void rpc<PeripheralSettings>('controller.peripherals.get')
      .then((value) => {
        if (!active) return
        const canonical = canonicalNames(value.peripheral_names ?? {}, value.peripherals ?? [])
        setCatalog(value.peripherals ?? [])
        setSaved(canonical)
        setDraft(canonical)
        setNotice('')
        setError(false)
      })
      .catch((cause) => {
        if (!active) return
        setNotice(cause instanceof Error ? cause.message : String(cause))
        setError(true)
      })
      .finally(() => { if (active) setBusy(false) })
    return () => { active = false }
  }, [])

  const normalizedDraft = useMemo(() => canonicalNames(draft, catalog), [catalog, draft])
  const dirty = !namesEqual(normalizedDraft, saved)
  const customCount = Object.keys(normalizedDraft).length
  const groups = useMemo(() => kindOrder.map((kind) => ({
    kind,
    entries: catalog.filter((descriptor) => descriptor.kind === kind),
  })).filter((group) => group.entries.length), [catalog])
  const groupLabel = (kind: string) => ({
    relay: copy('Relays', 'رله‌ها'),
    motion: copy('Motion sides', 'سمت‌های حرکت'),
    pwm: copy('PWM and lighting', 'PWM و نورپردازی'),
    display: copy('Displays', 'نمایشگرها'),
    sensor: copy('Sensors', 'حسگرها'),
  } as Record<string, string>)[kind] ?? kind

  const update = (key: string, value: string) => {
    setNotice('')
    setError(false)
    setDraft((current) => ({ ...current, [key]: normalizePeripheralNameInput(value) }))
  }

  const save = async () => {
    if (!dirty) return
    setBusy(true)
    setNotice('')
    setError(false)
    try {
      const result = await rpc<PeripheralSettings>('controller.peripherals.set', {
        peripheral_names: normalizedDraft,
      })
      const canonical = canonicalNames(result.peripheral_names ?? {}, result.peripherals ?? catalog)
      setCatalog(result.peripherals ?? catalog)
      setSaved(canonical)
      setDraft(canonical)
      setNotice(copy('Names saved and propagated to host, IPC, bridge, TUI, and Web surfaces.', 'نام‌ها ذخیره شدند و در میزبان، IPC، پل، TUI و وب اعمال شدند.'))
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : String(cause))
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  const feedback = notice || (busy
    ? copy('Synchronizing the host peripheral catalog…', 'در حال همگام‌سازی فهرست تجهیزات میزبان…')
    : dirty
      ? copy(`${customCount} custom name${customCount === 1 ? '' : 's'} in the validated draft.`, `${customCount} نام سفارشی در پیش‌نویس معتبر است.`)
      : copy(`${catalog.length} peripherals synchronized · ${customCount} customized.`, `${catalog.length} تجهیز همگام است · ${customCount} مورد سفارشی شده.`))

  return (
    <Card
      icon={PanelTop}
      iconTone="accent"
      title={copy('Peripheral names', 'نام تجهیزات')}
      eyebrow={busy ? copy('Synchronizing', 'در حال همگام‌سازی') : dirty ? copy('Unsaved changes', 'تغییرات ذخیره‌نشده') : copy('Host authoritative', 'مرجع میزبان')}
      className="settings-card settings-card--wide peripheral-names-card"
      menu={catalog.length ? [{
        label: copy('Restore every default name', 'بازگردانی همهٔ نام‌های پیش‌فرض'),
        icon: RotateCcw,
        onSelect: () => { setDraft({}); setNotice(''); setError(false) },
      }] : undefined}
    >
      <div className="peripheral-name-groups" aria-busy={busy}>
        {groups.map((group, groupIndex) => (
          <details key={group.kind} className="peripheral-name-group" open={groupIndex === 0}>
            <summary><span>{groupLabel(group.kind)}</span><small>{group.entries.length}</small></summary>
            <div className="peripheral-name-grid">
              {group.entries.map((descriptor) => {
                const value = draft[descriptor.key] ?? ''
                const validation = validatePeripheralName(value)
                const customized = Boolean(validation.normalized && validation.normalized !== descriptor.default_name)
                return <TextField
                  key={descriptor.key}
                  label={descriptor.default_name}
                  value={value}
                  placeholder={descriptor.default_name}
                  maxLength={64}
                  onInput={(event) => update(descriptor.key, event.currentTarget.value)}
                  onChange={(event) => update(descriptor.key, event.currentTarget.value)}
                  error={!validation.valid ? validation.error : undefined}
                  success={customized ? copy(`Custom · ${descriptor.key}`, `سفارشی · ${descriptor.key}`) : undefined}
                  action={<Button compact disabled={!value} icon={RotateCcw} onClick={() => update(descriptor.key, '')}>{copy('Default', 'پیش‌فرض')}</Button>}
                />
              })}
            </div>
          </details>
        ))}
      </div>
      <footer className="peripheral-names-card__footer">
        <span className={error ? 'text-warn' : notice && !dirty ? 'text-good' : ''} role="status" aria-live="polite">{feedback}</span>
        <Button tone="primary" icon={ShieldCheck} busy={busy} disabled={!dirty} onClick={() => void save()}>{copy('Save names', 'ذخیرهٔ نام‌ها')}</Button>
      </footer>
    </Card>
  )
}

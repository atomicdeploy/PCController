import {
  type ComponentType,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  Activity,
  Bell,
  Box,
  Boxes,
  Cable,
  Check,
  ChevronDown,
  Command,
  Cpu,
  Gauge,
  Keyboard,
  Languages,
  LayoutDashboard,
  Lightbulb,
  Menu,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  ShieldCheck,
  Sun,
  Volume2,
  VolumeX,
  Wrench,
  X,
} from 'lucide-react'
import { AnimatePresence, MotionConfig, motion } from 'motion/react'
import { createAudioEngine, type AudioEngine } from './audio-engine'
import { BootGate, Button, HotkeyHelp, Icon, Modal, NavButton, PageTransition, StatusBadge, ToastStack } from './components'
import { connectStream, execute, getSnapshot, getToken, getUIConfig, setToken as storeToken } from './api'
import {
  adjacentPageHotkey,
  ignoresGlobalHotkeys,
  isFreshAppAction,
  pageFromAppAction,
  pageFromGoChord,
  pageFromNumberHotkey,
  type PageID,
} from './hotkeys'
import { translator, type MessageKey } from './i18n'
import { redactSensitiveCommand } from './command-line'
import { effectiveProductTitle, productMark } from './product-identity'
import type {
  Appearance,
  ControllerEvent,
  DialogState,
  MetricSample,
  Snapshot,
  ToastMessage,
  UIConfig,
} from './types'
import { emptySnapshot } from './types'
import { DataWorkspaceView } from './data-workspace'
import { WorkbenchView } from './workbench'
import {
  ControlsView,
  DashboardView,
  EventsView,
  LocalDeviceView,
  SettingsView,
  type SharedViewProps,
} from './views'

interface NavDefinition {
  id: PageID
  label: MessageKey
  icon: typeof LayoutDashboard
  group?: 'core' | 'integrations' | 'system'
}

const navigation: NavDefinition[] = [
  { id: 'dashboard', label: 'dashboard', icon: LayoutDashboard, group: 'core' },
  { id: 'controls', label: 'controls', icon: Gauge, group: 'core' },
  { id: 'workbench', label: 'workbench', icon: Wrench, group: 'core' },
  { id: 'device', label: 'device', icon: Lightbulb, group: 'integrations' },
  { id: 'data', label: 'data', icon: Boxes, group: 'integrations' },
  { id: 'events', label: 'events', icon: Activity, group: 'system' },
  { id: 'settings', label: 'settings', icon: Settings, group: 'system' },
]

const defaultAppearance: Appearance = {
  theme: 'system',
  locale: 'en',
  direction: 'auto',
  reduceMotion: false,
  compactNumbers: false,
  audioMuted: false,
  audioVolume: 0.42,
}

function loadAppearance(): Appearance {
  try {
    const saved = JSON.parse(localStorage.getItem('pccontroller.appearance') ?? '{}') as Partial<Appearance>
    const value = { ...defaultAppearance, ...saved }
    value.audioVolume = Number.isFinite(value.audioVolume) ? Math.max(0, Math.min(1, value.audioVolume)) : defaultAppearance.audioVolume
    value.audioMuted = Boolean(value.audioMuted)
    return value
  } catch { return defaultAppearance }
}

function applyAppearance(value: Appearance): void {
  const theme = value.theme === 'system'
    ? matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
    : value.theme
  const direction = value.direction === 'auto' ? value.locale === 'fa' ? 'rtl' : 'ltr' : value.direction
  document.documentElement.dataset.theme = theme
  document.documentElement.lang = value.locale === 'fa' ? 'fa-IR' : 'en'
  document.documentElement.dir = direction
  document.documentElement.classList.toggle('reduce-motion', value.reduceMotion)
  document.documentElement.classList.toggle('compact-numbers', value.compactNumbers)
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#071117' : '#edf5f6')
}

function pageFromLocation(): PageID {
  const value = location.hash.replace(/^#\/?/, '').split('/')[0] as PageID
  return navigation.some((item) => item.id === value) ? value : 'dashboard'
}

function sampleFrom(snapshot: Snapshot, at = Date.now()): MetricSample {
  const status = snapshot.status
  return {
    at,
    supply: status.supply_mv / 1000,
    bus: status.bus_mv / 1000,
    current: status.current_ma,
    power: status.power_mw / 1000,
    ledTemp: status.temperature_led_centi_c / 100,
    btTemp: status.temperature_bt_audio_centi_c / 100,
  }
}

function demoSnapshot(now = Date.now()): Snapshot {
  const wave = Math.sin(now / 3600)
  return {
    ...emptySnapshot,
    connected: true,
    have_status: true,
    have_settings: true,
    connection_state: 'connected',
    connection_updated: new Date(now - 45_000).toISOString(),
    status_updated: new Date(now).toISOString(),
    port: { name: 'COM18', vid: '1A86', pid: '7523', friendly_name: 'USB-SERIAL CH340', manufacturer: 'wch.cn' },
    hello: { name: __PRODUCT_NAME__, capabilities: 0x001f7fff, build_hash: 0xc21af30d, build_timestamp: '260802010000' },
    status: {
      ...emptySnapshot.status,
      uptime_ms: 4_936_000 + (now % 500_000),
      supply_mv: Math.round(12180 + wave * 35),
      bus_mv: Math.round(12040 + wave * 28),
      current_ma: Math.round(386 + Math.sin(now / 1900) * 42),
      power_mw: Math.round(4660 + Math.sin(now / 2100) * 340),
      temperature_led_centi_c: Math.round(3640 + Math.sin(now / 7400) * 55),
      temperature_bt_audio_centi_c: Math.round(3310 + Math.sin(now / 6200) * 40),
      active_relays: 0b00110000,
      door_open: false,
      bluetooth_audio_state: 2,
      pwm_mode: 1,
      pwm_channel: 5,
      pwm_value: 2740,
      lcd_address: 0x27,
    },
    settings: { ...emptySnapshot.settings, on_brightness: 220, off_brightness: 18, display_brightness: 5, status_brightness: 165, stream_period_ms: 200 },
  }
}

function demoEvent(id: number): ControllerEvent {
  const definitions = [
    ['device.state', 'Authenticated controller identity on COM18', 'host'],
    ['door', 'Door input returned to closed', 'physical'],
    ['macro.completed', 'Ambient evening macro completed faithfully', 'host'],
    ['rf.received', 'Remote #3 · living-room toggle', 'rf'],
  ]
  const [kind, text, source] = definitions[id % definitions.length]
  return { id, kind, text, source, time: new Date(Date.now() - id * 37_000).toISOString() }
}

interface CommandWarning {
  title: string
  body: string
  confirmLabel: string
}

export function commandWarning(command: string, locale: Appearance['locale']): CommandWarning | null {
  const normalized = command.trim().toLowerCase().replace(/\s+/g, ' ')
  const fa = locale === 'fa'
  const warning = (title: string, body: string, confirmLabel: string): CommandWarning => ({ title, body, confirmLabel })
  if (/^(reset|boot|program|programmer|firmware|flash|upload|restore|query|write)(?: |$)/.test(normalized)) {
    return fa
      ? warning('ورود به مسیر بازیابی یا برنامه‌ریزی؟', 'این فرمان ممکن است درگاه سریال را آزاد کند، خطوط کنترل را تغییر دهد یا وارد مسیر محافظت‌شدهٔ برنامه‌ریزی شود. فرمان را یک‌بار دیگر بررسی کنید.', 'تأیید و ارسال')
      : warning('Enter a recovery or programming path?', 'This command may release the serial port, pulse control lines, or enter the guarded programming workflow. Review the exact command before dispatch.', 'Confirm and dispatch')
  }
  if (/^rf send(?: |$)/.test(normalized)) {
    return fa
      ? warning('فرمان رادیویی ارسال شود؟', 'این ارسال می‌تواند یک وسیلهٔ خارجی را فعال کند. کد، پروتکل و تعداد تکرار را پیش از ارسال کنترل کنید.', 'ارسال آگاهانه')
      : warning('Transmit the radio command?', 'This transmission can actuate an external device. Verify the code, protocol, and repeat count before dispatch.', 'Transmit explicitly')
  }
  if (/^(macro play|automation run|bridge call)(?: |$)/.test(normalized)) {
    return fa
      ? warning('اجرای گردش‌کار تأیید شود؟', 'تعریف انتخاب‌شده می‌تواند خروجی‌های برد، فرمان‌های میزبان یا یک میزبان راه‌دور را فعال کند. ابتدا نام و مقصد را بررسی کنید.', 'تأیید اجرا')
      : warning('Confirm workflow execution?', 'The selected definition can operate board outputs, host actions, or a remote peer. Verify its name and destination first.', 'Confirm run')
  }
  if (/^(os (power|sleep|suspend|hibernate|restart|shutdown|lock)|quit|exit)(?: |$)/.test(normalized)) {
    return fa
      ? warning('عملیات میزبان اجرا شود؟', 'این فرمان می‌تواند نشست یا رایانه را متوقف کند. سیاست و توکن تأیید میزبان همچنان در سمت سرویس اعمال می‌شود.', 'تأیید عملیات')
      : warning('Run the host operation?', 'This command can end the session or affect the computer. Host-side policy and confirmation-token checks still apply.', 'Confirm operation')
  }
  return null
}

export default function App() {
  const demo = new URLSearchParams(location.search).get('demo') === '1'
  const [appearance, setAppearance] = useState(loadAppearance)
  const [page, setPage] = useState<PageID>(pageFromLocation)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileNav, setMobileNav] = useState(false)
  const [snapshot, setSnapshot] = useState<Snapshot>(demo ? demoSnapshot() : emptySnapshot)
  const [samples, setSamples] = useState<MetricSample[]>(() => demo ? Array.from({ length: 48 }, (_, index) => sampleFrom(demoSnapshot(Date.now() - (47 - index) * 1000), Date.now() - (47 - index) * 1000)) : [])
  const [events, setEvents] = useState<ControllerEvent[]>(() => demo ? Array.from({ length: 12 }, (_, index) => demoEvent(index + 1)) : [])
  const [uiConfig, setUIConfig] = useState<UIConfig | null>(null)
  const [streamState, setStreamState] = useState<'connecting' | 'open' | 'waiting' | 'closed'>(demo ? 'open' : 'connecting')
  const [streamDetail, setStreamDetail] = useState('')
  const [toasts, setToasts] = useState<ToastMessage[]>([])
  const [dialog, setDialog] = useState<DialogState>({ open: false, title: '', body: '', confirmLabel: '' })
  const [dialogBusy, setDialogBusy] = useState(false)
  const [palette, setPalette] = useState(false)
  const [paletteQuery, setPaletteQuery] = useState('')
  const [paletteIndex, setPaletteIndex] = useState(0)
  const [hotkeyHelp, setHotkeyHelp] = useState(false)
  const [bootOpen, setBootOpen] = useState(true)
  const [bootProgress, setBootProgress] = useState(12)
  const [bootTarget, setBootTarget] = useState(demo ? 100 : 24)
  const [token, setTokenState] = useState(getToken)
  const toastID = useRef(0)
  const goChordUntil = useRef(0)
  const audioRef = useRef<AudioEngine | null>(null)
  const t = useMemo(() => translator(appearance.locale), [appearance.locale])
  const productTitle = effectiveProductTitle(uiConfig?.name, __PRODUCT_NAME__)
  const productShortName = productMark(productTitle, __PRODUCT_SHORT_NAME__)

  useEffect(() => {
    document.title = productTitle
  }, [productTitle])

  useEffect(() => {
    const engine = createAudioEngine({
      muted: appearance.audioMuted,
      volume: appearance.audioVolume,
      pauseWhenHidden: true,
    })
    audioRef.current = engine
    return () => {
      if (audioRef.current === engine) audioRef.current = null
      void engine.dispose()
    }
    // AudioContext creation remains gesture-gated inside engine.start().
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (bootProgress >= bootTarget) return
    const timer = window.setInterval(() => {
      setBootProgress((current) => {
        const gap = bootTarget - current
        if (gap <= 0) return current
        if (gap < 0.8) return bootTarget
        return Math.min(bootTarget, current + Math.max(0.75, gap * 0.14))
      })
    }, 72)
    return () => window.clearInterval(timer)
  }, [bootProgress, bootTarget])

  const notify = useCallback((tone: ToastMessage['tone'], title: string, detail?: string) => {
    toastID.current += 1
    const id = toastID.current
    setToasts((current) => [...current.slice(-3), { id, tone, title, detail }])
    if (tone === 'danger' || tone === 'warning') audioRef.current?.cue('warning')
    if (tone === 'success') audioRef.current?.cue('success')
    window.setTimeout(() => setToasts((current) => current.filter((item) => item.id !== id)), 5200)
  }, [])

  const refresh = useCallback(async () => {
    if (demo) {
      const value = demoSnapshot()
      setSnapshot(value)
      setSamples((current) => [...current.slice(-71), sampleFrom(value)])
      return
    }
    try {
      const value = await getSnapshot()
      setSnapshot(value)
      if (value.have_status) setSamples((current) => [...current.slice(-71), sampleFrom(value)])
    } catch (cause) {
      notify('warning', 'Snapshot unavailable', cause instanceof Error ? cause.message : String(cause))
    }
  }, [demo, notify])

  const dispatchCommand = useCallback(async (command: string, success?: string): Promise<string> => {
    if (demo) {
      const output = `[demo] ${redactSensitiveCommand(command)}`
      notify('info', success || 'Demonstration command', output)
      return output
    }
    try {
      const result = await execute(command)
      const output = result.output ?? ''
      notify('success', success || 'Command completed', output || redactSensitiveCommand(command))
      void refresh()
      return output
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      notify('danger', 'Command failed', detail)
      throw cause
    }
  }, [demo, notify, refresh])

  const runCommand = useCallback((command: string, success?: string): Promise<string> => {
    const caution = demo ? null : commandWarning(command, appearance.locale)
    if (!caution) return dispatchCommand(command, success)
    return new Promise<string>((resolve, reject) => {
      let settled = false
      setDialog({
        open: true,
        tone: 'danger',
        title: caution.title,
        body: `${caution.body}\n\n${redactSensitiveCommand(command)}`,
        confirmLabel: caution.confirmLabel,
        cancel: () => {
          if (settled) return
          settled = true
          resolve(appearance.locale === 'fa' ? 'پیش از ارسال لغو شد.' : 'Cancelled before dispatch.')
        },
        action: async () => {
          try {
            const output = await dispatchCommand(command, success)
            settled = true
            resolve(output)
          } catch (cause) {
            settled = true
            reject(cause)
            throw cause
          }
        },
      })
    })
  }, [appearance.locale, demo, dispatchCommand])

  const openDialog = useCallback((value: Omit<DialogState, 'open'>) => {
    setDialog({ ...value, open: true })
  }, [])

  const navigate = useCallback((value: PageID) => {
    setPage(value)
    setMobileNav(false)
    history.replaceState(null, '', `${location.pathname}${location.search}#/${value}`)
    document.querySelector('.app-main')?.scrollTo({ top: 0, behavior: appearance.reduceMotion ? 'auto' : 'smooth' })
  }, [appearance.reduceMotion])

  const saveAppearance = useCallback((value: Appearance) => {
    const safeValue = {
      ...value,
      audioMuted: Boolean(value.audioMuted),
      audioVolume: Number.isFinite(value.audioVolume) ? Math.max(0, Math.min(1, value.audioVolume)) : defaultAppearance.audioVolume,
    }
    setAppearance(safeValue)
    localStorage.setItem('pccontroller.appearance', JSON.stringify(safeValue))
    applyAppearance(safeValue)
    audioRef.current?.setVolume(safeValue.audioVolume)
    audioRef.current?.setMuted(safeValue.audioMuted)
    const engine = audioRef.current
    if (!safeValue.audioMuted && engine) {
      void engine.start({ muted: false }).then((started) => {
        if (started) engine.cue('select')
      })
    }
  }, [])

  const enterApp = useCallback((sound: boolean) => {
    const next = { ...appearance, audioMuted: !sound }
    saveAppearance(next)
    const engine = audioRef.current
    engine?.setVolume(next.audioVolume)
    if (engine) {
      void engine.start({ muted: !sound }).then((started) => {
        if (started && sound) engine.cue('success')
      })
    }
    setBootOpen(false)
  }, [appearance, saveAppearance])

  const toggleAudio = useCallback(() => {
    saveAppearance({ ...appearance, audioMuted: !appearance.audioMuted })
  }, [appearance, saveAppearance])

  const saveToken = useCallback((value: string) => {
    storeToken(value)
    setTokenState(value.trim())
    notify('success', value.trim() ? 'Session token applied' : 'Session token cleared')
    window.setTimeout(() => location.reload(), 180)
  }, [notify])

  useEffect(() => {
    applyAppearance(appearance)
    const media = matchMedia('(prefers-color-scheme: light)')
    const listener = () => appearance.theme === 'system' && applyAppearance(appearance)
    media.addEventListener('change', listener)
    return () => media.removeEventListener('change', listener)
  }, [appearance])

  useEffect(() => {
    const onHash = () => setPage(pageFromLocation())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (bootOpen) return
      const composing = event.isComposing || event.keyCode === 229
      if (palette) {
        if (composing) return
        const choices = navigation.filter((item) => t(item.label).toLowerCase().includes(paletteQuery.toLowerCase()))
        if (event.key === 'Escape') {
          event.preventDefault()
          setPalette(false)
          setPaletteQuery('')
          return
        }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
          event.preventDefault()
          setPaletteIndex((current) => {
            if (!choices.length) return 0
            if (event.key === 'Home') return 0
            if (event.key === 'End') return choices.length - 1
            return (current + (event.key === 'ArrowDown' ? 1 : -1) + choices.length) % choices.length
          })
          audioRef.current?.cue('navigation', event.key === 'ArrowUp' ? 'up' : 'down')
          return
        }
        if (event.key === 'Enter' && choices.length) {
          event.preventDefault()
          const selected = choices[Math.min(paletteIndex, choices.length - 1)]
          navigate(selected.id)
          setPalette(false)
          setPaletteQuery('')
          audioRef.current?.cue('select')
          return
        }
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
          event.preventDefault()
          setPalette(false)
        }
        return
      }

      if (hotkeyHelp) {
        if (event.key === 'Escape' || event.key === '?') {
          event.preventDefault()
          setHotkeyHelp(false)
        }
        return
      }
      if (dialog.open) return
      if (event.key === 'Escape' && mobileNav) {
        event.preventDefault()
        setMobileNav(false)
        return
      }
      if (ignoresGlobalHotkeys(event)) return

      if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setPaletteIndex(0)
        setPalette(true)
        return
      }
      if (!event.altKey && !event.ctrlKey && !event.metaKey && event.key === '?') {
        event.preventDefault()
        setHotkeyHelp(true)
        return
      }
      if (!event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey && event.key.toLowerCase() === 'm' && !event.repeat) {
        event.preventDefault()
        toggleAudio()
        return
      }

      const directPage = pageFromNumberHotkey(event)
      const adjacentPage = adjacentPageHotkey(event, page, document.documentElement.dir === 'rtl' ? 'rtl' : 'ltr')
      if (directPage || adjacentPage) {
        event.preventDefault()
        navigate(directPage ?? adjacentPage!)
        audioRef.current?.cue('navigation', event.key.includes('Left') ? 'left' : event.key.includes('Right') ? 'right' : 'forward')
        return
      }

      const plainKey = !event.altKey && !event.ctrlKey && !event.metaKey
      if (plainKey && performance.now() <= goChordUntil.current) {
        goChordUntil.current = 0
        const chordPage = pageFromGoChord(event.key)
        if (chordPage) {
          event.preventDefault()
          navigate(chordPage)
          audioRef.current?.cue('navigation', 'forward')
          return
        }
      }
      if (plainKey && !event.shiftKey && event.key.toLowerCase() === 'g' && !event.repeat) {
        event.preventDefault()
        goChordUntil.current = performance.now() + 1400
        audioRef.current?.cue('focus')
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [bootOpen, dialog.open, hotkeyHelp, mobileNav, navigate, page, palette, paletteIndex, paletteQuery, t, toggleAudio])

  useEffect(() => {
    setPaletteIndex(0)
  }, [paletteQuery])

  useEffect(() => {
    if (bootOpen) return
    const selector = 'button:not(:disabled), a[href], [role="button"]:not([aria-disabled="true"])'
    let focused: Element | null = null
    const actionable = (target: EventTarget | null) => target instanceof Element ? target.closest(selector) : null
    const onFocus = (event: FocusEvent | PointerEvent) => {
      const target = actionable(event.target)
      if (!target || target === focused) return
      focused = target
      audioRef.current?.cue('focus')
    }
    const onPointerOut = (event: PointerEvent) => {
      if (focused && !focused.contains(event.relatedTarget as Node | null)) focused = null
    }
    const onClick = (event: MouseEvent) => {
      if (actionable(event.target)) audioRef.current?.cue('select')
    }
    document.addEventListener('focusin', onFocus)
    document.addEventListener('pointerover', onFocus)
    document.addEventListener('pointerout', onPointerOut)
    document.addEventListener('click', onClick)
    return () => {
      document.removeEventListener('focusin', onFocus)
      document.removeEventListener('pointerover', onFocus)
      document.removeEventListener('pointerout', onPointerOut)
      document.removeEventListener('click', onClick)
    }
  }, [bootOpen])

  useEffect(() => {
    if (demo) {
      setBootTarget(100)
      const timer = window.setInterval(() => {
        const value = demoSnapshot()
        setSnapshot(value)
        setSamples((current) => [...current.slice(-71), sampleFrom(value)])
      }, 1000)
      return () => window.clearInterval(timer)
    }
    const abort = new AbortController()
    let stopStream = () => {}
    void (async () => {
      try {
        setBootTarget(42)
        const config = await getUIConfig(abort.signal)
        setUIConfig(config)
        setBootTarget(70)
        const value = await getSnapshot(abort.signal)
        setSnapshot(value)
        if (value.have_status) setSamples([sampleFrom(value)])
        setBootTarget(92)
        stopStream = connectStream(config, {
          status: (update) => {
            if (update.error) { setStreamDetail(update.error); return }
            setSnapshot((current) => ({ ...current, connected: true, have_status: true, status: update.status, status_updated: update.time }))
            setSamples((current) => [...current.slice(-71), sampleFrom({ ...emptySnapshot, status: update.status }, new Date(update.time).getTime())])
          },
          event: (event) => {
            setEvents((current) => [event, ...current.filter((item) => item.id !== event.id)].slice(0, 500))
            if (event.kind.toLowerCase() === 'app.page' && isFreshAppAction(event.time)) {
              const destination = pageFromAppAction(event.metadata?.page ?? event.metadata?.value ?? event.text)
              if (destination) {
                navigate(destination)
                audioRef.current?.cue('navigation', 'forward')
              }
            }
            if (/error|warning|hot|door/i.test(event.kind)) notify(eventToneForToast(event), event.kind, event.text)
            if (/config/i.test(event.kind)) {
              void getUIConfig().then(setUIConfig).catch(() => undefined)
            }
            if (/device|connection|settings/i.test(event.kind)) void refresh()
          },
          state: (state, detail) => { setStreamState(state); setStreamDetail(detail ?? '') },
        })
        setBootTarget(100)
      } catch (cause) {
        setStreamState('waiting')
        setStreamDetail(cause instanceof Error ? cause.message : String(cause))
        setBootTarget(100)
      }
    })()
    return () => { abort.abort(); stopStream() }
  }, [demo, navigate, notify, refresh, token])

  const shared: SharedViewProps = {
    appTitle: productTitle, snapshot, samples, events, locale: appearance.locale, t, command: runCommand, refresh, openDialog,
  }

  const view = (() => {
    switch (page) {
      case 'controls': return <ControlsView {...shared} />
      case 'workbench': return <WorkbenchView {...shared} />
      case 'device': return <LocalDeviceView {...shared} />
      case 'data': return <DataWorkspaceView {...shared} />
      case 'events': return <EventsView {...shared} />
      case 'settings': return <SettingsView {...shared} appearance={appearance} onAppearance={saveAppearance} token={token} onToken={saveToken} />
      default: return <DashboardView {...shared} />
    }
  })()

  const current = navigation.find((item) => item.id === page) ?? navigation[0]
  const filteredPalette = navigation.filter((item) => t(item.label).toLowerCase().includes(paletteQuery.toLowerCase()))

  const closeDialog = () => {
    if (dialogBusy) return
    dialog.cancel?.()
    setDialog((value) => ({ ...value, open: false }))
  }

  const confirmDialog = async () => {
    if (!dialog.action) { setDialog((value) => ({ ...value, open: false })); return }
    setDialogBusy(true)
    try { await dialog.action(); setDialog((value) => ({ ...value, open: false })) }
    catch { /* Command execution already surfaced the typed service error. */ }
    finally { setDialogBusy(false) }
  }

  return (
    <MotionConfig reducedMotion={appearance.reduceMotion ? 'always' : 'user'}>
    <div
      className={`app-shell${sidebarOpen ? '' : ' is-sidebar-compact'}`}
      inert={bootOpen || hotkeyHelp ? true : undefined}
      aria-hidden={bootOpen || hotkeyHelp ? true : undefined}
    >
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand">
          <div className="brand__mark" aria-hidden="true"><span>{productShortName}</span><i /><i /></div>
          <div className="brand__copy"><strong>{productTitle}</strong><span>{__PRODUCT_TAGLINE__}</span></div>
          <button className="sidebar-toggle" aria-label={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'} onClick={() => setSidebarOpen((value) => !value)}>{sidebarOpen ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}</button>
        </div>

        <div className="sidebar__status">
          <span className={`status-rail status-rail--${snapshot.connected ? 'good' : 'bad'}`} aria-hidden="true" />
          <div><strong>{snapshot.connected ? t('online') : t('offline')}</strong><small>{snapshot.port.name || snapshot.connection_state}</small></div>
          <Cpu size={18} />
        </div>

        <nav className="sidebar__nav">
          {(['core', 'integrations', 'system'] as const).map((group) => (
            <div className="nav-group" key={group}>
              <span className="nav-group__label">{group === 'core' ? 'SYSTEM' : group === 'integrations' ? t('integrations').toUpperCase() : 'OPERATIONS'}</span>
              {navigation.filter((item) => item.group === group).map((item) => <NavButton key={item.id} icon={item.icon} label={t(item.label)} active={page === item.id} badge={item.id === 'events' && events.length ? String(Math.min(events.length, 99)) : undefined} onClick={() => navigate(item.id)} />)}
            </div>
          ))}
        </nav>

        <div className="sidebar__footer"><ShieldCheck size={17} /><div><strong>Primary owner</strong><span>Authenticated IPC</span></div></div>
      </aside>

      <header className="topbar">
        <button className="mobile-menu" aria-label="Open navigation" onClick={() => setMobileNav(true)}><Menu size={20} /></button>
        <div className="breadcrumbs"><span>{productShortName}</span><i>/</i><strong>{t(current.label)}</strong></div>
        <button className="command-trigger" aria-keyshortcuts="Control+K Meta+K" onClick={() => { setPaletteIndex(0); setPalette(true) }}><Search size={16} /><span>{t('search')} commands and pages</span><kbd>Ctrl K</kbd></button>
        <div className="topbar__actions">
          {demo && <StatusBadge tone="warn">{t('demoMode')}</StatusBadge>}
          <StatusBadge tone={streamState === 'open' ? 'good' : streamState === 'connecting' ? 'info' : 'warn'} pulse={streamState === 'connecting'}>{streamState === 'open' ? t('live') : streamState}</StatusBadge>
          <button className="topbar-icon" aria-label="Toggle theme" onClick={() => saveAppearance({ ...appearance, theme: (document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark') })}>{document.documentElement.dataset.theme === 'dark' ? <Sun size={18} /> : <Moon size={18} />}</button>
          <button className="topbar-icon" aria-label="Switch language" onClick={() => saveAppearance({ ...appearance, locale: appearance.locale === 'en' ? 'fa' : 'en' })}><Languages size={18} /></button>
          <button className="topbar-icon topbar-audio" aria-label={appearance.audioMuted ? 'Enable interaction audio' : 'Mute interaction audio'} aria-pressed={appearance.audioMuted} aria-keyshortcuts="M" onClick={toggleAudio}>{appearance.audioMuted ? <VolumeX size={18} /> : <Volume2 size={18} />}</button>
          <button className="topbar-icon topbar-hotkeys" aria-label="Keyboard shortcuts" aria-keyshortcuts="?" onClick={() => setHotkeyHelp(true)}><Keyboard size={18} /></button>
          <button className="topbar-icon" aria-label="Notifications" onClick={() => navigate('events')}><Bell size={18} />{events.length > 0 && <i />}</button>
        </div>
      </header>

      <main className="app-main">
        {streamDetail && streamState !== 'open' && <div className="stream-notice"><Cable size={17} /><span>{streamDetail}</span><Button compact icon={Settings} onClick={() => navigate('settings')}>{t('settings')}</Button></div>}
        <PageTransition pageKey={page}>{view}</PageTransition>
      </main>

      <nav className="mobile-bottom-nav" aria-label="Mobile navigation">
        {navigation.slice(0, 5).map((item) => <button key={item.id} className={page === item.id ? 'is-active' : ''} onClick={() => navigate(item.id)}><Icon icon={item.icon} size={20} /><span>{t(item.label)}</span></button>)}
      </nav>

      <AnimatePresence>
        {mobileNav && <motion.div className="mobile-drawer-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}><button className="mobile-drawer-backdrop" aria-label="Close navigation" onClick={() => setMobileNav(false)} /><motion.aside className="mobile-drawer" initial={{ clipPath: 'inset(0 100% 0 0)', filter: 'blur(7px)' }} animate={{ clipPath: 'inset(0 0% 0 0)', filter: 'blur(0)' }} exit={{ clipPath: 'inset(0 100% 0 0)', filter: 'blur(7px)' }} transition={{ duration: .32, ease: [0.22, 1, 0.36, 1] }}><header><div className="brand__mark"><span>{productShortName}</span><i /><i /></div><strong>{productTitle}</strong><button onClick={() => setMobileNav(false)}><X size={19} /></button></header>{navigation.map((item) => <NavButton key={item.id} icon={item.icon} label={t(item.label)} active={page === item.id} onClick={() => navigate(item.id)} />)}</motion.aside></motion.div>}
      </AnimatePresence>

      <AnimatePresence>
        {palette && (
          <motion.div className="palette-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <button className="palette-backdrop" aria-label="Close command palette" onClick={() => { setPalette(false); setPaletteQuery('') }} />
            <motion.section className="palette" role="dialog" aria-modal="true" aria-label="Command palette" initial={{ opacity: 0, scale: .97, filter: 'blur(12px)', clipPath: 'inset(10% 5% 10% 5%)' }} animate={{ opacity: 1, scale: 1, filter: 'blur(0)', clipPath: 'inset(0% 0% 0% 0%)' }} exit={{ opacity: 0, scale: .98, filter: 'blur(8px)', clipPath: 'inset(8% 4% 8% 4%)' }} transition={{ type: 'spring', stiffness: 440, damping: 38 }}>
              <header><Command size={19} /><input autoFocus value={paletteQuery} placeholder="Navigate or run a safe command…" aria-controls="palette-results" aria-activedescendant={filteredPalette[paletteIndex] ? `palette-page-${filteredPalette[paletteIndex].id}` : undefined} onChange={(event) => setPaletteQuery(event.target.value)} /><kbd>ESC</kbd></header>
              <div className="palette__results" id="palette-results" role="listbox" aria-label="Matching pages">
                <span className="palette__label">PAGES</span>
                {filteredPalette.map((item, index) => <button id={`palette-page-${item.id}`} role="option" aria-selected={index === paletteIndex} className={index === paletteIndex ? 'is-highlighted' : ''} key={item.id} onPointerMove={() => setPaletteIndex(index)} onClick={() => { navigate(item.id); setPalette(false); setPaletteQuery('') }}><Icon icon={item.icon} size={18} /><div><strong>{t(item.label)}</strong><small>Open {item.id}</small></div><Check className={page === item.id ? 'is-visible' : ''} size={16} /></button>)}
                <span className="palette__label">QUICK COMMANDS</span>
                {[['status', 'Read fresh controller status'], ['relay off', 'Safely stop all relays'], ['rf list', 'List learned remotes'], ['macro list', 'List configured macros']].filter(([value, label]) => `${value} ${label}`.includes(paletteQuery.toLowerCase())).map(([value, label]) => <button role="option" aria-selected="false" key={value} onClick={() => { void runCommand(value); setPalette(false) }}><Box size={18} /><div><strong dir="ltr">{value}</strong><small>{label}</small></div></button>)}
              </div>
              <footer><span><kbd>↑↓</kbd> Navigate</span><span><kbd>↵</kbd> Select</span><span>All commands use the primary dispatcher</span></footer>
            </motion.section>
          </motion.div>
        )}
      </AnimatePresence>

      <Modal state={{ ...dialog, action: confirmDialog }} onClose={closeDialog} busy={dialogBusy} />
      <ToastStack messages={toasts} dismiss={(id) => setToasts((current) => current.filter((item) => item.id !== id))} />
    </div>
    <BootGate open={bootOpen} progress={bootProgress} locale={appearance.locale} productTitle={productTitle} productShortName={productShortName} productTagline={__PRODUCT_TAGLINE__} onEnter={enterApp} />
    <HotkeyHelp open={hotkeyHelp} locale={appearance.locale} onClose={() => setHotkeyHelp(false)} />
    </MotionConfig>
  )
}

function eventToneForToast(event: ControllerEvent): ToastMessage['tone'] {
  const kind = event.kind.toLowerCase()
  if (/error|fault|hot/.test(kind)) return 'danger'
  if (/warning|door|disconnect/.test(kind)) return 'warning'
  if (/connect|ready|complete/.test(kind)) return 'success'
  return 'info'
}

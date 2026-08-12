import {
  type ComponentType,
  type LazyExoticComponent,
  lazy,
  Suspense,
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
  PackageOpen,
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
import { createAudioEngine, type AudioCue, type AudioEngine } from './audio-engine'
import { BoardSettingsReadGate, boardSettingsGeneration } from './board-settings-read'
import { BootGate, Button, HotkeyHelp, Icon, KeyCombo, Modal, NavButton, PageTransition, StatusBadge, ToastStack } from './components'
import { connectStream, execute, getSnapshot, getToken, getUIConfig, rpc, setToken as storeToken } from './api'
import {
  adjacentPageHotkey,
  ignoresGlobalHotkeys,
  isFreshAppAction,
  pageFromAppAction,
  pageFromGoChord,
  pageFromNumberHotkey,
  type PageID,
} from './hotkeys'
import { formatClock, formatConnectionState, localizeDigits, translator, type MessageKey } from './i18n'
import { redactSensitiveCommand, shellArgument } from './command-line'
import { effectiveProductTitle, productMark } from './product-identity'
import { controllerFaviconState, updateRuntimeFavicon } from './state-favicon'
import {
  isSignificantControllerEvent,
  prependSignificantControllerEvent,
  significantControllerEvents,
} from './significant-events'
import { embeddedResourcesMismatch, reloadForResourceMismatch } from './resource-version'
import { emitStartupConsoleIntroduction } from './startup-console'
import {
  createTabChannel,
  type TabChannel,
  type TerminalEntry as TabTerminalEntry,
} from './tab-channel'
import { controllerChannelOrigin } from './transport-config'
import { matchesAppTarget } from './instance-routing'
import type {
  Appearance,
  BoardSettingsReadState,
  ControllerEvent,
  DialogState,
  HostUISettings,
  HistorySample,
  MetricSample,
  Snapshot,
  ToastMessage,
  UIConfig,
} from './types'
import { applyPushedOutputEvent } from './status-led-event'
import type { BuzzerPath } from './buzzer-routing'
import { emptySnapshot } from './types'
import type { SharedViewProps } from './views'

const DashboardPage = lazy(() => import('./views').then(({ DashboardView }) => ({ default: DashboardView })))
const ControlsPage = lazy(() => import('./views').then(({ ControlsView }) => ({ default: ControlsView })))
const LocalDevicePage = lazy(() => import('./views').then(({ LocalDeviceView }) => ({ default: LocalDeviceView })))
const EventsPage = lazy(() => import('./views').then(({ EventsView }) => ({ default: EventsView })))
const SettingsPage = lazy(() => import('./views').then(({ SettingsView }) => ({ default: SettingsView })))
const WorkbenchPage = lazy(() => import('./workbench').then(({ WorkbenchView }) => ({ default: WorkbenchView })))
const DataWorkspacePage = lazy(() => import('./data-workspace').then(({ DataWorkspaceView }) => ({ default: DataWorkspaceView })))
const UpdatesPage = lazy(() => import('./updates-view').then(({ UpdatesView }) => ({ default: UpdatesView })))

interface NavDefinition {
  id: PageID
  label: MessageKey
  icon: typeof LayoutDashboard
  view: LazyExoticComponent<ComponentType<any>>
  group?: 'core' | 'integrations' | 'system'
}

export interface RelayedTerminalEntry extends TabTerminalEntry {
  id: string
  tabId: string
}

export const navigation: NavDefinition[] = [
  { id: 'dashboard', label: 'dashboard', icon: LayoutDashboard, view: DashboardPage, group: 'core' },
  { id: 'controls', label: 'controls', icon: Gauge, view: ControlsPage, group: 'core' },
  { id: 'workbench', label: 'workbench', icon: Wrench, view: WorkbenchPage, group: 'core' },
  { id: 'device', label: 'device', icon: Lightbulb, view: LocalDevicePage, group: 'integrations' },
  { id: 'data', label: 'data', icon: Boxes, view: DataWorkspacePage, group: 'integrations' },
  { id: 'updates', label: 'updates', icon: PackageOpen, view: UpdatesPage, group: 'system' },
  { id: 'events', label: 'events', icon: Activity, view: EventsPage, group: 'system' },
  { id: 'settings', label: 'settings', icon: Settings, view: SettingsPage, group: 'system' },
]

export function pageViewFor(page: PageID): LazyExoticComponent<ComponentType<any>> {
  return navigation.find((item) => item.id === page)?.view ?? DashboardPage
}

export function shouldOpenSetup(config: Pick<UIConfig, 'setup_complete'> | null, demo = false): boolean {
  return demo || (config !== null && !config.setup_complete)
}

const defaultAppearance: Appearance = {
  theme: 'system',
  locale: 'en',
  direction: 'auto',
  reduceMotion: false,
  compactNumbers: false,
  audioMuted: false,
  audioVolume: 0.42,
}

const appearanceStorageKey = `${__PRODUCT_PROTOCOL__}.appearance`

function loadAppearance(): Appearance {
  try {
    const saved = JSON.parse(localStorage.getItem(appearanceStorageKey) ?? '{}') as Partial<Appearance>
    return normalizeAppearance(saved)
  } catch { return defaultAppearance }
}

export function normalizeAppearance(value: Partial<Appearance>, fallback: Appearance = defaultAppearance): Appearance {
  return {
    theme: value.theme === 'light' || value.theme === 'dark' || value.theme === 'system' ? value.theme : fallback.theme,
    locale: value.locale === 'fa' || value.locale === 'en' ? value.locale : fallback.locale,
    direction: value.direction === 'ltr' || value.direction === 'rtl' || value.direction === 'auto' ? value.direction : fallback.direction,
    reduceMotion: typeof value.reduceMotion === 'boolean' ? value.reduceMotion : fallback.reduceMotion,
    compactNumbers: typeof value.compactNumbers === 'boolean' ? value.compactNumbers : fallback.compactNumbers,
    audioMuted: typeof value.audioMuted === 'boolean' ? value.audioMuted : fallback.audioMuted,
    audioVolume: Number.isFinite(value.audioVolume) ? Math.max(0, Math.min(1, Number(value.audioVolume))) : fallback.audioVolume,
  }
}

function sameAppearance(left: Appearance, right: Appearance): boolean {
  return left.theme === right.theme && left.locale === right.locale && left.direction === right.direction &&
    left.reduceMotion === right.reduceMotion && left.compactNumbers === right.compactNumbers &&
    left.audioMuted === right.audioMuted && left.audioVolume === right.audioVolume
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
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#0b0a0e' : '#f5f3f6')
}

export function pageFromHash(hash: string): PageID {
  const value = hash.replace(/^#\/?/, '').split(/[/?#]/)[0] as PageID
  return navigation.some((item) => item.id === value) ? value : 'dashboard'
}

export function canonicalPageHash(page: PageID): string {
  return `#/${page}`
}

export function canonicalPageURL(page: PageID, pathname = location.pathname, search = location.search): string {
  return `${pathname}${search}${canonicalPageHash(page)}`
}

function pageFromLocation(): PageID {
  return pageFromHash(location.hash)
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

function samplesFromHistory(history: HistorySample[]): MetricSample[] {
  return history
    .filter((sample): sample is HistorySample & { status: Snapshot['status'] } => Boolean(sample.status))
    .map((sample) => sampleFrom(
      { ...emptySnapshot, status: sample.status },
      sample.time ? new Date(sample.time).getTime() : Date.now(),
    ))
    .filter((sample) => Number.isFinite(sample.at))
    .slice(-360)
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
      pwm_available: true,
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

export function snapshotAfterTransportLoss(
  current: Snapshot,
  state: 'connecting' | 'waiting' | 'closed',
  detail = '',
): Snapshot {
  return {
    ...current,
    connected: false,
    connection_state: current.paused ? 'paused' : state === 'connecting' ? 'connecting' : 'disconnected',
    connection_reason: detail || (state === 'connecting' ? 'Re-establishing the host event stream' : 'Host event stream unavailable'),
  }
}

export function isCompletedHostUpdate(event: Pick<ControllerEvent, 'kind' | 'metadata'>): boolean {
  return event.kind.toLowerCase() === 'update.completed' && event.metadata?.kind === 'host'
}

export function shouldNavigateToUpdates(
  event: Pick<ControllerEvent, 'kind' | 'time'>,
  currentPage: PageID,
  now = Date.now(),
): boolean {
  return currentPage !== 'updates' && event.kind.toLowerCase().startsWith('update.') &&
    isFreshAppAction(event.time, now)
}

export function connectionTransitionCue(
  previous: boolean | null,
  connected: boolean,
  startupResolved = true,
  demonstration = false,
): AudioCue | null {
  if (demonstration || !startupResolved || previous === null || previous === connected) return null
  return connected ? 'connect' : 'disconnect'
}

type ResourceConvergenceState = 'current' | 'reloading' | 'blocked'

export default function App() {
  const demo = new URLSearchParams(location.search).get('demo') === '1'
  const [appearance, setAppearance] = useState(loadAppearance)
  const [page, setPage] = useState<PageID>(pageFromLocation)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileNav, setMobileNav] = useState(false)
  const [snapshot, setSnapshot] = useState<Snapshot>(demo ? demoSnapshot() : emptySnapshot)
  const [samples, setSamples] = useState<MetricSample[]>(() => demo ? Array.from({ length: 48 }, (_, index) => sampleFrom(demoSnapshot(Date.now() - (47 - index) * 1000), Date.now() - (47 - index) * 1000)) : [])
  const [events, setEvents] = useState<ControllerEvent[]>(() => demo ? Array.from({ length: 12 }, (_, index) => demoEvent(index + 1)) : [])
  const [boardSettingsReadState, setBoardSettingsReadState] = useState<BoardSettingsReadState>(demo ? 'ready' : 'idle')
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
  const [bootOpen, setBootOpen] = useState(demo)
  const [bootResolved, setBootResolved] = useState(demo)
  const [bootProgress, setBootProgress] = useState(12)
  const [bootTarget, setBootTarget] = useState(demo ? 100 : 24)
  const [startupProbeResolved, setStartupProbeResolved] = useState(demo)
  const [token, setTokenState] = useState(getToken)
  const [tabBusSupported, setTabBusSupported] = useState(false)
  const [tabPeers, setTabPeers] = useState(0)
  const [appInstanceID, setAppInstanceID] = useState('')
  const [relayedTerminal, setRelayedTerminal] = useState<RelayedTerminalEntry[]>([])
  const toastID = useRef(0)
  const goChordUntil = useRef(0)
  const audioRef = useRef<AudioEngine | null>(null)
  const previousAudioConnection = useRef<boolean | null>(null)
  const tabChannelRef = useRef<TabChannel | null>(null)
  const appearanceETagRef = useRef('')
  const appearanceDesiredRef = useRef(appearance)
  const appearanceSaveChain = useRef<Promise<void>>(Promise.resolve())
  const refreshAfterHostRestart = useRef(false)
  const resourceProbeInFlight = useRef<Promise<ResourceConvergenceState> | null>(null)
  const resourceHintAllowedAt = useRef(0)
  const startupConsoleShown = useRef(false)
  const pageRef = useRef(page)
  const boardSettingsReadGate = useRef(new BoardSettingsReadGate())
  const boardSettingsRequestGeneration = useRef('')
  const t = useMemo(() => translator(appearance.locale), [appearance.locale])
  const productTitle = effectiveProductTitle(uiConfig?.name, __PRODUCT_NAME__)
  const productShortName = productMark(productTitle, __PRODUCT_SHORT_NAME__)
	const productTagline = uiConfig?.tagline?.trim() || __PRODUCT_TAGLINE__
  const resolvedDirection = appearance.direction === 'auto'
    ? appearance.locale === 'fa' ? 'rtl' : 'ltr'
    : appearance.direction
  const drawerClosedOffset = resolvedDirection === 'rtl' ? '18px' : '-18px'

  const applyLocalAppearance = useCallback((value: Appearance) => {
    setAppearance(value)
    try { localStorage.setItem(appearanceStorageKey, JSON.stringify(value)) } catch { /* private storage may be unavailable */ }
    applyAppearance(value)
    audioRef.current?.setVolume(value.audioVolume)
    audioRef.current?.setMuted(value.audioMuted)
  }, [])

  const adoptHostAppearance = useCallback((value: Appearance, etag: string) => {
    const authoritative = normalizeAppearance(value)
    appearanceETagRef.current = etag
    appearanceDesiredRef.current = authoritative
    applyLocalAppearance(authoritative)
  }, [applyLocalAppearance])

  const applyAuthoritativeUIConfig = useCallback((config: UIConfig, announceMismatch: boolean): ResourceConvergenceState => {
    const mismatch = embeddedResourcesMismatch(config)
    const reloading = reloadForResourceMismatch(config, {
      beforeReload: announceMismatch
        ? () => { tabChannelRef.current?.publishResourceVersion(config.host_version ?? '', config.build_time ?? '') }
        : undefined,
    })
    if (mismatch) return reloading ? 'reloading' : 'blocked'
    setUIConfig(config)
    adoptHostAppearance(config.appearance, config.appearance_etag)
    return 'current'
  }, [adoptHostAppearance])

  const refreshHostAppearance = useCallback(async () => {
    const config = await getUIConfig()
    const convergence = applyAuthoritativeUIConfig(config, true)
    if (convergence === 'blocked') {
      throw new Error('The host WebUI bundle changed, but this tab already used its safe reload attempt for that bundle')
    }
    return config
  }, [applyAuthoritativeUIConfig])

  const reconcileHostResources = useCallback((announceMismatch: boolean): Promise<ResourceConvergenceState> => {
    const active = resourceProbeInFlight.current
    if (active !== null) return active
    let probe: Promise<ResourceConvergenceState>
    probe = getUIConfig()
      .then((config) => applyAuthoritativeUIConfig(config, announceMismatch))
      .finally(() => {
        if (resourceProbeInFlight.current === probe) resourceProbeInFlight.current = null
      })
    resourceProbeInFlight.current = probe
    return probe
  }, [applyAuthoritativeUIConfig])

  useEffect(() => {
	const pageTitle = t(navigation.find((item) => item.id === page)?.label ?? 'dashboard')
	document.title = `${productTitle} — ${pageTitle}`
	}, [page, productTitle, t])

  useEffect(() => {
    updateRuntimeFavicon(demo ? 'offline' : controllerFaviconState(snapshot))
  }, [demo, snapshot.connected, snapshot.connection_reason, snapshot.connection_state, snapshot.have_status, snapshot.status.hot])

  useEffect(() => {
    if (!startupProbeResolved || startupConsoleShown.current) return
    startupConsoleShown.current = true
    emitStartupConsoleIntroduction({
      productTitle,
      config: uiConfig,
      boardConnected: !demo && snapshot.connected,
      port: snapshot.port.name,
      streamState,
      demonstration: demo,
    })
  }, [demo, productTitle, snapshot.connected, snapshot.port.name, startupProbeResolved, streamState, uiConfig])

  useEffect(() => { pageRef.current = page }, [page])

  useEffect(() => {
    const engine = createAudioEngine({
      muted: appearance.audioMuted,
      volume: appearance.audioVolume,
      pauseWhenHidden: true,
    })
    audioRef.current = engine
    // Returning users do not see the first-run gate. Unlock Web Audio on their
    // first real gesture without playing anything merely because the page
    // loaded; the gesture's eventual action supplies the first cue.
    const unlock = () => { void engine.start() }
    window.addEventListener('pointerdown', unlock, { capture: true, once: true })
    window.addEventListener('keydown', unlock, { capture: true, once: true })
    return () => {
      window.removeEventListener('pointerdown', unlock, { capture: true })
      window.removeEventListener('keydown', unlock, { capture: true })
      if (audioRef.current === engine) audioRef.current = null
      void engine.dispose()
    }
    // AudioContext creation remains gesture-gated inside engine.start().
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (demo || !startupProbeResolved) return
    const previous = previousAudioConnection.current
    previousAudioConnection.current = snapshot.connected
    // Establishing the initial truth is intentionally silent. Only a later,
    // real connection transition receives feedback, and cue() remains muted
    // until the user has explicitly unlocked audio.
    const cue = connectionTransitionCue(previous, snapshot.connected, startupProbeResolved, demo)
    if (cue) audioRef.current?.cue(cue)
  }, [demo, snapshot.connected, startupProbeResolved])

  useEffect(() => {
    const channel = createTabChannel({ origin: controllerChannelOrigin() })
    const peers = new Map<string, number>()
    tabChannelRef.current = channel
    setAppInstanceID(channel.tabId)
    setTabBusSupported(channel.supported)

    const updatePeers = () => {
      const cutoff = Date.now() - 40_000
      for (const [tabID, seenAt] of peers) if (seenAt < cutoff) peers.delete(tabID)
      setTabPeers(peers.size)
    }
    const unsubscribe = channel.subscribe((message) => {
      const payload = message.payload
      if (payload.type === 'presence') {
        if (payload.state === 'leaving') peers.delete(message.tabId)
        else peers.set(message.tabId, message.sentAt)
        updatePeers()
        return
      }

      peers.set(message.tabId, message.sentAt)
      updatePeers()
      if (payload.type === 'appearance') {
        if (!payload.etag || payload.etag !== appearanceETagRef.current) {
          void refreshHostAppearance().catch(() => undefined)
        }
      }
      if (payload.type === 'terminal') {
        setRelayedTerminal((current) => [
          ...current.filter((entry) => entry.id !== message.messageId).slice(-119),
          { id: message.messageId, tabId: message.tabId, ...payload.entry },
        ])
      }
      if (payload.type === 'resource-version') {
        const now = Date.now()
        if (now < resourceHintAllowedAt.current) return
        resourceHintAllowedAt.current = now + 2_000
        // BroadcastChannel is only a prompt. The no-store host response is the
        // sole authority allowed to trigger a reload.
        void reconcileHostResources(false).then((convergence) => {
          if (convergence === 'blocked') {
            setStreamDetail('The host WebUI bundle changed, but this tab already used its safe reload attempt for that bundle')
          }
        }).catch(() => undefined)
        return
      }
      if (payload.type === 'controller-event') {
        const event = payload.event as ControllerEvent
        setEvents((current) => prependSignificantControllerEvent(current, event))
        if (isCompletedHostUpdate(event)) refreshAfterHostRestart.current = true
      }
    })
    const announce = () => channel.publishPresence(document.hidden ? 'hidden' : 'active', pageRef.current)
    const onVisibility = () => announce()
    announce()
    document.addEventListener('visibilitychange', onVisibility)
    const heartbeat = window.setInterval(() => { announce(); updatePeers() }, 12_000)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.clearInterval(heartbeat)
      channel.publishPresence('leaving', pageRef.current)
      unsubscribe()
      channel.close()
      if (tabChannelRef.current === channel) tabChannelRef.current = null
      setAppInstanceID((current) => current === channel.tabId ? '' : current)
    }
  }, [reconcileHostResources, refreshHostAppearance])

  useEffect(() => {
    if (demo || !startupProbeResolved || !appInstanceID) return
    const report = (state = document.hidden ? 'hidden' : 'active') => {
      void rpc('controller.app.instance.report', {
        id: appInstanceID,
        surface: 'webui',
        page,
        state,
        lease_seconds: 45,
        self: {
          kind: 'browser',
          vars: {
            origin: window.location.origin,
            platform: navigator.platform || 'unknown',
            language: navigator.language || 'unknown',
            user_agent: navigator.userAgent,
          },
        },
        values: {
          color_mode: appearance.theme,
          locale: appearance.locale,
          direction: resolvedDirection,
        },
      }).catch(() => undefined)
    }
    const onVisibility = () => report()
    report()
    document.addEventListener('visibilitychange', onVisibility)
    // This is a bounded presence lease, not board-state polling. Board and UI
    // state continue to arrive through pushed events.
    const leaseRefresh = window.setInterval(() => report(), 30_000)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.clearInterval(leaseRefresh)
    }
  }, [appInstanceID, appearance.locale, appearance.theme, demo, page, resolvedDirection, startupProbeResolved, token])

  useEffect(() => () => {
    if (!appInstanceID) return
    void rpc('controller.app.instance.report', {
      id: appInstanceID, surface: 'webui', page: pageRef.current,
      state: 'leaving', lease_seconds: 45,
    }).catch(() => undefined)
  }, [appInstanceID])

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
    if (tone === 'danger') audioRef.current?.cue('error')
    if (tone === 'warning') audioRef.current?.cue('warning')
    if (tone === 'success') audioRef.current?.cue('success')
    window.setTimeout(() => setToasts((current) => current.filter((item) => item.id !== id)), 5200)
  }, [])

  useEffect(() => {
    const testFeedback = () => {
      const engine = audioRef.current
      notify('success', appearance.locale === 'fa' ? 'اعلان آزمایشی' : 'Test notification', appearance.locale === 'fa' ? 'صدا، لرزش و اعلان آماده‌اند.' : 'Audio, haptics, and notifications are ready.')
      if (engine) void engine.start().then((started) => { if (started) engine.cue('success') })
    }
    window.addEventListener('pccontroller:test-feedback', testFeedback)
    return () => window.removeEventListener('pccontroller:test-feedback', testFeedback)
  }, [appearance.locale, notify])

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

  useEffect(() => {
    const shouldRead = boardSettingsReadGate.current.shouldRead(snapshot, page === 'settings')
    if (!snapshot.connected) {
      boardSettingsRequestGeneration.current = ''
      setBoardSettingsReadState('idle')
      return
    }
    if (snapshot.have_settings) {
      setBoardSettingsReadState('ready')
      return
    }
    if (!shouldRead) return

    setBoardSettingsReadState('loading')
    const generation = boardSettingsGeneration(snapshot)
    boardSettingsRequestGeneration.current = generation
    void execute('settings')
      .then(() => refresh())
      .then(() => {
        if (boardSettingsRequestGeneration.current === generation) setBoardSettingsReadState('unavailable')
      })
      .catch(() => {
        if (boardSettingsRequestGeneration.current === generation) setBoardSettingsReadState('unavailable')
      })
  }, [
    page,
    refresh,
    snapshot.connected,
    snapshot.have_settings,
    snapshot.hello.build_hash,
    snapshot.hello.build_timestamp,
    snapshot.port.instance_id,
    snapshot.port.name,
    snapshot.port.serial_number,
  ])

  const dispatchCommand = useCallback(async (command: string, success?: string): Promise<string> => {
    const safeCommand = redactSensitiveCommand(command)
    tabChannelRef.current?.publishTerminal({ kind: 'command', text: `pc› ${safeCommand}`, at: Date.now() })
    if (demo) {
      const output = `[demo] ${safeCommand}`
      tabChannelRef.current?.publishTerminal({ kind: 'output', text: output, at: Date.now() })
      notify('info', success || 'Demonstration command', output)
      return output
    }
    try {
      const result = await execute(command)
      const output = result.output ?? ''
      tabChannelRef.current?.publishTerminal({ kind: 'output', text: output || '✓ accepted', at: Date.now() })
      notify('success', success || 'Command completed', output || safeCommand)
      void refresh()
      return output
    } catch (cause) {
      const detail = cause instanceof Error ? cause.message : String(cause)
      tabChannelRef.current?.publishTerminal({ kind: 'error', text: `! ${detail}`, at: Date.now() })
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
    const nextHash = canonicalPageHash(value)
    if (pageRef.current !== value || location.hash !== nextHash) {
      history.pushState({ page: value }, '', canonicalPageURL(value))
    }
    pageRef.current = value
    setPage(value)
    setMobileNav(false)
    tabChannelRef.current?.publishPresence(document.hidden ? 'hidden' : 'active', value)
    document.querySelector('.app-main')?.scrollTo({ top: 0, behavior: appearance.reduceMotion ? 'auto' : 'smooth' })
  }, [appearance.reduceMotion])

  const saveAppearance = useCallback((value: Appearance) => {
    const safeValue = normalizeAppearance(value, appearanceDesiredRef.current)
    appearanceDesiredRef.current = safeValue
    applyLocalAppearance(safeValue)
    const engine = audioRef.current
    if (!safeValue.audioMuted && engine) {
      void engine.start({ muted: false }).then((started) => {
        if (started) engine.cue('select')
      })
    }
    if (demo) {
      tabChannelRef.current?.publishAppearance(safeValue)
      return
    }
    appearanceSaveChain.current = appearanceSaveChain.current.then(async () => {
      const saved = await rpc<HostUISettings>('controller.ui.config.set', {
        appearance: safeValue,
        if_match: appearanceETagRef.current,
      })
      appearanceETagRef.current = saved.appearance_etag
      setUIConfig((current) => current ? {
        ...current, appearance: saved.appearance, appearance_etag: saved.appearance_etag,
      } : current)
      if (sameAppearance(appearanceDesiredRef.current, safeValue)) {
        adoptHostAppearance(saved.appearance, saved.appearance_etag)
      }
      tabChannelRef.current?.publishAppearance(saved.appearance, saved.appearance_etag)
    }).catch(async (cause) => {
      try { await refreshHostAppearance() } catch { /* retain the last locally rendered value until the host is reachable */ }
      notify('warning', appearance.locale === 'fa' ? 'تنظیم ظاهر ذخیره نشد' : 'Appearance was not saved', cause instanceof Error ? cause.message : String(cause))
    })
  }, [adoptHostAppearance, appearance.locale, applyLocalAppearance, demo, notify, refreshHostAppearance])

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
    if (demo || uiConfig?.setup_complete) {
      setBootOpen(false)
      return
    }
    if (!uiConfig) {
      notify('warning', 'Setup state is unavailable', 'The current host configuration has not loaded yet.')
      return
    }
    void rpc<HostUISettings>('controller.ui.config.set', { setup_complete: true })
      .then((saved) => {
        setUIConfig((current) => current ? {
          ...current,
          name: saved.app_title,
		  tagline: saved.tagline,
          setup_complete: saved.setup_complete,
          welcome_melody: saved.welcome_melody,
        } : current)
      })
      .catch((cause) => notify('warning', 'Setup state was not saved', cause instanceof Error ? cause.message : String(cause)))
      .finally(() => setBootOpen(false))
  }, [appearance, demo, notify, saveAppearance, uiConfig?.setup_complete])

  const saveAppTitle = useCallback(async (value: string): Promise<string> => {
    const saved = await rpc<HostUISettings>('controller.ui.config.set', { app_title: value.trim() })
    setUIConfig((current) => current ? {
      ...current,
      name: saved.app_title,
	  tagline: saved.tagline,
      setup_complete: saved.setup_complete,
      welcome_melody: saved.welcome_melody,
    } : current)
    notify('success', 'Application title saved', saved.app_title)
    return saved.app_title
  }, [notify])

  const setBuzzerPath = useCallback(async (value: BuzzerPath) => {
    await runCommand(`buzzer path ${value}`, 'Buzzer routing updated')
    await refreshHostAppearance()
  }, [refreshHostAppearance, runCommand])

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
    const initialPage = pageFromLocation()
    if (location.hash !== canonicalPageHash(initialPage)) {
      history.replaceState(history.state, '', canonicalPageURL(initialPage))
    }
    const syncFromHistory = () => {
      const next = pageFromLocation()
      pageRef.current = next
      setPage(next)
      setMobileNav(false)
    }
    window.addEventListener('hashchange', syncFromHistory)
    window.addEventListener('popstate', syncFromHistory)
    return () => {
      window.removeEventListener('hashchange', syncFromHistory)
      window.removeEventListener('popstate', syncFromHistory)
    }
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
		const startupConvergence = applyAuthoritativeUIConfig(config, true)
		if (startupConvergence === 'reloading') return
		if (startupConvergence === 'blocked') {
			throw new Error('The host WebUI bundle changed, but this tab already used its safe reload attempt for that bundle')
		}
        const firstSetup = shouldOpenSetup(config)
        setBootOpen(firstSetup)
        setBootResolved(true)
        setBootTarget(70)
        const value = await getSnapshot(abort.signal)
        setSnapshot(value)
        setStartupProbeResolved(true)
        if (value.have_status) setSamples([sampleFrom(value)])
        const since = new Date(Date.now() - 60 * 60_000).toISOString()
        const [statusHistory, eventHistory] = await Promise.allSettled([
          rpc<HistorySample[]>('controller.history.status', { since }, abort.signal),
          rpc<ControllerEvent[]>('controller.history.timeline', { since, limit: 500 }, abort.signal),
        ])
        if (statusHistory.status === 'fulfilled') {
          const historical = samplesFromHistory(statusHistory.value)
          setSamples((current) => [...historical, ...current].slice(-360))
        }
        if (eventHistory.status === 'fulfilled') {
          setEvents(significantControllerEvents(eventHistory.value).slice(-500).reverse())
        }
        setBootTarget(92)
        if (firstSetup && value.connected && config.welcome_melody?.trim()) {
          try {
            await execute(`melody wait ${shellArgument(config.welcome_melody.trim())}`, abort.signal)
          } catch (cause) {
            setStreamDetail(`Welcome melody unavailable: ${cause instanceof Error ? cause.message : String(cause)}`)
          }
        }
        stopStream = connectStream(config, {
          status: (update) => {
            if (update.error) { setStreamDetail(update.error); return }
            // A successful sample supersedes any transient controller error.
            // Keeping the old detail made the live badge expose stale offline
            // text through its tooltip after the transport had recovered.
            setStreamDetail('')
            setSnapshot((current) => ({ ...current, connected: true, have_status: true, status: update.status, status_updated: update.time }))
            setSamples((current) => [...current.slice(-71), sampleFrom({ ...emptySnapshot, status: update.status }, new Date(update.time).getTime())])
          },
          event: (event) => {
			const eventKind = event.kind.toLowerCase()
			if (event.kind.toLowerCase() === 'status_led.changed' || event.kind.toLowerCase() === 'front_panel.segment') {
				setSnapshot((current) => applyPushedOutputEvent(current, event))
			}
						if (config.integrations?.buzzer_web_audio && event.kind.toLowerCase() === 'buzzer.note') {
							const frequencyHz = Number(event.metadata?.frequency_hz)
							const durationMS = Number(event.metadata?.duration_ms)
							audioRef.current?.playTone(frequencyHz, durationMS)
						}
            if (isSignificantControllerEvent(event)) {
              setEvents((current) => prependSignificantControllerEvent(current, event))
              tabChannelRef.current?.publishControllerEvent(event)
            }
            if (event.kind.toLowerCase() === 'app.page' && isFreshAppAction(event.time) &&
                matchesAppTarget(event.metadata?.target_instance, appInstanceID, 'webui')) {
              const destination = pageFromAppAction(event.metadata?.page ?? event.metadata?.value ?? event.text)
              if (destination) {
                navigate(destination)
                audioRef.current?.cue('navigation', 'forward')
              }
            }
			if (shouldNavigateToUpdates(event, pageRef.current)) {
				navigate('updates')
				audioRef.current?.cue('navigation', 'forward')
			}
            if (/error|warning|hot|door/i.test(event.kind)) notify(eventToneForToast(event), event.kind, event.text)
            if (isCompletedHostUpdate(event)) {
              refreshAfterHostRestart.current = true
            }
            if (/config/i.test(event.kind)) void refreshHostAppearance().catch(() => undefined)
            if (/device|connection|settings/i.test(event.kind)) void refresh()
          },
          state: (state, detail) => {
            setStreamState(state)
            setStreamDetail(detail ?? '')
            if (state === 'open') {
              refreshAfterHostRestart.current = false
              // Probe on every open, not only after a witnessed update event.
              // This closes the missed-event, sleeping-tab, and reconnect gap.
              void reconcileHostResources(true).then((convergence) => {
                if (convergence === 'current') {
                  void refresh()
                } else if (convergence === 'blocked') {
                  setStreamDetail('The host WebUI bundle changed, but this tab already used its safe reload attempt for that bundle')
                }
              }).catch((cause) => {
                setStreamDetail(cause instanceof Error ? cause.message : String(cause))
              })
            } else {
              setSnapshot((current) => snapshotAfterTransportLoss(current, state, detail))
            }
          },
        })
        setBootTarget(100)
      } catch (cause) {
        setStartupProbeResolved(true)
        setBootOpen(false)
        setBootResolved(true)
        setStreamState('waiting')
        setStreamDetail(cause instanceof Error ? cause.message : String(cause))
        setBootTarget(100)
      }
    })()
    return () => { abort.abort(); stopStream() }
  }, [appInstanceID, applyAuthoritativeUIConfig, demo, navigate, notify, reconcileHostResources, refresh, refreshHostAppearance, token])

  const shared: SharedViewProps = {
    appTitle: productTitle, snapshot, samples, events, locale: appearance.locale, t, command: runCommand, refresh, openDialog,
    boardSettingsReadState,
    transport: { streamState, tabBusSupported, tabPeers },
    relayedTerminal,
    broadcastTerminal: (entry) => { tabChannelRef.current?.publishTerminal(entry) },
  }

  const PageView = pageViewFor(page)
  const view = (
    <Suspense fallback={<section className="page-loading" role="status" aria-live="polite"><span className="spinner" />{appearance.locale === 'fa' ? 'در حال بارگیری…' : 'Loading page…'}</section>}>
      {page === 'settings'
        ? <PageView {...shared} appearance={appearance} onAppearance={saveAppearance} token={token} onToken={saveToken} onAppTitle={saveAppTitle} uiConfig={uiConfig} onBuzzerPath={setBuzzerPath} />
        : <PageView {...shared} />}
    </Suspense>
  )

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

  const transportTone = snapshot.connected && streamState === 'open'
    ? 'good'
    : streamState === 'connecting'
      ? 'info'
      : streamState === 'open'
        ? 'neutral'
        : 'warn'
  const controllerConnectionLabel = formatConnectionState(appearance.locale, snapshot.connection_state, snapshot.connected, snapshot.paused)
  const transportLabel = snapshot.connected
    ? streamState === 'open' ? t('live') : streamState
    : streamState === 'open'
      ? appearance.locale === 'fa' ? 'IPC متصل' : 'IPC connected'
      : t('offline')
  const quickCommands = snapshot.connected
    ? [
        ['status', `${snapshot.port.name || (appearance.locale === 'fa' ? 'کنترلر' : 'Controller')} · ${snapshot.status_updated ? formatClock(appearance.locale, snapshot.status_updated) : t('online')}`],
        ['relay off', snapshot.status.active_relays
          ? appearance.locale === 'fa'
            ? `${snapshot.status.active_relays.toString(2).replace(/0/g, '').length} خروجی فعال`
            : `${snapshot.status.active_relays.toString(2).replace(/0/g, '').length} active outputs`
          : appearance.locale === 'fa' ? 'همهٔ خروجی‌ها آزادند' : 'All outputs released'],
        ['rf list', appearance.locale === 'fa' ? 'فهرست رادیویی کنترلر' : 'Controller radio inventory'],
        ['macro list', appearance.locale === 'fa' ? 'فهرست ماکروهای کنترلر' : 'Controller macro inventory'],
      ]
    : [
        ['ports', snapshot.connection_reason || (appearance.locale === 'fa' ? 'جستجوی درگاه‌های سریال موجود' : 'Discover available serial ports')],
        ['hotkeys status', appearance.locale === 'fa' ? 'سرویس میان‌برهای سراسری' : 'Global shortcut service'],
        ['os status', appearance.locale === 'fa' ? 'وضعیت سیستم‌عامل میزبان' : 'Host operating-system state'],
      ]
  const footerTransport = appearance.locale === 'fa'
    ? `WS ${streamState === 'open' ? 'باز' : streamState === 'connecting' ? 'در حال اتصال' : streamState === 'closed' ? 'بسته' : streamState} · ${localizeDigits('fa', tabPeers + 1)} زبانه`
    : `WS ${streamState} · ${tabPeers + 1} ${tabPeers === 0 ? 'tab' : 'tabs'}`

  return (
    <MotionConfig reducedMotion={appearance.reduceMotion ? 'always' : 'user'}>
    <div
      className={`app-shell${sidebarOpen ? '' : ' is-sidebar-compact'}${bootResolved ? '' : ' is-bootstrap-pending'}`}
      inert={!bootResolved || bootOpen || hotkeyHelp ? true : undefined}
      aria-hidden={!bootResolved || bootOpen || hotkeyHelp ? true : undefined}
    >
      <aside className="sidebar" aria-label={t('primaryNavigation')}>
        <div className="brand">
          <a className="brand__mark" href="#/dashboard" aria-label={`${productTitle} ${t('dashboardLink')}`}><span aria-hidden="true">{productShortName}</span><i /><i /></a>
          <a className="brand__copy" href="#/dashboard"><strong>{productTitle}</strong><span>{productTagline}</span></a>
          <button className="sidebar-toggle" aria-label={t(sidebarOpen ? 'collapseNavigation' : 'expandNavigation')} onClick={() => setSidebarOpen((value) => !value)}>{sidebarOpen ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}</button>
        </div>

        <div className="sidebar__status">
          <span className={`status-rail status-rail--${snapshot.connected ? 'good' : 'bad'}`} aria-hidden="true" />
          <div><strong>{snapshot.connected ? t('online') : t('offline')}</strong><small>{snapshot.port.name || snapshot.connection_state}</small></div>
          <Cpu size={18} />
        </div>

        <nav className="sidebar__nav">
          {(['core', 'integrations', 'system'] as const).map((group) => (
            <div className="nav-group" key={group}>
              <span className="nav-group__label">{t(group === 'core' ? 'system' : group === 'integrations' ? 'integrations' : 'operations').toUpperCase()}</span>
              {navigation.filter((item) => item.group === group).map((item) => <NavButton key={item.id} icon={item.icon} label={t(item.label)} active={page === item.id} badge={item.id === 'events' && events.length ? String(Math.min(events.length, 99)) : undefined} onClick={() => navigate(item.id)} />)}
            </div>
          ))}
        </nav>

        <div className="sidebar__footer"><ShieldCheck size={17} /><div><strong>{controllerConnectionLabel}</strong><span>{footerTransport}</span></div></div>
      </aside>

      <header className="topbar">
        <button className="mobile-menu" aria-label={t('openNavigation')} onClick={() => setMobileNav(true)}><Menu size={20} /></button>
        <div className="breadcrumbs"><span>{productShortName}</span><i>/</i><strong>{t(current.label)}</strong></div>
        <button className="command-trigger" aria-keyshortcuts="Control+K Meta+K" onClick={() => { setPaletteIndex(0); setPalette(true) }}><Search size={16} /><span>{t('searchCommands')}</span><KeyCombo keys={[["Ctrl", "⌘"], "K"]} /></button>
        <div className="topbar__actions">
          {demo && <StatusBadge tone="warn">{t('demoMode')}</StatusBadge>}
          <span title={streamDetail || undefined}><StatusBadge tone={transportTone} pulse={streamState === 'connecting'}>{transportLabel}</StatusBadge></span>
          <button className="topbar-icon" aria-label={t('toggleTheme')} onClick={() => saveAppearance({ ...appearance, theme: (document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark') })}>{document.documentElement.dataset.theme === 'dark' ? <Sun size={18} /> : <Moon size={18} />}</button>
          <button className="topbar-icon" aria-label={t('switchLanguage')} onClick={() => saveAppearance({ ...appearance, locale: appearance.locale === 'en' ? 'fa' : 'en' })}><Languages size={18} /></button>
          <button className="topbar-icon topbar-audio" aria-label={t(appearance.audioMuted ? 'enableAudio' : 'muteAudio')} aria-pressed={appearance.audioMuted} aria-keyshortcuts="M" onClick={toggleAudio}>{appearance.audioMuted ? <VolumeX size={18} /> : <Volume2 size={18} />}</button>
          <button className="topbar-icon topbar-hotkeys" aria-label={t('keyboardShortcuts')} aria-keyshortcuts="?" onClick={() => setHotkeyHelp(true)}><Keyboard size={18} /></button>
          <button className="topbar-icon" aria-label={t('notifications')} onClick={() => navigate('events')}><Bell size={18} />{events.length > 0 && <i />}</button>
        </div>
      </header>

      <main className="app-main">
        <PageTransition pageKey={page}>{view}</PageTransition>
      </main>

      <nav className="mobile-bottom-nav" aria-label={t('mobileNavigation')}>
        {navigation.slice(0, 5).map((item) => <button key={item.id} className={page === item.id ? 'is-active' : ''} onClick={() => navigate(item.id)}><Icon icon={item.icon} size={20} /><span>{t(item.label)}</span></button>)}
      </nav>

      <AnimatePresence>
        {mobileNav && (
          <motion.div className="mobile-drawer-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <button className="mobile-drawer-backdrop" aria-label={t('dismissNavigation')} onClick={() => setMobileNav(false)} />
            <motion.aside
              className="mobile-drawer"
              initial={{ x: drawerClosedOffset, opacity: 0 }}
              animate={{ x: 0, opacity: 1 }}
              exit={{ x: drawerClosedOffset, opacity: 0 }}
              transition={{ duration: .32, ease: [0.22, 1, 0.36, 1] }}
            >
              <header><div className="brand__mark"><span>{productShortName}</span><i /><i /></div><strong>{productTitle}</strong><button type="button" aria-label={t('closeNavigation')} onClick={() => setMobileNav(false)}><X size={19} /></button></header>
              {navigation.map((item) => <NavButton key={item.id} icon={item.icon} label={t(item.label)} active={page === item.id} onClick={() => navigate(item.id)} />)}
            </motion.aside>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {palette && (
          <motion.div className="palette-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
            <button className="palette-backdrop" aria-label={t('closeCommandPalette')} onClick={() => { setPalette(false); setPaletteQuery('') }} />
            <motion.section className="palette" role="dialog" aria-modal="true" aria-label={t('commandPalette')} initial={{ opacity: 0, scale: .985 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: .99 }} transition={{ duration: .2, ease: 'easeOut' }}>
              <header><Command size={19} /><input autoFocus value={paletteQuery} placeholder={t('commandPalettePlaceholder')} aria-controls="palette-results" aria-activedescendant={filteredPalette[paletteIndex] ? `palette-page-${filteredPalette[paletteIndex].id}` : undefined} onChange={(event) => setPaletteQuery(event.target.value)} /><KeyCombo keys={["Esc"]} /></header>
              <div className="palette__results" id="palette-results" role="listbox" aria-label={t('matchingPages')}>
                <span className="palette__label">{t('pagesLabel').toUpperCase()}</span>
                {filteredPalette.map((item, index) => <button id={`palette-page-${item.id}`} role="option" aria-selected={index === paletteIndex} className={index === paletteIndex ? 'is-highlighted' : ''} key={item.id} onPointerMove={() => setPaletteIndex(index)} onClick={() => { navigate(item.id); setPalette(false); setPaletteQuery('') }}><Icon icon={item.icon} size={18} /><div><strong>{t(item.label)}</strong><small>{page === item.id ? (appearance.locale === 'fa' ? 'صفحه فعلی' : 'Current page') : (appearance.locale === 'fa' ? 'باز کردن' : 'Open')}</small></div><Check className={page === item.id ? 'is-visible' : ''} size={16} /></button>)}
                <span className="palette__label">{t('quickCommandsLabel').toUpperCase()}</span>
                {quickCommands.filter(([value, label]) => `${value} ${label}`.toLowerCase().includes(paletteQuery.toLowerCase())).map(([value, label]) => <button role="option" aria-selected="false" key={value} onClick={() => { void runCommand(value); setPalette(false) }}><Box size={18} /><div><strong dir="ltr">{value}</strong><small>{label}</small></div></button>)}
              </div>
              <footer><span><KeyCombo keys={[["↑", "↓"]]} /> {t('navigate')}</span><span><KeyCombo keys={["Enter"]} /> {t('select')}</span><span>{filteredPalette.length + quickCommands.length} {t('actions')} · WS {streamState}</span></footer>
            </motion.section>
          </motion.div>
        )}
      </AnimatePresence>

      <Modal state={{ ...dialog, action: confirmDialog }} onClose={closeDialog} busy={dialogBusy} />
      <ToastStack messages={toasts} dismiss={(id) => setToasts((current) => current.filter((item) => item.id !== id))} />
    </div>
    <BootGate open={bootResolved && bootOpen} progress={bootProgress} locale={appearance.locale} productTitle={productTitle} productShortName={productShortName} productTagline={productTagline} onEnter={enterApp} />
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

import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'
import { BootGate, Card, HoldActionButton, HotkeyHelp, RangeField, TextField } from './components'
import type { Appearance } from './types'
import { emptySnapshot } from './types'
import { artifactUpdateAvailable, updateStateIsFresh, updateStatusFromEvent, UpdatesView } from './updates-view'
import {
  ControlsView,
  DashboardView,
  dashboardDeviceSummary,
  dashboardRelayToggleCommand,
  dashboardSocketIsFresh,
  LocalDeviceView,
  SettingsView,
  localDeviceControlsAvailable,
  localDeviceReconnectAvailable,
  localDeviceSnapshotIsFresh,
  type SharedViewProps,
} from './views'

const appearance: Appearance = {
  theme: 'dark',
  locale: 'en',
  direction: 'ltr',
  reduceMotion: false,
  compactNumbers: false,
  audioMuted: true,
  audioVolume: 0.35,
}

function shared(): SharedViewProps {
  return {
    appTitle: 'PCController',
    snapshot: emptySnapshot,
    samples: [],
    events: [],
    macroEvents: [],
    locale: 'en',
    t: (key) => key,
    command: vi.fn(async () => ''),
    relayToggle: vi.fn(async () => undefined),
    relayPending: new Set(),
    refresh: vi.fn(async () => undefined),
    openDialog: vi.fn(),
    transport: { streamState: 'open', tabBusSupported: true, tabPeers: 0 },
    relayedTerminal: [],
    broadcastTerminal: vi.fn(),
    boardSettingsReadState: 'idle',
    openAppPreferences: vi.fn(),
  }
}

describe('offline and settings UI contracts', () => {
  it('keeps neutral field guidance contextual while validation feedback stays visible', () => {
    const neutral = renderToStaticMarkup(<TextField label="Address" hint="Use a private service root" />)
    const invalid = renderToStaticMarkup(<TextField label="Address" hint="Use a private service root" error="Address is invalid" />)
    expect(neutral).toContain('text-field__message--contextual')
    expect(invalid).not.toContain('text-field__message--contextual')
    expect(invalid).toContain('role="alert"')
  })

  it('does not render controller-only controls while disconnected', () => {
    const markup = renderToStaticMarkup(<ControlsView {...shared()} />)
    expect(markup).toContain('Controller controls are unavailable')
    expect(markup).not.toContain('PWM matrix')
    expect(markup).not.toContain('Relays &amp; motion')
    expect(markup).not.toContain('Status lighting')
  })

  it('uses one real relay switch button so nested lamp clicks have one optimistic route', () => {
    const markup = renderToStaticMarkup(<ControlsView {...shared()} snapshot={{ ...emptySnapshot, connected: true, have_status: true }} />)
    expect(markup).toContain('relay-switch__toggle')
    expect(markup).toContain('data-relay="1"')
    expect(markup).toContain('<i aria-hidden="true"><b></b></i>')
  })

  it('renders only user PWM channels in the generic mixer and keeps system channels role-specific', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      status: { ...emptySnapshot.status, pwm_available: true, pwm_channel: 2, pwm_value: 1024 },
    }
    const markup = renderToStaticMarkup(<ControlsView {...shared()} snapshot={connected} />)
    expect((markup.match(/pwm-mixer__row/g) ?? []).length).toBe(11)
    expect(markup).toContain('11 user channels')
    expect(markup).toContain('Role-specific system PWM channels')
    expect(markup).not.toContain('Enclosure light duty')
    expect(markup).not.toContain('Command console')
    expect(markup).not.toContain('Controller command')
  })

  it('hides board telemetry entirely while the controller is disconnected', () => {
    const markup = renderToStaticMarkup(<DashboardView {...shared()} />)
    expect(markup).not.toContain('Telemetry history')
    expect(markup).not.toMatch(/\bLive\b/)
  })

  it('only treats an open, recent event stream as live and derives relay toggle commands directly', () => {
    const now = Date.now()
    const connected = { ...emptySnapshot, connected: true, status_updated: new Date(now - 300).toISOString() }
    expect(dashboardSocketIsFresh(connected, 'open', now)).toBe(true)
    expect(dashboardSocketIsFresh(connected, 'waiting', now)).toBe(false)
    expect(dashboardSocketIsFresh({ ...connected, status_updated: new Date(now - 1200).toISOString() }, 'open', now)).toBe(false)
    expect(dashboardRelayToggleCommand(3, false)).toBe('relay 3 on')
    expect(dashboardRelayToggleCommand(3, true)).toBe('relay 3 off')
  })

  it('hides manual refresh only for a fresh open stream and labels the direct relay action', () => {
    const liveSnapshot = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      status_updated: new Date().toISOString(),
    }
    const live = renderToStaticMarkup(<DashboardView {...shared()} snapshot={liveSnapshot} />)
    const stale = renderToStaticMarkup(<DashboardView {...shared()} snapshot={{ ...liveSnapshot, status_updated: new Date(Date.now() - 2500).toISOString() }} />)
    expect(live).not.toContain('>refresh<')
    expect(stale).toContain('>refresh<')
    expect(live).toContain('aria-label="Turn relay 1 on"')
    expect(live.toLowerCase()).not.toMatch(/\breleased\b|\bconfirmed\b/)
    expect(live).toContain('All relay and motion outputs are off')
    expect(live).toContain('Live physical seven-segment display')
    expect(live).toContain('aria-label="Controller page"')
    expect(live).toContain('aria-label="LCD line 1"')
    expect(live).toContain('aria-label="LCD line 2"')
    expect(live).toContain('aria-label="Buzzer frequency Hz"')
    expect(live).toContain('Test beep')
  })

  it('uses exact one-second freshness gates for local-device and update controls', () => {
    const now = Date.now()
    const updatedAt = new Date(now - 250).toISOString()
    expect(localDeviceSnapshotIsFresh({ events_online: true, updated_at: updatedAt }, 'open', now)).toBe(true)
    expect(localDeviceSnapshotIsFresh({ events_online: true, updated_at: updatedAt }, 'waiting', now)).toBe(false)
    expect(localDeviceSnapshotIsFresh({ events_online: false, updated_at: updatedAt }, 'open', now)).toBe(false)
    expect(localDeviceSnapshotIsFresh({ events_online: true, updated_at: new Date(now - 1000).toISOString() }, 'open', now)).toBe(false)
    expect(updateStateIsFresh('open', updatedAt, now)).toBe(true)
    expect(updateStateIsFresh('closed', updatedAt, now)).toBe(false)
    expect(updateStateIsFresh('open', new Date(now - 1000).toISOString(), now)).toBe(false)
  })

  it('reduces typed update events immediately without a status poll', () => {
    const event = {
      id: 41,
      time: '2026-08-12T10:00:00.250Z',
      kind: 'update.programming',
      text: 'verified write in progress',
      metadata: { operation_id: 'op-9', kind: 'firmware', progress_percent: '42', programming_method: 'urclock' },
    }
    expect(updateStatusFromEvent(null, event)).toMatchObject({
      id: 'op-9', kind: 'firmware', state: 'programming', progress_percent: 42,
      updated_at: event.time, detail: event.text, programming_method: 'urclock',
    })
    expect(updateStatusFromEvent(null, { ...event, kind: 'door' })).toBeNull()
  })

  it('shows useful controller identity states instead of a placeholder', () => {
    expect(dashboardDeviceSummary(emptySnapshot, 'en')).toEqual({ device: 'Awaiting controller', firmware: 'No controller connected' })
    expect(dashboardDeviceSummary({
      ...emptySnapshot,
      connected: true,
      port: { friendly_name: 'USB Serial COM4' },
      hello: { firmware_major: 1, firmware_minor: 2, firmware_patch: 3, build_hash: 0x1234ABCD },
    }, 'en')).toEqual({ device: 'USB Serial COM4', firmware: 'v1.2.3 · #1234ABCD' })
  })

  it('keeps the first-run synchronization phase truthful before a controller is known', () => {
    const markup = renderToStaticMarkup(<BootGate
      open
      progress={82}
      locale="en"
      productTitle="PCController"
      productShortName="PC"
      productTagline="CONTROL CENTER"
      onEnter={vi.fn()}
    />)
    expect(markup).toContain('Synchronizing host state')
    expect(markup.replace(/<[^>]+>/g, ' ')).not.toMatch(/\bLive\b/i)
  })

  it('keeps staging and host updates available offline but hides board programming actions', () => {
    expect(artifactUpdateAvailable(false, 'host-executable')).toBe(true)
    expect(artifactUpdateAvailable(false, 'firmware')).toBe(false)
    expect(artifactUpdateAvailable(true, 'firmware')).toBe(true)
    const markup = renderToStaticMarkup(<UpdatesView {...shared()} />)
    expect(markup).toContain('Stage a local artifact')
    expect(markup).toContain('LOADING')
    expect(markup).not.toContain('DISABLED')
    expect(markup).not.toContain('>Refresh<')
    expect(markup).not.toContain('Review firmware programming')
    expect(markup).not.toContain('Review EEPROM restore')
    expect(markup).not.toContain('Review ISP programming')
  })

  it('keeps settings actions in the field control and omits offline EEPROM controls', () => {
    const markup = renderToStaticMarkup(<SettingsView
      {...shared()}
      appearance={appearance}
      onAppearance={vi.fn()}
      token=""
      onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)}
			uiConfig={null}
			onBuzzerPath={vi.fn(async () => undefined)}
    />)
    expect(markup).toContain('text-field__control')
    expect(markup).toContain('text-field__action')
    expect(markup).toContain('Application title')
    expect(markup).toContain('Peripheral names')
    expect(markup).toContain('Global shortcuts')
    expect(markup).toContain('Record shortcut')
    expect(markup).not.toContain('TM1637')
    expect(markup).not.toContain('Write controller settings')
  })

  it('never presents empty board settings as an authoritative EEPROM report', () => {
    const connectedWithoutSettings = {
      ...emptySnapshot,
      connected: true,
      connection_state: 'connected',
      have_settings: false,
    }
    const markup = renderToStaticMarkup(<SettingsView
      {...shared()}
      snapshot={connectedWithoutSettings}
      boardSettingsReadState="loading"
      appearance={appearance}
      onAppearance={vi.fn()}
      token=""
      onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)}
			uiConfig={null}
			onBuzzerPath={vi.fn(async () => undefined)}
    />)
    expect(markup).toContain('Reading board settings')
    expect(markup).toContain('Waiting for the controller to return its live EEPROM settings')
    expect(markup).not.toContain('EEPROM report')
    expect(markup).not.toContain('Write board settings')
  })

  it('renders offline controls and settings copy in Persian', () => {
    const persianShared = { ...shared(), locale: 'fa' as const }
    const persianAppearance: Appearance = { ...appearance, locale: 'fa', direction: 'rtl' }
    const controls = renderToStaticMarkup(<ControlsView {...persianShared} />)
    const settings = renderToStaticMarkup(<SettingsView
      {...persianShared}
      appearance={persianAppearance}
      onAppearance={vi.fn()}
      token=""
      onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)}
			uiConfig={null}
			onBuzzerPath={vi.fn(async () => undefined)}
    />)
    expect(controls).toContain('کنترل‌های برد در دسترس نیست')
    expect(settings).toContain('هویت میزبان رایانه')
    expect(settings).toContain('سرویس‌ها و چرخهٔ میزبان')
    expect(settings).toContain('ایمنی نشست و توان')
    expect(settings).toContain('هنگام قفل‌شدن ویندوز')
    expect(settings).not.toContain('Application title')
  })

  it('hides companion commands until its HTTP transport is actually reachable', () => {
    expect(localDeviceControlsAvailable({ http_reachable: false })).toBe(false)
    expect(localDeviceControlsAvailable({ http_reachable: true })).toBe(true)
    expect(localDeviceReconnectAvailable({ configured: true, http_reachable: false })).toBe(true)
    expect(localDeviceReconnectAvailable({ configured: false, http_reachable: false })).toBe(false)

    const markup = renderToStaticMarkup(<LocalDeviceView {...shared()} />)
    expect(markup).toContain('Checking local companion availability')
    expect(markup).toContain('Refresh status')
    expect(markup).toContain('Open integration settings')
    expect(markup).not.toContain('A bounded display message')
    expect(markup).not.toContain('Inspection loaded')
    expect(markup).not.toContain('Turn device on')
    expect(markup).not.toContain('Turn device off')
  })

  it('does not start polling timers while server-rendering offline views', () => {
    vi.useFakeTimers()
    try {
      renderToStaticMarkup(<DashboardView {...shared()} />)
      renderToStaticMarkup(<ControlsView {...shared()} />)
      renderToStaticMarkup(<UpdatesView {...shared()} />)
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('keyboard and card action semantics', () => {
  it('pairs every slider with an exact bounded numeric input', () => {
    const markup = renderToStaticMarkup(<RangeField label="Brightness" value={42} min={0} max={100} unit="%" onChange={vi.fn()} />)
    expect(markup).toContain('type="range"')
    expect(markup).toContain('type="number"')
    expect(markup).toContain('min="0"')
    expect(markup).toContain('max="100"')
    expect(markup).toContain('Brightness exact value')
  })

  it('renders every physical key in its own kbd element', () => {
    const markup = renderToStaticMarkup(<HotkeyHelp open onClose={vi.fn()} locale="en" />)
    const keyValues = [...markup.matchAll(/<kbd>(.*?)<\/kbd>/g)].map((match) => match[1])
    expect(keyValues.length).toBeGreaterThan(10)
    expect(keyValues).toContain('1…8')
    expect(keyValues).not.toContain('1…7')
    for (const key of keyValues) expect(key).not.toMatch(/[+\/]|\s{2,}/)
    expect(markup).toContain('key-combo__separator')
  })

  it('exposes one semantic action trigger for pointer and keyboard menus', () => {
    const markup = renderToStaticMarkup(<Card title="Actions" menu={[{ label: 'Refresh', onSelect: vi.fn() }]} />)
    expect(markup).toContain('aria-haspopup="menu"')
    expect(markup).toContain('aria-label="Card actions"')
    expect(markup).toContain('aria-controls="card-menu-')
    expect(markup).toContain('tabindex="0"')
  })

  it('maps every interrupted pointer, keyboard, and page lifecycle to hold release', () => {
    const markup = renderToStaticMarkup(<HoldActionButton onHoldStart={vi.fn()} onHoldStop={vi.fn()}>Hold</HoldActionButton>)
    expect(markup).toContain('aria-pressed="false"')
    expect(markup).toContain('hold-action')
    const source = readFileSync(new URL('./components.tsx', import.meta.url), 'utf8')
    for (const contract of [
      'setPointerCapture', 'onPointerCancel', 'onLostPointerCapture', 'onPointerLeave',
      'onKeyDown', 'onKeyUp', "addEventListener('blur'", "addEventListener('pagehide'",
      "addEventListener('visibilitychange'", 'session.release(false)',
    ]) expect(source).toContain(contract)
  })
})

import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'
import { BootGate, Card, HoldActionButton, HotkeyHelp, RangeField, TextField } from './components'
import type { Appearance } from './types'
import { emptySnapshot } from './types'
import { artifactUpdateAvailable, UpdatesView } from './updates-view'
import { translator } from './i18n'
import { sessionAuthenticationGuidanceRequired } from './authentication-guidance'
import { WorkbenchView } from './workbench'
import {
  ControlsView,
  DashboardView,
  LocalDeviceView,
  SettingsView,
  localDeviceControlsAvailable,
  localDeviceReconnectAvailable,
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
    locale: 'en',
    t: (key) => key,
    command: vi.fn(async () => ''),
    refresh: vi.fn(async () => undefined),
    openDialog: vi.fn(),
    transport: { streamState: 'open', authenticationRequired: false, tabBusSupported: true, tabPeers: 0 },
    relayedTerminal: [],
    broadcastTerminal: vi.fn(),
    boardSettingsReadState: 'idle',
  }
}

describe('offline and settings UI contracts', () => {
  it('distinguishes missing or rejected credentials from ordinary authenticated transport loss', () => {
    const base = {
      hostRequiresAuthentication: true,
      streamState: 'waiting' as const,
      token: 'valid-looking-token',
    }
    expect(sessionAuthenticationGuidanceRequired({ ...base, hostRequiresAuthentication: false })).toBe(false)
    expect(sessionAuthenticationGuidanceRequired({ ...base, streamState: 'open' })).toBe(false)
    expect(sessionAuthenticationGuidanceRequired({ ...base, token: '' })).toBe(true)
    expect(sessionAuthenticationGuidanceRequired({ ...base, streamDetail: 'HTTP 401: authentication required' })).toBe(true)
    expect(sessionAuthenticationGuidanceRequired({ ...base, streamDetail: 'network timeout' })).toBe(false)
  })

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

  it('exposes the complete display presentation policy on a connected controller', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      connection_state: 'connected',
      status: { ...emptySnapshot.status, lcd_address: 0x27 },
    }
    const markup = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={connected} />)
    expect(markup).toContain('TM1637 + LCD')
    expect(markup).toContain('Display target')
    expect(markup).toContain('Marquee step speed')
    expect(markup).toContain('Visible duration')
    expect(markup).toContain('Repeat policy')
    expect(markup).toContain('Force marquee')
    expect(markup).toContain('Overflow scrolls automatically')
    expect(markup).toContain('Show text')
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

  it('never labels disconnected dashboard telemetry as live', () => {
    const markup = renderToStaticMarkup(<DashboardView {...shared()} />)
    expect(markup).not.toContain('Telemetry history')
    expect(markup).not.toMatch(/\bLive\b/)
  })

  it('routes an unauthenticated dashboard directly to secure session settings', () => {
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      t={translator('en')}
      transport={{ ...shared().transport, streamState: 'waiting', authenticationRequired: true }}
    />)
    expect(markup).toContain('href="#/settings"')
    expect(markup).toContain('Authentication required — open Settings to connect securely.')
    expect(markup).toContain('Apply the edge session access token')
    expect(markup).not.toContain('The dashboard is ready')
  })

  it('does not mislabel an authenticated host with an offline board as an authentication failure', () => {
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      t={translator('en')}
      snapshot={{ ...emptySnapshot, connection_reason: 'Serial controller is offline' }}
    />)
    expect(markup).toContain('href="#/dashboard"')
    expect(markup).toContain('The dashboard is ready')
    expect(markup).toContain('Serial controller is offline')
    expect(markup).not.toContain('Authentication required')
  })

  it('hides unavailable peripherals and their invalid readings', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      status: {
        ...emptySnapshot.status,
        supply_mv: -2147483648,
        bus_mv: -2147483648,
        current_ma: -2147483648,
        power_mw: -2147483648,
        temperature_led_centi_c: -32768,
        temperature_bt_audio_centi_c: -32768,
      },
    }
    const dashboard = renderToStaticMarkup(<DashboardView {...shared()} snapshot={connected} />)
    expect(dashboard).not.toContain('metric-grid')
    expect(dashboard).not.toContain('Telemetry history')
    expect(dashboard).not.toContain('-2147483648')
    expect(dashboard).not.toContain('-32768')

    const workbench = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={connected} />)
    expect(workbench).toContain('TM1637')
    expect(workbench).not.toContain('TM1637 + LCD')
    expect(workbench).not.toContain('Display target')
    expect(workbench).not.toContain('Temperature identities')
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

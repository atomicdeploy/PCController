import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'
import { BootGate, Card, HoldActionButton, HotkeyHelp, RangeField, TextField } from './components'
import type { Appearance, UIConfig } from './types'
import { emptySnapshot } from './types'
import { artifactUpdateAvailable, localUpdateEvent, peerUpdateStatusFromEvent, UpdatesView } from './updates-view'
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

const uiConfig: UIConfig = {
  name: 'PCController',
  setup_complete: true,
  appearance,
  appearance_etag: 'a'.repeat(64),
  status_interval_ms: 275,
  measurement_freshness_ms: 1600,
  websocket_path: '/ipc',
  session_ticket_path: '/api/session/ticket',
  auth_required: false,
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
    refresh: vi.fn(async () => undefined),
    openDialog: vi.fn(),
    transport: { streamState: 'open', authenticationRequired: false, boardState: 'ready', tabBusSupported: true, tabPeers: 0 },
    relayedTerminal: [],
    broadcastTerminal: vi.fn(),
    boardSettingsReadState: 'idle',
  }
}

describe('offline and settings UI contracts', () => {
  it.each([true, false])('renders only the Physical LED mirror hex value in monospace (live=%s)', haveStatusLED => {
    const snapshot = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      have_status_led: haveStatusLED,
      hello: { ...emptySnapshot.hello, capabilities: 0xFFFFFFFF },
      status_led: { red: 171, green: 205, blue: 239, brightness: 128, effect: 2, condition: 3 },
    }
    const markup = renderToStaticMarkup(<ControlsView {...shared()} snapshot={snapshot} />)
    if (haveStatusLED) {
      expect(markup).toContain('<span class="mono">#ABCDEF</span> · effect 2 · condition 3')
    } else {
      expect(markup).toContain('Awaiting pushed board state')
      expect(markup).not.toContain('<span class="mono">#ABCDEF</span>')
    }
  })

  it('distinguishes missing or rejected credentials from ordinary authenticated transport loss', () => {
    const base = {
      hostRequiresAuthentication: true,
      streamState: 'waiting' as const,
      token: 'valid-looking-token',
    }
    expect(sessionAuthenticationGuidanceRequired({ ...base, hostRequiresAuthentication: false })).toBe(false)
    expect(sessionAuthenticationGuidanceRequired({
      ...base,
      hostRequiresAuthentication: false,
      streamDetail: 'HTTP 401: authentication required',
    })).toBe(true)
    expect(sessionAuthenticationGuidanceRequired({ ...base, streamState: 'open' })).toBe(false)
    expect(sessionAuthenticationGuidanceRequired({ ...base, token: '' })).toBe(true)
    expect(sessionAuthenticationGuidanceRequired({ ...base, streamDetail: 'HTTP 401: authentication required' })).toBe(true)
    expect(sessionAuthenticationGuidanceRequired({
      ...base,
      streamDetail: 'HTTP 403: remote read capability is disabled',
    })).toBe(false)
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
    expect(markup).toContain('Controller board disconnected')
    expect(markup).not.toContain('to reveal its controls')
    expect(markup).not.toContain('PWM matrix')
    expect(markup).not.toContain('Relays &amp; motion')
    expect(markup).not.toContain('Status lighting')
  })

  it('keeps DTR hardware reset visible for an enumerated board before firmware authentication', () => {
    const snapshot = {
      ...emptySnapshot,
      connected: false,
      connection_phase: 'waiting_retry' as const,
      connection_candidate: { name: 'COM3', friendly_name: 'USB-SERIAL CH340' },
      reset_lines_available: true,
      reset_lines_port: { name: 'COM3', friendly_name: 'USB-SERIAL CH340' },
    }
    const transport = { ...shared().transport, streamState: 'open' as const, boardState: 'unavailable' as const }
    const controls = renderToStaticMarkup(<ControlsView {...shared()} snapshot={snapshot} transport={transport} />)
    const workbench = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={snapshot} transport={transport} />)
    expect(controls).toContain('Hardware reset')
    expect(workbench).toContain('Hardware reset (DTR)')
  })

  it('renders concise transport states without connection narration', () => {
    const offline = renderToStaticMarkup(<DashboardView
      {...shared()}
      t={translator('en')}
      snapshot={{ ...emptySnapshot, connection_reason: 'Controller offline — check the connection details below.' }}
      transport={{ ...shared().transport, streamState: 'waiting', boardState: 'unavailable' }}
    />)
    expect(offline).toContain('Reconnecting…')
    expect(offline).toContain('connection-fuji')
    expect(offline).not.toContain('Controller offline')
    expect(offline).not.toContain('check the connection details below')

    const connecting = renderToStaticMarkup(<ControlsView
      {...shared()}
      transport={{ ...shared().transport, streamState: 'connecting', boardState: 'loading' }}
    />)
    expect(connecting).toContain('Connecting…')
    expect(connecting).toContain('connection-fuji')
    expect(connecting).not.toContain('to reveal its controls')
  })

  it('exposes the complete display presentation policy on a connected controller', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      hello: { ...emptySnapshot.hello, capabilities: (1 << 5) | (1 << 6) },
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

  it('renders an empty host-backed terminal combobox without a fabricated command', () => {
    const markup = renderToStaticMarkup(<WorkbenchView {...shared()} />)
    expect(markup).toContain('id="workbench-command"')
    expect(markup).toContain('aria-autocomplete="list"')
    expect(markup).toContain('aria-expanded="false"')
    expect(markup).not.toContain('value="status"')
    expect(markup).not.toContain('workbench-command-completion-0')
  })

  it.each([null, undefined, []])('renders an empty macro draft without crashing when steps is %s', (steps) => {
    const snapshot = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      macros: {
        library: [{ id: 4, name: 'New draft', mode: 'host', steps }],
        playback: { running: false, name: '', mode: 'host', step: 0, step_count: 0, faithful: false, maximum_timing_error_us: 0 },
        recording: { active: false, name: '', mode: 'host', steps: 0 },
      },
    }
    const markup = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={snapshot} />)
    expect(markup).toContain('New draft')
    expect(markup).toContain('#4 · Uncategorized · host')
    expect(markup).toContain('Effect library')
    expect(markup).toContain('Play selected')
  })

  it('renders only user PWM channels in the generic mixer and keeps system channels role-specific', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      hello: { ...emptySnapshot.hello, capabilities: 1 << 2 },
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
    expect(markup).toContain('Authentication required')
    expect(markup).toContain('Enter this host’s access token.')
    expect(markup).toContain('Enter access token')
    expect(markup).not.toContain('The dashboard is ready')
  })

  it('renders no stale board keys or values while host authentication is required', () => {
    const stale = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      hello: { capabilities: 0xffffffff, build_hash: 0xdeadbeef, build_timestamp: '260812120000' },
      port: { name: 'COM18', vid: '1A86', pid: '7523' },
      status: { ...emptySnapshot.status, door_open: true, bluetooth_audio_state: 2, reset_count: 9, uptime_ms: 1000 },
    }
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      snapshot={stale}
      t={translator('en')}
      transport={{ ...shared().transport, streamState: 'waiting', boardState: 'unavailable', authenticationRequired: true }}
    />)
    for (const forbidden of ['device-card', 'Door', 'Bluetooth audio', 'Firmware', 'BUILD', 'UPTIME', 'COM18', 'DEADBEEF', 'Events']) {
      expect(markup).not.toContain(forbidden)
    }
    expect(markup).toContain('Authentication required')
  })

  it('shows Bluetooth Audio state only when HELLO advertises capability bit 11', () => {
    const base = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      status: { ...emptySnapshot.status, bluetooth_audio_state: 2 },
    }
    const withoutCapability = renderToStaticMarkup(<DashboardView {...shared()} t={translator('en')} snapshot={base} />)
    expect(withoutCapability).not.toContain('Bluetooth audio')
    const advertised = renderToStaticMarkup(<DashboardView {...shared()} t={translator('en')} snapshot={{ ...base, hello: { capabilities: 1 << 11 } }} />)
    expect(advertised).toContain('Bluetooth audio')
  })

  it('reports advertised invalid measurements without formatting sentinel values', () => {
    const snapshot = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      hello: { capabilities: (1 << 0) | (1 << 1) },
      status: {
        ...emptySnapshot.status,
        flags: (1 << 0) | (1 << 2) | (1 << 3),
        supply_mv: -2147483648,
        bus_mv: -2147483648,
        current_ma: -2147483648,
        power_mw: -2147483648,
        temperature_led_centi_c: -32768,
        temperature_bt_audio_centi_c: 32767,
      },
    }
    const markup = renderToStaticMarkup(<DashboardView {...shared()} snapshot={snapshot} />)
    expect(markup).toContain('Power measurements unavailable')
    expect(markup).toContain('LED temperature unavailable')
    expect(markup).toContain('BT Amplifier temperature unavailable')
    expect(markup).not.toContain('Measurement unavailable')
    expect(markup).not.toContain('Invalid controller sample')
    expect(markup).not.toContain('The controller advertised')
    expect(markup).not.toContain('-2147483648')
    expect(markup).not.toContain('-32768')
    expect(markup).not.toContain('32767')
  })

  it('does not mislabel an authenticated host with an offline board as an authentication failure', () => {
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      t={translator('en')}
      snapshot={{ ...emptySnapshot, connection_reason: 'Serial controller is offline' }}
    />)
    expect(markup).toContain('PCController host online')
    expect(markup).toContain('Controller board disconnected')
    expect(markup).toContain('Serial controller is offline')
    expect(markup).not.toContain('Authentication required')
    expect(markup).not.toContain('The dashboard is ready')
  })

  it('shows an accessible actionable hardware warning without misclassifying another device', () => {
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      t={translator('en')}
      snapshot={{
        ...emptySnapshot,
        hardware_problems: [{
          code: 'usb_descriptor_failure',
          severity: 'error',
          impact: 'active_operation_outcome_unknown',
          os_problem_code: 43,
          device_id: 'USB\\VID_0000&PID_0002\\physical-controller-instance',
          location: 'Port 2, Hub 3',
          observed_at: '2026-09-28T10:00:00Z',
        }],
      }}
    />)
    expect(markup).toContain('role="alert"')
    expect(markup).toContain('Controller USB connection failed')
    expect(markup).toContain('Check the controller data cable, power, or try another USB port.')
    expect(markup).toContain('Communication was lost during an active operation')
    expect(markup).toContain('Windows code 43 · Port 2, Hub 3')
    expect(markup).not.toContain('physical-controller-instance')
  })

  it('renders the Persian hardware warning as native joined-script text', () => {
    const markup = renderToStaticMarkup(<DashboardView
      {...shared()}
      locale="fa"
      t={translator('fa')}
      snapshot={{
        ...emptySnapshot,
        hardware_problems: [{
          code: 'usb_descriptor_failure',
          severity: 'error',
          observed_at: '2026-09-28T10:00:00Z',
        }],
      }}
    />)
    expect(markup).toContain('خرابی اتصال USB کنترلر')
    expect(markup).toContain('کابل داده، برق و درگاه USB کنترلر را بررسی کنید')
  })

  it('hides unavailable peripherals and their invalid readings', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      hello: { ...emptySnapshot.hello, capabilities: 1 << 5 },
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
    expect(dashboard).not.toContain('Power measurements unavailable')
    expect(dashboard).not.toContain('LED temperature unavailable')
    expect(dashboard).not.toContain('BT Amplifier temperature unavailable')

    const workbench = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={connected} />)
    expect(workbench).toContain('TM1637')
    expect(workbench).not.toContain('TM1637 + LCD')
    expect(workbench).not.toContain('Display target')
    expect(workbench).not.toContain('Temperature identities')
  })

  it('presents authoritative TM1637 ACK detection without inferring legacy state', () => {
    const connected = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      hello: { ...emptySnapshot.hello, capabilities: 1 << 5 },
      front_panel: {
        schema: 2, raw_segments: [0, 0, 0, 0] as [number, number, number, number], brightness: 5,
        blink: false, segments_active: false, category_selector: false,
        lcd_address: 0, lcd_available: false, lcd_backlight: false,
        lcd_line_1: '', lcd_line_2: '', pressed_keys: 0, menu_page: 0, program_mode: 0,
        host_captured: false, host_state: 0, host_editable_value: 0,
        tm1637_detection_known: true, tm1637_detected: false,
      },
      have_front_panel: true,
      have_front_panel_segments: true,
    }
    const missing = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={connected} />)
    expect(missing).toContain('TM1637 not detected')

    const detected = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={{
      ...connected,
      front_panel: { ...connected.front_panel, tm1637_detected: true },
    }} />)
    expect(detected).toContain('TM1637 detected')

    const legacy = renderToStaticMarkup(<WorkbenchView {...shared()} snapshot={{
      ...connected,
      front_panel: { ...connected.front_panel, tm1637_detection_known: false },
    }} />)
    expect(legacy).not.toContain('TM1637 not detected')
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

  it('does not mix a bridged peer update event into local update status', () => {
    expect(localUpdateEvent({ kind: 'update.queued', source: 'host' })).toBe(true)
    expect(localUpdateEvent({
      kind: 'update.queued', source: 'bridge', metadata: { 'bridge.ingress': 'edge' },
    })).toBe(false)
    expect(localUpdateEvent({ kind: 'peer-update.remote-queued', source: 'bridge' })).toBe(false)
  })

  it('derives a separate shared peer staging status from pushed events', () => {
    expect(peerUpdateStatusFromEvent({
      kind: 'peer-update.remote-staged',
      text: 'remote staging accepted',
      metadata: {
        peer: 'peer-host', state: 'remote-staged', progress_known: 'false', progress_percent: '0',
        bytes_done: '6419456', bytes_total: '6419456',
        operation_id: 'source-intent', remote_operation_id: 'remote-host-7',
        terminal_verified: 'false', sha256: 'a'.repeat(64),
        idempotency_key: 'intent:shared',
      },
    })).toEqual({
      peer: 'peer-host', state: 'remote-staged', progressPercent: 0, progressKnown: false,
      bytesDone: 6419456, bytesTotal: 6419456,
      operationID: 'remote-host-7', detail: 'remote staging accepted',
      artifactSHA256: 'a'.repeat(64), idempotencyKey: 'intent:shared',
      retrySameIntent: false,
      terminalVerified: false,
    })
    expect(peerUpdateStatusFromEvent({ kind: 'update.queued', text: 'local' })).toBeNull()

		expect(peerUpdateStatusFromEvent({
			kind: 'peer-update.completed',
			text: 'peer restarted and acknowledged active SHA',
			metadata: {
				peer: 'peer-host', state: 'completed', progress_known: 'true', progress_percent: '100',
				operation_id: 'source-intent', remote_operation_id: 'remote-host-7',
				terminal_verified: 'true', active_sha256: 'a'.repeat(64),
				sha256: 'a'.repeat(64), idempotency_key: 'intent:shared',
			},
		})).toMatchObject({ state: 'completed', progressPercent: 100, terminalVerified: true })
  })

  it('presents an uncertain peer outcome as retryable rather than failed', () => {
    const markup = renderToStaticMarkup(<UpdatesView
      {...shared()}
      events={[{
        id: 17,
        time: '2026-08-13T18:00:00Z',
        kind: 'peer-update.outcome-uncertain',
        text: 'peer outcome uncertain; retry with the same idempotency key',
        source: 'bridge',
        metadata: {
          peer: 'peer-host', state: 'outcome-uncertain', progress_percent: '0',
          operation_id: 'source-intent', retry_same_idempotency_key: 'true',
          terminal_verified: 'false', sha256: 'b'.repeat(64),
          idempotency_key: 'intent:shared-retry',
        },
      }]}
    />)
    expect(markup).toContain('Outcome uncertain — retry this update')
    expect(markup).not.toContain('Peer attempt failed')
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
      navigationSync
      onNavigationSync={vi.fn()}
    />)
    expect(markup).toContain('text-field__control')
    expect(markup).toContain('text-field__action')
    expect(markup).toContain('Application title')
    expect(markup).toContain('Peripheral names')
    expect(markup).toContain('Global shortcuts')
    expect(markup).toContain('Record shortcut')
    expect(markup).toContain('Sync this tab with other instances')
    expect(markup).not.toContain('This tab follows the live default navigation group')
    expect(markup).not.toContain('TM1637')
    expect(markup).not.toContain('Write controller settings')
    expect(markup).not.toContain('Security')
    expect(markup).not.toContain('authToken')
    expect(markup).not.toContain('No session token')
  })

  it('shows only host-advertised live measurement timing', () => {
    const markup = renderToStaticMarkup(<SettingsView
      {...shared()}
      appearance={appearance}
      onAppearance={vi.fn()}
      token=""
      onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)}
      uiConfig={uiConfig}
      onBuzzerPath={vi.fn(async () => undefined)}
      navigationSync
      onNavigationSync={vi.fn()}
    />)
    expect(markup).toContain('Live measurements')
    expect(markup).toContain('value="275"')
    expect(markup).toContain('value="1600"')
    expect(markup).toContain('Apply live timing')
  })

  it('shows navigation synchronization state only while it is factual and actionable', () => {
    const pending = renderToStaticMarkup(<SettingsView
      {...shared()} appearance={appearance} onAppearance={vi.fn()} token="" onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)} uiConfig={null}
      onBuzzerPath={vi.fn(async () => undefined)} navigationSync
      navigationSyncStatus={{ state: 'pending', detail: '' }} onNavigationSync={vi.fn()}
    />)
    expect(pending).toContain('Synchronizing')
    const failed = renderToStaticMarkup(<SettingsView
      {...shared()} appearance={appearance} onAppearance={vi.fn()} token="" onToken={vi.fn()}
      onAppTitle={vi.fn(async (value: string) => value)} uiConfig={null}
      onBuzzerPath={vi.fn(async () => undefined)} navigationSync
      navigationSyncStatus={{ state: 'error', detail: 'Coordinator unavailable' }} onNavigationSync={vi.fn()}
    />)
    expect(failed).toContain('Coordinator unavailable')
  })

  it('never presents empty board settings as an authoritative EEPROM report', () => {
    const connectedWithoutSettings = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      connection_state: 'connected',
      have_settings: false,
      hello: { ...emptySnapshot.hello, capabilities: 1 << 8 },
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
      navigationSync
      onNavigationSync={vi.fn()}
    />)
    expect(markup).toContain('Reading board settings')
    expect(markup).toContain('Waiting for the controller to return its live EEPROM settings')
		expect(markup).toContain('Board state unavailable')
		expect(markup).not.toContain('Board active')
		expect(markup).not.toContain('Board silent')
    expect(markup).not.toContain('EEPROM report')
    expect(markup).not.toContain('Write board settings')
  })

  it('renders complete authoritative enclosure illumination settings and state', () => {
	const connected = {
		...emptySnapshot,
		connected: true,
		have_status: true,
		have_settings: true,
		connection_state: 'connected',
		hello: { ...emptySnapshot.hello, capabilities: (1 << 2) | (1 << 8) },
		status: { ...emptySnapshot.status, pwm_available: true, door_open: true },
		settings: { ...emptySnapshot.settings, persisted: true, light_mode: 1, on_brightness: 180, off_brightness: 12 },
		illumination: {
			available: true, mode: 1, on_brightness: 180, off_brightness: 12,
			door_open: true, target_brightness: 180, target_pwm: 2891,
			applied_brightness: 160, applied_pwm: 2570, at_target: false, persisted: true,
		},
	}
	const markup = renderToStaticMarkup(<SettingsView
		{...shared()}
		snapshot={connected}
		appearance={appearance}
		onAppearance={vi.fn()}
		token=""
		onToken={vi.fn()}
		onAppTitle={vi.fn(async (value: string) => value)}
		uiConfig={null}
		onBuzzerPath={vi.fn(async () => undefined)}
		navigationSync
		onNavigationSync={vi.fn()}
	/>)
	expect(markup).toContain('Enclosure illumination')
	expect(markup).toContain('Auto · door')
	expect(markup).toContain('Door-open / On brightness')
	expect(markup).toContain('Door-closed / Off brightness')
	expect(markup).toContain('Applied MOSFET channel 12')
	expect(markup).toContain('2570/4095')
	expect(markup).toContain('Manual raw override · MOSFET channel 12')
	expect(markup).toContain('Apply manual override')
	expect(markup).toContain('Apply policy')
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
      navigationSync
      onNavigationSync={vi.fn()}
    />)
    expect(controls).toContain('برد کنترلر قطع است')
    expect(controls).not.toContain('برای نمایش کنترل‌ها')
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

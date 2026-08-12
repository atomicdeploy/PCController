import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { WorkbenchView } from './workbench'
import { emptySnapshot } from './types'
import type { SharedViewProps } from './views'

function shared(): SharedViewProps {
  return {
    appTitle: 'PCController',
    snapshot: { ...emptySnapshot, connected: true, connection_state: 'connected', have_status: true },
    samples: [], events: [], macroEvents: [], locale: 'en', t: (key) => key,
    command: vi.fn(async () => ''), relayToggle: vi.fn(async () => undefined), relayPending: new Set(),
    refresh: vi.fn(async () => undefined), openDialog: vi.fn(),
    transport: { streamState: 'open', tabBusSupported: true, tabPeers: 0 },
    relayedTerminal: [], broadcastTerminal: vi.fn(), boardSettingsReadState: 'idle', openAppPreferences: vi.fn(),
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('Workbench view', () => {
  it('renders only the selected nested audio surface with a custom empty melody selector', () => {
    vi.stubGlobal('location', { hash: '#/workbench/interface/audio' })
    const markup = renderToStaticMarkup(<WorkbenchView {...shared()} />)
    expect(markup).toContain('Buzzer &amp; melody')
    expect(markup).toContain('select-menu__trigger')
    expect(markup).toContain('Loading configured melodies')
    expect(markup).not.toContain('value="welcome"')
    expect(markup).not.toContain('>List<')
    expect(markup).not.toContain('Bridge terminal')
    expect(markup).not.toContain('Addressable strip')
  })

  it('keeps terminal history and catalog completion semantics on the overview only', () => {
    vi.stubGlobal('location', { hash: '#/workbench' })
    const markup = renderToStaticMarkup(<WorkbenchView {...shared()} />)
    expect(markup).toContain('Bridge terminal')
    expect(markup).toContain('aria-autocomplete="list"')
    expect(markup).not.toContain('Buzzer &amp; melody')
    expect(markup).not.toContain('<h2>Temperature probes</h2>')
  })

  it('does not present in-range temperature bytes when availability flags are absent', () => {
    vi.stubGlobal('location', { hash: '#/workbench/sensors/temperature' })
    const props = shared()
    props.snapshot = {
      ...props.snapshot,
      status: { ...props.snapshot.status, flags: 0, temperature_led_centi_c: 2500, temperature_bt_audio_centi_c: 2200 },
    }
    const markup = renderToStaticMarkup(<WorkbenchView {...props} />)
    expect(markup).toContain('Temperature probes')
    expect(markup).not.toContain('25.0 °C')
    expect(markup).not.toContain('22.0 °C')
  })
})

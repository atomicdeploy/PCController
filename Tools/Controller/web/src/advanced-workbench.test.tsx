import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { AdvancedWorkbench } from './advanced-workbench'
import { emptySnapshot } from './types'
import type { SharedViewProps } from './views'

function shared(): SharedViewProps {
  return {
    appTitle: 'PCController',
    snapshot: { ...emptySnapshot, connected: true, connection_state: 'connected' },
    samples: [], events: [], macroEvents: [], locale: 'en', t: (key) => key,
    command: vi.fn(async () => ''), relayToggle: vi.fn(async () => undefined), relayPending: new Set(),
    refresh: vi.fn(async () => undefined), openDialog: vi.fn(),
    transport: { streamState: 'open', tabBusSupported: true, tabPeers: 1 },
    relayedTerminal: [], broadcastTerminal: vi.fn(), boardSettingsReadState: 'idle', openAppPreferences: vi.fn(),
  }
}

describe('advanced front-panel navigation', () => {
  it('uses only the typed board catalog selector and never accepts an arbitrary page string', () => {
    const markup = renderToStaticMarkup(<AdvancedWorkbench {...shared()} run={vi.fn(async () => '')} busy="" />)
    expect(markup).toContain('<select aria-label="Firmware page"')
    expect(markup).toContain('Loading verified catalog')
    expect(markup).not.toContain('Firmware page ID or key')
    expect(markup).not.toMatch(/<input[^>]+aria-label="Firmware page"/)
  })
})

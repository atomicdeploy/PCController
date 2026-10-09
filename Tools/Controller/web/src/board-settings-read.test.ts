import { describe, expect, it } from 'vitest'
import { BoardSettingsReadGate } from './board-settings-read'
import { emptySnapshot, type Snapshot } from './types'

function connected(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    ...emptySnapshot,
    connected: true,
    connection_state: 'connected',
    port: { name: 'COM18', instance_id: 'USB\\VID_1A86&PID_7523' },
    hello: { build_hash: 0x7DB71F64, build_timestamp: '260802114654' },
    ...overrides,
  }
}

describe('board settings entry read gate', () => {
  it('requests exactly once for one connected settings absence', () => {
    const gate = new BoardSettingsReadGate()
    const snapshot = connected()
    expect(gate.shouldRead(snapshot, false)).toBe(false)
    expect(gate.shouldRead(snapshot, true)).toBe(true)
    expect(gate.shouldRead(snapshot, true)).toBe(false)
    expect(gate.shouldRead({ ...snapshot, status_updated: 'later' }, true)).toBe(false)
  })

  it('re-arms after settings arrive, disconnect, or controller identity changes', () => {
    const gate = new BoardSettingsReadGate()
    const first = connected()
    expect(gate.shouldRead(first, true)).toBe(true)
    expect(gate.shouldRead({ ...first, have_settings: true }, true)).toBe(false)
    expect(gate.shouldRead(first, true)).toBe(true)
    expect(gate.shouldRead({ ...first, connected: false }, true)).toBe(false)
    expect(gate.shouldRead(first, true)).toBe(true)
    expect(gate.shouldRead(connected({ hello: { ...first.hello, build_hash: 0x12345678 } }), true)).toBe(true)
  })
})

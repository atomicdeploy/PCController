import { describe, expect, it } from 'vitest'
import {
  applyMacroEventToSnapshot,
  isMacroControllerEvent,
  macroEventNeedsSnapshot,
  prependMacroControllerEvent,
  shouldUseLegacyMacroFallback,
} from './macro-live'
import type { ControllerEvent, MacroSnapshot } from './types'

function snapshot(): MacroSnapshot {
  return {
    library: [],
    latest_event_id: 0,
    recording: {
      active: false, id: 0, name: '', steps: 0, host_steps: 0, panel_steps: 0,
      rf_steps: 0, last_at_us: 0, last_delta_us: 0, last_opcode: 0, last_source: 0,
    },
    playback: {
      running: false, id: 0, name: '', step: 0, step_count: 0, duration_us: 0,
      accepted_bytes: 0, buffer_fill: 0, underruns: 0, dispatch_errors: 0,
      dropped_steps: 0, evidence_steps: 0, timing_violations: 0,
      last_timing_delta_us: 0, maximum_timing_error_us: 0, timing_tolerance_us: 2500,
      faithful: false,
    },
  }
}

function event(id: number, kind: string, metadata: Record<string, string> = {}): ControllerEvent {
  return { id, time: '2026-08-12T10:00:00.000Z', kind, text: kind, metadata }
}

describe('typed macro live state', () => {
  it('applies exact recording and playback delta evidence without polling', () => {
    const recording = applyMacroEventToSnapshot(snapshot(), {
      ...event(41, 'macro.recording.step', {
        macro_id: '7', macro_name: 'Door close', step: '3', at_us: '125040',
        delta_us: '16733', opcode: '0x31', source: '2',
      }),
      lifecycle: 'captured',
    })
    expect(recording.recording).toMatchObject({
      active: true, id: 7, name: 'Door close', steps: 3,
      last_at_us: 125040, last_delta_us: 16733, last_opcode: 0x31, last_source: 2,
    })

    const playback = applyMacroEventToSnapshot(recording, {
      ...event(42, 'macro.step', {
        macro_id: '7', macro_name: 'Door close', step: '2', steps: '5', mcu_delta_us: '-86',
      }),
      lifecycle: 'executed', state: 'acknowledged',
    })
    expect(playback.playback).toMatchObject({
      running: true, id: 7, name: 'Door close', step: 2, step_count: 5,
      last_timing_delta_us: -86,
    })
    expect(playback.latest_event_id).toBe(42)
  })

  it('keeps macro timing in its dedicated bounded stream and refreshes only material lifecycles', () => {
    const timing = event(2, 'macro.recording.step')
    expect(isMacroControllerEvent(timing)).toBe(true)
    expect(macroEventNeedsSnapshot(timing)).toBe(false)
    expect(macroEventNeedsSnapshot(event(3, 'macro.library'))).toBe(true)
    expect(prependMacroControllerEvent([event(1, 'macro')], timing, 1)).toEqual([timing])
    expect(prependMacroControllerEvent([timing], event(4, 'relay.state'))).toEqual([timing])
  })

  it('uses legacy commands only for missing typed methods, never validation or transport failures', () => {
    expect(shouldUseLegacyMacroFallback(new Error('method not found'))).toBe(true)
    expect(shouldUseLegacyMacroFallback(new Error('unknown RPC method controller.macro.play'))).toBe(true)
    expect(shouldUseLegacyMacroFallback(new Error('macro name is required'))).toBe(false)
    expect(shouldUseLegacyMacroFallback(new Error('network connection closed'))).toBe(false)
  })
})

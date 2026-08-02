import { describe, expect, it } from 'vitest'
import {
  defaultRFMapDraft,
  isRFGuidedCaptureEvent,
  rfEntryNeedsReview,
  rfMapRPCParams,
  stableRFIdentity,
} from './rf-guided-workflow'
import type { ControllerEvent, RFLearnedEntry } from './types'

function event(overrides: Partial<ControllerEvent> = {}): ControllerEvent {
  return {
    id: 42,
    time: '2026-08-02T12:00:00Z',
    kind: 'rf.learn.mapping-required',
    text: 'capture',
    rf_id: 7,
    ...overrides,
  }
}

function entry(overrides: Partial<RFLearnedEntry> = {}): RFLearnedEntry {
  return {
    id: 1,
    code: 0x123456,
    code_display: '0x00123456',
    bits: 24,
    protocol: 1,
    pulse_us: 350,
    action_kind: 1,
    action_value: 0,
    behavior: 0,
    ...overrides,
  }
}

describe('guided RF capture state helpers', () => {
  it('accepts only fresh learned events with an exact slot identity', () => {
    expect(isRFGuidedCaptureEvent(event(), 41)).toBe(true)
    expect(isRFGuidedCaptureEvent(event(), 42)).toBe(false)
    expect(isRFGuidedCaptureEvent(event({ kind: 'rf.receive' }), 0)).toBe(false)
    expect(isRFGuidedCaptureEvent(event({ rf_id: undefined }), 0)).toBe(false)
  })

  it('builds semantic validated RPC mappings for A/B/C/D defaults', () => {
    expect(defaultRFMapDraft(1)).toEqual({ action: 'key', target: '2', behavior: 'press' })
    expect(rfMapRPCParams(7, defaultRFMapDraft(1))).toEqual({
      id: 7, action: 'key', target: '2', behavior: 'press',
    })
    expect(rfMapRPCParams(7, { action: 'menu', target: 'next', behavior: 'press' })).toEqual({
      id: 7, action: 'menu', target: 'next',
    })
    expect(rfMapRPCParams(7, { action: 'none', target: '4', behavior: 'toggle' })).toEqual({
      id: 7, action: 'none',
    })
  })

  it('marks unmapped and duplicate identities for stale-record review', () => {
    const first = entry({ id: 1 })
    const duplicate = entry({ id: 8, action_value: 1 })
    const unmapped = entry({ id: 9, code: 0x777777, action_kind: 0 })
    const records = [first, duplicate, unmapped]
    expect(stableRFIdentity(first)).toBe('1193046:24:1')
    expect(rfEntryNeedsReview(first, records)).toBe(false)
    expect(rfEntryNeedsReview(duplicate, records)).toBe(true)
    expect(rfEntryNeedsReview(unmapped, records)).toBe(true)
  })
})

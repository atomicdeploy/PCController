import { describe, expect, it } from 'vitest'
import { activeUpdateProgress, updateStatusFromEvent } from './updates-view'
import { updateIsRunning, updateElapsed } from './update-operation-panel'
import type { UpdateStatus } from './updates-api'

function status(state: UpdateStatus['state'], progress_percent = 0): UpdateStatus {
  return { id: 'update-test', kind: 'firmware', state, progress_percent, progress_known: true }
}

describe('update progress truth', () => {
  it('does not present progress for idle or terminal results', () => {
    expect(activeUpdateProgress(null)).toBeNull()
    for (const state of ['downloaded', 'staged', 'completed', 'failed'] as const) {
      expect(activeUpdateProgress(status(state, 100))).toBeNull()
    }
  })

  it('allows zero percent only for an active operation and clamps bad producers', () => {
    expect(activeUpdateProgress(status('queued', 0))).toBe(0)
    expect(activeUpdateProgress(status('programming', 42))).toBe(42)
    expect(activeUpdateProgress(status('verifying', 130))).toBe(100)
  })

  it('keeps unmeasured stages busy without fabricated percentages', () => {
    const waiting = { ...status('reconnecting', 40), progress_known: false }
    expect(updateIsRunning(waiting)).toBe(true)
    expect(activeUpdateProgress(waiting)).toBeNull()
    expect(activeUpdateProgress({ ...waiting, progress_known: undefined })).toBeNull()
    expect(activeUpdateProgress({ ...waiting, progress_known: true, progress_percent: NaN })).toBeNull()
  })

  it('freezes elapsed duration when an operation fails', () => {
    const failed = { ...status('failed', 40), started_at: '2026-09-29T01:00:00Z', updated_at: '2026-09-29T01:00:35Z' }
    expect(updateElapsed(failed, Date.parse('2026-09-29T02:00:00Z'))).toBe('35s')
    expect(updateIsRunning(failed)).toBe(false)
  })

  it('uses pushed stage telemetry immediately and rejects peer events', () => {
    const event = { kind: 'update.writing', source: 'artifact-service', text: 'flash', metadata: {
      operation_id: 'op-test', kind: 'firmware', state: 'writing', stage: 'writing', progress_known: 'true', progress_percent: '42',
      detail: 'Writing flash', started_at: '2026-09-29T01:00:00Z', updated_at: '2026-09-29T01:00:03Z',
    } }
    expect(updateStatusFromEvent(event)).toMatchObject({ stage: 'writing', progress_percent: 42, progress_known: true, detail: 'Writing flash' })
    expect(updateStatusFromEvent({ ...event, source: 'bridge' })).toBeNull()
  })

  it('hydrates toolchain readiness from websocket event metadata', () => {
    const event = { kind: 'update.toolchain-ready', source: 'artifact-service', text: 'ready', metadata: {
      operation_id: 'op-toolchain', kind: 'firmware', state: 'toolchain-ready', stage: 'toolchain-ready',
      progress_known: 'false', progress_percent: '0', toolchain_ready: 'true',
      toolchain_policy: 'controllerboardmini-atmega328p', toolchain_provider: 'pccontroller-managed-arduino-cli',
      toolchain_version: '1.5.1', toolchain_compatible_sources: '2',
      toolchain_providers: 'pccontroller-policy:controllerboardmini-atmega328p,arduino-cli:1.5.1:selected',
    } }
    expect(updateStatusFromEvent(event)).toMatchObject({
      progress_known: false,
      toolchain: {
        ready: true, policy: 'controllerboardmini-atmega328p', provider: 'pccontroller-managed-arduino-cli',
        version: '1.5.1', compatible_sources: 2,
        providers: ['pccontroller-policy:controllerboardmini-atmega328p', 'arduino-cli:1.5.1:selected'],
      },
    })
  })
})

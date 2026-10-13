import { describe, expect, it } from 'vitest'
import { mergePushedPWM, mergePWMSnapshot } from './pwm-push'
import type { PWMValues, Snapshot } from './types'
const snapshot = { connected: true, host_instance_id: 'primary', connection_generation: 2, pwm_revision: 3 } as Snapshot
const push = (): PWMValues => ({ available: true, selected_channel: 0,
  host_instance_id: 'primary', connection_generation: 2, revision: 4, time: new Date().toISOString(),
  values: Array.from({ length: 16 }, (_, i) => i * 100),
  channels: Array.from({ length: 16 }, (_, i) => ({ channel: i, raw_value: i * 100, logical_percent: i * 5,
    name: `Output ${i}`, config: { output_type: 'general', icon: 'power', curve: 'linear', gamma: 1 } })) })
describe('selected primary PWM pushes', () => {
  it('retains calibrated percentages and factual zero without another read', () => {
    const next = mergePushedPWM(snapshot, push(), 'primary')
    expect(next.pwm_revision).toBe(4)
    expect(next.pwm_values?.channels?.[0].logical_percent).toBe(0)
    expect(next.pwm_values?.channels?.[3].logical_percent).toBe(15)
  })
  it('rejects stale, malformed, and previous-owner observations', () => {
    for (const patch of [{ revision: 3 }, { connection_generation: 1 }, { host_instance_id: 'other' }, { channels: [] }])
      expect(mergePushedPWM(snapshot, { ...push(), ...patch }, 'primary')).toBe(snapshot)
  })
  it('protects newer pushes from an older snapshot but drops them across board ownership', () => {
    const next = mergePushedPWM(snapshot, push(), 'primary')
    expect(mergePWMSnapshot(next, snapshot).pwm_revision).toBe(4)
    expect(mergePWMSnapshot(next, { ...snapshot, connection_generation: 3 }).pwm_revision).toBe(3)
  })
})

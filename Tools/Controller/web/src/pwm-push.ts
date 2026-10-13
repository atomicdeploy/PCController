import type { PWMValues, Snapshot } from './types'
import { normalizePWMValues } from './pwm-authority'

export function mergePushedPWM(snapshot: Snapshot, value: PWMValues, instanceID?: string): Snapshot {
  if (!snapshot.connected || !instanceID || instanceID !== snapshot.host_instance_id ||
      value.host_instance_id !== instanceID || value.connection_generation !== snapshot.connection_generation ||
      !Number.isSafeInteger(value.revision) || value.revision! <= (snapshot.pwm_revision ?? 0) ||
      !value.time || !Number.isFinite(Date.parse(value.time)) ||
      !Array.isArray(value.channels) || value.channels.length !== 16 ||
      new Set(value.channels.map(c => c.channel)).size !== 16 ||
      value.channels.some(c => !Number.isInteger(c.channel) || c.channel < 0 || c.channel > 15 ||
        !Number.isInteger(c.raw_value) || c.raw_value < 0 || c.raw_value > 4095 ||
        !Number.isFinite(c.logical_percent) || c.logical_percent < 0 || c.logical_percent > 100)) return snapshot
  try {
    const values = normalizePWMValues(value)
    return { ...snapshot, pwm_values: values, pwm_revision: value.revision, pwm_updated: value.time }
  } catch { return snapshot }
}

export function mergePWMSnapshot(current: Snapshot, incoming: Snapshot): Snapshot {
  if (incoming.connected && current.host_instance_id === incoming.host_instance_id &&
      current.connection_generation === incoming.connection_generation &&
      (current.pwm_revision ?? 0) > (incoming.pwm_revision ?? 0)) {
    return { ...incoming, pwm_values: current.pwm_values, pwm_revision: current.pwm_revision, pwm_updated: current.pwm_updated }
  }
  return incoming
}

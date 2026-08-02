import type { Snapshot } from './types'

export function boardSettingsGeneration(snapshot: Snapshot): string {
  return [
    snapshot.port.instance_id || snapshot.port.serial_number || snapshot.port.name || 'controller',
    snapshot.hello.build_hash ?? '',
    snapshot.hello.build_timestamp || '',
  ].join('|')
}

// Allows one quiet read per connected identity while settings are absent.
export class BoardSettingsReadGate {
  private requestedGeneration = ''

  shouldRead(snapshot: Snapshot, settingsPageActive: boolean): boolean {
    if (!snapshot.connected || snapshot.have_settings) {
      this.requestedGeneration = ''
      return false
    }
    if (!settingsPageActive) return false
    const generation = boardSettingsGeneration(snapshot)
    if (generation === this.requestedGeneration) return false
    this.requestedGeneration = generation
    return true
  }
}

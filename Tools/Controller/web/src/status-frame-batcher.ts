import { peripheralAvailability } from './peripheral-availability'
import type { MetricSample, Snapshot, StatusUpdate } from './types'

export interface AnimationFrameClock {
  request(callback: FrameRequestCallback): number
  cancel(handle: number): void
}

const browserAnimationFrameClock: AnimationFrameClock = {
  request: (callback) => window.requestAnimationFrame(callback),
  cancel: (handle) => window.cancelAnimationFrame(handle),
}

/**
 * Coalesces transport notifications at the browser paint boundary while keeping
 * every frame in arrival order for history construction.
 */
export class StatusFrameBatcher {
  private generation = 0
  private pending: StatusUpdate[] = []
  private frame: number | null = null
  private disposed = false

  constructor(
    private readonly deliver: (updates: readonly StatusUpdate[], generation: number) => void,
    private readonly clock: AnimationFrameClock = browserAnimationFrameClock,
  ) {}

  adoptGeneration(generation: number): boolean {
    if (this.disposed || !Number.isSafeInteger(generation) || generation <= 0 || generation < this.generation) return false
    if (generation === this.generation) return true
    this.generation = generation
    this.pending = []
    if (this.frame !== null) {
      this.clock.cancel(this.frame)
      this.frame = null
    }
    return true
  }

  enqueue(generation: number, update: StatusUpdate): boolean {
    if (!this.adoptGeneration(generation)) return false
    this.pending.push(update)
    if (this.frame !== null) return true
    const scheduledGeneration = this.generation
    this.frame = this.clock.request(() => {
      this.frame = null
      if (this.disposed || scheduledGeneration !== this.generation || this.pending.length === 0) return
      const updates = this.pending
      this.pending = []
      this.deliver(updates, scheduledGeneration)
    })
    return true
  }

  cancelPending(generation: number): void {
    if (this.disposed || generation !== this.generation) return
    this.pending = []
    if (this.frame !== null) {
      this.clock.cancel(this.frame)
      this.frame = null
    }
  }

  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.pending = []
    if (this.frame !== null) {
      this.clock.cancel(this.frame)
      this.frame = null
    }
  }
}

export function sampleFrom(snapshot: Snapshot, at = Date.now()): MetricSample {
  const status = snapshot.status
  const available = peripheralAvailability(snapshot)
  return {
    at,
    ...(available.ina219 ? {
      supply: status.supply_mv / 1000,
      bus: status.bus_mv / 1000,
      current: status.current_ma,
      power: status.power_mw / 1000,
    } : {}),
    ...(available.temperatureLED ? { ledTemp: status.temperature_led_centi_c / 100 } : {}),
    ...(available.temperatureBTAudio ? { btTemp: status.temperature_bt_audio_centi_c / 100 } : {}),
  }
}

export function controllerSnapshotIdentity(snapshot: Snapshot): string {
  if (!snapshot.connected) return ''
  return JSON.stringify([
    snapshot.port.instance_id || '', snapshot.port.serial_number || '', snapshot.port.name || '',
    snapshot.hello.board_kind ?? null, snapshot.hello.name || '',
    snapshot.hello.build_hash ?? null, snapshot.hello.build_timestamp || '',
    snapshot.hello.capabilities ?? null,
  ])
}

export function metricSamplesAfterSnapshot(
  current: MetricSample[],
  previous: Snapshot,
  next: Snapshot,
  at = Date.now(),
): MetricSample[] {
  if (!next.connected || !next.have_status) return []
  const sample = sampleFrom(next, at)
  if (controllerSnapshotIdentity(previous) !== controllerSnapshotIdentity(next)) return [sample]
  return [...current.slice(-71), sample]
}

export interface AppliedStatusFrameBatch {
  snapshot: Snapshot
  samples: MetricSample[]
  detail: string
  applied: number
}

/** Applies all successful samples in order, while exposing only the newest snapshot. */
export function applyStatusFrameBatch(
  previous: Snapshot,
  currentSamples: MetricSample[],
  updates: readonly StatusUpdate[],
): AppliedStatusFrameBatch {
  let snapshot = previous
  let samples = currentSamples
  let detail = ''
  let applied = 0
  for (const update of updates) {
    if (update.error) {
      detail = update.error
      continue
    }
    detail = ''
    const next = {
      ...snapshot,
      connected: true,
      have_status: true,
      status: update.status,
      status_updated: update.time,
    }
    samples = metricSamplesAfterSnapshot(samples, snapshot, next, new Date(update.time).getTime())
    snapshot = next
    applied += 1
  }
  return { snapshot, samples, detail, applied }
}

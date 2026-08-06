import type { PWMValues } from './types'

export const USER_PWM_CHANNELS = Object.freeze(Array.from({ length: 11 }, (_, channel) => channel))

export function clampPWMValue(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(4095, Math.round(value)))
}

export function pwmPercent(value: number): number {
  return Math.round(clampPWMValue(value) * 100 / 4095)
}

export function pwmValue(percent: number): number {
  if (!Number.isFinite(percent)) return 0
  return clampPWMValue(Math.max(0, Math.min(100, percent)) * 4095 / 100)
}

export function normalizePWMValues(value: PWMValues): PWMValues {
  if (typeof value?.available !== 'boolean' || !Number.isInteger(value?.selected_channel) ||
    value.selected_channel < 0 || value.selected_channel > 15 || !Array.isArray(value?.values) ||
    value.values.length !== 16) {
    throw new Error('Controller returned an invalid PWM snapshot')
  }
  const values = value.values.map((candidate) => {
    if (!Number.isInteger(candidate) || candidate < 0 || candidate > 4095) {
      throw new Error('Controller returned an out-of-range PWM value')
    }
    return candidate
  })
  return { available: value.available, selected_channel: value.selected_channel, values }
}

interface DesiredPWMValue {
  value: number
  revision: number
}

type QueuedPWMOperation = () => void

// PWM endpoints return the complete 16-channel snapshot. Serializing every
// read and write prevents an older response from racing a newer mutation and
// replacing authoritative values out of order.
export class PWMOperationQueue {
  private readonly queued: QueuedPWMOperation[] = []
  private active = false

  run<T>(operation: () => Promise<T>): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      this.queued.push(() => {
        let result: Promise<T>
        try {
          result = operation()
        } catch (cause) {
          reject(cause)
          this.advance()
          return
        }
        void result.then(
          (value) => {
            resolve(value)
            this.advance()
          },
          (cause) => {
            reject(cause)
            this.advance()
          },
        )
      })
      this.drain()
    })
  }

  private drain(): void {
    if (this.active) return
    const operation = this.queued.shift()
    if (!operation) return
    this.active = true
    operation()
  }

  private advance(): void {
    this.active = false
    this.drain()
  }
}

export const PWM_RECONCILIATION_INTERVAL_MS = 1_500

export interface PWMReconcilerOptions {
  intervalMS?: number
  operations: PWMOperationQueue
  canRead: () => boolean
  read: (signal: AbortSignal) => Promise<PWMValues>
  onAuthoritative: (values: PWMValues) => void
  onError: (cause: Error) => void
}

// PWMReconciler uses a chained timeout rather than setInterval, so a slow
// controller can never accumulate overlapping reads. Demand is checked before
// every request; stop() also invalidates an in-flight response.
export class PWMReconciler {
  private readonly intervalMS: number
  private readonly options: PWMReconcilerOptions
  private timer: ReturnType<typeof setTimeout> | null = null
  private readAbort: AbortController | null = null
  private generation = 0
  private running = false

  constructor(options: PWMReconcilerOptions) {
    this.options = options
    this.intervalMS = Math.max(250, Math.min(30_000, Math.round(options.intervalMS ?? PWM_RECONCILIATION_INTERVAL_MS)))
  }

  start(): void {
    if (this.running) return
    this.running = true
    const generation = ++this.generation
    this.schedule(0, generation)
  }

  stop(): void {
    this.running = false
    this.generation++
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
    this.readAbort?.abort()
    this.readAbort = null
  }

  private schedule(delayMS: number, generation: number): void {
    if (!this.running || generation !== this.generation) return
    this.timer = setTimeout(() => {
      this.timer = null
      void this.tick(generation)
    }, delayMS)
  }

  private async tick(generation: number): Promise<void> {
    if (!this.running || generation !== this.generation) return
    if (!this.options.canRead()) {
      this.schedule(this.intervalMS, generation)
      return
    }
    const readAbort = new AbortController()
    this.readAbort = readAbort
    try {
      const response = normalizePWMValues(await this.options.operations.run(() => this.options.read(readAbort.signal)))
      if (this.running && generation === this.generation) this.options.onAuthoritative(response)
    } catch (cause) {
      if (this.running && generation === this.generation) {
        this.options.onError(cause instanceof Error ? cause : new Error(String(cause)))
      }
    } finally {
      if (this.readAbort === readAbort) this.readAbort = null
      this.schedule(this.intervalMS, generation)
    }
  }
}

export interface PWMMutationSchedulerOptions {
  delayMS?: number
  operations?: PWMOperationQueue
  commit: (channel: number, value: number) => Promise<PWMValues>
  onDraft: (channel: number, value: number) => void
  onAuthoritative: (values: PWMValues, acknowledgedChannel: number | null) => void
  onPending: (channels: readonly number[]) => void
  onError: (channel: number, cause: Error) => void
}

// PWMMutationScheduler keeps at most one write in flight per channel and one
// latest coalesced successor. Acknowledgements carry the controller's complete
// 16-channel state; drafts never masquerade as board-reported values.
export class PWMMutationScheduler {
  private readonly delayMS: number
  private readonly options: PWMMutationSchedulerOptions
  private readonly operations: PWMOperationQueue
  private readonly desired = new Map<number, DesiredPWMValue>()
  private readonly inFlight = new Set<number>()
  private readonly timers = new Map<number, ReturnType<typeof setTimeout>>()
  private revision = 0
  private disposed = false

  constructor(options: PWMMutationSchedulerOptions) {
    this.options = options
    this.operations = options.operations ?? new PWMOperationQueue()
    this.delayMS = Math.max(0, Math.round(options.delayMS ?? 140))
  }

  schedule(channel: number, value: number, immediate = false): void {
    if (this.disposed) return
    if (!USER_PWM_CHANNELS.includes(channel)) {
      this.options.onError(channel, new Error('Generic PWM controls are limited to user channels 0..10'))
      return
    }
    const bounded = clampPWMValue(value)
    this.desired.set(channel, { value: bounded, revision: ++this.revision })
    this.options.onDraft(channel, bounded)
    const timer = this.timers.get(channel)
    if (timer) clearTimeout(timer)
    this.timers.delete(channel)
    this.notifyPending()
    if (immediate) {
      void this.flush(channel)
      return
    }
    this.timers.set(channel, setTimeout(() => {
      this.timers.delete(channel)
      void this.flush(channel)
    }, this.delayMS))
  }

  pendingChannels(): number[] {
    return [...new Set([...this.desired.keys(), ...this.inFlight])].sort((a, b) => a - b)
  }

  dispose(): void {
    this.disposed = true
    for (const timer of this.timers.values()) clearTimeout(timer)
    this.timers.clear()
    this.desired.clear()
  }

  private async flush(channel: number): Promise<void> {
    if (this.disposed || this.inFlight.has(channel)) return
    const requested = this.desired.get(channel)
    if (!requested) return
    this.inFlight.add(channel)
    this.notifyPending()
    try {
      const response = normalizePWMValues(await this.operations.run(() => this.options.commit(channel, requested.value)))
      if (this.disposed) return
      const latest = this.desired.get(channel)
      const acknowledged = latest?.revision === requested.revision
      if (acknowledged) this.desired.delete(channel)
      this.options.onAuthoritative(response, acknowledged ? channel : null)
    } catch (cause) {
      if (this.disposed) return
      const latest = this.desired.get(channel)
      if (latest?.revision === requested.revision) this.desired.delete(channel)
      this.options.onError(channel, cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      this.inFlight.delete(channel)
      if (!this.disposed && this.desired.has(channel)) {
        void this.flush(channel)
      }
      this.notifyPending()
    }
  }

  private notifyPending(): void {
    if (!this.disposed) this.options.onPending(this.pendingChannels())
  }
}

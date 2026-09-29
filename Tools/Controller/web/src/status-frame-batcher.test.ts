import { describe, expect, it } from 'vitest'
import { applyStatusFrameBatch, type AnimationFrameClock, StatusFrameBatcher } from './status-frame-batcher'
import { emptySnapshot, type StatusUpdate } from './types'

class FakeAnimationFrameClock implements AnimationFrameClock {
  nextID = 1
  callbacks = new Map<number, FrameRequestCallback>()
  cancelled: number[] = []

  request(callback: FrameRequestCallback): number {
    const id = this.nextID++
    this.callbacks.set(id, callback)
    return id
  }

  cancel(handle: number): void {
    this.cancelled.push(handle)
  }

  fire(handle: number): void {
    this.callbacks.get(handle)?.(handle)
  }
}

function update(index: number): StatusUpdate {
  return {
    time: new Date(Date.UTC(2026, 8, 29, 0, 0, 0, index)).toISOString(),
    status: {
      ...emptySnapshot.status,
      uptime_ms: index,
      supply_mv: 10_000 + index,
      ina219_available: true,
    },
  }
}

describe('status paint-frame batching', () => {
  it('delivers a burst once per animation frame and retains ordered bounded history', () => {
    const clock = new FakeAnimationFrameClock()
    const deliveries: (readonly StatusUpdate[])[] = []
    const batcher = new StatusFrameBatcher((updates) => deliveries.push(updates), clock)
    for (let index = 0; index < 100; index += 1) batcher.enqueue(7, update(index))

    expect(clock.callbacks.size).toBe(1)
    clock.fire(1)
    expect(deliveries).toHaveLength(1)
    expect(deliveries[0].map(({ status }) => status.uptime_ms)).toEqual(
      Array.from({ length: 100 }, (_, index) => index),
    )

    const initial = {
      ...emptySnapshot,
      connected: true,
      have_status: true,
      port: { instance_id: 'controller-a' },
      hello: { capabilities: 1 },
    }
    const result = applyStatusFrameBatch(initial, [], deliveries[0])
    expect(result.snapshot.status.uptime_ms).toBe(99)
    expect(result.snapshot.status_updated).toBe(update(99).time)
    expect(result.samples).toHaveLength(72)
    expect(result.samples.map(({ supply }) => supply)).toEqual(
      Array.from({ length: 72 }, (_, index) => (10_028 + index) / 1000),
    )
  })

  it('cancels a pending callback when a newer transport generation is adopted', () => {
    const clock = new FakeAnimationFrameClock()
    const deliveries: number[][] = []
    const batcher = new StatusFrameBatcher(
      (updates) => deliveries.push(updates.map(({ status }) => status.uptime_ms)),
      clock,
    )
    batcher.enqueue(3, update(3))
    expect(batcher.adoptGeneration(4)).toBe(true)
    expect(clock.cancelled).toEqual([1])
    clock.fire(1)
    expect(deliveries).toEqual([])
    expect(batcher.enqueue(3, update(30))).toBe(false)
    batcher.enqueue(4, update(4))
    clock.fire(2)
    expect(deliveries).toEqual([[4]])
  })

  it('cancels pending work on disposal and never delivers it', () => {
    const clock = new FakeAnimationFrameClock()
    let deliveries = 0
    const batcher = new StatusFrameBatcher(() => { deliveries += 1 }, clock)
    batcher.enqueue(1, update(1))
    batcher.dispose()
    expect(clock.cancelled).toEqual([1])
    clock.fire(1)
    expect(deliveries).toBe(0)
    expect(batcher.enqueue(2, update(2))).toBe(false)
  })

  it('lets a later success in the same frame supersede a transient error', () => {
    const error = { ...update(1), error: 'temporarily unavailable' }
    expect(applyStatusFrameBatch(emptySnapshot, [], [error]).detail).toBe('temporarily unavailable')
    const recovered = applyStatusFrameBatch(emptySnapshot, [], [error, update(2)])
    expect(recovered.detail).toBe('')
    expect(recovered.snapshot.status.uptime_ms).toBe(2)
  })
})

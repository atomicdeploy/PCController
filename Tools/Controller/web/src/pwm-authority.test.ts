import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  normalizePWMValues,
  PWMMutationScheduler,
  PWMOperationQueue,
  PWMReconciler,
  USER_PWM_CHANNELS,
} from './pwm-authority'
import type { PWMValues } from './types'

function values(channel: number, value: number): PWMValues {
  const all = Array(16).fill(0)
  all[channel] = value
  return { available: true, selected_channel: channel, values: all }
}

afterEach(() => vi.useRealTimers())

describe('authoritative PWM contract', () => {
  it('exposes only user channels through the generic mixer and validates full snapshots', () => {
    expect(USER_PWM_CHANNELS).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
    expect(normalizePWMValues(values(10, 4095)).values[10]).toBe(4095)
    expect(() => normalizePWMValues({ ...values(0, 0), values: Array(15).fill(0) })).toThrow(/invalid PWM snapshot/)
    expect(() => normalizePWMValues(values(0, 4096))).toThrow(/out-of-range/)
  })

  it('coalesces rapid drafts and acknowledges only the board-reported result', async () => {
    vi.useFakeTimers()
    const commits: Array<[number, number]> = []
    const acknowledgements: Array<[number, number | null]> = []
    const scheduler = new PWMMutationScheduler({
      delayMS: 100,
      commit: async (channel, value) => {
        commits.push([channel, value])
        return values(channel, value)
      },
      onDraft: vi.fn(),
      onAuthoritative: (snapshot, channel) => acknowledgements.push([snapshot.values[snapshot.selected_channel], channel]),
      onPending: vi.fn(),
      onError: vi.fn(),
    })
    scheduler.schedule(3, 100)
    scheduler.schedule(3, 200)
    scheduler.schedule(3, 300)
    await vi.advanceTimersByTimeAsync(100)
    await Promise.resolve()
    expect(commits).toEqual([[3, 300]])
    expect(acknowledgements).toEqual([[300, 3]])
    expect(scheduler.pendingChannels()).toEqual([])
    scheduler.dispose()
  })

  it('serializes an in-flight channel and retains only its latest successor', async () => {
    let release: ((snapshot: PWMValues) => void) | undefined
    const commits: number[] = []
    const scheduler = new PWMMutationScheduler({
      delayMS: 0,
      commit: (channel, value) => {
        commits.push(value)
        if (commits.length === 1) return new Promise((resolve) => { release = resolve })
        return Promise.resolve(values(channel, value))
      },
      onDraft: vi.fn(), onAuthoritative: vi.fn(), onPending: vi.fn(), onError: vi.fn(),
    })
    scheduler.schedule(4, 500, true)
    scheduler.schedule(4, 600, true)
    scheduler.schedule(4, 700, true)
    expect(commits).toEqual([500])
    release?.(values(4, 500))
    await Promise.resolve()
    await Promise.resolve()
    expect(commits).toEqual([500, 700])
    scheduler.dispose()
  })

  it('serializes writes across channels because acknowledgements are complete snapshots', async () => {
    let release: ((snapshot: PWMValues) => void) | undefined
    const commits: Array<[number, number]> = []
    const scheduler = new PWMMutationScheduler({
      delayMS: 0,
      commit: (channel, value) => {
        commits.push([channel, value])
        if (commits.length === 1) return new Promise((resolve) => { release = resolve })
        return Promise.resolve(values(channel, value))
      },
      onDraft: vi.fn(), onAuthoritative: vi.fn(), onPending: vi.fn(), onError: vi.fn(),
    })

    scheduler.schedule(1, 400, true)
    scheduler.schedule(7, 800, true)
    expect(commits).toEqual([[1, 400]])
    release?.(values(1, 400))
    await Promise.resolve()
    await Promise.resolve()
    expect(commits).toEqual([[1, 400], [7, 800]])
    scheduler.dispose()
  })

  it('reconciles an externally changed unselected channel from complete snapshots', async () => {
    vi.useFakeTimers()
    const operations = new PWMOperationQueue()
    let board = values(2, 600)
    let displayed = Array(16).fill(0)
    const read = vi.fn(async () => ({ ...board, values: [...board.values] }))
    const reconciler = new PWMReconciler({
      intervalMS: 250,
      operations,
      canRead: () => true,
      read,
      onAuthoritative: (snapshot) => { displayed = snapshot.values },
      onError: vi.fn(),
    })

    reconciler.start()
    await vi.advanceTimersByTimeAsync(0)
    expect(displayed[2]).toBe(600)
    board = { ...board, values: board.values.map((value, channel) => channel === 8 ? 2800 : value) }
    await vi.advanceTimersByTimeAsync(250)

    expect(read).toHaveBeenCalledTimes(2)
    expect(board.selected_channel).toBe(2)
    expect(displayed[8]).toBe(2800)
    reconciler.stop()
  })

  it('skips reads without demand and stops scheduling when the view goes offline', async () => {
    vi.useFakeTimers()
    let online = false
    const read = vi.fn(async () => values(1, 100))
    const reconciler = new PWMReconciler({
      intervalMS: 250,
      operations: new PWMOperationQueue(),
      canRead: () => online,
      read,
      onAuthoritative: vi.fn(),
      onError: vi.fn(),
    })

    reconciler.start()
    await vi.advanceTimersByTimeAsync(750)
    expect(read).not.toHaveBeenCalled()
    online = true
    await vi.advanceTimersByTimeAsync(250)
    expect(read).toHaveBeenCalledTimes(1)
    online = false
    reconciler.stop()
    expect(vi.getTimerCount()).toBe(0)
    online = true
    await vi.advanceTimersByTimeAsync(1_000)
    expect(read).toHaveBeenCalledTimes(1)
  })

  it('suppresses an in-flight reconciliation result after unmount', async () => {
    vi.useFakeTimers()
    let release: ((snapshot: PWMValues) => void) | undefined
    let readSignal: AbortSignal | undefined
    const onAuthoritative = vi.fn()
    const reconciler = new PWMReconciler({
      intervalMS: 250,
      operations: new PWMOperationQueue(),
      canRead: () => true,
      read: (signal) => {
        readSignal = signal
        return new Promise((resolve) => { release = resolve })
      },
      onAuthoritative,
      onError: vi.fn(),
    })

    reconciler.start()
    await vi.advanceTimersByTimeAsync(0)
    reconciler.stop()
    expect(readSignal?.aborted).toBe(true)
    release?.(values(5, 900))
    await Promise.resolve()
    await Promise.resolve()

    expect(onAuthoritative).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('serializes reconciliation reads with writes on the shared operation queue', async () => {
    const operations = new PWMOperationQueue()
    let active = 0
    let peak = 0
    let releaseFirst: (() => void) | undefined
    const operation = (hold = false) => operations.run(async () => {
      active++
      peak = Math.max(peak, active)
      if (hold) await new Promise<void>((resolve) => { releaseFirst = resolve })
      active--
      return values(0, 0)
    })

    const first = operation(true)
    const second = operation()
    expect(active).toBe(1)
    releaseFirst?.()
    await Promise.all([first, second])

    expect(peak).toBe(1)
  })
})

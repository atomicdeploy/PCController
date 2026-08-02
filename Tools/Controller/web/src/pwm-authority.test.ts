import { afterEach, describe, expect, it, vi } from 'vitest'
import { normalizePWMValues, PWMMutationScheduler, USER_PWM_CHANNELS } from './pwm-authority'
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
})

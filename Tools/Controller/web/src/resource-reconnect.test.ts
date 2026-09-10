import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createResourceReconnectCheck, embeddedResourcesMismatch } from './resource-version'

const original = { host_version: 'release-a', build_time: '2026-09-10T01:00:00Z' }
const updated = { host_version: 'release-b', build_time: '2026-09-10T02:00:00Z' }
const embedded = { hostVersion: original.host_version, buildTime: original.build_time }
const settle = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve() }

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('host resource checks on transport reconnect', () => {
  it('updates an already-open client after external replacement without a completion event', async () => {
    const load = vi.fn().mockResolvedValueOnce(original).mockResolvedValueOnce(updated)
    const reload = vi.fn()
    const check = createResourceReconnectCheck(load, (config) => {
      if (!embeddedResourcesMismatch(config, embedded)) return false
      reload()
      return true
    })
    check.state('open')
    await settle()
    expect(reload).not.toHaveBeenCalled()
    check.state('waiting')
    check.state('connecting')
    check.state('open')
    await settle()
    expect(load).toHaveBeenCalledTimes(2)
    expect(reload).toHaveBeenCalledOnce()
    check.state('open') // repeated diagnostic, not another attachment
    await settle()
    expect(load).toHaveBeenCalledTimes(2)
    check.dispose()
  })

  it('cancels old-generation requests and ignores late replies even if a loader ignores abort', async () => {
    const replies: Array<(config: typeof original) => void> = []
    const signals: AbortSignal[] = []
    const load = vi.fn((signal: AbortSignal) => {
      signals.push(signal)
      return new Promise<typeof original>((resolve) => replies.push(resolve))
    })
    const apply = vi.fn(() => false)
    const check = createResourceReconnectCheck(load, apply)
    check.state('open')
    check.state('waiting')
    expect(signals[0].aborted).toBe(true)
    check.state('open')
    replies[0](updated)
    await settle()
    expect(apply).not.toHaveBeenCalled()
    replies[1](original)
    await settle()
    expect(apply).toHaveBeenCalledExactlyOnceWith(original)
    check.dispose()
  })

  it('disposes pending checks with the owning view and never reloads afterward', async () => {
    const owner = new AbortController()
    let reply!: (config: typeof original) => void
    let request!: AbortSignal
    const load = vi.fn((signal: AbortSignal) => {
      request = signal
      return new Promise<typeof original>((resolve) => { reply = resolve })
    })
    const apply = vi.fn(() => true)
    const check = createResourceReconnectCheck(load, apply, { signal: owner.signal })
    check.state('open')
    owner.abort()
    expect(request.aborted).toBe(true)
    reply(updated)
    check.state('open')
    await settle()
    expect(apply).not.toHaveBeenCalled()
    expect(load).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('bounds startup endpoint recovery to two five-second attempts', async () => {
    const load = vi.fn((signal: AbortSignal) => new Promise<typeof original>((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(signal.reason), { once: true })
    }))
    const failure = vi.fn()
    const apply = vi.fn(() => false)
    const check = createResourceReconnectCheck(load, apply, { onError: failure })
    check.state('open')
    await vi.advanceTimersByTimeAsync(10_250)
    expect(load).toHaveBeenCalledTimes(2)
    expect(failure).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ message: 'Host resource identity check timed out' }))
    expect(apply).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(load).toHaveBeenCalledTimes(2)
    check.dispose()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('recovers one transient endpoint failure and cancels scheduled retry on disconnect', async () => {
    const load = vi.fn().mockRejectedValueOnce(new Error('starting')).mockResolvedValue(original)
    const apply = vi.fn(() => false)
    const failure = vi.fn()
    const check = createResourceReconnectCheck(load, apply, { onError: failure })
    check.state('open')
    await vi.advanceTimersByTimeAsync(250)
    expect(load).toHaveBeenCalledTimes(2)
    expect(apply).toHaveBeenCalledExactlyOnceWith(original)
    expect(failure).not.toHaveBeenCalled()
    check.state('waiting')
    load.mockRejectedValueOnce(new Error('starting again'))
    check.state('open')
    await settle()
    check.state('waiting')
    await vi.advanceTimersByTimeAsync(1_000)
    expect(load).toHaveBeenCalledTimes(3)
    check.dispose()
  })
})

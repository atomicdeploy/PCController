import { describe, expect, it, vi } from 'vitest'
import { HoldActionSession } from './hold-action'

const flush = async () => {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

describe('fail-safe hold actions', () => {
  it('orders stop after an in-flight start and emits each exactly once', async () => {
    let finishStart: (() => void) | undefined
    const start = vi.fn(() => new Promise<void>((resolve) => { finishStart = resolve }))
    const stop = vi.fn(async () => undefined)
    const holding: boolean[] = []
    const session = new HoldActionSession(start, stop, vi.fn(), (value) => holding.push(value))

    expect(session.begin()).toBe(true)
    expect(session.begin()).toBe(false)
    await flush()
    expect(start).toHaveBeenCalledTimes(1)
    expect(session.release()).toBe(true)
    expect(session.release()).toBe(false)
    expect(stop).not.toHaveBeenCalled()
    finishStart?.()
    await flush()
    expect(stop).toHaveBeenCalledTimes(1)
    expect(holding).toEqual([true, false])
  })

  it('best-effort stops after a rejected start', async () => {
    const error = new Error('start failed')
    const stop = vi.fn(async () => undefined)
    const onError = vi.fn()
    const session = new HoldActionSession(async () => { throw error }, stop, onError, vi.fn())

    session.begin()
    await flush()
    expect(onError).toHaveBeenCalledWith(error)
    expect(stop).toHaveBeenCalledTimes(1)
    expect(session.isHolding()).toBe(false)
  })

  it('supports silent unmount release without losing the stop command', async () => {
    const holding = vi.fn()
    const stop = vi.fn(async () => undefined)
    const session = new HoldActionSession(async () => undefined, stop, vi.fn(), holding)

    session.begin()
    session.release(false)
    await flush()
    expect(stop).toHaveBeenCalledTimes(1)
    expect(holding).toHaveBeenCalledTimes(1)
    expect(holding).toHaveBeenCalledWith(true)
  })
})

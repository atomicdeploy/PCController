import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { browserControllerStateEvent, publishBrowserController, publishBrowserControllerState } from './browser-controller'

describe('living browser controller', () => {
  beforeEach(() => {
    vi.stubGlobal('window', new EventTarget())
    vi.stubGlobal('CustomEvent', class TestCustomEvent<T> extends Event {
      readonly detail: T
      constructor(type: string, init: CustomEventInit<T>) {
        super(type)
        this.detail = init.detail as T
      }
    })
  })

  afterEach(() => vi.unstubAllGlobals())

  it('publishes a frozen unversioned controller and restores the previous value', () => {
    const previous = window.PCController
    const controller = {
      api: 'PCController.browser' as const,
      inspect: () => ({ title: 'PCController', hostVersion: 'x', page: 'dashboard', hostOnline: true, boardConnected: false, port: '', transport: 'open', eventCount: 0 }),
      command: vi.fn(), refresh: vi.fn(), navigate: vi.fn(),
    }
    const dispose = publishBrowserController(controller)
    expect(window.PCController).toBe(controller)
    expect(Object.isFrozen(window.PCController)).toBe(true)
    dispose()
    expect(window.PCController).toBe(previous)
  })

  it('emits immutable host and board state without conflating them', () => {
    const listener = vi.fn()
    window.addEventListener(browserControllerStateEvent, listener)
    publishBrowserControllerState({ title: 'PCController', hostVersion: 'x', page: 'dashboard', hostOnline: true, boardConnected: false, port: '', transport: 'open', eventCount: 2 })
    expect(listener).toHaveBeenCalledOnce()
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toMatchObject({ hostOnline: true, boardConnected: false, eventCount: 2 })
  })
})

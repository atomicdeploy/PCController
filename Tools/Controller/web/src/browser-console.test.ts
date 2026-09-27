import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { browserControllerStateEvent, normalizeBrowserBeep, publishBrowserConsole, publishBrowserConsoleState } from './browser-console'

describe('browser console controller', () => {
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

  it('publishes a frozen normalized controller and restores the previous value', () => {
    const previous = window.PCController
    const controller = { api: 'PCController.browser/1' as const, inspect: () => ({ title: 'PCController', hostVersion: 'x', page: 'dashboard', connected: true, port: 'COM4', transport: 'open', eventCount: 0 }), command: vi.fn(), beep: vi.fn(), refresh: vi.fn(), navigate: vi.fn() }
    const dispose = publishBrowserConsole(controller)
    expect(window.PCController).toBe(controller)
    expect(Object.isFrozen(window.PCController)).toBe(true)
    dispose()
    expect(window.PCController).toBe(previous)
  })

  it('normalizes safe browser-console beep arguments', () => {
    expect(normalizeBrowserBeep()).toEqual({ frequencyHz: 2000, durationMS: 40, target: 'both' })
    expect(normalizeBrowserBeep(440.4, 70.8, 'browser')).toEqual({ frequencyHz: 440, durationMS: 71, target: 'browser' })
    expect(() => normalizeBrowserBeep(0)).toThrow(/frequencyHz/)
  })

  it('emits immutable browser state events', () => {
    const listener = vi.fn()
    ;(window as unknown as EventTarget).addEventListener(browserControllerStateEvent, listener)
    publishBrowserConsoleState({ title: 'PCController', hostVersion: 'x', page: 'dashboard', connected: true, port: 'COM4', transport: 'open', eventCount: 2 })
    expect(listener).toHaveBeenCalledOnce()
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toMatchObject({ port: 'COM4', eventCount: 2 })
    ;(window as unknown as EventTarget).removeEventListener(browserControllerStateEvent, listener)
  })
})

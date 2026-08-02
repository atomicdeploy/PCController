import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  clearControllerOrigin,
  controllerChannelOrigin,
  controllerHTTPURL,
  controllerOriginStorageKey,
  controllerWebSocketURL,
  resolveControllerTransport,
  setControllerOrigin,
  validateControllerOrigin,
} from './transport-config'

class MemoryStorage {
  private readonly values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

afterEach(() => vi.unstubAllGlobals())

describe('portable controller transport', () => {
  it('keeps the embedded REST and WebSocket transport same-origin by default', () => {
    const transport = resolveControllerTransport({
      href: 'http://127.0.0.1:8787/#/dashboard',
      origin: 'http://127.0.0.1:8787',
      localStorage: new MemoryStorage(),
    })
    expect(transport).toMatchObject({
      source: 'same-origin',
      external: false,
      httpOrigin: 'http://127.0.0.1:8787',
      websocketOrigin: 'ws://127.0.0.1:8787',
    })
  })

  it('accepts, normalizes, and persists an explicit local query target', () => {
    const storage = new MemoryStorage()
    const transport = resolveControllerTransport({
      href: 'http://localhost:4177/?controller=ws%3A%2F%2F192.168.10.20%3A8787#/dashboard',
      origin: 'http://localhost:4177',
      localStorage: storage,
    })
    expect(transport).toMatchObject({
      source: 'query',
      controllerOrigin: 'ws://192.168.10.20:8787',
      httpOrigin: 'http://192.168.10.20:8787',
      websocketOrigin: 'ws://192.168.10.20:8787',
      external: true,
    })
    expect(storage.getItem(controllerOriginStorageKey)).toBe('ws://192.168.10.20:8787')

    expect(resolveControllerTransport({
      href: 'http://localhost:4177/#/dashboard',
      origin: 'http://localhost:4177',
      localStorage: storage,
    }).source).toBe('local-setting')
  })

  it('fails closed for credentials, paths, queries, and untrusted public targets', () => {
    for (const value of [
      'https://user:password@controller.example',
      'https://controller.example/api',
      'https://controller.example?access_token=secret',
      'file:///tmp/controller',
      'https://controller.example',
    ]) {
      expect(validateControllerOrigin(value).valid, value).toBe(false)
    }
    expect(() => resolveControllerTransport({
      href: 'https://ui.example/?controller=https%3A%2F%2Fhostile.example',
      origin: 'https://ui.example',
      localStorage: new MemoryStorage(),
    })).toThrow(/Invalid controller query target/)
  })

  it('allows a public secure target only through an exact generated trust entry', () => {
    const config = {
      controller_origin: 'wss://controller.example:9443',
      trusted_controller_origins: ['https://other.example', 'wss://controller.example:9443'],
    }
    const transport = resolveControllerTransport({
      href: 'https://ui.example/',
      origin: 'https://ui.example',
      localStorage: new MemoryStorage(),
      generatedConfig: config,
    })
    expect(transport).toMatchObject({
      source: 'generated-config',
      httpOrigin: 'https://controller.example:9443',
      websocketOrigin: 'wss://controller.example:9443',
    })
  })

  it('builds canonical public API and local WebSocket URLs without versioned aliases', () => {
    const storage = new MemoryStorage()
    vi.stubGlobal('location', new URL('http://localhost:4177/?controller=http%3A%2F%2F127.0.0.1%3A8787'))
    vi.stubGlobal('localStorage', storage)
    vi.stubGlobal('PCControllerWebConfig', undefined)

    expect(controllerHTTPURL('/api/v1/ui-config')).toBe('http://127.0.0.1:8787/api/v1/ui-config')
    expect(controllerWebSocketURL('/ipc')).toBe('ws://127.0.0.1:8787/ipc')
    expect(controllerChannelOrigin()).toBe('http://127.0.0.1:8787')
    expect(controllerHTTPURL('/api/v1/ui-config')).toContain('/api/v1/')
  })

  it('supports deliberate local-setting updates without storing secrets', () => {
    const storage = new MemoryStorage()
    vi.stubGlobal('location', new URL('http://localhost:4177/'))
    vi.stubGlobal('localStorage', storage)
    vi.stubGlobal('PCControllerWebConfig', undefined)

    expect(setControllerOrigin('http://[::1]:8787').httpOrigin).toBe('http://[::1]:8787')
    expect(storage.getItem(controllerOriginStorageKey)).toBe('http://[::1]:8787')
    expect(() => setControllerOrigin('http://user:secret@127.0.0.1:8787')).toThrow()
    expect(storage.getItem(controllerOriginStorageKey)).not.toContain('secret')
    clearControllerOrigin()
    expect(storage.getItem(controllerOriginStorageKey)).toBeNull()
  })
})

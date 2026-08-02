import { localServiceHostname, normalizeRootURLInput } from './settings-validation'

export const controllerOriginQueryKey = 'controller'
export const controllerOriginStorageKey = 'pccontroller.controller-origin'

type TransportSource = 'same-origin' | 'query' | 'local-setting' | 'generated-config'

export interface ControllerTransport {
  source: TransportSource
  controllerOrigin: string
  httpOrigin: string
  websocketOrigin: string
  external: boolean
}

interface TransportEnvironment {
  href?: string
  origin?: string
  localStorage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null
  generatedConfig?: PCControllerWebConfig | null
}

function defaultEnvironment(): TransportEnvironment {
  let href = ''
  let origin = ''
  let storage: TransportEnvironment['localStorage'] = null
  try {
    href = globalThis.location?.href ?? ''
    origin = globalThis.location?.origin ?? ''
  } catch {
    // Hardened browser contexts may deny location access.
  }
  try {
    storage = globalThis.localStorage
  } catch {
    // Storage can be disabled without disabling the same-origin UI.
  }
  return {
    href,
    origin,
    localStorage: storage,
    generatedConfig: globalThis.PCControllerWebConfig,
  }
}

function canonicalTarget(value: string): URL | null {
  const input = normalizeRootURLInput(value)
  if (!input) return null
  const candidate = /^[a-z][a-z\d+.-]*:\/\//i.test(input) ? input : `http://${input}`
  let parsed: URL
  try {
    parsed = new URL(candidate)
  } catch {
    return null
  }
  if (!['http:', 'https:', 'ws:', 'wss:'].includes(parsed.protocol) ||
    parsed.username || parsed.password || parsed.search || parsed.hash ||
    (parsed.pathname !== '' && parsed.pathname !== '/')) return null
  parsed.pathname = ''
  return parsed
}

function canonicalOrigin(value: string): string {
  const target = canonicalTarget(value)
  return target ? target.origin.toLowerCase() : ''
}

function trustedOrigins(config: PCControllerWebConfig | null | undefined): Set<string> {
  const result = new Set<string>()
  for (const value of config?.trusted_controller_origins ?? []) {
    if (typeof value !== 'string') continue
    const target = canonicalTarget(value)
    if (!target || (target.protocol !== 'https:' && target.protocol !== 'wss:')) continue
    result.add(target.origin.toLowerCase())
  }
  return result
}

export function validateControllerOrigin(
  value: string,
  explicitTrustedOrigins: ReadonlySet<string> = new Set(),
): { valid: boolean; normalized: string; error: string } {
  const target = canonicalTarget(value)
  if (!target) {
    return { valid: false, normalized: '', error: 'Use an HTTP(S) or WS(S) service root without credentials, a path, query, or fragment.' }
  }
  const local = localServiceHostname(target.hostname)
  const trustedSecure = (target.protocol === 'https:' || target.protocol === 'wss:') &&
    explicitTrustedOrigins.has(target.origin.toLowerCase())
  if (!local && !trustedSecure) {
    return { valid: false, normalized: target.origin, error: 'Use a local/private target or an explicitly trusted HTTPS/WSS origin.' }
  }
  return { valid: true, normalized: target.origin, error: '' }
}

function transportFromOrigin(source: TransportSource, controllerOrigin: string): ControllerTransport {
  const parsed = new URL(controllerOrigin)
  const secure = parsed.protocol === 'https:' || parsed.protocol === 'wss:'
  parsed.protocol = secure ? 'https:' : 'http:'
  const httpOrigin = parsed.origin
  parsed.protocol = secure ? 'wss:' : 'ws:'
  return {
    source,
    controllerOrigin,
    httpOrigin,
    websocketOrigin: parsed.origin,
    external: true,
  }
}

function sameOriginTransport(origin: string): ControllerTransport {
  const normalized = canonicalOrigin(origin)
  if (!normalized) {
    return { source: 'same-origin', controllerOrigin: '', httpOrigin: '', websocketOrigin: '', external: false }
  }
  const parsed = new URL(normalized)
  const secure = parsed.protocol === 'https:'
  parsed.protocol = secure ? 'wss:' : 'ws:'
  return {
    source: 'same-origin',
    controllerOrigin: normalized,
    httpOrigin: normalized,
    websocketOrigin: parsed.origin,
    external: false,
  }
}

export function resolveControllerTransport(environment: TransportEnvironment = defaultEnvironment()): ControllerTransport {
  const generated = environment.generatedConfig
  const trusted = trustedOrigins(generated)
  const generatedValue = typeof generated?.controller_origin === 'string' ? generated.controller_origin.trim() : ''
  if (generatedValue) {
    const validation = validateControllerOrigin(generatedValue, trusted)
    if (!validation.valid) throw new TypeError(`Invalid generated controller origin: ${validation.error}`)
    return transportFromOrigin('generated-config', validation.normalized)
  }

  let queryValue: string | null = null
  if (environment.href) {
    try {
      queryValue = new URL(environment.href).searchParams.get(controllerOriginQueryKey)
    } catch {
      // A malformed page URL cannot create an external transport.
    }
  }
  if (queryValue !== null) {
    const validation = validateControllerOrigin(queryValue, trusted)
    if (!validation.valid) throw new TypeError(`Invalid controller query target: ${validation.error}`)
    try {
      environment.localStorage?.setItem(controllerOriginStorageKey, validation.normalized)
    } catch {
      // Persistence is optional; the current explicit query still applies.
    }
    return transportFromOrigin('query', validation.normalized)
  }

  let stored = ''
  try {
    stored = environment.localStorage?.getItem(controllerOriginStorageKey)?.trim() ?? ''
  } catch {
    // Continue with the ordinary relative transport.
  }
  if (stored) {
    const validation = validateControllerOrigin(stored, trusted)
    if (validation.valid) return transportFromOrigin('local-setting', validation.normalized)
    try {
      environment.localStorage?.removeItem(controllerOriginStorageKey)
    } catch {
      // Invalid storage is ignored even when it cannot be removed.
    }
  }
  return sameOriginTransport(environment.origin ?? '')
}

export function setControllerOrigin(value: string): ControllerTransport {
  const environment = defaultEnvironment()
  const validation = validateControllerOrigin(value, trustedOrigins(environment.generatedConfig))
  if (!validation.valid) throw new TypeError(validation.error)
  environment.localStorage?.setItem(controllerOriginStorageKey, validation.normalized)
  return transportFromOrigin('local-setting', validation.normalized)
}

export function clearControllerOrigin(): void {
  try {
    globalThis.localStorage?.removeItem(controllerOriginStorageKey)
  } catch {
    // Storage is optional.
  }
}

function safeAbsolutePath(value: string): string {
  const path = value.trim()
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\') || /[\u0000-\u001f\u007f]/.test(path)) {
    throw new TypeError('controller transport path must be an absolute local path')
  }
  return path
}

export function controllerHTTPURL(path: string): string {
  const safePath = safeAbsolutePath(path)
  const transport = resolveControllerTransport()
  return transport.external ? new URL(safePath, `${transport.httpOrigin}/`).toString() : safePath
}

export function controllerWebSocketURL(path: string): string {
  const safePath = safeAbsolutePath(path)
  const transport = resolveControllerTransport()
  if (transport.websocketOrigin) return new URL(safePath, `${transport.websocketOrigin}/`).toString()
  const scheme = globalThis.location?.protocol === 'https:' ? 'wss:' : 'ws:'
  return new URL(safePath, `${scheme}//${globalThis.location?.host ?? ''}`).toString()
}

// The tab bus is scoped to the selected controller so two tabs targeting
// different hosts never exchange terminal or controller events.
export function controllerChannelOrigin(): string | undefined {
  const transport = resolveControllerTransport()
  return transport.httpOrigin || undefined
}

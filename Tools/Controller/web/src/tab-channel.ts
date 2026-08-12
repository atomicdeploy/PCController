import type { Appearance, ControllerEvent } from './types'

/** Same-origin protocol name used to isolate controller tab messages. */
export const TAB_CHANNEL_PROTOCOL = `${__PRODUCT_PROTOCOL__}.tab-sync`
/** Wire schema version for same-origin tab synchronization. */
export const TAB_CHANNEL_VERSION = 1 as const

const localTabOrigin = `${__PRODUCT_PROTOCOL__}://local`

const defaultTTLMS = 30_000
const maximumTTLMS = 5 * 60_000
const maximumEnvelopeBytes = 32 * 1024
const maximumTerminalBytes = 8 * 1024
const maximumEventTextBytes = 4 * 1024
const maximumResourceIdentityBytes = 192
const maximumMetadataEntries = 32
const maximumSeenMessages = 512

/** Visibility lifecycle advertised by a browser tab. */
export type PresenceState = 'active' | 'hidden' | 'leaving'
/** Terminal entry classes synchronized between browser tabs. */
export type TerminalEntryKind = 'command' | 'output' | 'error' | 'system'

/** Presence update shared with other same-origin controller tabs. */
export interface PresencePayload {
  type: 'presence'
  state: PresenceState
  page?: string
}

/** Appearance fields permitted to synchronize between tabs. */
export type AppearancePatch = Partial<Pick<
  Appearance,
  'theme' | 'locale' | 'direction' | 'reduceMotion' | 'compactNumbers' | 'audioMuted' | 'audioVolume'
>>

/** Validated appearance update shared between tabs. */
export interface AppearancePayload {
  type: 'appearance'
  appearance: AppearancePatch
  etag?: string
}

/** Bounded terminal record suitable for cross-tab synchronization. */
export interface TerminalEntry {
  kind: TerminalEntryKind
  text: string
  at?: number
}

/** Terminal update shared between tabs. */
export interface TerminalPayload {
  type: 'terminal'
  entry: TerminalEntry
}

/** Credential-free controller event fields allowed on the tab channel. */
export type SharedControllerEvent = Pick<ControllerEvent, 'id' | 'time' | 'kind' | 'text'> &
  Partial<Pick<
    ControllerEvent,
    'state' | 'lifecycle' | 'reason' | 'source' | 'target' | 'message_type' |
    'action' | 'gesture' | 'key' | 'rf_id' | 'rf_code' | 'rf_bits' | 'rf_protocol' |
    'metadata'
  >>

/** Controller event update shared between tabs. */
export interface ControllerEventPayload {
  type: 'controller-event'
  event: SharedControllerEvent
}

/**
 * Credential-free hint that prompts peers to re-fetch the host's authoritative
 * resource identity. Receivers must not reload from this payload alone.
 */
export interface ResourceVersionPayload {
  type: 'resource-version'
  hostVersion: string
  buildTime: string
}

/** Payload variants accepted by the tab-channel wire contract. */
export type TabChannelPayload =
  | PresencePayload
  | AppearancePayload
  | TerminalPayload
  | ControllerEventPayload
  | ResourceVersionPayload

/** Versioned and expiring envelope sent through BroadcastChannel. */
export interface TabChannelEnvelope {
  protocol: typeof TAB_CHANNEL_PROTOCOL
  version: typeof TAB_CHANNEL_VERSION
  origin: string
  messageId: string
  tabId: string
  sentAt: number
  expiresAt: number
  payload: TabChannelPayload
}

/** Subscriber invoked for each validated, non-duplicate envelope. */
export type TabChannelListener = (message: TabChannelEnvelope) => void
/** Identifier namespace requested from a custom ID factory. */
export type TabChannelIDKind = 'tab' | 'message'

/** Minimal BroadcastChannel surface used by the transport and its tests. */
export interface BroadcastChannelPort {
  postMessage(message: unknown): void
  addEventListener(type: 'message', listener: EventListener): void
  removeEventListener(type: 'message', listener: EventListener): void
  close(): void
}

/** Constructor shape accepted for native or test BroadcastChannel adapters. */
export type BroadcastChannelConstructor = new (name: string) => BroadcastChannelPort

/** Optional transport, clock, identity, origin, and expiry dependencies. */
export interface TabChannelOptions {
  origin?: string
  ttlMS?: number
  BroadcastChannel?: BroadcastChannelConstructor | null
  now?: () => number
  idFactory?: (kind: TabChannelIDKind) => string
}

/** Validated same-origin channel for presence, appearance, terminal, and events. */
export interface TabChannel {
  readonly channelName: string
  readonly origin: string
  readonly supported: boolean
  readonly tabId: string
  publish(payload: TabChannelPayload, ttlMS?: number): string | null
  publishPresence(state: PresenceState, page?: string): string | null
  publishAppearance(appearance: AppearancePatch, etag?: string): string | null
  publishTerminal(entry: TerminalEntry): string | null
  publishControllerEvent(event: SharedControllerEvent): string | null
  publishResourceVersion(hostVersion: string, buildTime: string): string | null
  subscribe(listener: TabChannelListener): () => void
  close(): void
}

interface RecordValue { [key: string]: unknown }

let localTabSequence = 0

const secretKeyPattern = /(?:authorization|cookie|password|passwd|secret|session(?:[_-]?id)?|(?:access|refresh)?[_-]?token|api[_-]?key)/i
const secretValuePatterns = [
  /\b(?:authorization|access[_-]?token|refresh[_-]?token|api[_-]?key|password|passwd|secret|session[_-]?id)\s*[:=]\s*\S+/i,
  /\b(?:bearer|basic)\s+[a-z0-9._~+/=-]{8,}/i,
  /\beyJ[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\b/i,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----/i,
]
const unsafeControlPattern = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f\u202a-\u202e\u2066-\u2069\ufeff]/g
const identifierPattern = /^[a-z0-9._:-]{1,180}$/i
const pagePattern = /^[a-z0-9._/-]{1,96}$/i

function defaultOrigin(): string {
  try {
    const value = globalThis.location?.origin
    if (value && value !== 'null') return value
  } catch {
    // Access to location can be denied in hardened or synthetic environments.
  }
  return localTabOrigin
}

function defaultIDFactory(): string {
  const cryptoAPI = globalThis.crypto
  if (cryptoAPI && typeof cryptoAPI.randomUUID === 'function') return cryptoAPI.randomUUID()
  const random = Math.random().toString(36).slice(2)
  return `${Date.now().toString(36)}-${random}-${(++localTabSequence).toString(36)}`
}

function normalizeOrigin(value: string): string {
  const candidate = value.trim()
  if (!candidate || candidate.length > 256 || unsafeControlPattern.test(candidate)) {
    unsafeControlPattern.lastIndex = 0
    throw new TypeError('tab channel origin is invalid')
  }
  unsafeControlPattern.lastIndex = 0
  if (candidate.toLowerCase() === localTabOrigin) return localTabOrigin
  try {
    const parsed = new URL(candidate)
    if (parsed.username || parsed.password || parsed.origin === 'null') {
      throw new TypeError('tab channel origin cannot contain credentials')
    }
    return parsed.origin.toLowerCase()
  } catch (cause) {
    if (cause instanceof TypeError && cause.message.includes('credentials')) throw cause
    throw new TypeError('tab channel origin is invalid')
  }
}

function originHash(value: string): string {
  let hash = 0x811c9dc5
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(36)
}

function safeID(value: string, fallback: string, maximumLength: number): string {
  const normalized = value.trim()
  return normalized.length <= maximumLength && identifierPattern.test(normalized) ? normalized : fallback
}

function isRecord(value: unknown): value is RecordValue {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasOnlyKeys(value: RecordValue, allowed: readonly string[]): boolean {
  const permitted = new Set(allowed)
  return Object.keys(value).every((key) => permitted.has(key))
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

function containsSecret(value: string): boolean {
  return secretValuePatterns.some((pattern) => pattern.test(value))
}

function safeText(value: unknown, maximumBytes: number, allowEmpty = false): string | null {
  if (typeof value !== 'string' || byteLength(value) > maximumBytes) return null
  const sanitized = value.replace(unsafeControlPattern, '')
  // Check after removing controls so an embedded bidi/control character cannot
  // disguise a credential marker from the same-origin transport guard.
  if (containsSecret(sanitized) || (!allowEmpty && sanitized.length === 0)) return null
  return sanitized
}

function safeOptionalText(value: unknown, maximumBytes: number): string | undefined | null {
  if (value === undefined) return undefined
  return safeText(value, maximumBytes, true)
}

function safeInteger(value: unknown, minimum: number, maximum: number): number | undefined | null {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < minimum || value > maximum) return null
  return value
}

function sanitizePresence(raw: RecordValue): PresencePayload | null {
  if (!hasOnlyKeys(raw, ['type', 'state', 'page'])) return null
  if (raw.state !== 'active' && raw.state !== 'hidden' && raw.state !== 'leaving') return null
  if (raw.page !== undefined && (typeof raw.page !== 'string' || !pagePattern.test(raw.page))) return null
  return {
    type: 'presence',
    state: raw.state,
    ...(raw.page === undefined ? {} : { page: raw.page }),
  }
}

function sanitizeAppearance(raw: RecordValue): AppearancePayload | null {
  if (!hasOnlyKeys(raw, ['type', 'appearance', 'etag']) || !isRecord(raw.appearance)) return null
  if (raw.etag !== undefined && (typeof raw.etag !== 'string' || !/^[a-f0-9]{64}$/.test(raw.etag))) return null
  const value = raw.appearance
  const keys = ['theme', 'locale', 'direction', 'reduceMotion', 'compactNumbers', 'audioMuted', 'audioVolume'] as const
  if (!hasOnlyKeys(value, keys) || Object.keys(value).length === 0) return null
  const appearance: AppearancePatch = {}
  if (value.theme !== undefined) {
    if (value.theme !== 'system' && value.theme !== 'light' && value.theme !== 'dark') return null
    appearance.theme = value.theme
  }
  if (value.locale !== undefined) {
    if (value.locale !== 'en' && value.locale !== 'fa') return null
    appearance.locale = value.locale
  }
  if (value.direction !== undefined) {
    if (value.direction !== 'auto' && value.direction !== 'ltr' && value.direction !== 'rtl') return null
    appearance.direction = value.direction
  }
  for (const key of ['reduceMotion', 'compactNumbers', 'audioMuted'] as const) {
    if (value[key] !== undefined) {
      if (typeof value[key] !== 'boolean') return null
      appearance[key] = value[key]
    }
  }
  if (value.audioVolume !== undefined) {
    if (typeof value.audioVolume !== 'number' || !Number.isFinite(value.audioVolume) || value.audioVolume < 0 || value.audioVolume > 1) return null
    appearance.audioVolume = value.audioVolume
  }
  return { type: 'appearance', appearance, ...(raw.etag === undefined ? {} : { etag: raw.etag }) }
}

function sanitizeTerminal(raw: RecordValue): TerminalPayload | null {
  if (!hasOnlyKeys(raw, ['type', 'entry']) || !isRecord(raw.entry)) return null
  const entry = raw.entry
  if (!hasOnlyKeys(entry, ['kind', 'text', 'at'])) return null
  if (entry.kind !== 'command' && entry.kind !== 'output' && entry.kind !== 'error' && entry.kind !== 'system') return null
  const text = safeText(entry.text, maximumTerminalBytes)
  const at = safeInteger(entry.at, 0, Number.MAX_SAFE_INTEGER)
  if (text === null || at === null) return null
  return { type: 'terminal', entry: { kind: entry.kind, text, ...(at === undefined ? {} : { at }) } }
}

function sanitizeMetadata(value: unknown): Record<string, string> | undefined | null {
  if (value === undefined) return undefined
  if (!isRecord(value) || Object.keys(value).length > maximumMetadataEntries) return null
  const metadata: Record<string, string> = {}
  for (const [key, rawValue] of Object.entries(value)) {
    if (!key || key.length > 64 || secretKeyPattern.test(key)) return null
    const safeValue = safeText(rawValue, 1024, true)
    if (safeValue === null) return null
    metadata[key] = safeValue
  }
  return metadata
}

function sanitizeControllerEvent(raw: RecordValue): ControllerEventPayload | null {
  if (!hasOnlyKeys(raw, ['type', 'event']) || !isRecord(raw.event)) return null
  const event = raw.event
  const allowed = [
    'id', 'time', 'kind', 'text', 'state', 'lifecycle', 'reason', 'source', 'target',
    'message_type', 'action', 'gesture', 'key', 'rf_id', 'rf_code', 'rf_bits',
    'rf_protocol', 'metadata',
  ] as const
  if (!hasOnlyKeys(event, allowed)) return null
  const id = safeInteger(event.id, 0, Number.MAX_SAFE_INTEGER)
  const time = safeText(event.time, 80)
  const kind = safeText(event.kind, 128)
  const text = safeText(event.text, maximumEventTextBytes, true)
  if (id === null || id === undefined || time === null || kind === null || text === null) return null

  const optionalTextKeys = ['state', 'lifecycle', 'reason', 'source', 'target', 'message_type', 'action', 'gesture'] as const
  const optionalTexts: Partial<Record<(typeof optionalTextKeys)[number], string>> = {}
  for (const key of optionalTextKeys) {
    const value = safeOptionalText(event[key], key === 'reason' ? 1024 : 256)
    if (value === null) return null
    if (value !== undefined) optionalTexts[key] = value
  }

  const optionalIntegerKeys = ['key', 'rf_id', 'rf_code', 'rf_bits', 'rf_protocol'] as const
  const optionalIntegers: Partial<Record<(typeof optionalIntegerKeys)[number], number>> = {}
  for (const key of optionalIntegerKeys) {
    const maximum = key === 'rf_code' ? 0xffffffff : Number.MAX_SAFE_INTEGER
    const value = safeInteger(event[key], 0, maximum)
    if (value === null) return null
    if (value !== undefined) optionalIntegers[key] = value
  }

  const metadata = sanitizeMetadata(event.metadata)
  if (metadata === null) return null
  return {
    type: 'controller-event',
    event: {
      id,
      time,
      kind,
      text,
      ...optionalTexts,
      ...optionalIntegers,
      ...(metadata === undefined ? {} : { metadata }),
    },
  }
}

function sanitizeResourceVersion(raw: RecordValue): ResourceVersionPayload | null {
  if (!hasOnlyKeys(raw, ['type', 'hostVersion', 'buildTime'])) return null
  const hostVersion = safeText(raw.hostVersion, maximumResourceIdentityBytes)
  const buildTime = safeText(raw.buildTime, maximumResourceIdentityBytes)
  if (hostVersion === null || buildTime === null) return null
  if (!/^[a-z0-9][a-z0-9._:+-]*$/i.test(hostVersion) || !/^[a-z0-9][a-z0-9._:+-]*$/i.test(buildTime)) return null
  return { type: 'resource-version', hostVersion, buildTime }
}

function sanitizePayload(value: unknown): TabChannelPayload | null {
  if (!isRecord(value) || typeof value.type !== 'string') return null
  switch (value.type) {
    case 'presence': return sanitizePresence(value)
    case 'appearance': return sanitizeAppearance(value)
    case 'terminal': return sanitizeTerminal(value)
    case 'controller-event': return sanitizeControllerEvent(value)
    case 'resource-version': return sanitizeResourceVersion(value)
    default: return null
  }
}

function envelopeSizeIsSafe(value: unknown): boolean {
  try {
    return byteLength(JSON.stringify(value)) <= maximumEnvelopeBytes
  } catch {
    return false
  }
}

/** Creates an isolated, bounded, credential-filtering tab synchronization channel. */
export function createTabChannel(options: TabChannelOptions = {}): TabChannel {
  const origin = normalizeOrigin(options.origin ?? defaultOrigin())
  const now = options.now ?? Date.now
  const idFactory = options.idFactory ?? (() => defaultIDFactory())
  const ttlMS = options.ttlMS ?? defaultTTLMS
  if (!Number.isSafeInteger(ttlMS) || ttlMS < 1 || ttlMS > maximumTTLMS) {
    throw new RangeError(`tab channel ttlMS must be 1..${maximumTTLMS}`)
  }

  const nativeConstructor = typeof globalThis.BroadcastChannel === 'function'
    ? globalThis.BroadcastChannel as unknown as BroadcastChannelConstructor
    : null
  const Constructor = options.BroadcastChannel === undefined ? nativeConstructor : options.BroadcastChannel
  const instanceSequence = ++localTabSequence
  const tabSeed = safeID(idFactory('tab'), `fallback-${instanceSequence.toString(36)}`, 64)
  const tabId = `tab:${tabSeed}:${instanceSequence.toString(36)}`
  const channelName = `${TAB_CHANNEL_PROTOCOL}:v${TAB_CHANNEL_VERSION}:${originHash(origin)}`
  const listeners = new Set<TabChannelListener>()
  const seen = new Map<string, number>()
  let messageSequence = 0
  let closed = false
  let port: BroadcastChannelPort | null = null

  const pruneSeen = (timestamp: number) => {
    for (const [messageId, expiresAt] of seen) {
      if (expiresAt <= timestamp) seen.delete(messageId)
    }
    while (seen.size > maximumSeenMessages) {
      const oldest = seen.keys().next().value as string | undefined
      if (oldest === undefined) break
      seen.delete(oldest)
    }
  }

  const decodeEnvelope = (value: unknown): TabChannelEnvelope | null => {
    if (!isRecord(value) || !envelopeSizeIsSafe(value)) return null
    if (!hasOnlyKeys(value, ['protocol', 'version', 'origin', 'messageId', 'tabId', 'sentAt', 'expiresAt', 'payload'])) return null
    if (value.protocol !== TAB_CHANNEL_PROTOCOL || value.version !== TAB_CHANNEL_VERSION || value.origin !== origin) return null
    if (typeof value.messageId !== 'string' || !identifierPattern.test(value.messageId)) return null
    if (typeof value.tabId !== 'string' || !identifierPattern.test(value.tabId) || value.tabId === tabId) return null
    if (typeof value.sentAt !== 'number' || !Number.isSafeInteger(value.sentAt) || value.sentAt < 0) return null
    if (typeof value.expiresAt !== 'number' || !Number.isSafeInteger(value.expiresAt) || value.expiresAt <= value.sentAt || value.expiresAt - value.sentAt > maximumTTLMS) return null
    const timestamp = now()
    if (value.expiresAt <= timestamp || value.sentAt > timestamp + maximumTTLMS) return null
    pruneSeen(timestamp)
    if (seen.has(value.messageId)) return null
    const payload = sanitizePayload(value.payload)
    if (payload === null) return null
    seen.set(value.messageId, value.expiresAt)
    return {
      protocol: TAB_CHANNEL_PROTOCOL,
      version: TAB_CHANNEL_VERSION,
      origin,
      messageId: value.messageId,
      tabId: value.tabId,
      sentAt: value.sentAt,
      expiresAt: value.expiresAt,
      payload,
    }
  }

  const onMessage: EventListener = (event) => {
    if (closed) return
    const envelope = decodeEnvelope((event as MessageEvent<unknown>).data)
    if (envelope === null) return
    for (const listener of [...listeners]) {
      try {
        listener(envelope)
      } catch {
        // A consumer cannot break delivery to the remaining subscribers.
      }
    }
  }

  if (Constructor !== null) {
    try {
      port = new Constructor(channelName)
      port.addEventListener('message', onMessage)
    } catch {
      port = null
    }
  }

  const publish = (value: TabChannelPayload, requestedTTL = ttlMS): string | null => {
    if (closed || port === null) return null
    if (!Number.isSafeInteger(requestedTTL) || requestedTTL < 1 || requestedTTL > maximumTTLMS) return null
    const payload = sanitizePayload(value)
    if (payload === null) return null
    const sentAt = now()
    if (!Number.isSafeInteger(sentAt) || sentAt < 0 || sentAt > Number.MAX_SAFE_INTEGER - requestedTTL) return null
    messageSequence += 1
    const seed = safeID(idFactory('message'), `message-${messageSequence.toString(36)}`, 64)
    const messageId = `${tabId}:${messageSequence.toString(36)}:${seed}`
    const envelope: TabChannelEnvelope = {
      protocol: TAB_CHANNEL_PROTOCOL,
      version: TAB_CHANNEL_VERSION,
      origin,
      messageId,
      tabId,
      sentAt,
      expiresAt: sentAt + requestedTTL,
      payload,
    }
    if (!envelopeSizeIsSafe(envelope)) return null
    try {
      port.postMessage(envelope)
      seen.set(messageId, envelope.expiresAt)
      pruneSeen(sentAt)
      return messageId
    } catch {
      return null
    }
  }

  return {
    channelName,
    origin,
    supported: port !== null,
    tabId,
    publish,
    publishPresence: (state, page) => publish({ type: 'presence', state, ...(page === undefined ? {} : { page }) }),
    publishAppearance: (appearance, etag) => publish({ type: 'appearance', appearance, ...(etag === undefined ? {} : { etag }) }),
    publishTerminal: (entry) => publish({ type: 'terminal', entry }),
    publishControllerEvent: (event) => publish({ type: 'controller-event', event }),
    publishResourceVersion: (hostVersion, buildTime) => publish({ type: 'resource-version', hostVersion, buildTime }),
    subscribe(listener) {
      if (closed) return () => undefined
      listeners.add(listener)
      let subscribed = true
      return () => {
        if (!subscribed) return
        subscribed = false
        listeners.delete(listener)
      }
    },
    close() {
      if (closed) return
      closed = true
      listeners.clear()
      seen.clear()
      if (port !== null) {
        port.removeEventListener('message', onMessage)
        port.close()
        port = null
      }
    },
  }
}

import type {
  CommandResult,
  ControllerEvent,
  RPCResponse,
  Snapshot,
  StatusUpdate,
  UIConfig,
} from './types'
import { controllerHTTPURL, controllerWebSocketURL } from './transport-config'

const tokenKey = 'pccontroller.session-token'
const browserTicketPrefix = 'pccontroller.ticket.'
let nextID = 1
let streamSocket: WebSocket | null = null

interface PendingSocketRPC {
  socket: WebSocket
  resolve: (value: unknown) => void
  reject: (cause: Error) => void
  cleanup: () => void
}

const pendingSocketRPC = new Map<number, PendingSocketRPC>()

/** Returns the session-scoped bearer token used by authenticated transports. */
export function getToken(): string {
  return sessionStorage.getItem(tokenKey) ?? ''
}

/** Stores or clears the session-scoped bearer token. */
export function setToken(value: string): void {
  const token = value.trim()
  if (token) sessionStorage.setItem(tokenKey, token)
  else sessionStorage.removeItem(tokenKey)
}

function headers(json = false): HeadersInit {
  const result: Record<string, string> = {}
  if (json) result['Content-Type'] = 'application/json'
  const token = getToken()
  if (token) result.Authorization = `Bearer ${token}`
  return result
}

/** Extracts a human-readable error from an HTTP response body. */
export function responseErrorDetail(value: unknown, statusText: string, status: number): string {
  if (typeof value === 'object' && value && 'error' in value) {
    const error = (value as { error: unknown }).error
    if (typeof error === 'object' && error && 'message' in error) {
      return String((error as { message: unknown }).message)
    }
    return String(error)
  }
  if (typeof value === 'string') return value
  return statusText || `HTTP ${status}`
}

async function decode<T>(response: Response): Promise<T> {
  const raw = await response.text()
  let value: unknown = null
  if (raw) {
    try {
      value = JSON.parse(raw)
    } catch {
      value = raw
    }
  }
  if (!response.ok) {
    const detail = responseErrorDetail(value, response.statusText, response.status)
    throw new Error(detail || `HTTP ${response.status}`)
  }
  return value as T
}

/** Fetches the bootstrap UI configuration required before opening live transports. */
export async function getUIConfig(signal?: AbortSignal): Promise<UIConfig> {
  const response = await fetch(controllerHTTPURL('/api/ui-config'), { signal, cache: 'no-store' })
  const config = await decode<UIConfig>(response)
  if (typeof config?.setup_complete !== 'boolean') {
    throw new Error('UI configuration is missing required setup_complete state')
  }
  if (!config.appearance || typeof config.appearance !== 'object' ||
      typeof config.appearance_etag !== 'string' || !/^[a-f0-9]{64}$/.test(config.appearance_etag)) {
    throw new Error('UI configuration is missing host-authoritative appearance state')
  }
  if (typeof config.session_ticket_path !== 'string' ||
      config.session_ticket_path.startsWith('//') ||
      !/^\/[A-Za-z0-9._~!$&'()*+,;=:@%/-]+$/.test(config.session_ticket_path)) {
    throw new Error('UI configuration is missing a safe session-ticket path')
  }
  return config
}

/** Fetches the authenticated current controller snapshot. */
export async function getSnapshot(signal?: AbortSignal): Promise<Snapshot> {
  const response = await fetch(controllerHTTPURL('/api/snapshot'), {
    headers: headers(), signal, cache: 'no-store',
  })
  return decode<Snapshot>(response)
}

async function restRPC<T>(request: { jsonrpc: '2.0'; id: number; method: string; params: unknown }, signal?: AbortSignal): Promise<T> {
  const response = await fetch(controllerHTTPURL('/api/rpc'), {
    method: 'POST',
    headers: headers(true),
    body: JSON.stringify(request),
    signal,
  })
  const envelope = await decode<RPCResponse<T>>(response)
  if (envelope.error) throw new Error(envelope.error.message)
  return envelope.result as T
}

function rejectSocketRequests(socket: WebSocket, detail: string): void {
  for (const [id, pending] of pendingSocketRPC) {
    if (pending.socket !== socket) continue
    pendingSocketRPC.delete(id)
    pending.cleanup()
    pending.reject(new Error(detail))
  }
}

function socketRPC<T>(
  socket: WebSocket,
  request: { jsonrpc: '2.0'; id: number; method: string; params: unknown },
  signal?: AbortSignal,
): Promise<T> {
  if (signal?.aborted) return Promise.reject(signal.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError'))
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => {
      pendingSocketRPC.delete(request.id)
      reject(signal?.reason instanceof Error ? signal.reason : new DOMException('Aborted', 'AbortError'))
    }
    const cleanup = () => signal?.removeEventListener('abort', onAbort)
    pendingSocketRPC.set(request.id, {
      socket,
      resolve: (value) => resolve(value as T),
      reject,
      cleanup,
    })
    signal?.addEventListener('abort', onAbort, { once: true })
    try {
      socket.send(JSON.stringify(request))
    } catch (cause) {
      pendingSocketRPC.delete(request.id)
      cleanup()
      reject(cause instanceof Error ? cause : new Error(String(cause)))
    }
  })
}

/** Invokes JSON-RPC over the live WebSocket, falling back to authenticated HTTP. */
export function rpc<T>(method: string, params: unknown = {}, signal?: AbortSignal): Promise<T> {
  const request = { jsonrpc: '2.0' as const, id: nextID++, method, params }
  const socket = streamSocket
  if (socket && socket.readyState === WebSocket.OPEN) return socketRPC<T>(socket, request, signal)
  return restRPC<T>(request, signal)
}

/** Executes one canonical controller command through JSON-RPC. */
export function execute(command: string, signal?: AbortSignal): Promise<CommandResult> {
	return rpc<CommandResult>('controller.command.execute', { command }, signal)
}

/** Receives live status, event, and transport-lifecycle notifications. */
export interface StreamHandlers {
  status: (value: StatusUpdate) => void
  event: (value: ControllerEvent) => void
  state: (state: 'connecting' | 'open' | 'waiting' | 'closed', detail?: string) => void
}

interface SessionTicket {
  ticket: string
  protocol: string
  expires_at: string
  expires_in_ms: number
  principal: string
}

async function websocketProtocols(config: UIConfig, signal?: AbortSignal): Promise<string[]> {
  const token = getToken()
  if (!config.auth_required) return []
  const response = await fetch(controllerHTTPURL(config.session_ticket_path), {
    method: 'POST',
    headers: headers(true),
    body: JSON.stringify({ transport: 'websocket' }),
    signal,
    cache: 'no-store',
  })
  const ticket = await decode<SessionTicket>(response)
  if (!/^[a-f0-9]{64}$/.test(ticket.ticket) || ticket.protocol !== 'pccontroller' ||
      !Number.isFinite(ticket.expires_in_ms) || ticket.expires_in_ms <= 0 || ticket.expires_in_ms > 15_000 ||
      Number.isNaN(Date.parse(ticket.expires_at)) || typeof ticket.principal !== 'string' || !ticket.principal.trim()) {
    throw new Error('Controller returned an invalid WebSocket session ticket')
  }
  return [ticket.protocol, `${browserTicketPrefix}${ticket.ticket}`]
}

/** Calculates Socket.IO-style exponential reconnect delay with a hard cap. */
export function streamRetryDelay(retry: number, random = Math.random): number {
  const exponential = 500 * 2 ** Math.min(Math.max(0, retry), 5)
  return Math.min(12_000, exponential + Math.floor(Math.max(0, Math.min(0.999, random())) * 250))
}

/** Opens the reconnecting event stream and returns a function that closes it. */
export function connectStream(config: UIConfig, handlers: StreamHandlers): () => void {
  let socket: WebSocket | null = null
  let stopped = false
  let retry = 0
  let timer = 0
  let attempt = 0
  let ticketAbort: AbortController | null = null

  const scheduleRetry = (detail: string) => {
    if (stopped) return
    retry += 1
    const delay = streamRetryDelay(retry)
    handlers.state('waiting', detail || `retrying in ${Math.ceil(delay / 1000)}s`)
    window.clearTimeout(timer)
    timer = window.setTimeout(() => { void open() }, delay)
  }

  const open = async () => {
    if (stopped) return
    const currentAttempt = ++attempt
    handlers.state('connecting')
    ticketAbort?.abort()
    const authorizationAbort = new AbortController()
    ticketAbort = authorizationAbort
    let protocols: string[]
    try {
      protocols = await websocketProtocols(config, authorizationAbort.signal)
    } catch (cause) {
      if (stopped || currentAttempt !== attempt || authorizationAbort.signal.aborted) return
      scheduleRetry(cause instanceof Error ? cause.message : String(cause))
      return
    } finally {
      if (ticketAbort === authorizationAbort) ticketAbort = null
    }
    if (stopped || currentAttempt !== attempt) return

    let activeSocket: WebSocket
    try {
      const url = controllerWebSocketURL(config.websocket_path)
      activeSocket = protocols.length > 0 ? new WebSocket(url, protocols) : new WebSocket(url)
    } catch (cause) {
      scheduleRetry(cause instanceof Error ? cause.message : String(cause))
      return
    }
    socket = activeSocket
    activeSocket.addEventListener('open', () => {
      retry = 0
      streamSocket = activeSocket
      handlers.state('open')
      activeSocket.send(JSON.stringify({
        jsonrpc: '2.0',
        id: nextID++,
        method: 'controller.subscribe',
		params: { topics: ['events', 'state', 'status'], interval_ms: 500, after_id: 0 },
      }))
    })
    activeSocket.addEventListener('message', (message) => {
      try {
        const value = JSON.parse(String(message.data)) as {
          id?: number
          method?: string
          params?: unknown
          result?: unknown
          error?: { message?: string }
        }
        if (typeof value.id === 'number') {
          const pending = pendingSocketRPC.get(value.id)
          if (pending && pending.socket === activeSocket) {
            pendingSocketRPC.delete(value.id)
            pending.cleanup()
            if (value.error) pending.reject(new Error(value.error.message || 'WebSocket RPC failed'))
            else pending.resolve(value.result)
          }
        }
        if (value.method === 'controller.status') handlers.status(value.params as StatusUpdate)
		if (value.method === 'controller.event' || value.method === 'controller.state') {
			handlers.event(value.params as ControllerEvent)
		}
        if (value.method === 'controller.error') {
          const detail = (value.params as { error?: string } | undefined)?.error
          handlers.state('open', detail)
        }
      } catch {
        // The controller protocol is JSON-only; malformed frames are ignored and
        // surfaced by the next valid service notification instead of breaking UI state.
      }
    })
    activeSocket.addEventListener('close', (event) => {
      if (socket === activeSocket) socket = null
      if (streamSocket === activeSocket) streamSocket = null
      rejectSocketRequests(activeSocket, event.reason || 'WebSocket RPC connection closed')
      if (stopped) {
        handlers.state('closed')
        return
      }
      scheduleRetry(event.reason || 'WebSocket connection closed')
    })
    activeSocket.addEventListener('error', () => activeSocket.close())
  }

  void open()
  return () => {
    stopped = true
    attempt += 1
    ticketAbort?.abort()
    window.clearTimeout(timer)
    const closed = socket
    socket?.close(1000, 'view closed')
    if (closed) {
      if (streamSocket === closed) streamSocket = null
      rejectSocketRequests(closed, 'WebSocket RPC view closed')
    }
    socket = null
  }
}

/** Calls an authenticated HTTP endpoint owned by a configured integration. */
export async function integrationFetch<T>(
  integration: 'datahub',
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const response = await fetch(controllerHTTPURL(`/api/integrations/${integration}/${safePath}`), {
    ...init,
    headers: { ...headers(Boolean(init.body)), ...(init.headers ?? {}) },
  })
  return decode<T>(response)
}

function safeDownloadName(response: Response, path: string): string {
  const disposition = response.headers.get('Content-Disposition') ?? ''
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  const quoted = disposition.match(/filename="([^"]+)"/i)?.[1]
  let decoded = ''
  if (encoded) {
    try { decoded = decodeURIComponent(encoded) } catch { decoded = '' }
  }
  let candidate = decoded || quoted || path.split('/').filter(Boolean).at(-1) || 'controller-data.bin'
  candidate = candidate.replace(/[\\/:*?"<>|\u0000-\u001f\u007f]/g, '_').trim().slice(0, 180)
  return candidate || 'controller-data.bin'
}

/** Downloads a file from an authenticated integration endpoint. */
export async function downloadIntegration(
  integration: 'datahub',
  path: string,
  signal?: AbortSignal,
): Promise<void> {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const target = controllerHTTPURL(`/api/integrations/${integration}/${safePath}`)
  const response = await fetch(target, { headers: headers(), signal })
  if (!response.ok) await decode(response)
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = safeDownloadName(response, safePath)
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(objectURL)
}

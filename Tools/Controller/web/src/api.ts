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
let nextID = 1
let streamSocket: WebSocket | null = null

interface PendingSocketRPC {
  socket: WebSocket
  resolve: (value: unknown) => void
  reject: (cause: Error) => void
  cleanup: () => void
}

const pendingSocketRPC = new Map<number, PendingSocketRPC>()

export function getToken(): string {
  return sessionStorage.getItem(tokenKey) ?? ''
}

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

export async function getUIConfig(signal?: AbortSignal): Promise<UIConfig> {
  const response = await fetch(controllerHTTPURL('/api/v1/ui-config'), { signal, cache: 'no-store' })
  const config = await decode<UIConfig>(response)
  if (typeof config?.setup_complete !== 'boolean') {
    throw new Error('UI configuration is missing required setup_complete state')
  }
  return config
}

export async function getSnapshot(signal?: AbortSignal): Promise<Snapshot> {
  const response = await fetch(controllerHTTPURL('/api/v1/snapshot'), {
    headers: headers(), signal, cache: 'no-store',
  })
  return decode<Snapshot>(response)
}

async function restRPC<T>(request: { jsonrpc: '2.0'; id: number; method: string; params: unknown }, signal?: AbortSignal): Promise<T> {
  const response = await fetch(controllerHTTPURL('/api/v1/rpc'), {
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

export function rpc<T>(method: string, params: unknown = {}, signal?: AbortSignal): Promise<T> {
  const request = { jsonrpc: '2.0' as const, id: nextID++, method, params }
  const socket = streamSocket
  if (socket && socket.readyState === WebSocket.OPEN) return socketRPC<T>(socket, request, signal)
  return restRPC<T>(request, signal)
}

export function execute(command: string, signal?: AbortSignal): Promise<CommandResult> {
	return rpc<CommandResult>('controller.command.execute', { command }, signal)
}

export interface StreamHandlers {
  status: (value: StatusUpdate) => void
  event: (value: ControllerEvent) => void
  state: (state: 'connecting' | 'open' | 'waiting' | 'closed', detail?: string) => void
}

export function connectStream(config: UIConfig, handlers: StreamHandlers): () => void {
  let socket: WebSocket | null = null
  let stopped = false
  let retry = 0
  let timer = 0

  const open = () => {
    if (stopped) return
    handlers.state('connecting')
    const url = new URL(controllerWebSocketURL(config.websocket_path || '/ipc'))
    const token = getToken()
    if (token) url.searchParams.set('access_token', token)
    const activeSocket = new WebSocket(url)
    socket = activeSocket
    activeSocket.addEventListener('open', () => {
      retry = 0
      streamSocket = activeSocket
      handlers.state('open')
      activeSocket.send(JSON.stringify({
        jsonrpc: '2.0',
        id: nextID++,
        method: 'controller.subscribe',
        params: { topics: ['events', 'status'], interval_ms: 500, after_id: 0 },
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
        if (value.method === 'controller.event') handlers.event(value.params as ControllerEvent)
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
      retry += 1
      const delay = Math.min(12_000, 500 * 2 ** Math.min(retry, 5)) + Math.floor(Math.random() * 250)
      handlers.state('waiting', event.reason || `retrying in ${Math.ceil(delay / 1000)}s`)
      timer = window.setTimeout(open, delay)
    })
    activeSocket.addEventListener('error', () => activeSocket.close())
  }

  open()
  return () => {
    stopped = true
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

export async function integrationFetch<T>(
  integration: 'datahub',
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const response = await fetch(controllerHTTPURL(`/api/v1/integrations/${integration}/${safePath}`), {
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

export async function downloadIntegration(
  integration: 'datahub',
  path: string,
  signal?: AbortSignal,
): Promise<void> {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const target = controllerHTTPURL(`/api/v1/integrations/${integration}/${safePath}`)
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

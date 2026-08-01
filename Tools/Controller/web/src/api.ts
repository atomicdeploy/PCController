import type {
  CommandResult,
  ControllerEvent,
  RPCResponse,
  Snapshot,
  StatusUpdate,
  UIConfig,
} from './types'

const tokenKey = 'pccontroller.session-token'
let nextID = 1

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
  const response = await fetch('/api/v1/ui-config', { signal, cache: 'no-store' })
  return decode<UIConfig>(response)
}

export async function getSnapshot(signal?: AbortSignal): Promise<Snapshot> {
  const response = await fetch('/api/v1/snapshot', {
    headers: headers(), signal, cache: 'no-store',
  })
  return decode<Snapshot>(response)
}

export async function rpc<T>(method: string, params: unknown = {}, signal?: AbortSignal): Promise<T> {
  const request = { jsonrpc: '2.0', id: nextID++, method, params }
  const response = await fetch('/api/v1/rpc', {
    method: 'POST',
    headers: headers(true),
    body: JSON.stringify(request),
    signal,
  })
  const envelope = await decode<RPCResponse<T>>(response)
  if (envelope.error) throw new Error(envelope.error.message)
  return envelope.result as T
}

export function execute(command: string, signal?: AbortSignal): Promise<CommandResult> {
  return rpc<CommandResult>('controller.execute', { command }, signal)
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
    const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = new URL(config.websocket_path || '/ipc', `${scheme}//${location.host}`)
    const token = getToken()
    if (token) url.searchParams.set('access_token', token)
    socket = new WebSocket(url)
    socket.addEventListener('open', () => {
      retry = 0
      handlers.state('open')
      socket?.send(JSON.stringify({
        jsonrpc: '2.0',
        id: nextID++,
        method: 'controller.subscribe',
        params: { topics: ['events', 'status'], interval_ms: 500, after_id: 0 },
      }))
    })
    socket.addEventListener('message', (message) => {
      try {
        const value = JSON.parse(String(message.data)) as {
          method?: string
          params?: unknown
          error?: { message?: string }
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
    socket.addEventListener('close', (event) => {
      socket = null
      if (stopped) {
        handlers.state('closed')
        return
      }
      retry += 1
      const delay = Math.min(12_000, 500 * 2 ** Math.min(retry, 5)) + Math.floor(Math.random() * 250)
      handlers.state('waiting', event.reason || `retrying in ${Math.ceil(delay / 1000)}s`)
      timer = window.setTimeout(open, delay)
    })
    socket.addEventListener('error', () => socket?.close())
  }

  open()
  return () => {
    stopped = true
    window.clearTimeout(timer)
    socket?.close(1000, 'view closed')
    socket = null
  }
}

export async function integrationFetch<T>(
  integration: 'datahub',
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const response = await fetch(`/api/v1/integrations/${integration}/${safePath}`, {
    ...init,
    headers: { ...headers(Boolean(init.body)), ...(init.headers ?? {}) },
  })
  return decode<T>(response)
}

export function downloadURL(integration: 'datahub', path: string): string {
  const safePath = path.startsWith('/') ? path.slice(1) : path
  const token = getToken()
  const url = new URL(`/api/v1/integrations/${integration}/${safePath}`, location.href)
  if (token) url.searchParams.set('access_token', token)
  return `${url.pathname}${url.search}`
}

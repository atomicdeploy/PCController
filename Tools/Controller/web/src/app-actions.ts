import { pageFromAppAction, type PageID } from './hotkeys'
import { matchesAppTarget } from './instance-routing'
import type { ControllerEvent } from './types'

export type AppActionOutcomeState = 'applied' | 'rejected'
export type WebProgressState = 'normal' | 'error' | 'indeterminate' | 'warning'

export interface PushedAppAction {
  operationID: string
  deliveryID: string
  receiptKey: string
  kind: 'app.page' | 'app.title' | 'app.progress'
  value: string
}

export interface AppActionAck {
  operation_id: string
  delivery_id: string
  instance_id: string
  state: AppActionOutcomeState
  reason?: string
}

export type AppActionAckSender = (acknowledgement: AppActionAck) => Promise<unknown>
export type AppActionAckWait = (milliseconds: number) => Promise<void>

export interface WebActionProgress {
  state: WebProgressState
  percent?: number
}

export type WebActionEffect =
  | { outcome: 'applied'; page: PageID }
  | { outcome: 'applied'; title: string | null }
  | { outcome: 'applied'; progress: WebActionProgress | null }
  | { outcome: 'rejected'; reason: string }

export type ProcessedWebAppAction =
  | { action: PushedAppAction; effect: WebActionEffect; acknowledgement: AppActionAck; duplicate: false }
  | { action: PushedAppAction; acknowledgement: AppActionAck; duplicate: true }

const supportedKinds = new Set<PushedAppAction['kind']>([
  'app.page', 'app.title', 'app.progress',
])

export function pushedAppAction(
  event: ControllerEvent,
  instanceID: string,
  now = Date.now(),
): PushedAppAction | null {
  const operationID = event.metadata?.operation_id?.trim() ?? ''
  const deliveryID = event.metadata?.operation_delivery_id?.trim() ?? ''
  const expiresAt = event.metadata?.operation_expires_at?.trim() ?? ''
  const kind = event.kind.trim().toLowerCase() as PushedAppAction['kind']
  if (!operationID || !deliveryID || !expiresAt || !supportedKinds.has(kind) ||
      !matchesAppTarget(event.metadata?.target_instance, instanceID, 'webui')) return null
  const deadline = Date.parse(expiresAt)
  if (!Number.isFinite(deadline) || deadline <= now) return null
  return {
    operationID,
    deliveryID,
    receiptKey: `${operationID}\u0000${deliveryID}`,
    kind,
    value: (kind === 'app.page' ? event.metadata?.page : event.metadata?.value)?.trim() ?? '',
  }
}

export function applyWebAppAction(action: PushedAppAction): WebActionEffect {
  switch (action.kind) {
    case 'app.page': {
      const page = pageFromAppAction(action.value)
      return page ? { outcome: 'applied', page } : { outcome: 'rejected', reason: 'unknown_page' }
    }
    case 'app.title': {
      if (action.value.toLowerCase() === 'auto') return { outcome: 'applied', title: null }
      if (!action.value || [...action.value].length > 120 || /[\u0000-\u001f\u007f]/u.test(action.value)) {
        return { outcome: 'rejected', reason: 'invalid_title' }
      }
      return { outcome: 'applied', title: action.value }
    }
    case 'app.progress': {
      const progress = parseWebProgress(action.value)
      return progress === undefined
        ? { outcome: 'rejected', reason: 'invalid_progress' }
        : { outcome: 'applied', progress }
    }
  }
}

export function parseWebProgress(value: string): WebActionProgress | null | undefined {
  const words = value.trim().toLowerCase().split(/\s+/u).filter(Boolean)
  if (words.length === 0 || words.length > 2) return undefined
  const aliases: Record<string, WebProgressState | 'clear'> = {
    '0': 'clear', clear: 'clear',
    '1': 'normal', normal: 'normal',
    '2': 'error', error: 'error',
    '3': 'indeterminate', indeterminate: 'indeterminate',
    '4': 'warning', warning: 'warning',
  }
  const state = aliases[words[0]]
  if (!state) return undefined
  if (state === 'clear') return words.length === 1 ? null : undefined
  if (state === 'indeterminate') {
    return words.length === 1 ? { state } : undefined
  }
  if (words.length !== 2 || !/^\d{1,3}$/u.test(words[1])) return undefined
  const percent = Number(words[1])
  if (!Number.isInteger(percent) || percent < 0 || percent > 100) return undefined
  return { state, percent }
}

export class AppActionReceiptCache {
  private readonly values = new Map<string, AppActionAck>()

  constructor(private readonly maximum = 256) {}

  get(receiptKey: string): AppActionAck | undefined {
    return this.values.get(receiptKey)
  }

  remember(receiptKey: string, value: AppActionAck): void {
    if (!this.values.has(receiptKey) && this.values.size >= this.maximum) {
      const oldest = this.values.keys().next().value as string | undefined
      if (oldest) this.values.delete(oldest)
    }
    this.values.set(receiptKey, value)
  }
}

// Process one pushed Web action from event selection through factual effect,
// deduplication, and acknowledgement construction. The app owns only the
// actual DOM/router side effects and RPC transport around this pure contract.
export function processWebAppAction(
  event: ControllerEvent,
  instanceID: string,
  receipts: AppActionReceiptCache,
  now = Date.now(),
): ProcessedWebAppAction | null {
  const action = pushedAppAction(event, instanceID, now)
  if (!action) return null
  const existing = receipts.get(action.receiptKey)
  if (existing) {
    return { action, acknowledgement: existing, duplicate: true }
  }
  const effect = applyWebAppAction(action)
  const acknowledgement: AppActionAck = {
    operation_id: action.operationID,
    delivery_id: action.deliveryID,
    instance_id: instanceID,
    state: effect.outcome,
    ...(effect.outcome === 'rejected' ? { reason: effect.reason } : {}),
  }
  receipts.remember(action.receiptKey, acknowledgement)
  return { action, effect, acknowledgement, duplicate: false }
}

const maximumAppActionAckAttempts = 3
const appActionAckRetryDelayMilliseconds = 100

function waitForAppActionAckRetry(milliseconds: number): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, milliseconds))
}

// A pushed action's effect is applied once and cached before this transport
// helper runs. Retry only the idempotent acknowledgement, and surface nothing
// unless all bounded attempts fail.
export async function acknowledgeWebAppAction(
  acknowledgement: AppActionAck,
  send: AppActionAckSender,
  wait: AppActionAckWait = waitForAppActionAckRetry,
): Promise<void> {
  let failure: unknown
  for (let attempt = 1; attempt <= maximumAppActionAckAttempts; attempt += 1) {
    try {
      await send(acknowledgement)
      return
    } catch (cause) {
      failure = cause
      if (attempt < maximumAppActionAckAttempts) {
        await wait(attempt * appActionAckRetryDelayMilliseconds)
      }
    }
  }
  throw failure
}

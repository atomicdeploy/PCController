import { pageFromAppAction, type PageID } from './hotkeys'
import { matchesAppTarget } from './instance-routing'
import type { ControllerEvent } from './types'

export type AppActionOutcomeState = 'applied' | 'rejected'
export type WebProgressState = 'normal' | 'error' | 'indeterminate' | 'warning'

export interface PushedAppAction {
  operationID: string
  kind: 'app.page' | 'app.title' | 'app.progress'
  value: string
}

export interface AppActionAck {
  operation_id: string
  instance_id: string
  state: AppActionOutcomeState
  reason?: string
}

export interface WebActionProgress {
  state: WebProgressState
  percent?: number
}

export type WebActionEffect =
  | { outcome: 'applied'; page: PageID }
  | { outcome: 'applied'; title: string | null }
  | { outcome: 'applied'; progress: WebActionProgress | null }
  | { outcome: 'rejected'; reason: string }

const supportedKinds = new Set<PushedAppAction['kind']>([
  'app.page', 'app.title', 'app.progress',
])

export function pushedAppAction(
  event: ControllerEvent,
  instanceID: string,
): PushedAppAction | null {
  const operationID = event.metadata?.operation_id?.trim() ?? ''
  const kind = event.kind.trim().toLowerCase() as PushedAppAction['kind']
  if (!operationID || !supportedKinds.has(kind) ||
      !matchesAppTarget(event.metadata?.target_instance, instanceID, 'webui')) return null
  return {
    operationID,
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

  get(operationID: string): AppActionAck | undefined {
    return this.values.get(operationID)
  }

  remember(value: AppActionAck): void {
    if (!this.values.has(value.operation_id) && this.values.size >= this.maximum) {
      const oldest = this.values.keys().next().value as string | undefined
      if (oldest) this.values.delete(oldest)
    }
    this.values.set(value.operation_id, value)
  }
}

export const browserControllerStateEvent = 'pccontroller:state'

export interface BrowserConsoleState {
  readonly title: string
  readonly hostVersion: string
  readonly page: string
  readonly connected: boolean
  readonly port: string
  readonly transport: string
  readonly eventCount: number
}

export interface BrowserConsoleController {
  readonly api: 'PCController.browser/1'
  inspect(): BrowserConsoleState
  command(value: string): Promise<string>
  beep(frequencyHz?: number, durationMS?: number, target?: BrowserBeepTarget): Promise<void>
  refresh(): Promise<void>
  navigate(page: string): void
}

export type BrowserBeepTarget = 'board' | 'browser' | 'both'

export function normalizeBrowserBeep(
  frequencyHz = 2000,
  durationMS = 40,
  target: BrowserBeepTarget = 'both',
): { frequencyHz: number; durationMS: number; target: BrowserBeepTarget } {
  if (!Number.isFinite(frequencyHz) || frequencyHz < 20 || frequencyHz > 20000) throw new RangeError('frequencyHz must be between 20 and 20000')
  if (!Number.isFinite(durationMS) || durationMS < 1 || durationMS > 60000) throw new RangeError('durationMS must be between 1 and 60000')
  if (!['board', 'browser', 'both'].includes(target)) throw new TypeError('target must be board, browser, or both')
  return { frequencyHz: Math.round(frequencyHz), durationMS: Math.round(durationMS), target }
}

declare global {
  interface Window {
    PCController?: BrowserConsoleController
  }
}

export function publishBrowserConsole(controller: BrowserConsoleController): () => void {
  if (typeof window === 'undefined') return () => undefined
  const previous = window.PCController
  Object.defineProperty(window, 'PCController', { configurable: true, value: Object.freeze(controller) })
  return () => {
    if (window.PCController !== controller) return
    if (previous === undefined) delete window.PCController
    else Object.defineProperty(window, 'PCController', { configurable: true, value: previous })
  }
}

export function publishBrowserConsoleState(state: BrowserConsoleState): void {
  if (typeof window === 'undefined' || typeof CustomEvent === 'undefined') return
  window.dispatchEvent(new CustomEvent(browserControllerStateEvent, { detail: Object.freeze({ ...state }) }))
}

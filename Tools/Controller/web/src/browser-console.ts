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
  refresh(): Promise<void>
  navigate(page: string): void
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

export const browserControllerStateEvent = 'pccontroller:state'

export interface BrowserControllerState {
  readonly title: string
  readonly hostVersion: string
  readonly page: string
  readonly hostOnline: boolean
  readonly boardConnected: boolean
  readonly port: string
  readonly transport: string
  readonly eventCount: number
}

/** Living, unversioned browser control surface backed by the same host APIs as the UI. */
export interface BrowserController {
  readonly api: 'PCController.browser'
  inspect(): BrowserControllerState
  command(value: string): Promise<string>
  refresh(): Promise<void>
  navigate(page: string): void
}

declare global {
  interface Window {
    PCController?: BrowserController
  }
}

export function publishBrowserController(controller: BrowserController): () => void {
  if (typeof window === 'undefined') return () => undefined
  const previous = window.PCController
  Object.defineProperty(window, 'PCController', { configurable: true, value: Object.freeze(controller) })
  return () => {
    if (window.PCController !== controller) return
    if (previous === undefined) delete window.PCController
    else Object.defineProperty(window, 'PCController', { configurable: true, value: previous })
  }
}

export function publishBrowserControllerState(state: BrowserControllerState): void {
  if (typeof window === 'undefined' || typeof CustomEvent === 'undefined') return
  window.dispatchEvent(new CustomEvent(browserControllerStateEvent, { detail: Object.freeze({ ...state }) }))
}

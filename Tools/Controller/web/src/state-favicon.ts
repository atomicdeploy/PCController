import type { Snapshot } from './types'

export type ControllerFaviconState = 'connected' | 'connecting' | 'fault' | 'offline'

type FaviconSnapshot = Pick<Snapshot, 'connected' | 'connection_state' | 'connection_reason' | 'have_status'> & {
  status: Pick<Snapshot['status'], 'hot'>
}

const statePresentation: Record<ControllerFaviconState, { color: string; label: string; glyph: string }> = {
  connected: { color: '#43a86f', label: 'Controller connected', glyph: '<path d="m46.5 50 3 3 6-7"/>' },
  connecting: { color: '#d19345', label: 'Controller reconnecting', glyph: '<path d="M47 50h1m3 0h1m3 0h1"/>' },
  fault: { color: '#d96369', label: 'Controller alert', glyph: '<path d="M51.5 46v6m0 3v.2"/>' },
  offline: { color: '#77717f', label: 'Controller offline', glyph: '<path d="M47 51h9"/>' },
}

export function controllerFaviconState(snapshot: FaviconSnapshot): ControllerFaviconState {
  const connectionState = snapshot.connection_state.trim().toLowerCase()
  const reason = (snapshot.connection_reason ?? '').trim().toLowerCase()
  if ((snapshot.have_status && snapshot.status.hot) || /^(error|fault|failed|rejected)$/.test(connectionState) || /authentication|permission|protocol mismatch|unsupported firmware/.test(reason)) {
    return 'fault'
  }
  if (snapshot.connected) return 'connected'
  if (connectionState === 'connecting' || connectionState === 'reconnecting') return 'connecting'
  return 'offline'
}

export function controllerFaviconDataURL(state: ControllerFaviconState): string {
  const presentation = statePresentation[state]
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><title>${presentation.label}</title><rect x="3" y="3" width="58" height="58" rx="15" fill="#18161f" stroke="#3d3849" stroke-width="3"/><path d="M18 17h18c7 0 11 4 11 10s-4 10-11 10H27v10h-9V17Zm9 8v5h8c2 0 3-1 3-2.5S37 25 35 25h-8Z" fill="#8b6de0"/><rect x="42" y="42" width="18" height="18" rx="5" fill="${presentation.color}" stroke="#18161f" stroke-width="3"/><g fill="none" stroke="#fff" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">${presentation.glyph}</g></svg>`
  return `data:image/svg+xml,${encodeURIComponent(svg)}`
}

export function updateRuntimeFavicon(state: ControllerFaviconState, root: Document = document): void {
  let link = root.querySelector<HTMLLinkElement>('link[data-controller-state-icon]')
  if (!link) {
    link = root.createElement('link')
    link.rel = 'icon'
    link.type = 'image/svg+xml'
    link.sizes = 'any'
    link.dataset.controllerStateIcon = ''
    root.head.append(link)
  }
  if (link.dataset.state === state) return
  link.dataset.state = state
  link.href = controllerFaviconDataURL(state)
}

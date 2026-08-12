import type { Snapshot } from './types'

export type ControllerFaviconState = 'connected' | 'connecting' | 'fault' | 'offline'

type FaviconSnapshot = Pick<Snapshot, 'connected' | 'connection_state' | 'connection_reason' | 'have_status'> & {
  status: Pick<Snapshot['status'], 'hot'>
}

const statePresentation: Record<ControllerFaviconState, { color: string; label: string }> = {
	connected: { color: '#43a86f', label: 'Controller connected' },
	connecting: { color: '#d19345', label: 'Controller reconnecting' },
	fault: { color: '#d96369', label: 'Controller alert' },
	offline: { color: '#77717f', label: 'Controller offline' },
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
	// Preserve the shared product mark as the icon; connection state is a small
	// additive dot, never a letter/logo replacement.
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><title>${presentation.label}</title><defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#15121d"/><stop offset="1" stop-color="#241827"/></linearGradient><linearGradient id="mark" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#8b5cf6"/><stop offset=".58" stop-color="#ec4899"/><stop offset="1" stop-color="#f59e0b"/></linearGradient></defs><rect x="1" y="1" width="62" height="62" rx="12" fill="url(#bg)"/><rect x="13" y="13" width="38" height="38" rx="8" fill="url(#mark)"/><rect x="17" y="17" width="30" height="30" rx="5" fill="#0e0c13"/><rect x="23" y="23" width="18" height="18" rx="4" fill="url(#mark)"/><rect x="27" y="27" width="10" height="10" rx="2" fill="#f8f4ff"/><path d="M8 20h7M8 30h7M8 40h7M49 20h7M49 30h7M49 40h7M20 8v7M30 8v7M40 8v7M20 49v7M30 49v7M40 49v7" stroke="#d8ccff" stroke-width="2.5" stroke-linecap="round"/><circle cx="51" cy="51" r="8" fill="${presentation.color}" stroke="#0e0c13" stroke-width="3"/></svg>`
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

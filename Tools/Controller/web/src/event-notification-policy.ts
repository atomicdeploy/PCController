import type { ControllerEvent } from './types'

/** Relay state toasts are for actions the operator did not initiate from this UI. */
export function shouldToastControllerEvent(event: Pick<ControllerEvent, 'kind' | 'source'>): boolean {
  const kind = event.kind.trim().toLowerCase()
  const source = event.source?.trim().toLowerCase() ?? ''
  if (/(^|[._-])(relay|output)([._-]|$)/.test(kind)) return source === 'physical' || source === 'rf'
  return /error|warning|fault|hot|door/.test(kind)
}

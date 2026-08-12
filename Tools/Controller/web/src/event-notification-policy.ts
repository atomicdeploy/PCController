import type { ControllerEvent } from './types'

/** Relay state toasts are for actions the operator did not initiate from this UI. */
export function shouldToastControllerEvent(event: Pick<ControllerEvent, 'kind' | 'source'> & Partial<Pick<ControllerEvent, 'text'>>): boolean {
  const kind = event.kind.trim().toLowerCase()
  const source = event.source?.trim().toLowerCase() ?? ''
  const text = event.text?.trim().toLowerCase() ?? ''
  // HELLO and status frames are normal connection parsing, not operator events.
  if (/^(hello|status)(?:[._-]|$)/.test(kind) || /^(hello|status)\b/.test(text)) return false
  if (/(^|[._-])(relay|output)([._-]|$)/.test(kind)) return source === 'physical' || source === 'rf'
  return /error|warning|fault|hot|door/.test(kind)
}

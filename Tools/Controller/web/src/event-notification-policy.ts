import type { ControllerEvent } from './types'

function targetsSurface(event: Partial<Pick<ControllerEvent, 'target' | 'targets'>>, surface: string): boolean {
  const targets = [...(event.targets ?? []), ...(event.target ?? '').split(',')]
  return targets.some((target) => {
    const value = target.trim().toLowerCase()
    return value === 'all' || value === surface
  })
}

/** Relay state toasts are for actions the operator did not initiate from this UI. */
export function shouldToastControllerEvent(
  event: Pick<ControllerEvent, 'kind'> & Partial<Pick<ControllerEvent, 'source' | 'text' | 'target' | 'targets' | 'metadata'>>,
): boolean {
  const kind = event.kind.trim().toLowerCase()
  const directSource = event.source?.trim().toLowerCase() ?? ''
  const detailedSource = event.metadata?.source?.trim().toLowerCase() ?? ''
  // Firmware envelopes often use "board" for transport provenance while the
  // action metadata retains the real origin (physical, RF, macro, Web UI).
  const source = directSource && directSource !== 'board' ? directSource : detailedSource || directSource
  const text = event.text?.trim().toLowerCase() ?? ''
  if (kind === 'message') return targetsSurface(event, 'web')
  // HELLO and status frames are normal connection parsing, not operator events.
  if (/^(hello|status)(?:[._-]|$)/.test(kind) || /^(hello|status)\b/.test(text)) return false
  if (/(^|[._-])(relay|output)([._-]|$)/.test(kind)) return source === 'physical' || source === 'rf'
  return /error|warning|fault|hot|door/.test(kind)
}

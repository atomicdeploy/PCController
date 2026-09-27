import type { ControllerEvent } from './types'

/** Human-facing toasts are reserved for one-shot operator and safety events. */
export function shouldToastControllerEvent(
  event: Pick<ControllerEvent, 'kind'> & Partial<Pick<ControllerEvent, 'source' | 'text' | 'metadata' | 'target'>>,
): boolean {
  const kind = event.kind.trim().toLowerCase()
  const directSource = event.source?.trim().toLowerCase() ?? ''
  const detailedSource = event.metadata?.source?.trim().toLowerCase() ?? ''
  const source = directSource && directSource !== 'board' ? directSource : detailedSource || directSource
  const text = event.text?.trim().toLowerCase() ?? ''

  if (/^(hello|status|telemetry|rx|tx)(?:[._-]|$)/.test(kind) || /^(hello|status)\b/.test(text)) return false
  if (kind === 'message') {
    const targets = (event.target ?? '').split(',').map((value) => value.trim().toLowerCase())
    return targets.includes('all') || targets.includes('web') || targets.includes('webui')
  }
  if (/(^|[._-])(relay|output)([._-]|$)/.test(kind)) return source === 'physical' || source === 'rf'
  return /error|warning|fault|hot|door/.test(kind)
}

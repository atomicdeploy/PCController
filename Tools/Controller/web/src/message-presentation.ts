import type { ControllerEvent, Locale, ToastMessage } from './types'

function messageTone(event: ControllerEvent): ToastMessage['tone'] {
  switch (event.severity?.trim().toLowerCase()) {
    case 'success': return 'success'
    case 'warning': return 'warning'
    case 'error': return 'danger'
    default: return 'info'
  }
}

function copy(locale: Locale, english: string, persian: string): string {
  return locale === 'fa' ? persian : english
}

/**
 * Converts one canonical message event into its Web presentation without
 * turning received action metadata into executable input. Action execution is
 * owned by the authenticated command and exact-target app-action contracts.
 */
export function messageToast(
  event: ControllerEvent,
  locale: Locale,
): Omit<ToastMessage, 'id'> | null {
  if (event.kind.trim().toLowerCase() !== 'message' ||
      !Number.isSafeInteger(event.id) || event.id <= 0) return null
  const action = event.action?.trim() || undefined
  const correlation = event.correlation?.trim() || undefined
  return {
    tone: messageTone(event),
    title: event.metadata?.title?.trim() || event.message_type?.trim() ||
      copy(locale, 'Message', 'پیام'),
    detail: event.text,
    messageEventID: event.id,
    correlation,
    action,
    actionLabel: action
      ? event.metadata?.action_label?.trim() || copy(locale, 'Suggested action', 'عملیات پیشنهادی')
      : undefined,
    // Correlated or actionable messages stay available until the operator
    // dismisses them. Merely presenting either field never performs work.
    persistent: Boolean(action || correlation),
  }
}

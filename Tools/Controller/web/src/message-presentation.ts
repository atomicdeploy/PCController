import type { ControllerEvent, ToastMessage } from './types'

function tone(event: ControllerEvent): ToastMessage['tone'] {
  switch (event.severity) {
    case 'success': return 'success'
    case 'warning': return 'warning'
    case 'error': return 'danger'
    default: return 'info'
  }
}

/** Converts one targeted message event without dropping its action identity. */
export function messageToast(event: ControllerEvent): Omit<ToastMessage, 'id'> | null {
  if (event.kind.trim().toLowerCase() !== 'message' || !Number.isSafeInteger(event.id) || event.id <= 0) return null
  const title = event.metadata?.title?.trim() || event.message_type?.trim() || 'message'
  const action = event.action?.trim() || undefined
  const correlation = event.correlation?.trim() || undefined
  return {
    tone: tone(event),
    title,
    detail: event.text,
    messageEventID: event.id,
    correlation,
    action,
    actionLabel: action ? event.metadata?.action_label?.trim() || 'Run action' : undefined,
    // An actionable message remains available until the operator explicitly
    // chooses or dismisses it; presentation alone never runs the action.
    persistent: Boolean(action),
  }
}

export function messageDeliveryParams(message: Pick<ToastMessage, 'messageEventID'>) {
  return { event_id: message.messageEventID, surface: 'web' as const }
}

export function messageActionParams(
  message: Pick<ToastMessage, 'messageEventID'>,
  instanceID: string,
) {
  return {
    event_id: message.messageEventID,
    surface: 'web' as const,
    ...(instanceID.trim() ? { instance_id: instanceID.trim() } : {}),
  }
}

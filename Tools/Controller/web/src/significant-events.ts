import type { ControllerEvent } from './types'

// Transport chatter belongs to the status sample/chart path, not the human activity timeline.
const routineEventKinds = new Set([
  'telemetry', 'rx', 'tx', 'front_panel.segment', 'status_led.changed', 'buzzer.note',
  'action.applied', 'device event 13',
])

export function isSignificantControllerEvent(event: Pick<ControllerEvent, 'kind' | 'stream'>): boolean {
  if (event.stream) return event.stream === 'activity'
  return !routineEventKinds.has(event.kind.trim().toLowerCase())
}

export function significantControllerEvents(events: readonly ControllerEvent[]): ControllerEvent[] {
  return events.filter(isSignificantControllerEvent)
}

export function prependSignificantControllerEvent(
  current: readonly ControllerEvent[],
  event: ControllerEvent,
  limit = 500,
): ControllerEvent[] {
  const retained = significantControllerEvents(current)
  if (!isSignificantControllerEvent(event)) return retained.slice(0, limit)
  return [event, ...retained.filter((item) => item.id !== event.id)].slice(0, limit)
}

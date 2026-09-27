import type { ControllerEvent } from './types'

// Transport chatter belongs to the status sample/chart path, not the human activity timeline.
const routineEventKinds = new Set([
  'telemetry', 'rx', 'tx', 'front_panel.segment', 'status_led.changed', 'buzzer.note',
  'action.applied', 'device event 13', 'relay.state',
])

const materialInstanceLifecycles = new Set(['joined', 'connected', 'left', 'disconnected', 'expired', 'removed'])

export function isSignificantControllerEvent(event: Pick<ControllerEvent, 'kind' | 'stream' | 'lifecycle' | 'state' | 'metadata'>): boolean {
  const kind = event.kind.trim().toLowerCase()
  // An explicit host stream cannot promote known high-rate state/debug frames
  // into the human activity list. This also covers older hosts that classified
  // unknown device event 13 as activity.
  if (routineEventKinds.has(kind)) return false
  if (kind === 'app.instance.changed') {
    const lifecycle = (event.lifecycle || event.state || event.metadata?.change || '').trim().toLowerCase()
    return materialInstanceLifecycles.has(lifecycle)
  }
  if (event.stream) return event.stream === 'activity'
  return true
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

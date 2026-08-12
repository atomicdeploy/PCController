import type { ControllerEvent, MacroSnapshot } from './types'

const terminalPlaybackLifecycles = new Set(['cancelled', 'completed', 'failed', 'local-cancelled', 'local-completed', 'local-failed'])
const terminalRecordingLifecycles = new Set(['saved', 'saved-truncated', 'discarded', 'failed', 'cleared'])

function metadataNumber(event: ControllerEvent, key: string, fallback: number): number {
  const value = Number(event.metadata?.[key])
  return Number.isFinite(value) ? value : fallback
}

function macroIdentity(event: ControllerEvent, current: { id: number; name: string }) {
  return {
    id: metadataNumber(event, 'macro_id', current.id),
    name: event.metadata?.macro_name || current.name,
  }
}

/** Identifies every structured macro library, recording, playback, and timing event. */
export function isMacroControllerEvent(event: Pick<ControllerEvent, 'kind'>): boolean {
  const kind = event.kind.trim().toLowerCase()
  return kind === 'macro' || kind.startsWith('macro.')
}

/** Retains a small dedicated live stream without promoting timing chatter to Activity. */
export function prependMacroControllerEvent(
  current: readonly ControllerEvent[],
  event: ControllerEvent,
  limit = 80,
): ControllerEvent[] {
  if (!isMacroControllerEvent(event)) return current.slice(0, limit)
  return [event, ...current.filter((item) => item.id !== event.id)].slice(0, limit)
}

/** Lifecycle changes need the full catalog/state; individual steps are complete on the wire. */
export function macroEventNeedsSnapshot(event: Pick<ControllerEvent, 'kind'>): boolean {
  const kind = event.kind.trim().toLowerCase()
  return kind === 'macro.library' || kind === 'macro.recording' || kind === 'macro.recovery'
}

/** Applies low-latency structured timing evidence while an authoritative snapshot catches up. */
export function applyMacroEventToSnapshot(snapshot: MacroSnapshot, event: ControllerEvent): MacroSnapshot {
  if (!isMacroControllerEvent(event)) return snapshot
  const kind = event.kind.trim().toLowerCase()
  const lifecycle = (event.lifecycle || event.state || '').trim().toLowerCase()
  const next: MacroSnapshot = {
    ...snapshot,
    latest_event_id: Math.max(snapshot.latest_event_id, event.id),
    playback: { ...snapshot.playback },
    recording: { ...snapshot.recording },
    library: snapshot.library,
  }

  if (kind === 'macro.recording.step') {
    const identity = macroIdentity(event, next.recording)
    next.recording = {
      ...next.recording,
      ...identity,
      active: true,
      steps: metadataNumber(event, 'step', next.recording.steps),
      last_at_us: metadataNumber(event, 'at_us', next.recording.last_at_us),
      last_delta_us: metadataNumber(event, 'delta_us', next.recording.last_delta_us),
      last_opcode: metadataNumber(event, 'opcode', next.recording.last_opcode),
      last_source: metadataNumber(event, 'source', next.recording.last_source),
    }
    return next
  }

  if (kind === 'macro.recording') {
    const active = !terminalRecordingLifecycles.has(lifecycle) && !lifecycle.startsWith('saved') && lifecycle !== 'idle'
    next.recording = {
      ...next.recording,
      active,
      last_error: lifecycle === 'failed' ? event.reason || event.text : next.recording.last_error,
    }
    return next
  }

  if (kind === 'macro' || kind === 'macro.playback' || kind === 'macro.step') {
    const identity = macroIdentity(event, next.playback)
    const terminal = terminalPlaybackLifecycles.has(lifecycle)
    next.playback = {
      ...next.playback,
      ...identity,
      category: event.metadata?.category ?? next.playback.category,
      color: event.metadata?.color ?? next.playback.color,
      running: kind === 'macro.step' || (!terminal && lifecycle !== 'idle'),
      lifecycle: lifecycle || next.playback.lifecycle,
      step: metadataNumber(event, 'step', next.playback.step),
      step_count: metadataNumber(event, 'steps', next.playback.step_count),
      last_timing_delta_us: metadataNumber(event, 'mcu_delta_us', next.playback.last_timing_delta_us),
      maximum_timing_error_us: metadataNumber(event, 'timing_error_us', next.playback.maximum_timing_error_us),
      underruns: metadataNumber(event, 'underruns', next.playback.underruns),
      dispatch_errors: metadataNumber(event, 'dispatch_errors', next.playback.dispatch_errors),
      faithful: event.metadata?.faithful === undefined ? next.playback.faithful : event.metadata.faithful === 'true',
      last_error: lifecycle === 'failed' ? event.reason || event.text : next.playback.last_error,
    }
  }
  return next
}

/** Legacy command parsing is allowed only when a typed method truly does not exist. */
export function shouldUseLegacyMacroFallback(cause: unknown): boolean {
  const detail = cause instanceof Error ? cause.message : String(cause)
  return /(?:method\s+not\s+found|unknown\s+(?:rpc\s+)?method|unsupported\s+(?:rpc\s+)?method|not\s+implemented)/i.test(detail)
}

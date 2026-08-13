import type { ControllerEvent, FrontPanelState, Snapshot, StatusLEDState } from './types'

const byte = (value: string | undefined): number | null => {
  if (value === undefined || value.trim() === '') return null
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 255 ? parsed : null
}

export function statusLEDFromEvent(event: ControllerEvent): StatusLEDState | null {
  if (event.kind.toLowerCase() !== 'status_led.changed') return null
  const red = byte(event.metadata?.red)
  const green = byte(event.metadata?.green)
  const blue = byte(event.metadata?.blue)
  const brightness = byte(event.metadata?.brightness)
  const effect = byte(event.metadata?.effect)
  const condition = byte(event.metadata?.condition)
  if ([red, green, blue, brightness, effect, condition].some((value) => value === null)) return null
  return { red: red!, green: green!, blue: blue!, brightness: brightness!, effect: effect!, condition: condition! }
}

export function applyStatusLEDEvent(snapshot: Snapshot, event: ControllerEvent): Snapshot {
  const state = statusLEDFromEvent(event)
  if (!state) return snapshot
  return {
    ...snapshot,
    status_led: state,
    have_status_led: true,
    status_led_updated: event.time,
  }
}

export function segmentStateFromEvent(event: ControllerEvent): Pick<FrontPanelState, 'raw_segments' | 'brightness'> | null {
  if (event.kind.toLowerCase() !== 'front_panel.segment') return null
  const raw = event.metadata?.raw_segments?.trim()
  const brightness = byte(event.metadata?.brightness)
  if (!raw || !/^[0-9a-f]{8}$/i.test(raw) || brightness === null) return null
  return {
    raw_segments: [0, 2, 4, 6].map((offset) => Number.parseInt(raw.slice(offset, offset + 2), 16)) as [number, number, number, number],
    brightness,
  }
}

export function applyPushedOutputEvent(snapshot: Snapshot, event: ControllerEvent): Snapshot {
  const led = statusLEDFromEvent(event)
  if (led) return applyStatusLEDEvent(snapshot, event)
  const segment = segmentStateFromEvent(event)
  if (!segment) return snapshot
  // SEGMENT_CHANGED carries only four raw digits and brightness. It may
  // refresh those fields inside a previously fetched exact snapshot, but it
  // must never synthesize menu/LCD/key authority from zero values.
  if (!snapshot.have_front_panel || !snapshot.front_panel) return snapshot
  return {
    ...snapshot,
    front_panel: { ...snapshot.front_panel, ...segment, segments_active: true },
    front_panel_updated: event.time,
  }
}

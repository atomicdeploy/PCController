import type { ControllerEvent, FrontPanelState, Snapshot, StatusLEDState } from './types'

const byte = (value: string | undefined): number | null => {
  if (value === undefined || value.trim() === '') return null
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 255 ? parsed : null
}

export function isPushedOutputEvent(event: Pick<ControllerEvent, 'kind'>): boolean {
  return ['status_led.changed', 'front_panel.segment', 'relay.state', 'relay.changed'].includes(event.kind.trim().toLowerCase())
}

export function relayMaskFromEvent(event: ControllerEvent): number | null {
  const kind = event.kind.trim().toLowerCase()
  if (kind !== 'relay.state' && kind !== 'relay.changed') return null
  const raw = event.metadata?.active_relays ?? event.metadata?.relay_mask
  if (raw === undefined || raw.trim() === '') return null
  const parsed = Number(raw)
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 0xff ? parsed : null
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
  const relays = relayMaskFromEvent(event)
  if (relays !== null) {
    return {
      ...snapshot,
      status: { ...snapshot.status, active_relays: relays },
      have_status: true,
      status_updated: event.time,
    }
  }
  const segment = segmentStateFromEvent(event)
  if (!segment) return snapshot
  const current: FrontPanelState = snapshot.front_panel ?? {
    schema: 2,
    raw_segments: [0, 0, 0, 0],
    brightness: 0,
    blink: false,
    segments_active: true,
    category_selector: false,
    lcd_address: 0,
    lcd_available: false,
    lcd_backlight: false,
    lcd_line_1: '',
    lcd_line_2: '',
    pressed_keys: 0,
    menu_page: 0,
    program_mode: 0,
    host_captured: false,
    host_state: 0,
    host_editable_value: 0,
  }
  return {
    ...snapshot,
    front_panel: { ...current, ...segment, segments_active: true },
    have_front_panel: true,
    front_panel_updated: event.time,
  }
}

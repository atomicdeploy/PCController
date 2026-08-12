import type { ControllerEvent, FrontPanelState, Snapshot, StatusLEDState } from './types'

export interface StatusLEDSource {
  epoch: number
  instanceID?: string
}

export interface StatusLEDSnapshotSource extends StatusLEDSource {
  authoritativeInstanceID?: string
}

export function statusLEDSourceUnchanged(
  started: StatusLEDSource,
  current: StatusLEDSource,
): boolean {
  return started.epoch === current.epoch &&
    (started.instanceID?.trim() ?? '') === (current.instanceID?.trim() ?? '')
}

export function advanceStatusLEDSource(
  current: StatusLEDSource,
  incoming: StatusLEDSource,
): StatusLEDSource | null {
  if (incoming.epoch < current.epoch) return null
  const incomingInstanceID = incoming.instanceID?.trim() ?? ''
  if (incoming.epoch > current.epoch) {
    return {
      epoch: incoming.epoch,
      ...(incomingInstanceID ? { instanceID: incomingInstanceID } : {}),
    }
  }
  if (!incomingInstanceID) return current
  return { epoch: current.epoch, instanceID: incomingInstanceID }
}

export function statusLEDSnapshotMatchesSource(
  snapshot: Pick<Snapshot, 'host_instance_id'>,
  source: StatusLEDSource,
): boolean {
  const incomingInstanceID = snapshot.host_instance_id?.trim() ?? ''
  const authoritativeInstanceID = source.instanceID?.trim() ?? ''
  return incomingInstanceID === '' || authoritativeInstanceID === '' ||
    incomingInstanceID === authoritativeInstanceID
}

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

const revisionFromEvent = (event: ControllerEvent): number => {
  const revision = Number(event.metadata?.revision)
  return Number.isSafeInteger(revision) && revision > 0 ? revision : 0
}

type Order = 'unknown' | 'older' | 'equal' | 'newer'

function compareOrder(currentEpoch = 0, currentRevision = 0, incomingEpoch = 0, incomingRevision = 0): Order {
  if (currentEpoch === 0 && incomingEpoch === 0) return 'unknown'
  if (currentEpoch === 0) return 'newer'
  if (incomingEpoch === 0) return 'older'
  if (incomingEpoch < currentEpoch) return 'older'
  if (incomingEpoch > currentEpoch) return 'newer'
  if (currentRevision === 0 && incomingRevision === 0) return 'unknown'
  if (currentRevision === 0) return 'newer'
  if (incomingRevision === 0 || incomingRevision < currentRevision) return 'older'
  if (incomingRevision === currentRevision) return 'equal'
  return 'newer'
}

export function applyStatusLEDEvent(snapshot: Snapshot, event: ControllerEvent, source: StatusLEDSource = { epoch: 0 }): Snapshot {
  const state = statusLEDFromEvent(event)
  if (!state) return snapshot
  const revision = revisionFromEvent(event)
  const incomingInstanceID = source.instanceID?.trim() ?? ''
  const currentInstanceID = snapshot.host_instance_id?.trim() ?? ''
  const order = compareOrder(snapshot.status_led_epoch, snapshot.status_led_revision, source.epoch, revision)
  // The acknowledged live source is authoritative within its transport
  // generation. This also closes a snapshot-A / reconnect-B race without
  // comparing random process IDs lexically.
  const sourceChanged = source.epoch === snapshot.status_led_epoch &&
    incomingInstanceID !== '' && currentInstanceID !== '' && incomingInstanceID !== currentInstanceID
  if (order === 'older' || (snapshot.have_status_led && !sourceChanged && order === 'equal')) return snapshot
  return {
    ...snapshot,
    host_instance_id: incomingInstanceID || snapshot.host_instance_id,
    status_led: state,
    have_status_led: true,
    status_led_updated: event.time,
    status_led_revision: revision,
    status_led_epoch: source.epoch || snapshot.status_led_epoch,
  }
}

export function mergeStatusLEDSnapshot(current: Snapshot, incoming: Snapshot, source: StatusLEDSnapshotSource): Snapshot {
  const candidate: Snapshot = { ...incoming, status_led_epoch: source.epoch }
  const incomingInstanceID = incoming.host_instance_id?.trim() ?? ''
	const authoritativeInstanceID = source.authoritativeInstanceID?.trim() ?? ''
	const identityConflict = authoritativeInstanceID !== '' && incomingInstanceID !== '' &&
		incomingInstanceID !== authoritativeInstanceID
	if (identityConflict) return current
	const order = compareOrder(
    current.status_led_epoch, current.status_led_revision,
    source.epoch, incoming.status_led_revision,
  )
  const newSourceEpoch = source.epoch > (current.status_led_epoch ?? 0)
  if (order === 'older' ||
      (current.have_status_led && (order === 'equal' || (!incoming.have_status_led && !newSourceEpoch)))) {
    return {
      ...candidate,
      host_instance_id: current.host_instance_id,
      status_led: current.status_led,
      have_status_led: true,
      status_led_updated: current.status_led_updated,
      status_led_revision: current.status_led_revision,
      status_led_epoch: current.status_led_epoch,
    }
  }
  if (current.have_status_led && !incoming.have_status_led) {
    return {
      ...candidate,
      status_led: current.status_led,
      have_status_led: true,
      status_led_updated: current.status_led_updated,
    }
  }
  if (!current.have_status_led) return candidate
  return candidate
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

export function applyPushedOutputEvent(snapshot: Snapshot, event: ControllerEvent, source: StatusLEDSource = { epoch: 0 }): Snapshot {
  const led = statusLEDFromEvent(event)
  if (led) return applyStatusLEDEvent(snapshot, event, source)
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

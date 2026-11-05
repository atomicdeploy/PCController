import { useEffect, useRef, useState } from 'react'
import { rpc } from './api'
import type { ControllerEvent, HostUISettings, Snapshot } from './types'

export function channelPresentationEventID(events: readonly ControllerEvent[]): string {
  return ['config', 'peripherals.changed'].map(kind => events.find(event => event.kind === kind)?.id ?? '').join('/')
}

export function channelPresentationOwner(snapshot: Snapshot): string {
  return `${snapshot.host_instance_id}/${snapshot.connection_generation}/${snapshot.port.name}`
}

// Metadata follows change events and reconnects, not the telemetry frame rate.
// Failed reads keep the last good catalog for this owner. A replaced board or
// primary cannot inherit another board's names, icons or locked state.
export function useChannelSettings(ready: boolean, snapshot: Snapshot, events: readonly ControllerEvent[], streamState: string) {
  const [settings, setSettings] = useState<HostUISettings | null>(null)
  const owner = channelPresentationOwner(snapshot)
  const storedOwner = useRef(owner)
  const revision = channelPresentationEventID(events)
  useEffect(() => {
    const abort = new AbortController()
    if (!ready || storedOwner.current !== owner) setSettings(null)
    storedOwner.current = owner
    if (ready) void rpc<HostUISettings>('controller.ui.config.get', {}, abort.signal)
      .then(value => { if (!abort.signal.aborted) setSettings(value) })
      .catch(() => undefined)
    return () => abort.abort()
  }, [ready, owner, revision, streamState])
  return [storedOwner.current === owner ? settings : null, setSettings] as const
}

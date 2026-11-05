import { useEffect, useRef, useState } from 'react'
import { ArrowRightLeft, Power, Square } from 'lucide-react'
import { rpc } from './api'
import { Button, Card, TextField } from './components'
import { ChannelIcon } from './channel-icon'
import { useRoutingCatalog } from './channel-routing-editor'
import { channelPresentationEventID, channelPresentationOwner } from './use-channel-settings'
import type { SemanticChannel } from './channel-routing'
import type { ControllerEvent, Locale, Snapshot } from './types'

export function semanticObservedValue(channel: SemanticChannel, snapshot: Snapshot): number | null {
  if (!snapshot.connected) return null
  const index = Number(channel.target.split('.')[1])
  if (channel.kind === 'relay') return snapshot.have_status ? Number(Boolean(snapshot.status.active_relays & (1 << (index - 1)))) : null
  const value = snapshot.pwm_values?.available ? snapshot.pwm_values.values[index] : undefined
  return typeof value === 'number' ? value : null
}

export function SemanticChannelControls({snapshot, events, locale, streamState}: {snapshot: Snapshot; events: ControllerEvent[]; locale: Locale; streamState: string}) {
  const scope = channelPresentationOwner(snapshot)
  const {catalog} = useRoutingCatalog(`${channelPresentationEventID(events)}/${streamState}`, scope)
  const text = (en: string, fa: string) => locale === 'fa' ? fa : en
  const [pending, setPending] = useState(false)
  const busy = useRef(false)
  const [error, setError] = useState('')
  const [values, setValues] = useState<Record<string,string>>({})
  const owner = useRef(scope)
  owner.current = scope
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => { setValues({}); setError('') }, [scope])
  if (!catalog?.routing.enabled || !catalog.routing.channels.length) return null
  const set = async (channel: SemanticChannel, value: number) => {
    if (busy.current) return
    busy.current = true; setPending(true); setError('')
    const requestOwner = scope
    try {
      await rpc('controller.channel.set', {key: channel.key, value, board_identity: catalog.board_profile.board_identity, expected_revision: catalog.board_profile.revision,
        expected_host_instance_id:catalog.host_instance_id, expected_connection_generation:catalog.connection_generation})
      if (mounted.current && owner.current === requestOwner) setValues(previous => {const next = {...previous}; delete next[channel.key]; return next})
    } catch (cause) { if (mounted.current && owner.current === requestOwner) setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { busy.current = false; if (mounted.current) setPending(false) }
  }
  return <Card icon={ArrowRightLeft} title={text('Semantic channels', 'کانال‌های معنایی')} iconTone="accent">
    <div className="semantic-channel-controls">
      {catalog.routing.channels.map(channel => {
        const observed = semanticObservedValue(channel, snapshot)
        const target = catalog.targets.find(item => item.key === channel.target)
        const blocked = pending || !snapshot.connected || !catalog.board_profile.attached || !target || target.locked
        const raw = values[channel.key] ?? String(observed ?? '')
        const valid = /^\d{1,4}$/.test(raw) && Number(raw) <= 4095
        return <article key={channel.key}>
          <header><ChannelIcon icon={channel.icon}/><strong title={channel.name}>{channel.name}</strong><output>{observed === null ? '—' : channel.kind === 'relay' ? observed ? text('On', 'روشن') : text('Off', 'خاموش') : `${observed} / 4095`}</output></header>
          <code title={channel.key}>{channel.key} → {channel.target}</code>
          {channel.kind === 'pwm' && <TextField label={text('PWM value', 'مقدار PWM')} inputMode="numeric" value={raw} disabled={blocked} onChange={event => setValues({...values, [channel.key]: event.currentTarget.value})}
            error={raw && !valid ? text('Enter 0–4095.', 'مقداری بین ۰ تا ۴۰۹۵ وارد کنید.') : undefined}
            action={<Button compact disabled={blocked || !valid || snapshot.emergency_stop?.active && Number(raw) !== 0} onClick={() => void set(channel, Number(raw))}>{text('Set', 'اعمال')}</Button>}/>}
          <div className="channel-routing-footer">
            <Button compact icon={Square} disabled={blocked} onClick={() => void set(channel, 0)}>{text('Off', 'خاموش')}</Button>
            <Button compact icon={Power} tone="primary" disabled={blocked || snapshot.emergency_stop?.active} onClick={() => void set(channel, channel.kind === 'relay' ? 1 : 4095)}>{text('On', 'روشن')}</Button>
          </div>
        </article>
      })}
    </div>
    {error && <p role="alert" className="text-warn">{error}</p>}
  </Card>
}

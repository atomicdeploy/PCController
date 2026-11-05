import { useEffect, useRef, useState } from 'react'
import { ArrowRightLeft, Plus, RotateCcw, Save, Trash2 } from 'lucide-react'
import { rpc } from './api'
import { Button, Card, TextField, Toggle } from './components'
import { ChannelIcon } from './channel-icon'
import { rebindChannel, routingDirty, semanticKeyValid } from './channel-routing'
import type { ChannelRouting, ChannelRoutingCatalog, SemanticChannel } from './channel-routing'
import type { Locale } from './types'
import './channel-routing.css'

export function useRoutingCatalog(refreshToken: string, scope: string) {
  const [catalog, setCatalog] = useState<ChannelRoutingCatalog | null>(null)
  const [error, setError] = useState('')
  const [reload, setReload] = useState(0)
  const owner = useRef(scope)
  useEffect(() => {
    const abort = new AbortController()
    if (owner.current !== scope) setCatalog(null)
    owner.current = scope
    void rpc<ChannelRoutingCatalog>('controller.channel_routing.get', {}, abort.signal).then(value => {
      if (!abort.signal.aborted) { setCatalog(value); setError('') }
    }).catch(cause => { if (!abort.signal.aborted) setError(String(cause instanceof Error ? cause.message : cause)) })
    return () => abort.abort()
  }, [refreshToken, scope, reload])
  return {catalog: owner.current === scope ? catalog : null, error, accept: setCatalog, refresh: () => setReload(value => value + 1)}
}

export function ChannelRoutingEditor({locale, refreshToken, scope}: {locale: Locale; refreshToken: string; scope: string}) {
  const text = (en: string, fa: string) => locale === 'fa' ? fa : en
  const {catalog, error, accept, refresh} = useRoutingCatalog(refreshToken, scope)
  const [base, setBase] = useState<ChannelRoutingCatalog | null>(null)
  const [draft, setDraft] = useState<ChannelRouting | null>(null)
  const [conflict, setConflict] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [newKey, setNewKey] = useState('')
  const [newName, setNewName] = useState('')
  const [newTarget, setNewTarget] = useState('')
  const dirty = Boolean(base && draft && routingDirty(base.routing, draft))
  const state = useRef({base, draft, dirty, busy, scope})
  state.current = {base, draft, dirty, busy, scope}
  const draftOwner = useRef(scope)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const adopt = (next: ChannelRoutingCatalog) => {
    setBase(next); setDraft(structuredClone(next.routing)); setConflict(false); setNotice(''); draftOwner.current = scope
  }
  useEffect(() => {
    if (!catalog || state.current.busy) return
    if (state.current.dirty && (draftOwner.current !== scope || state.current.base?.board_profile.revision !== catalog.board_profile.revision)) {
      setConflict(true)
    } else if (!state.current.dirty) adopt(catalog)
  }, [catalog, scope, busy])
  const blocked = busy || conflict || draftOwner.current !== scope || !catalog?.board_profile.configured
  const edit = (key: string, values: Partial<SemanticChannel>) => {
    if (draft) setDraft({...draft, channels: draft.channels.map(channel => channel.key === key ? {...channel, ...values} : channel)})
  }
  const targets = catalog?.targets ?? []
  const unused = targets.filter(target => !draft?.channels.some(channel => channel.target === target.key))
  const selectedTarget = unused.find(target => target.key === newTarget) ?? unused[0]
  const addValid = semanticKeyValid(newKey) && newName.trim().length > 0 && !draft?.channels.some(channel => channel.key === newKey) && selectedTarget
  const valid = draft?.channels.every(channel => channel.name.trim().length > 0 && channel.name.length <= 64)
  const save = async () => {
    if (state.current.busy || blocked || !base || !draft || !dirty || !valid) return
    state.current.busy = true; setBusy(true); setNotice('')
    const owner = scope
    try {
      const result = await rpc<ChannelRoutingCatalog>('controller.channel_routing.update', {
        board_identity: base.board_profile.board_identity, expected_revision: base.board_profile.revision,
        routing: {...draft, channels: draft.channels.map(channel => ({...channel, name: channel.name.trim()}))},
      })
      if (!mounted.current) return
      if (state.current.scope !== owner) { setConflict(true); setNotice(text('Connection changed; reload to confirm the save.', 'اتصال تغییر کرد؛ برای بررسی ذخیره دوباره بارگذاری کنید.')); return }
      adopt(result)
      accept(result)
      refresh()
    } catch (cause) {
      if (mounted.current) setNotice(cause instanceof Error ? cause.message : String(cause))
    } finally { if (mounted.current) setBusy(false) }
  }
  // An unconfigured installation has no board identity to assign wiring to.
  if (!catalog?.board_profile.configured && !base) return error ? <div role="alert">{error}</div> : null
  return <Card icon={ArrowRightLeft} iconTone="accent" title={text('Semantic channel routing', 'مسیریابی معنایی کانال‌ها')}
    className="settings-card settings-card--wide channel-routing-card"
    eyebrow={dirty ? text('Unsaved changes', 'تغییرات ذخیره‌نشده') : undefined}>
    <Toggle label={text('Enable semantic channels', 'فعال‌سازی کانال‌های معنایی')} checked={draft?.enabled ?? false}
      disabled={blocked} onChange={enabled => draft && setDraft({...draft, enabled})}/>
    <div className="channel-routing-list">
      {draft?.channels.map(channel => <div key={channel.key} className="channel-routing-row">
        <div className="channel-routing-identity"><ChannelIcon icon={channel.icon}/><code title={channel.key}>{channel.key}</code></div>
        <TextField label={text('Name', 'نام')} value={channel.name} maxLength={64} disabled={blocked} onChange={event => edit(channel.key, {name: event.currentTarget.value})}/>
        <label className="channel-routing-target"><span>{text('Physical output', 'خروجی فیزیکی')}</span>
          <select disabled={blocked} value={channel.target} onChange={event => draft && setDraft(rebindChannel(draft, channel.key, event.currentTarget.value))}>
            {targets.filter(target => target.kind === channel.kind).map(target => {
              const peer = draft.channels.find(item => item.target === target.key && item.key !== channel.key)
              return <option key={target.key} value={target.key}>{target.key} · {target.name ?? target.default_name}{peer ? ` ↔ ${peer.name}` : ''}</option>
            })}
          </select>
        </label>
        <Button compact icon={Trash2} disabled={blocked} title={text(`Remove ${channel.name}`, `حذف ${channel.name}`)} aria-label={text(`Remove ${channel.name}`, `حذف ${channel.name}`)} onClick={() => draft && setDraft({...draft, channels: draft.channels.filter(item => item.key !== channel.key)})}/>
      </div>)}
    </div>
    {unused.length > 0 && <form className="channel-routing-new" onSubmit={event => {
      event.preventDefault()
      if (blocked || !addValid || !draft || !selectedTarget) return
      setDraft({...draft, channels: [...draft.channels, {key: newKey, name: newName.trim(), kind: selectedTarget.kind as 'relay' | 'pwm', target: selectedTarget.key, icon: selectedTarget.icon ?? (selectedTarget.kind === 'relay' ? 'plug' : 'lightbulb')}]})
      setNewKey(''); setNewName('')
    }}>
      <TextField label={text('Stable key', 'کلید ثابت')} placeholder="lighting.aisle" dir="ltr" value={newKey} maxLength={64} disabled={blocked} onChange={event => setNewKey(event.currentTarget.value)} error={newKey && !semanticKeyValid(newKey) ? text('Use a unique lowercase key outside physical namespaces.', 'یک کلید یکتای لاتین کوچک خارج از نام‌های فیزیکی وارد کنید.') : undefined}/>
      <TextField label={text('Name', 'نام')} value={newName} maxLength={64} disabled={blocked} onChange={event => setNewName(event.currentTarget.value)}/>
      <label className="channel-routing-target"><span>{text('Physical output', 'خروجی فیزیکی')}</span><select disabled={blocked} value={selectedTarget?.key ?? ''} onChange={event => setNewTarget(event.currentTarget.value)}>{unused.map(target => <option key={target.key} value={target.key}>{target.key} · {target.name ?? target.default_name}</option>)}</select></label>
      <Button compact type="submit" icon={Plus} disabled={blocked || !addValid}>{text('Add', 'افزودن')}</Button>
    </form>}
    <footer className="channel-routing-footer">
      <span role="status" className="text-warn">{conflict ? text('Bindings changed elsewhere. Your draft is preserved; reload before saving.', 'اتصال‌ها در جای دیگری تغییر کردند؛ پیش‌نویس محفوظ است. پیش از ذخیره دوباره بارگذاری کنید.') : notice || error}</span>
      <Button compact icon={RotateCcw} disabled={busy || !catalog} onClick={() => { if (catalog) adopt(catalog); refresh() }}>{dirty ? text('Discard draft', 'لغو پیش‌نویس') : text('Refresh', 'تازه‌سازی')}</Button>
      <Button icon={Save} tone="primary" busy={busy} disabled={blocked || !dirty || !valid} onClick={() => void save()}>{text('Save bindings', 'ذخیرهٔ اتصال‌ها')}</Button>
    </footer>
  </Card>
}

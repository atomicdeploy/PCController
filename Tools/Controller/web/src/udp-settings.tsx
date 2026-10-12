import { useEffect, useState } from 'react'
import { RefreshCw, Save, Trash2 } from 'lucide-react'
import { rpc } from './api'
import { Button, TextField, Toggle } from './components'

type Config = { enabled: boolean; listen: string; discovery: boolean; broadcast_events: boolean; peers?: Record<string,string> }
type Status = { running: boolean; error?: string; peers?: { id: string; address: string; identity: { app?: string } }[] }

export function UDPSettings({ locale }: { locale: string }) {
  const text = (en: string, fa: string) => locale === 'fa' ? fa : en
  const [config,setConfig] = useState<Config>()
  const [status,setStatus] = useState<Status>()
  const [error,setError] = useState('')
  const [busy,setBusy] = useState(false)
  const [name,setName] = useState('')
  const [address,setAddress] = useState('')
  const load = async () => {
    try { const [c,s] = await Promise.all([rpc<Config>('controller.udp.config.get'),rpc<Status>('controller.udp.status')]); setConfig({...c,listen:c.listen || '127.0.0.1:8787'}); setStatus(s); setError('') }
    catch(e) { setError(String(e)) }
  }
  useEffect(() => { void load() }, [])
  if (!config) return error ? <p role="alert">{error}</p> : null
  const update = (values: Partial<Config>) => setConfig({...config,...values})
  const save = async () => {
    setBusy(true)
    try { await rpc('controller.udp.config.set',config); setError(''); setStatus(undefined) }
    catch(e) { setError(String(e)) }
    finally { setBusy(false) }
  }
  return <section className="advanced-fields" aria-label={text('Binary UDP','UDP باینری')}>
    <h3>{text('Binary UDP','UDP باینری')}</h3>
    <Toggle checked={config.enabled} onChange={(enabled)=>update({enabled})} label={text('Enable transport','فعال‌سازی ارتباط')} />
    <TextField label={text('Listening address','نشانی گوش دادن')} value={config.listen} dir="ltr" onChange={(e)=>update({listen:e.target.value})} />
    <Toggle checked={config.discovery} onChange={(discovery)=>update({discovery,broadcast_events:discovery?config.broadcast_events:false})} label={text('Discover and announce peers','کشف و معرفی همتاها')} />
    {config.discovery && <Toggle checked={config.broadcast_events} onChange={(broadcast_events)=>update({broadcast_events})} label={text('Broadcast events','پخش رویدادها')} />}
    {Object.entries(config.peers ?? {}).map(([key,value])=><div className="advanced-fields advanced-fields--split" key={key}>
      <span>{key} · <bdi>{value}</bdi></span><Button icon={Trash2} onClick={()=>{const peers={...config.peers};delete peers[key];update({peers})}}>{text('Remove','حذف')}</Button>
    </div>)}
    <div className="advanced-fields advanced-fields--split">
      <TextField label={text('Peer name','نام همتا')} value={name} onChange={(e)=>setName(e.target.value)} />
      <TextField label={text('Peer address','نشانی همتا')} value={address} dir="ltr" onChange={(e)=>setAddress(e.target.value)} />
    </div>
    <Button disabled={!name.trim()||!address.trim()} onClick={()=>{update({peers:{...config.peers,[name.trim()]:address.trim()}});setName('');setAddress('')}}>{text('Add peer','افزودن همتا')}</Button>
    <div><Button icon={Save} busy={busy} onClick={()=>void save()}>{text('Save UDP settings','ذخیره تنظیمات UDP')}</Button><Button icon={RefreshCw} onClick={()=>void load()}>{text('Refresh','تازه‌سازی')}</Button></div>
    {(error||status?.error) && <p role="alert">{error||status?.error}</p>}
    {status && <span>{status.running ? text('Listening','در حال گوش دادن') : text('Stopped','متوقف')}</span>}
    {status?.peers?.map((peer)=><div key={peer.id}>{peer.identity?.app} · <bdi>{peer.address}</bdi>
      <Button disabled={Object.values(config.peers??{}).includes(peer.address)} onClick={()=>{setName(peer.identity?.app??'peer');setAddress(peer.address)}}>{text('Configure peer','پیکربندی همتا')}</Button>
    </div>)}
  </section>
}

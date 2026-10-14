import { useEffect, useMemo, useRef, useState } from 'react'
import './rf-manager.css'
import { Plus, Radio, RefreshCw, Save, Settings2, Square, Trash2 } from 'lucide-react'
import { rpc } from './api'
import { Button, Card, RangeField, StatusBadge, Toggle } from './components'
import { rfKey, rfPresent, RfReceptionSession } from './rf-reception'
import type { RfLearnPreferences, RfReception, RfSignal } from './rf-reception'
import type { ControllerEvent, DialogState, Locale, Snapshot } from './types'

type Entry = RfSignal & { id?:number; name?:string; pulse_us?:number; code_display?:string; action_kind?:number; action_value?:number; behavior?:number; board_assignment?:{label:string;action:string;target?:string;behavior?:string} }
type Mapping = { action:string;targets:string[];behaviors:string[];behavior_labels?:Record<string,string> }
type Catalog = { entries:Entry[];entries_sampled?:boolean;receptions:RfReception[];preferences:RfLearnPreferences;host_instance_id:string;connection_generation:number;learning:{active:boolean;remaining_ms:number;mode:string};bindings:{name:string;match:{rf_code:number;rf_bits:number;rf_protocol:number}}[];board_mapping_options:Mapping[];peripherals:{controls:{key:string;name:string}[]} }

export function RFManager({ snapshot,events,locale,openDialog }: {snapshot:Snapshot;events:ControllerEvent[];locale:Locale;openDialog:(dialog:DialogState)=>void}) {
 const copy=(en:string,fa:string)=>locale==='fa'?fa:en
 const [catalog,setCatalog]=useState<Catalog>()
 const [receipts,setReceipts]=useState<RfReception[]>([])
 const [settings,setSettings]=useState(false)
 const [preferences,setPreferences]=useState<RfLearnPreferences>()
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[now,setNow]=useState(Date.now())
 const [mapping,setMapping]=useState<{id:number;action:string;target?:string;behavior?:string}>()
 const session=useRef(new RfReceptionSession()),initialized=useRef(false),lastEvent=useRef(0)
 const selectedOwner=useRef(''),revision=useRef(0)
 const load=async(readBoard=true)=>{
  const token=++revision.current
  try {const value=await rpc<Catalog>('controller.rf.catalog',{read_board:readBoard});if(token!==revision.current)return
   const owner=`${value.host_instance_id}:${value.connection_generation}`
   const replaced=!initialized.current||selectedOwner.current!==owner
   if(replaced){session.current.open(value.receptions);initialized.current=true;selectedOwner.current=owner}
   setCatalog(old=>value.entries_sampled?value:{...value,entries:replaced?[]:old?.entries??[]});setPreferences(old=>replaced||!old||JSON.stringify(old)===JSON.stringify(catalog?.preferences)?value.preferences:old)
   setReceipts(old=>value.receptions.map(r=>{const prior=replaced?undefined:old.find(p=>rfKey(p)===rfKey(r));return prior&&prior.connection_generation===r.connection_generation&&prior.received_count>r.received_count?prior:r}))
   setError('')
  }catch(cause){setError(String(cause))}
 }
 useEffect(()=>{initialized.current=false;lastEvent.current=0;void load();return()=>{revision.current++}},[snapshot.connection_generation])
 useEffect(()=>{
  const fresh=events.filter(event=>event.id>lastEvent.current);if(!fresh.length)return
  lastEvent.current=Math.max(...fresh.map(event=>event.id))
  const incoming=fresh.filter(e=>e.kind==='rf.receive'&&e.source==='board'&&e.rf_code)
  if(incoming.length){setReceipts(old=>{const byKey=new Map(old.map(r=>[rfKey(r),r]));for(const e of incoming){const receipt:RfReception={code:e.rf_code!,bits:e.rf_bits!,protocol:e.rf_protocol!,pulse_us:e.rf_pulse_us,received_count:Number(e.metadata?.received_count),last_seen:e.time,connection_generation:Number(e.metadata?.connection_generation)};if(!Number.isFinite(receipt.received_count))continue;const previous=byKey.get(rfKey(receipt));if(!previous||receipt.received_count>previous.received_count)byKey.set(rfKey(receipt),receipt)};return [...byKey.values()].sort((a,b)=>Date.parse(b.last_seen)-Date.parse(a.last_seen)).slice(0,256)});setNow(Date.now())}
  if(fresh.some(e=>e.kind.startsWith('rf.learn.')||e.kind==='rf.preferences.changed'))void load(fresh.some(e=>e.kind==='rf.learn.capture'||e.kind==='rf.learn.mapping-required'||e.kind==='rf.learn.ended'||e.kind==='rf.learn.cancelled'))
 },[events])
 useEffect(()=>{const expiries=receipts.map(r=>Date.parse(r.last_seen)+250).filter(t=>t>now);if(!expiries.length)return;const timer=setTimeout(()=>setNow(Date.now()),Math.max(1,Math.min(...expiries)-Date.now()));return()=>clearTimeout(timer)},[receipts,now])
 const command=async(method:string,params:Record<string,unknown>={})=>{if(busy)return;setBusy(true);try{await rpc(`controller.rf.${method}`,params);await load();}catch(cause){setError(String(cause))}finally{setBusy(false)}}
 const rows=useMemo(()=>{const map=new Map<string,Entry>();for(const e of catalog?.entries??[])map.set(rfKey(e),e);for(const r of receipts)if(!map.has(rfKey(r)))map.set(rfKey(r),r);for(const b of catalog?.bindings??[]){const s={code:b.match.rf_code,bits:b.match.rf_bits,protocol:b.match.rf_protocol};if(!map.has(rfKey(s)))map.set(rfKey(s),s)}return [...map.values()]},[catalog,receipts])
 const active=snapshot.rf_learning?.active??catalog?.learning?.active
 const option=catalog?.board_mapping_options.find(o=>o.action===mapping?.action)
 return <Card icon={Radio} iconTone="violet" title={copy('RF controls','کنترل RF')} action={<Button tone="ghost" icon={Settings2} aria-label={copy('RF settings','تنظیمات RF')} onClick={()=>setSettings(!settings)} />}>
  {error&&<div role="alert" className="inline-error">{error}</div>}
  <div className="rf-manager__toolbar">
   {active?<><StatusBadge tone="info">{copy('Learning','یادگیری')}</StatusBadge><Button icon={Square} busy={busy} onClick={()=>void command('learn.cancel')}>{copy('Stop learning','توقف یادگیری')}</Button></>:<Button icon={Plus} busy={busy} disabled={!snapshot.connected} onClick={()=>void command('learn.start')}>{copy('Learn buttons','یادگیری دکمه‌ها')}</Button>}
   <Button tone="ghost" icon={RefreshCw} aria-label={copy('Refresh learned buttons','تازه‌سازی دکمه‌ها')} disabled={busy||!snapshot.connected} onClick={()=>void load()} />
  </div>
  {settings&&preferences&&<div className="rf-manager__settings">
   <Toggle label={copy('Timed learning','یادگیری زمان‌دار')} checked={preferences.timed} disabled={busy} onChange={timed=>setPreferences({...preferences,timed})}/>
   <RangeField label={copy('Timeout','مهلت')} min={1} max={120} step={1} unit="s" value={preferences.timeout_seconds} disabled={busy} onChange={timeout_seconds=>setPreferences({...preferences,timeout_seconds})}/>
   <Toggle label={copy('Continuously learn buttons','یادگیری پیوستهٔ دکمه‌ها')} checked={preferences.continuous} disabled={busy} onChange={continuous=>setPreferences({...preferences,continuous})}/>
   <Button icon={Save} busy={busy} disabled={JSON.stringify(preferences)===JSON.stringify(catalog?.preferences)} onClick={()=>void command('preferences.set',preferences)}>{copy('Save settings','ذخیرهٔ تنظیمات')}</Button>
  </div>}
  <div className="rf-manager__list">{rows.map(entry=>{const receipt=receipts.find(r=>rfKey(r)===rfKey(entry)),on=rfPresent(receipt,snapshot.connected,now),learned=entry.id!==undefined
   return <div key={rfKey(entry)} className="rf-manager__row">
    <span className={`rf-receive-indicator${on?' is-on':''}`} role="img" aria-label={on?copy('Receiving','در حال دریافت'):copy('Not receiving','دریافت نمی‌شود')}/>
    <span className="rf-receive-count" title={copy('Receptions since this view opened','دریافت‌ها از باز شدن این نما')}>×{session.current.count(receipt)}</span>
    <div className={`rf-manager__identity${learned?'':' is-unlearned'}`}><strong>{entry.name||`0x${entry.code.toString(16).toUpperCase()}`}</strong><small>{entry.bits} bits · {copy('Protocol','پروتکل')} {entry.protocol}{entry.board_assignment&&` · ${entry.board_assignment.label}`}</small>{catalog?.bindings.filter(b=>b.match.rf_code===entry.code&&b.match.rf_bits===entry.bits&&b.match.rf_protocol===entry.protocol).map(b=><small key={b.name}>{b.name}</small>)}</div>
    <div className="rf-manager__actions">{learned?<><Button tone="ghost" icon={Settings2} disabled={busy} aria-label={copy('Edit board assignment','ویرایش نگاشت برد')} onClick={()=>setMapping({id:entry.id!,action:entry.board_assignment?.action??'none',target:entry.board_assignment?.target,behavior:entry.board_assignment?.behavior})}/><Button tone="ghost" icon={Trash2} disabled={busy} aria-label={copy('Remove learned button','حذف دکمهٔ آموخته')} onClick={()=>{openDialog({open:true,tone:'danger',title:copy('Remove learned RF button?','دکمهٔ آموخته حذف شود؟'),body:entry.code_display??`0x${entry.code.toString(16)}`,confirmLabel:copy('Remove','حذف'),action:()=>command('remove',{id:entry.id})})}}/></>:<Button icon={Plus} disabled={busy||!receipt||active} onClick={()=>void command('observed.add',{code:entry.code,bits:entry.bits,protocol:entry.protocol})}>{copy('Add','افزودن')}</Button>}</div>
   </div>})}</div>
  {mapping&&<div className="rf-manager__settings"><select aria-label={copy('Board action','عمل برد')} value={mapping.action} onChange={e=>setMapping({id:mapping.id,action:e.target.value})}>{catalog?.board_mapping_options.map(o=><option key={o.action} value={o.action}>{o.action}</option>)}</select>
   {!!option?.targets.length&&<select aria-label={copy('Target','هدف')} value={mapping.target??''} onChange={e=>setMapping({...mapping,target:e.target.value})}><option value="">—</option>{option.targets.map(target=><option key={target} value={target}>{catalog?.peripherals.controls.find(c=>c.key===`${mapping.action==='pwm'?'pwm':mapping.action==='side'?'seat':'relay'}.${target}`)?.name??target}</option>)}</select>}
   {!!option?.behaviors.length&&<select aria-label={copy('Behavior','رفتار')} value={mapping.behavior??''} onChange={e=>setMapping({...mapping,behavior:e.target.value})}><option value="">—</option>{option.behaviors.map(behavior=><option key={behavior} value={behavior}>{option.behavior_labels?.[behavior]??behavior}</option>)}</select>}
   <Button icon={Save} busy={busy} disabled={!!option?.targets.length&&!mapping.target||!!option?.behaviors.length&&!mapping.behavior} onClick={()=>void command('map',mapping).then(()=>setMapping(undefined))}>{copy('Save on board','ذخیره روی برد')}</Button><Button tone="ghost" onClick={()=>setMapping(undefined)}>{copy('Cancel','لغو')}</Button>
  </div>}
 </Card>
}

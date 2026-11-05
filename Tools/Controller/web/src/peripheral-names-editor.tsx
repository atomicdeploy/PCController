import { useEffect, useRef, useState } from 'react'
import { PanelTop, RotateCcw, ShieldCheck } from 'lucide-react'
import { rpc } from './api'
import { Button, Card, TextField } from './components'
import { normalizePeripheralNameInput, validatePeripheralName } from './settings-validation'
import { ChannelIcon } from './channel-icon'
import { channelNameDraft, changedChannelNames, receiveChannelNames, saveChannelNames } from './channel-name-draft'
import type { ChannelNameDraft } from './channel-name-draft'
import type { Locale, PeripheralSettings, PresentationUpdateResult } from './types'

const kindOrder = ['relay', 'seat', 'motion', 'pwm', 'display', 'sensor'] as const

export function PeripheralNamesEditor({ locale, refreshToken, scope }: { locale:Locale; refreshToken:string; scope:string }) {
  const copy = (english:string,persian:string) => locale === 'fa' ? persian : english
  const [model,setModel] = useState<ChannelNameDraft|null>(null)
  const current = useRef<ChannelNameDraft|null>(null)
  const [loading,setLoading] = useState(true)
  const [saving,setSaving] = useState(false)
  const inSave = useRef(false)
  const queuedRefresh = useRef(false)
  const mounted = useRef(true)
  const readGeneration = useRef(0)
  const currentScope = useRef(scope)
  currentScope.current = scope
  const [notice,setNotice] = useState('')
  const [error,setError] = useState(false)
  const apply = (value:ChannelNameDraft) => { current.current=value; setModel(value) }

  const load = async (discardDraft=false) => {
    if (inSave.current) { queuedRefresh.current=true; return }
    const generation=++readGeneration.current
    const owner=currentScope.current
    setLoading(true)
    try {
      const catalog=await rpc<PeripheralSettings>('controller.peripherals.get')
      if (!mounted.current || generation!==readGeneration.current || owner!==currentScope.current) return
      apply(discardDraft ? channelNameDraft(catalog) : receiveChannelNames(current.current,catalog))
      if (discardDraft) { setNotice(''); setError(false) }
    } catch (cause) {
      if (mounted.current && generation===readGeneration.current) {
        setNotice(cause instanceof Error ? cause.message : String(cause)); setError(true)
      }
    } finally {
      if (mounted.current && generation===readGeneration.current) setLoading(false)
    }
  }
  useEffect(() => {
    mounted.current=true
    return () => { mounted.current=false; readGeneration.current++ }
  },[])
  useEffect(() => {
    // A host change must never let an old draft write into the replacement.
    if (current.current && changedChannelNames(current.current).length && scope!==lastScope.current) apply({...current.current,conflict:true})
    lastScope.current=scope
    void load()
    return () => { readGeneration.current++ }
  },[refreshToken,scope])
  const lastScope = useRef(scope)

  const catalog=model?.catalog.peripherals ?? []
  const dirty=Boolean(model && changedChannelNames(model).length)
  const busy=saving || !model && loading
  const editable=Boolean(model?.catalog.board_profile.attached && model.catalog.board_profile.configured)
  const groups=kindOrder.map(kind=>({kind,entries:catalog.filter(item=>item.kind===kind)})).filter(group=>group.entries.length)
  const groupLabel=(kind:string)=>({
    relay:copy('Relays','رله‌ها'),seat:copy('Seats','صندلی‌ها'),motion:copy('Motion sides','سمت‌های حرکت'),
    pwm:copy('PWM and lighting','PWM و نورپردازی'),display:copy('Displays','نمایشگرها'),sensor:copy('Sensors','حسگرها'),
  } as Record<string,string>)[kind] ?? kind

  const update=(key:string,value:string)=>{
    if (!current.current || busy) return
    apply({...current.current,values:{...current.current.values,[key]:normalizePeripheralNameInput(value)}})
    setNotice(''); setError(false)
  }
  const save=async()=>{
    if (!current.current || inSave.current || !dirty) return
    inSave.current=true; setSaving(true); readGeneration.current++
    setNotice(''); setError(false)
    const owner=currentScope.current
    try {
      await saveChannelNames(current.current,async(key,name,revision)=>{
        if (!mounted.current || owner!==currentScope.current) throw new Error(copy('Connection changed; remaining names were not saved.','اتصال تغییر کرد؛ نام‌های باقی‌مانده ذخیره نشدند.'))
        const result=await rpc<PresentationUpdateResult>('controller.peripheral.presentation.update',{key,name,expected_revision:revision})
        if (!mounted.current || owner!==currentScope.current) throw new Error(copy('Connection changed; reload to confirm the last save.','اتصال تغییر کرد؛ برای بررسی آخرین ذخیره دوباره بارگذاری کنید.'))
        return result
      },apply)
      setNotice(copy('Names saved.','نام‌ها ذخیره شدند.'))
    } catch(cause) {
      if (mounted.current) { setNotice(cause instanceof Error ? cause.message : String(cause)); setError(true) }
    } finally {
      inSave.current=false
      if (mounted.current) {
        setSaving(false)
        if (queuedRefresh.current) { queuedRefresh.current=false; void load() }
      }
    }
  }

  return <Card icon={PanelTop} iconTone="accent" title={copy('Channel names','نام کانال‌ها')}
    eyebrow={dirty ? copy('Unsaved changes','تغییرات ذخیره‌نشده') : undefined}
    className="settings-card settings-card--wide peripheral-names-card"
    menu={catalog.length && !busy ? [{
      label:copy('Restore every default name','بازگردانی همهٔ نام‌های پیش‌فرض'),icon:RotateCcw,
      onSelect:()=>{if(current.current)apply({...current.current,values:Object.fromEntries(catalog.map(item=>[item.key,item.default_name]))})},
    }] : undefined}>
    <div className="peripheral-name-groups" aria-busy={busy}>
      {groups.map((group,index)=><details key={group.kind} className="peripheral-name-group" open={index===0}>
        <summary><span>{groupLabel(group.kind)}</span><small>{group.entries.length}</small></summary>
        <div className="peripheral-name-grid">
          {group.entries.map(descriptor=>{
            const value=model?.values[descriptor.key] ?? ''
            const validation=validatePeripheralName(value)
            return <TextField key={descriptor.key}
              label={<><ChannelIcon icon={descriptor.icon}/>{descriptor.default_name}</>}
              value={value} placeholder={descriptor.default_name} maxLength={64} disabled={busy || !editable}
              onChange={event=>update(descriptor.key,event.currentTarget.value)}
              error={!validation.valid ? validation.error : undefined}
              action={<Button compact disabled={busy || !editable || value===descriptor.default_name} icon={RotateCcw}
                onClick={()=>update(descriptor.key,descriptor.default_name)}>{copy('Default','پیش‌فرض')}</Button>}/>
          })}
        </div>
      </details>)}
    </div>
    <footer className="peripheral-names-card__footer">
      <span className={error || model?.conflict ? 'text-warn' : ''} role="status" aria-live="polite">
        {model?.conflict ? copy('Names changed elsewhere. Your draft is preserved; reload before saving.','نام‌ها در جای دیگری تغییر کرده‌اند. پیش‌نویس محفوظ است؛ پیش از ذخیره دوباره بارگذاری کنید.') : notice}
      </span>
      <Button compact icon={RotateCcw} disabled={busy} onClick={()=>void load(true)}>{dirty ? copy('Discard draft and reload','لغو پیش‌نویس و بارگذاری مجدد') : copy('Refresh','تازه‌سازی')}</Button>
      <Button tone="primary" icon={ShieldCheck} busy={saving} disabled={busy || !dirty || !editable || model?.conflict} onClick={()=>void save()}>{copy('Save names','ذخیرهٔ نام‌ها')}</Button>
    </footer>
  </Card>
}

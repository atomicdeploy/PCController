import { describe, expect, it } from 'vitest'
import { acknowledgeChannelName, channelNameDraft, changedChannelNames, receiveChannelNames, saveChannelNames } from './channel-name-draft'
import type { ChannelNameDraft } from './channel-name-draft'
import type { PeripheralSettings, PresentationUpdateResult } from './types'

const catalog = ():PeripheralSettings => ({
  board_profile:{board_identity:'board-a',revision:'one',configured:true,attached:true},
  peripheral_names:{'seat.a':'Obsolete UI name'},
  peripherals:[
    {key:'seat.a',kind:'seat',role:'cinema-seat-motion',index:1,default_name:'Seat A',name:'Left seats',icon:'seat',control:'seat'},
    {key:'pwm.0',kind:'pwm',role:'user-output',index:0,default_name:'MOSFET 1',name:'Window',icon:'fan',control:'pwm-user'},
  ],controls:[],
})
const ack=(key:string,name:string,revision:string):PresentationUpdateResult=>({
  board_profile:{...catalog().board_profile,revision},
  peripheral:{...catalog().peripherals.find(item=>item.key===key)!,name},controls:[],
})

describe('profile-owned channel name editor',()=>{
  it('uses resolved names and includes cinema seats, not the old names map',()=>{
    const state=channelNameDraft(catalog())
    expect(state.values['seat.a']).toBe('Left seats')
    expect(changedChannelNames(state)).toEqual([])
  })
  it('adopts event refresh while clean and preserves a conflicting dirty draft',()=>{
    const current=channelNameDraft(catalog())
    const next=catalog(); next.board_profile.revision='two'; next.peripherals[0].name='Remote name'
    expect(receiveChannelNames(current,next).values['seat.a']).toBe('Remote name')
    current.values['seat.a']='My draft'
    const pending=receiveChannelNames(current,next)
    expect(pending.values['seat.a']).toBe('My draft')
    expect(pending.catalog.board_profile.revision).toBe('one')
    expect(pending.conflict).toBe(true)
  })
  it('chains revision-fenced ACKs and never rewrites unchanged rows',async()=>{
    let state=channelNameDraft(catalog()); state.values={'seat.a':'Lift','pwm.0':'Lamp'}
    const revisions:string[]=[]
    await saveChannelNames(state,async(key,name,revision)=>{revisions.push(revision);return ack(key,name,revision==='one'?'two':'three')},value=>state=value)
    expect(revisions).toEqual(['one','two'])
    expect(changedChannelNames(state)).toEqual([])
    await saveChannelNames(state,async()=>{throw new Error('unchanged row was written')},()=>{})
  })
  it('keeps unsaved remainder after a partial failure and does not retry',async()=>{
    let state=channelNameDraft(catalog()); state.values={'seat.a':'Lift','pwm.0':'Lamp'}
    let calls=0
    await expect(saveChannelNames(state,async(key,name)=>{
      if(++calls===2)throw new Error('revision conflict')
      return ack(key,name,'two')
    },value=>state=value)).rejects.toThrow('revision conflict')
    expect(calls).toBe(2)
    expect(state.saved['seat.a']).toBe('Lift')
    expect(state.saved['pwm.0']).toBe('Window')
    expect(changedChannelNames(state)).toEqual([['pwm.0','Lamp']])
  })
  it('blocks wrong-board ACKs and conflicts without losing drafts',async()=>{
    const state=channelNameDraft(catalog());state.values['seat.a']='Lift'
    const wrong=ack('seat.a','Lift','two');wrong.board_profile.board_identity='board-b'
    expect(()=>acknowledgeChannelName(state,'seat.a',wrong)).toThrow('does not match')
    let calls=0
    await expect(saveChannelNames({...state,conflict:true},async()=>{calls++;return wrong},()=>{})).rejects.toThrow('Reload')
    expect(calls).toBe(0)
    expect(state.values['seat.a']).toBe('Lift')
  })
  it('restores the actual default even when the old UI map contains a name',()=>{
    const state:ChannelNameDraft=channelNameDraft(catalog());state.values['seat.a']=''
    expect(changedChannelNames(state)).toEqual([['seat.a','Seat A']])
  })
})

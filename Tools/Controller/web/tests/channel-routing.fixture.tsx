// Isolated UI acceptance fixture; not imported by the production application.
// Every RPC is intercepted here. No hardware or running host is contacted.
import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ChannelRoutingEditor } from '../src/channel-routing-editor'
import type { ChannelRoutingCatalog } from '../src/channel-routing'
import '../src/styles.css'

const params = new URLSearchParams(location.search)
const fa = params.get('lang') === 'fa'
document.documentElement.lang = fa ? 'fa' : 'en'
document.documentElement.dir = fa ? 'rtl' : 'ltr'
document.documentElement.dataset.theme = params.get('theme') ?? 'dark'
let catalog: ChannelRoutingCatalog = {
  host_instance_id:'fixture:host',connection_generation:1,
  board_profile:{board_identity:'fixture:board', revision:'initial', configured:true, attached:false},
  routing:{enabled:true,channels:[
    {key:'lighting.aisle',kind:'relay',target:'relay.5',name:fa?'نور راهرو':'Aisle lighting',icon:'lamp'},
    {key:'lighting.screen',kind:'relay',target:'relay.6',name:fa?'نور پرده':'Screen lighting',icon:'stage-light'},
    {key:'lighting.ambient',kind:'pwm',target:'pwm.0',name:fa?'نور محیط':'Ambient lighting',icon:'lightbulb'},
  ]},
  targets:[5,6,7,8].map(n=>({key:`relay.${n}`,kind:'relay',role:'user-output',index:n,default_name:`Relay ${n}`,name:`Relay ${n}`,control:'relay'})),
}
catalog.targets.push(...[0,1,2].map(n=>({key:`pwm.${n}`,kind:'pwm' as const,role:'user-output',index:n,default_name:`PWM ${n}`,name:`PWM ${n}`,control:'pwm-user' as const})))
let writes = 0
window.fetch = async (_input, init) => {
  const request = JSON.parse(String(init?.body))
  if (request.method === 'controller.channel_routing.get') return Response.json({jsonrpc:'2.0',id:request.id,result:catalog})
  if (request.method === 'controller.channel_routing.update') {
    if (request.params.expected_revision !== catalog.board_profile.revision) return Response.json({jsonrpc:'2.0',id:request.id,error:{code:-32000,message:'revision conflict'}})
    writes++
    catalog = {...catalog,board_profile:{...catalog.board_profile,revision:`saved-${writes}`},routing:request.params.routing}
    return Response.json({jsonrpc:'2.0',id:request.id,result:catalog})
  }
  throw new Error(`Unexpected fixture RPC ${request.method}`)
}
let refresh = () => {}
function Fixture() {
  const [revision,setRevision] = useState(0)
  refresh = () => setRevision(value=>value+1)
  return <main style={{padding:16,maxWidth:1100,margin:'0 auto',minWidth:0}}>
    <p style={{color:'var(--text-muted)',fontSize:12}}>UI test fixture · no hardware connection</p>
    <ChannelRoutingEditor locale={fa?'fa':'en'} refreshToken={String(revision)} scope="fixture:host"/>
  </main>
}
createRoot(document.getElementById('root')!).render(<Fixture/> )
const wait = (ms=100) => new Promise(resolve=>setTimeout(resolve,ms))
async function run() {
  for(let attempt=0;attempt<40 && !document.querySelector('.channel-routing-row select');attempt++) await wait()
  const select = document.querySelector<HTMLSelectElement>('.channel-routing-row select')
  if (!select) throw new Error('Routing rows did not render')
  select.value='relay.6'; select.dispatchEvent(new Event('change',{bubbles:true})); await wait()
  const choices=[...document.querySelectorAll<HTMLSelectElement>('.channel-routing-row select')].map(item=>item.value)
  if(choices[0]!=='relay.6'||choices[1]!=='relay.5') throw new Error('Occupied target did not atomically swap both rows')
  const save=[...document.querySelectorAll<HTMLButtonElement>('button')].find(button=>button.textContent?.includes(fa?'ذخیرهٔ اتصال‌ها':'Save bindings'))
  if(!save||save.disabled) throw new Error('Changed binding cannot be saved')
  save.click();await wait(400)
  if(writes!==1||catalog.routing.channels[0].key!=='lighting.aisle'||catalog.routing.channels[0].target!=='relay.6'||catalog.routing.channels[1].target!=='relay.5') throw new Error('Save lost stable key or atomic swap')
  if(!save.disabled) throw new Error('Unchanged post-ACK draft stayed dirty')
  // A new remote revision must not erase an active local binding draft.
  select.value='relay.7';select.dispatchEvent(new Event('change',{bubbles:true}));await wait()
  catalog={...catalog,board_profile:{...catalog.board_profile,revision:'external'}}
  refresh();await wait(400)
  if(select.value!=='relay.7'||!save.disabled) throw new Error('Incoming event overwrote draft or allowed conflicting save')
  const discard=[...document.querySelectorAll<HTMLButtonElement>('button')].find(button=>button.textContent?.includes(fa?'لغو پیش‌نویس':'Discard draft'))
  discard?.click();await wait(300)
  const card=document.querySelector('.channel-routing-card')!.getBoundingClientRect()
  const overflow=[...document.querySelectorAll<HTMLElement>('.channel-routing-row input,.channel-routing-row select,.channel-routing-row button,.channel-routing-new input,.channel-routing-new select,.channel-routing-new button')].filter(element=>{
    const rect=element.getBoundingClientRect(); return rect.left<card.left-1||rect.right>card.right+1
  })
  if(overflow.length||document.documentElement.scrollWidth>innerWidth) throw new Error(`Horizontal overflow: ${overflow.length}`)
  document.body.dataset.acceptance='passed'
}
void run().catch(error=>{document.body.dataset.acceptance='failed';const report=document.createElement('pre');report.id='fixture-error';report.textContent=String(error);document.body.append(report)})

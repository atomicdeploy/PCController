// Headless browser acceptance for the isolated fixture. Does not attach to a
// user's browser profile, invoke the real controller API, or actuate hardware.
import { spawn } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const [browser, output, baseURL = 'http://127.0.0.1:4179'] = process.argv.slice(2)
if (!browser || !output) throw new Error('Usage: node verify-channel-routing.mjs <browser.exe> <evidence-directory> [fixture-origin]')
const origin = new URL(baseURL)
if (!['127.0.0.1','localhost'].includes(origin.hostname)) throw new Error('UI fixture must be loopback-only')
mkdirSync(output,{recursive:true})
const child = spawn(browser,[
  '--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--hide-scrollbars',
  `--user-data-dir=${resolve(output,'browser-fixture-profile')}`,'--remote-debugging-port=0','about:blank',
],{windowsHide:true,stdio:['ignore','ignore','pipe']})
const endpoint = await new Promise((resolveEndpoint,reject)=>{
  let log=''
  const timer=setTimeout(()=>reject(new Error('Browser debugging endpoint did not become ready')),15000)
  child.once('error',reject)
  child.stderr.on('data',chunk=>{
    log+=chunk.toString()
    const match=log.match(/DevTools listening on (ws:\/\/\S+)/)
    if(match){clearTimeout(timer);resolveEndpoint(match[1])}
  })
})
const socket=new WebSocket(endpoint)
await new Promise((resolveOpen,reject)=>{socket.addEventListener('open',resolveOpen,{once:true});socket.addEventListener('error',reject,{once:true})})
let id=0
const pending=new Map()
socket.addEventListener('message',event=>{
  const message=JSON.parse(event.data)
  const call=pending.get(message.id)
  if(call){pending.delete(message.id);clearTimeout(call.timer);message.error?call.reject(new Error(JSON.stringify(message.error))):call.resolve(message.result)}
})
function command(method,params={},sessionId){
  return new Promise((resolveCall,reject)=>{
    const next=++id
    const timer=setTimeout(()=>{pending.delete(next);reject(new Error(`${method} timed out`))},10000)
    pending.set(next,{resolve:resolveCall,reject,timer})
    socket.send(JSON.stringify({id:next,method,params,sessionId}))
  })
}
try {
for (const [width,theme,lang] of [[390,'dark','en'],[768,'light','en'],[1280,'dark','en'],[390,'light','fa']]) {
  const name=`channel-routing-${width}-${theme}-${lang}`
  const {targetId}=await command('Target.createTarget',{url:'about:blank'})
  const {sessionId}=await command('Target.attachToTarget',{targetId,flatten:true})
  await command('Emulation.setDeviceMetricsOverride',{width,height:1200,deviceScaleFactor:1,mobile:false},sessionId)
  await command('Page.navigate',{url:`${origin.origin}/tests/channel-routing.html?theme=${theme}&lang=${lang}`},sessionId)
  let state
  for(let attempt=0;attempt<120;attempt++){
    const result=await command('Runtime.evaluate',{expression:'JSON.stringify({status:document.body?.dataset.acceptance,error:document.querySelector("#fixture-error")?.textContent,width:innerWidth,scroll:document.documentElement.scrollWidth,height:document.documentElement.scrollHeight})',returnByValue:true},sessionId)
    state=JSON.parse(result.result.value)
    if(state.status)break
    await new Promise(resolveWait=>setTimeout(resolveWait,100))
  }
  const screenshot=await command('Page.captureScreenshot',{format:'png',captureBeyondViewport:true,clip:{x:0,y:0,width,height:Math.min(state.height,2400),scale:1}},sessionId)
  writeFileSync(resolve(output,`${name}.png`),Buffer.from(screenshot.data,'base64'))
  await command('Target.closeTarget',{targetId})
  if(state.status!=='passed'||state.width!==width||state.scroll>width)throw new Error(`${name}: ${JSON.stringify(state)}`)
  console.log(`${name}: exact ${state.width}px viewport, swap, save ACK, conflict/draft preservation, reload and width checks passed`)
}
} finally {
  await command('Browser.close').catch(()=>{})
  socket.close()
}

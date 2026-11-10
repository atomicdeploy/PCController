import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createConnection, createServer } from 'node:net'
const binary=process.argv[2]
assert(binary)
const u16=n=>{const b=Buffer.alloc(2);b.writeUInt16LE(n);return b}
const u32=n=>{const b=Buffer.alloc(4);b.writeUInt32LE(n);return b}
const str=s=>Buffer.concat([u16(Buffer.byteLength(s)+1),Buffer.from(s+'\0')])
const packet=(id,body=Buffer.alloc(0))=>Buffer.concat([Buffer.from('ORGB'),u32(0),u32(id),u32(body.length),body])
const parts=[u32(4),u32(4),str('Fixture ARGB'),str('Fixture'),str('Test only'),str('1'),str('serial'),str('loopback'),u16(1),u32(0),str('Direct'),Buffer.alloc(36),u16(0),u16(1),str('Zone'),u32(1),u32(2),u32(2),u32(2),u16(0),u16(2),str('LED 1'),u32(0),str('LED 2'),u32(1),u16(2),Buffer.alloc(8)]
const data=Buffer.concat(parts);data.writeUInt32LE(data.length)
let sdk,connections=0,direct=false,frames=[]
const server=createServer(socket=>{
  sdk=socket;connections++;let pending=Buffer.alloc(0)
  socket.on('error',()=>{})
  socket.on('data',chunk=>{
    pending=Buffer.concat([pending,chunk])
    while(pending.length>=16){
      assert.equal(pending.toString('ascii',0,4),'ORGB')
      const id=pending.readUInt32LE(8),size=pending.readUInt32LE(12)
      if(pending.length<16+size)break
      const body=pending.subarray(16,16+size);pending=pending.subarray(16+size)
      let reply
      if(id===40){assert.equal(body.readUInt32LE(),2);reply=packet(40,u32(6))}
      else if(id===0)reply=packet(0,u32(1))
      else if(id===1){assert.equal(body.readUInt32LE(),2);reply=packet(1,data)}
      else if(id===1100)direct=true
      else if(id===1050){assert(direct);assert.equal(body.readUInt32LE(),body.length);assert.equal(body.readUInt16LE(4),2);frames.push(Buffer.from(body))}
      if(reply){socket.write(reply.subarray(0,7));setTimeout(()=>socket.write(reply.subarray(7)),5)}
    }
  })
})
await new Promise(ok=>server.listen(0,'127.0.0.1',ok))
const portProbe=createServer();await new Promise(ok=>portProbe.listen(0,'127.0.0.1',ok));const boardPort=portProbe.address().port;await new Promise(ok=>portProbe.close(ok))
const child=spawn(binary,['--memory','--port',String(boardPort),'--speaker','off','--i2c','off','--no-stdin','--strip-openrgb',`0@127.0.0.1:${server.address().port}`],{windowsHide:true,stdio:['ignore','pipe','pipe']})
child.stdout.resume();let diagnostics='';child.stderr.on('data',b=>diagnostics+=b)
let board
async function until(check){const deadline=Date.now()+5000;while(!check()){if(Date.now()>deadline)throw Error('fixture timeout: '+diagnostics);await new Promise(ok=>setTimeout(ok,10))}}
function wire(opcode,sequence,payload){const raw=[165,1,opcode,sequence,payload.length,...payload];let crc=0;for(const b of raw){crc^=b;for(let i=0;i<8;i++)crc=(crc&128?(crc<<1)^7:crc<<1)&255}raw.push(crc);const out=[0];let at=0,code=1;for(const b of raw){if(b===0){out[at]=code;at=out.length;out.push(0);code=1}else{out.push(b);code++}}out[at]=code;return Buffer.from([...out,0])}
try{
  await until(()=>frames.length>0)
  board=createConnection({host:'127.0.0.1',port:boardPort});await new Promise((ok,fail)=>{board.once('connect',ok);board.once('error',fail)});board.on('data',()=>{})
  board.write(wire(22,1,[255,255,0,0,128]))
  const red=process.argv[3]==='0'?255:128
  await until(()=>frames.some(f=>f[6]===red && f[7]===0 && f[8]===0))
  sdk.write(packet(100));await until(()=>diagnostics.includes('device list changed'))
  const count=frames.length
  board.write(wire(22,2,[255,0,255,0,255]));await new Promise(ok=>setTimeout(ok,100))
  assert.equal(frames.length,count,'changed topology must disarm, not write a new index occupant')
  assert.equal(connections,1,'no automatic reconnect/replay')
  console.log('OpenRGB negotiated metadata, fragmented framing, rendered RGB/brightness, LED count and topology disarm passed (fake SDK, no physical devices)')
}finally{board?.destroy();sdk?.destroy();child.kill('SIGTERM');await new Promise(ok=>server.close(ok))}

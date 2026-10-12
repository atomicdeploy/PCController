// Shared-firmware development session. Never reads production configuration,
// discovers serial ports, flashes a device, or replaces a running coordinator.
import { spawn } from 'node:child_process'
import { createConnection, createServer } from 'node:net'
import { mkdir, open, access } from 'node:fs/promises'
import { constants } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createInterface } from 'node:readline'
import { loadProjectEnv } from '../Build/env.mjs'

loadProjectEnv()

const here = dirname(fileURLToPath(import.meta.url))
const suffix = process.platform === 'win32' ? '.exe' : ''
const options = { board: join(here, '.build/release/bin/virtual_board' + suffix), controller: join(here, '../Controller/bin/controller' + suffix),
  'board-port': '18765', 'host-port': '18987', 'state-root': process.platform === 'win32'
    ? join(process.env.LOCALAPPDATA, 'Programs/PCController/development/virtual-board')
    : join(homedir(), '.local/share/pccontroller/virtual-development'), 'sensor-temp0':'', 'sensor-temp1':'', 'sensor-voltage':'', 'sensor-current':'' }
for (let i=2; i<process.argv.length; i++) {
  const key = process.argv[i].replace(/^--/, '')
  if (!(key in options) || !process.argv[i+1]) throw new Error('Usage: node dev-host.mjs [--board FILE] [--controller FILE] [--board-port N] [--host-port N] [--state-root DIR]')
  options[key] = process.argv[++i]
}
const boardPort = Number(options['board-port']), hostPort = Number(options['host-port'])
for (const port of [boardPort, hostPort]) if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error('development ports must be 1024..65535')
if (boardPort === hostPort) throw new Error('board and coordinator ports must differ')
await access(options.board); await access(options.controller)
for (const port of [boardPort, hostPort]) {
  const listener = createServer()
  await new Promise((ok, fail) => { listener.once('error', fail); listener.listen(port, '127.0.0.1', ok) })
  await new Promise(ok => listener.close(ok))
}
const state = resolve(options['state-root']), configPath = join(state, 'config.json')
await mkdir(state, { recursive: true })
const marker = join(state, 'virtual-development.json')
const seed = JSON.stringify({ schema: 2, connection: { port: `tcp://127.0.0.1:${boardPort}`, reset_on_reconnect: false },
  ipc: { listen: `127.0.0.1:${hostPort}`, allow_remote: false }, ui: { history_hours: 0 },
  integrations: { discovery: { mdns_enabled:false, dns_sd_enabled:false, ssdp_enabled:false, upnp_enabled:false, ws_discovery_enabled:false, broadcast_enabled:false, netbios_enabled:false },
    buzzer_mirror: { enabled:false }, keyboard_control: { enabled:false } } }, null, 2) + '\n'
const readFlags = constants.O_RDONLY | (constants.O_NOFOLLOW || 0)
const createFlags = constants.O_RDWR | constants.O_CREAT | constants.O_EXCL | (constants.O_NOFOLLOW || 0)
async function readJson(handle) {
  const stat = await handle.stat()
  if (!stat.isFile() || stat.size > 1048576) throw new Error('development metadata must be a bounded regular file')
  return JSON.parse(await handle.readFile('utf8'))
}
let owned = false
try {
  const handle = await open(marker, readFlags)
  try { owned = (await readJson(handle)).owner === 'pccontroller-virtual-development' } finally { await handle.close() }
  if (!owned) throw new Error('wrong development directory owner')
} catch (error) {
  if (error.code !== 'ENOENT') throw error
}
let config
let handle
let created = false
if (owned) {
  try { handle = await open(configPath, readFlags) } catch (error) { if (error.code !== 'ENOENT') throw error }
}
// An unowned directory must win exclusive creation, never probe-then-overwrite
// an existing configuration. Read/check/write against the opened descriptors.
if (!handle) { handle = await open(configPath, createFlags, 0o600); created = true }
try {
  if (created) { await handle.writeFile(seed); config = JSON.parse(seed) }
  else config = await readJson(handle)
  if (!owned) {
    const ownerHandle = await open(marker, 'wx', 0o600)
    try { await ownerHandle.writeFile(JSON.stringify({owner:'pccontroller-virtual-development'})+'\n') } finally { await ownerHandle.close() }
  }
} finally { await handle.close() }
if (config.ipc?.allow_remote || config.integrations?.websocket_clients?.length
    || config.integrations?.board_mirror?.groups?.length || config.integrations?.board_mirror?.receive_from)
  throw new Error('dev launcher refuses a remotely connected/shared configuration')
const children = []
let socket, shuttingDown = false
function stop() {
  if (shuttingDown) return
  shuttingDown = true; socket?.destroy()
  for (const child of children) if (child.exitCode === null && !child.killed) child.kill()
}
process.on('SIGINT', stop); process.on('SIGTERM', stop)
function start(binary, args) {
  const child = spawn(binary, args, { windowsHide: true, stdio: ['ignore', 'inherit', 'inherit'] })
  children.push(child)
  child.on('error', error => { console.error(error.message); process.exitCode = 1; stop() })
  child.on('exit', code => { if (!shuttingDown) { process.exitCode = code || 1; stop() } })
  return child
}
async function connect(port) {
  const deadline = Date.now() + 10000
  while (Date.now() < deadline && !shuttingDown) {
    const connection = createConnection({ host:'127.0.0.1', port })
    try { await new Promise((ok, fail) => { connection.once('connect', ok); connection.once('error', fail) }); return connection }
    catch { connection.destroy(); await new Promise(ok => setTimeout(ok, 50)) }
  }
  throw new Error('development process did not become ready')
}
try {
  const sensorArgs = Object.entries(options).filter(([key,value]) => key.startsWith('sensor-') && value).flatMap(([key,value]) => ['--'+key,value])
  start(options.board, ['--bind','127.0.0.1','--port',String(boardPort),'--memory','--no-stdin','--quiet',...sensorArgs])
  const ready = await connect(boardPort); ready.destroy()
  start(options.controller, ['--config',configPath,'ipc','serve','--listen',`127.0.0.1:${hostPort}`,'--port',`tcp://127.0.0.1:${boardPort}`,'--no-connect'])
  socket = await connect(hostPort)
  const pending = new Map(); let nextId = 1
  const lines = createInterface({ input:socket })
  lines.on('line', line => {
    let message
    try { message = JSON.parse(line) } catch { console.error('malformed coordinator response'); process.exitCode = 1; stop(); return }
    const request = pending.get(message.id)
    if (!request) return
    pending.delete(message.id); clearTimeout(request.timeout)
    if (message.error) request.fail(new Error(message.error.message)); else request.ok(message.result)
  })
  socket.on('error', error => { for (const request of pending.values()) request.fail(error); stop() })
  const call = (method, params={}) => new Promise((ok, fail) => {
    const id = nextId++, timeout = setTimeout(() => { pending.delete(id); fail(new Error(method + ' timed out')) }, 10000)
    pending.set(id,{ok,fail,timeout}); socket.write(JSON.stringify({jsonrpc:'2.0',id,method,params})+'\n')
  })
  await call('controller.app.instance.report',{id:'virtual-development',surface:'bridge',page:'development',state:'active',lease_seconds:45})
  const snapshot = await call('controller.connect',{port:`tcp://127.0.0.1:${boardPort}`})
  if (!snapshot.connected || !(snapshot.hello.build_features & 128)) throw new Error('Refusing a non-native/obsolete firmware target')
  await call('controller.command.execute',{command:'settings'})
  await call('controller.front_panel')
  const profile = await call('controller.board_profile.get')
  if (!profile.configured) await call('controller.board_profile.update',{key:'development',mode:'cinema-seat-motion',expected_revision:profile.revision})
  socket.destroy(); lines.close()
  console.log(`Pealayer hardware endpoint: pccontroller://127.0.0.1:${hostPort}\nIsolated authoring profile: ${state}\nRAM-only shared firmware; Ctrl+C stops only these development processes.`)
  await Promise.all(children.map(child => child.exitCode !== null ? Promise.resolve() : new Promise(ok => child.once('exit',ok))))
} catch (error) { console.error(error.message); process.exitCode = 1; stop() }

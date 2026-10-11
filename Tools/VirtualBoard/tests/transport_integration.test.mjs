import { spawn, execFileSync } from 'node:child_process'
import { createConnection, createServer } from 'node:net'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'

// Independent wire oracle in TESTS only. Running firmware never uses this codec.
function crc(data) { let c = 0; for (const b of data) { c ^= b; for (let i = 0; i < 8; i++) c = (c & 128 ? (c << 1) ^ 7 : c << 1) & 255 } return c }
function encode(opcode, sequence, payload = []) {
  const raw = [165, 1, opcode, sequence, payload.length, ...payload]; raw.push(crc(raw))
  const out = [0]; let code = 1, at = 0
  for (const b of raw) { if (!b) { out[at] = code; at = out.length; out.push(0); code = 1 } else { out.push(b); code++ } }
  out[at] = code; return Buffer.from([...out, 0])
}
function parsePacket(encoded) {
  const raw = []; let i = 0
  while (i < encoded.length) { const code = encoded[i++]; assert(code > 0); for (let n = 1; n < code; n++) raw.push(encoded[i++]); if (code !== 255 && i < encoded.length) raw.push(0) }
  const data = Buffer.from(raw); assert.equal(data[0], 165); assert.equal(data.length, data[4] + 6); assert.equal(data.at(-1), crc(data.subarray(0, -1)))
  return { opcode: data[2], sequence: data[3], payload: data.subarray(5, -1) }
}
function channel(stream) {
  let encoded = [], frames = [], failure
  stream.on('data', data => { try { for (const b of data) { if (b) encoded.push(b); else if (encoded.length) { frames.push(parsePacket(encoded)); encoded = [] } } } catch (error) { failure = error } })
  return async (predicate, milliseconds = 5000) => {
    const until = Date.now() + milliseconds
    while (Date.now() < until) {
      if (failure) throw failure
      const index = frames.findIndex(predicate); if (index >= 0) return frames.splice(index, 1)[0]
      await new Promise(resolve => setTimeout(resolve, 5))
    }
    throw new Error('timed out waiting for firmware frame')
  }
}
async function freePort() {
  const server = createServer(); await new Promise(resolve => server.listen(0, '127.0.0.1', resolve)); const port = server.address().port
  await new Promise(resolve => server.close(resolve)); return port
}
async function connect(port) {
  for (let n = 0; n < 200; n++) {
    const socket = createConnection({ host: '127.0.0.1', port })
    try { await new Promise((resolve, reject) => { socket.once('connect', resolve); socket.once('error', reject) }); socket.on('error', () => {}); return socket }
    catch { socket.destroy(); await new Promise(resolve => setTimeout(resolve, 10)) }
  }
  throw new Error('native listener never became ready')
}
const binary = process.argv[2]
assert(binary, 'native binary required')
const state = await mkdtemp(join(tmpdir(), 'pccontroller-native-conformance-'))
const children = [], sockets = []
const pidFile = join(state, 'board.pid')
function start(args, argv0 = binary) {
  const child = spawn(binary, args, { argv0, stdio: ['pipe', 'pipe', 'pipe'] }); children.push(child)
  let diagnostics = ''; child.stderr.on('data', chunk => { diagnostics += chunk })
  child.on('error', error => { diagnostics += error.message })
  child.diagnostics = () => diagnostics
  return child
}
try {
  const stdio = start(['--stdio', '--memory'])
  const stream = channel(stdio.stdout)
  stdio.stdin.write(encode(1, 41))
  const hello = await stream(f => f.opcode === 129 && f.sequence === 41)
  assert([3, 4].includes(hello.payload[0]))
  assert.notEqual(hello.payload.readUInt32LE(6), 0, 'real source identity must be advertised')
  stdio.stdin.write(encode(2, 42))
  const status = await stream(f => f.opcode === 144 && f.sequence === 42)
  assert.equal(status.payload.length, 48); assert.equal(status.payload.readInt32LE(4), -2147483648)
  assert.equal(status.payload.readInt16LE(20), -32768)
  stdio.stdin.end(); await new Promise(resolve => stdio.once('exit', resolve)); assert.equal(stdio.exitCode, 0, stdio.diagnostics())

  const port = await freePort(), eeprom = join(state, 'board.bin')
  // Restart must resolve the loaded OS image, not this untrusted argv[0].
  let child = start(['--port', String(port), '--eeprom', eeprom, '--pid-file', pidFile, '--no-stdin'], join(state, 'not-a-firmware-executable'))
  let outputLog = ''; child.stdout.on('data', bytes => { outputLog += bytes })
  let socket = await connect(port); sockets.push(socket); let receive = channel(socket)
  socket.write(encode(1, 50)); const tcpHello = await receive(f => f.opcode === 129 && f.sequence === 50)
  assert.deepEqual(tcpHello.payload, hello.payload, 'stdio and TCP must report identical production identity/capabilities')
  socket.write(encode(49, 51, [4, 1])); await receive(f => f.opcode === 128 && f.sequence === 51)
  socket.write(encode(2, 52)); const before = await receive(f => f.opcode === 144 && f.sequence === 52)
  assert(before.payload[28] & 16)
  await new Promise(resolve => setTimeout(resolve, 1100))
  assert.equal((await readFile(eeprom)).length, 1024)
  const duplicate = start(['--port', String(await freePort()), '--eeprom', eeprom, '--no-stdin'])
  await new Promise(resolve => duplicate.once('exit', resolve)); assert.notEqual(duplicate.exitCode, 0); assert.match(duplicate.diagnostics(), /already owned/)
  socket.write(Buffer.from([3, 165])); socket.destroy()
  socket = await connect(port); sockets.push(socket); receive = channel(socket)
  socket.write(encode(2, 53)); await receive(f => f.opcode === 144 && f.sequence === 53)
  socket.write(encode(53, 54, [0])); await receive(f => f.opcode === 128 && f.sequence === 54)
  await new Promise(resolve => setTimeout(resolve, 400)); socket.destroy()
  socket = await connect(port); sockets.push(socket); receive = channel(socket)
  socket.write(encode(2, 55)); const reboot = await receive(f => f.opcode === 144 && f.sequence === 55)
  assert.equal(reboot.payload[43], 8); assert.equal(reboot.payload.readUInt32LE(44), before.payload.readUInt32LE(44) + 1)
  assert.equal(reboot.payload[28], 0, 'safe reset must clear and persist relay outputs')
  assert.match(outputLog, /ON:5/); assert.match(outputLog, /OFF:5/)
  console.log('production stdio/TCP parity, fragmented reconnect, EEPROM ownership/persistence and real process reset passed')
} finally {
  for (const socket of sockets) socket.destroy()
  for (const child of children) if (child.exitCode === null) child.kill('SIGTERM')
  // Windows exec changes PID. This exact PID file belongs only to the process
  // we spawned with this private temp path; never match all board processes.
  try {
    const pid = Number(await readFile(pidFile, 'utf8'))
    assert(Number.isSafeInteger(pid) && pid > 0)
    if (process.platform === 'win32') execFileSync('taskkill', ['/PID', String(pid), '/T', '/F'], { stdio: 'ignore' })
    else process.kill(pid, 'SIGTERM')
  } catch {}
  await new Promise(resolve => setTimeout(resolve, 200))
  await rm(state, { recursive: true, force: true })
}

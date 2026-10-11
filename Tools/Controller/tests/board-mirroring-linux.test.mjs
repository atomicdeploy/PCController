// Live, real-protocol acceptance smoke. No fake transport or physical-I/O claim.
// node tests/board-mirroring-linux.test.mjs CONTROLLER VIRTUAL_BOARD [--native-buzzer]
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises'
import { createServer } from 'node:net'
import { join, resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

if (process.platform !== 'linux') throw new Error('This acceptance smoke requires Linux')
if (process.argv.length < 4 || process.argv.length > 5 || (process.argv[4] && process.argv[4] !== '--native-buzzer')) {
  throw new Error('Pass controller and VirtualBoard executable paths, optionally --native-buzzer')
}
const [controller, firmware] = process.argv.slice(2, 4).map(value => resolve(value))
const nativeBuzzer = process.argv[4] === '--native-buzzer'
const root = await mkdtemp('/dev/shm/pccontroller-mirror-smoke-')
const children = []
const outputs = new Map()
let nextID = 0
let endpointA, endpointB

async function unusedPort() {
  const server = createServer()
  await new Promise((ok, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', ok) })
  const port = server.address().port
  await new Promise(ok => server.close(ok))
  return port
}
function launch(name, file, args, env = {}) {
  const child = spawn(file, args, { env: { ...process.env, ...env }, stdio: ['pipe', 'pipe', 'pipe'] })
  children.push(child)
  outputs.set(name, '')
  for (const stream of [child.stdout, child.stderr]) stream.on('data', data => outputs.set(name, (outputs.get(name) + data).slice(-65536)))
  child.on('error', error => outputs.set(name, String(error)))
  return child
}
async function rpc(endpoint, method, params = {}, timeout = 4000) {
  const response = await fetch(`${endpoint}/api/rpc`, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: ++nextID, method, params }),
    signal: AbortSignal.timeout(timeout)
  })
  const value = await response.json()
  if (value.error) throw new Error(`${method}: ${value.error.message}`)
  return value.result
}
async function eventually(label, check, timeout = 10000) {
  const deadline = Date.now() + timeout
  let last
  while (Date.now() < deadline) {
    try { const result = await check(); if (result) return result } catch (error) { last = error }
    await delay(75)
  }
  throw new Error(`${label} timed out${last ? `: ${last.message}` : ''}`)
}
const policy = { domains: ['menu', 'buzzer', 'relay', 'pwm'], relays: [5], pwm_channels: [0] }
function mirrorConfig(coordinatorSide, nextPolicy = policy) {
  const config = { local_board_id: coordinatorSide ? 'smoke-a' : 'smoke-b', receive_from: 'smoke-coordinator', receive: nextPolicy }
  if (coordinatorSide) Object.assign(config, { coordinator_id: 'smoke-coordinator', groups: [{ id: 'smoke-pair', members: [{ board_id: 'smoke-a' }, { board_id: 'smoke-b', peer: 'second' }], policy: nextPolicy }] })
  return config
}
async function inspect(endpoint) { return rpc(endpoint, 'controller.mirror.inspect') }
async function waitValue(endpoint, key, match) {
  let lastTrace
  return eventually(`${key} at ${endpoint}`, async () => {
    const state = await inspect(endpoint)
    if (process.env.PCCONTROLLER_MIRROR_TRACE && key === 'segments') {
      const [group] = await rpc(endpointA, 'controller.mirror.status')
      const trace = JSON.stringify({ endpoint, value: state.values[key], sequence: group?.sequence, origin: group?.last_origin, results: group?.last_results })
      if (trace !== lastTrace) { console.log(trace); lastTrace = trace }
    }
    return match(state.values[key]) && state
  })
}
async function localBuzzerEvent(endpoint, afterID, frequency) {
  let cursor = afterID
  return eventually(`local ${frequency} Hz note at ${endpoint}`, async () => {
    const event = await rpc(endpoint, 'controller.event.next', { after_id: cursor, kind: 'buzzer.note', timeout_ms: 1000 }, 1500)
    cursor = event.id
    return !event.metadata?.['bridge.ingress'] && Number(event.metadata?.frequency_hz) === frequency && event
  })
}

try {
  const [boardPortA, boardPortB, hostPortA, hostPortB] = await Promise.all(Array.from({ length: 4 }, unusedPort))
  endpointA = `http://127.0.0.1:${hostPortA}`; endpointB = `http://127.0.0.1:${hostPortB}`
  const boardA = launch('firmware-a', firmware, ['--bind', '127.0.0.1', '--port', String(boardPortA), '--memory'])
  const boardB = launch('firmware-b', firmware, ['--bind', '127.0.0.1', '--port', String(boardPortB), '--memory'])
  boardA.stdin.write('door closed\n'); boardB.stdin.write('door closed\n')
  for (const [name, hostPort, boardPort, coordinatorSide] of [['a', hostPortA, boardPortA, true], ['b', hostPortB, boardPortB, false]]) {
    const stateRoot = join(root, name); await mkdir(stateRoot)
    const configPath = join(stateRoot, 'config.json')
    await writeFile(configPath, JSON.stringify({
      connection: { port: `tcp://127.0.0.1:${boardPort}`, startup_wait_ms: 10, reset_on_reconnect: false },
      ui: { setup_complete: true },
      integrations: {
        discovery: { enabled: false }, notifications: { enabled: false }, status_led: { enabled: false },
        buzzer_mirror: { enabled: nativeBuzzer && coordinatorSide, native_enabled: nativeBuzzer && coordinatorSide, backend: 'native', web_audio_enabled: false },
        board_mirror: mirrorConfig(coordinatorSide),
        websocket_clients: coordinatorSide ? [{ name: 'second', enabled: true, protocol: 'jsonrpc', url: `ws://127.0.0.1:${hostPortB}/ipc`, allow_commands: true, forward_events: false }] : []
      }
    }))
    launch(`controller-${name}`, controller, ['--config', configPath, 'ipc', 'serve', '--listen', `127.0.0.1:${hostPort}`, '--no-connect'], {
      XDG_DATA_HOME: stateRoot, XDG_STATE_HOME: stateRoot, XDG_CACHE_HOME: stateRoot, XDG_CONFIG_HOME: stateRoot
    })
  }
  await eventually('host A ping', () => rpc(endpointA, 'controller.ping'))
  await eventually('host B ping', () => rpc(endpointB, 'controller.ping'))
  await rpc(endpointA, 'controller.open', { port: `tcp://127.0.0.1:${boardPortA}` })
  await rpc(endpointB, 'controller.open', { port: `tcp://127.0.0.1:${boardPortB}` })
  await eventually('two connected identified members', async () => {
    const [group] = await rpc(endpointA, 'controller.mirror.status')
    return group?.members['smoke-a']?.connected && group?.members['smoke-b']?.connected
  })
  const initialSync = await rpc(endpointA, 'controller.mirror.resync', { group_id: 'smoke-pair', source_board_id: 'smoke-a' })
  assert.ok(initialSync.length && initialSync.every(result => result.verified), JSON.stringify(initialSync))
  assert.ok(!(await rpc(endpointA, 'controller.mirror.status'))[0].resync_required?.length)
  for (const endpoint of [endpointA, endpointB]) {
    const state = await inspect(endpoint)
    assert.equal(state.observations.status.ina219_available, false)
    assert.equal(state.observations.status.temperature_led_available, false)
    assert.ok(state.values.menu, 'new volatile menu capability unavailable')
    assert.ok(state.values['pwm.0'], 'PCA9685 output unavailable')
  }
  await rpc(endpointA, 'controller.pwm.set', { channel: 0, raw_value: 1234 })
  await waitValue(endpointB, 'pwm.0', value => value?.value === 1234)
  await rpc(endpointB, 'controller.pwm.set', { channel: 0, raw_value: 2345 })
  await waitValue(endpointA, 'pwm.0', value => value?.value === 2345)
  let [group] = await rpc(endpointA, 'controller.mirror.status')
  assert.ok(group.last_results.every(result => result.acknowledged && result.verified))
  const stableSequence = group.sequence
  await delay(700)
  ;[group] = await rpc(endpointA, 'controller.mirror.status')
  assert.equal(group.sequence, stableSequence, 'state echo storm')
  console.log('PASS real-wire full-duplex PWM/PCA9685 + verified readback + no state echo')

  await rpc(endpointA, 'controller.command.execute', { command: 'relay 5 on' })
  await waitValue(endpointB, 'relay.5', value => value?.active === true)
  await rpc(endpointB, 'controller.command.execute', { command: 'relay 5 off' })
  await waitValue(endpointA, 'relay.5', value => value?.active === false)
  assert.match(outputs.get('firmware-a'), /ON:5/)
  assert.match(outputs.get('firmware-b'), /ON:5/)
  console.log('PASS full-duplex general relay + production stdout ON:5/OFF:5')

  await rpc(endpointA, 'controller.menu.page', { page: '1' })
  await waitValue(endpointB, 'menu', value => value?.page === 1)
  await rpc(endpointB, 'controller.menu.page', { page: '2' })
  await waitValue(endpointA, 'menu', value => value?.page === 2)
  console.log('PASS full-duplex menu on capability-advertised volatile receiver')

  const afterB = (await rpc(endpointB, 'controller.event.latest')).id
  await rpc(endpointA, 'controller.command.execute', { command: 'buzzer 523 150' })
  await localBuzzerEvent(endpointB, afterB, 523)
  const afterA = (await rpc(endpointA, 'controller.event.latest')).id
  await rpc(endpointB, 'controller.command.execute', { command: 'buzzer 659 150' })
  await localBuzzerEvent(endpointA, afterA, 659)
  await delay(700)
  ;[group] = await rpc(endpointA, 'controller.mirror.status')
  const noteSequence = group.sequence
  assert.ok(group.last_results.every(result => result.acknowledged && !result.verified), 'tone ACK falsely claimed physical verification')
  await delay(700)
  ;[group] = await rpc(endpointA, 'controller.mirror.status')
  assert.equal(group.sequence, noteSequence, 'buzzer echo storm')
  console.log('PASS pushed full-duplex buzzer + bounded echo suppression + honest ACK')
  if (nativeBuzzer) {
    const { buzzer_runtime: runtime } = await rpc(endpointA, 'controller.integrations.status')
    assert.equal(runtime.host_mirror, true)
    assert.equal(runtime.backend_effective, 'native', JSON.stringify(runtime))
    assert.ok(!runtime.backend_error, JSON.stringify(runtime))
    console.log('PASS native Linux host buzzer enabled during real board notes, no backend error (not audibility proof)')
  }

  const segmentPolicy = { ...policy, domains: [...policy.domains, 'segments', 'status_led'] }
  await rpc(endpointB, 'controller.mirror.configure', mirrorConfig(false, segmentPolicy))
  await rpc(endpointA, 'controller.mirror.configure', mirrorConfig(true, segmentPolicy))
  await delay(600)
  const displaySync = await rpc(endpointA, 'controller.mirror.resync', { group_id: 'smoke-pair', source_board_id: 'smoke-a' })
  assert.ok(displaySync.length && displaySync.every(result => result.verified), JSON.stringify(displaySync))
  await rpc(endpointA, 'controller.command.execute', { command: 'rgb 120 30 10 128' })
  let expectedRGB = [60, 15, 5]
  await waitValue(endpointA, 'status_led', value => JSON.stringify(value?.rgb) === JSON.stringify(expectedRGB))
  await waitValue(endpointB, 'status_led', value => JSON.stringify(value?.rgb) === JSON.stringify(expectedRGB))
  await rpc(endpointB, 'controller.command.execute', { command: 'rgb 10 80 150 64' })
  expectedRGB = [2, 20, 38]
  await waitValue(endpointB, 'status_led', value => JSON.stringify(value?.rgb) === JSON.stringify(expectedRGB))
  await waitValue(endpointA, 'status_led', value => JSON.stringify(value?.rgb) === JSON.stringify(expectedRGB))
  console.log('PASS pushed full-duplex static physical RGB without double brightness scaling')
  await rpc(endpointA, 'controller.display.send', { target: 'segments', text: '1234', duration_ms: 10000 })
  let expected = [0x06, 0x5b, 0x4f, 0x66] // rendered 1234, not the pre-render ACK snapshot
  await waitValue(endpointA, 'segments', value => JSON.stringify(value?.cells) === JSON.stringify(expected))
  await waitValue(endpointB, 'segments', value => JSON.stringify(value?.cells) === JSON.stringify(expected))
  await delay(2500) // beyond the destination lease; renewal must be real
  assert.deepEqual((await inspect(endpointB)).values.segments.cells, expected)
  await rpc(endpointB, 'controller.display.send', { target: 'segments', text: '5678', duration_ms: 10000 })
  expected = [0x6d, 0x7d, 0x07, 0x7f] // rendered 5678
  await waitValue(endpointB, 'segments', value => JSON.stringify(value?.cells) === JSON.stringify(expected))
  await waitValue(endpointA, 'segments', value => JSON.stringify(value?.cells) === JSON.stringify(expected))
  console.log('PASS four-cell segments both ways + renewable bounded presentation lease')
  await rpc(endpointA, 'controller.mirror.configure', mirrorConfig(true))
  await rpc(endpointB, 'controller.mirror.configure', mirrorConfig(false))
  await delay(500)
  await rpc(endpointA, 'controller.mirror.resync', { group_id: 'smoke-pair', source_board_id: 'smoke-a' })

  const beforeDisconnect = (await inspect(endpointB)).values['pwm.0'].value
  await rpc(endpointB, 'controller.close')
  await eventually('disconnected B observed', async () => (await rpc(endpointA, 'controller.mirror.status'))[0]?.errors['smoke-b'])
  await rpc(endpointA, 'controller.pwm.set', { channel: 0, raw_value: 3210 })
  await delay(400)
  await rpc(endpointB, 'controller.open', { port: `tcp://127.0.0.1:${boardPortB}` })
  await eventually('reconnected B baseline', async () => (await rpc(endpointA, 'controller.mirror.status'))[0]?.members['smoke-b']?.connected)
  await delay(600)
  assert.equal((await inspect(endpointB)).values['pwm.0'].value, beforeDisconnect, 'reconnect replayed actuation')
  const resync = await rpc(endpointA, 'controller.mirror.resync', { group_id: 'smoke-pair', source_board_id: 'smoke-a' })
  assert.ok(resync.length && resync.every(result => result.verified), JSON.stringify(resync))
  await waitValue(endpointB, 'pwm.0', value => value?.value === 3210)
  console.log('PASS disconnect quarantine + no queued replay + explicit verified resync')

  boardA.stdin.write('tm1637 off\n')
  await eventually('actual native ACK absence', async () => (await inspect(endpointA)).observations.front_panel.tm1637_detected === false)
  boardA.stdin.write('tm1637 on\n')
  await eventually('ACK recovery without board reset', async () => (await inspect(endpointA)).observations.front_panel.tm1637_detected === true)
  boardB.stdin.write('door open\n')
  await eventually('board-scoped door input', async () => (await inspect(endpointB)).observations.status.door_open)
  assert.equal((await inspect(endpointA)).observations.status.door_open, false, 'physical input was fabricated on another board')
  console.log('PASS native TM1637 ACK recovery + truthful independent physical-input observations')
  console.log(JSON.stringify({ result: 'passed', platform: process.platform, architecture: process.arch, physicalAvrAcceptance: false, gpioAdapter: false }))
} catch (error) {
  for (const endpoint of [endpointA, endpointB].filter(Boolean)) {
    try { const state = await inspect(endpoint); const mirror = await rpc(endpoint, 'controller.mirror.status'); console.error(JSON.stringify({ endpoint, connected: state.connected, generation: state.generation, values: Object.fromEntries(['pwm.0', 'relay.5', 'menu', 'segments', 'status_led'].map(key => [key, state.values[key]])), mirror: mirror.map(group => ({ id: group.id, sequence: group.sequence, origin: group.last_origin, errors: group.errors, results: group.last_results, dropped: group.dropped_notes })) })) } catch {}
  }
  for (const [name, output] of outputs) console.error(`${name}: ${output.slice(-8000)}`)
  throw error
} finally {
  for (const child of children) if (child.exitCode === null) child.kill('SIGTERM')
  await Promise.all(children.map(child => child.exitCode !== null ? undefined : new Promise(ok => { child.once('exit', ok); const timer = setTimeout(() => { child.kill('SIGKILL'); ok() }, 3000); timer.unref() })))
  // Only this mkdtemp-created RAM fixture directory is eligible for cleanup.
  if (!root.startsWith('/dev/shm/pccontroller-mirror-smoke-')) throw new Error('unsafe fixture cleanup path')
  await rm(root, { recursive: true, force: true })
}

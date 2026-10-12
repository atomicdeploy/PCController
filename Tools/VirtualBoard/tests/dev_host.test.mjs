import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'node:net'
import { execFile } from 'node:child_process'

const launcher = fileURLToPath(new URL('../dev-host.mjs', import.meta.url))
async function port() {
  const listener = createServer()
  await new Promise(ok => listener.listen(0, '127.0.0.1', ok))
  const value = listener.address().port
  await new Promise(ok => listener.close(ok))
  return value
}
async function run(state) {
  let board = await port(), host = await port()
  while (board === host) host = await port()
  // Node deliberately rejects firmware argv. No board/host/device is started;
  // these tests exercise preservation and atomic seeding before readiness.
  return await new Promise(ok => execFile(process.execPath, [launcher,'--state-root',state,'--board',process.execPath,
    '--controller',process.execPath,'--board-port',String(board),'--host-port',String(host)],
  { timeout:15000, windowsHide:true }, (error,stdout,stderr) => ok({error,stdout,stderr})))
}
test('unowned existing configuration is neither read as a profile nor overwritten', async () => {
  const state = await mkdtemp(join(tmpdir(),'pccontroller-dev-ownership-'))
  try {
    const content = 'private existing state\n'
    await writeFile(join(state,'config.json'),content)
    const result = await run(state)
    assert(result.error); assert.match(result.stderr,/EEXIST/)
    assert.equal(await readFile(join(state,'config.json'),'utf8'),content)
    assert.equal(await readFile(join(state,'virtual-development.json'),'utf8'),'','failed exclusive seed must not publish valid ownership')
  } finally { await rm(state,{recursive:true,force:true}) }
})
test('owned remote configuration is refused without overwriting authoring', async () => {
  const state = await mkdtemp(join(tmpdir(),'pccontroller-dev-remote-'))
  try {
    await writeFile(join(state,'virtual-development.json'),JSON.stringify({owner:'pccontroller-virtual-development'}))
    const content = JSON.stringify({ipc:{allow_remote:true},macros:[{name:'keep draft'}]})
    await writeFile(join(state,'config.json'),content)
    const result = await run(state)
    assert(result.error); assert.match(result.stderr,/remotely connected/)
    assert.equal(await readFile(join(state,'config.json'),'utf8'),content)
  } finally { await rm(state,{recursive:true,force:true}) }
})
test('fresh development seed uses bounded RAM-only isolated defaults', async () => {
  const state = await mkdtemp(join(tmpdir(),'pccontroller-dev-seed-'))
  try {
    const result = await run(state)
    assert(result.error, 'fake native binary must not become ready')
    const config = JSON.parse(await readFile(join(state,'config.json'),'utf8'))
    assert.equal(config.integrations.keyboard_control.enabled,false)
    assert.equal(config.integrations.buzzer_mirror.enabled,false)
    assert.equal(config.ui.history_hours,0)
    assert.equal(config.connection.reset_on_reconnect,false)
    assert.equal(JSON.parse(await readFile(join(state,'virtual-development.json'),'utf8')).owner,'pccontroller-virtual-development')
  } finally { await rm(state,{recursive:true,force:true}) }
})

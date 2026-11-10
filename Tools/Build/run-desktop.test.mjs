import assert from 'node:assert/strict'
import { test } from 'node:test'
import { desktopInvocation } from './run-desktop.mjs'

const options = { platform: 'win32', root: 'C:/source/PCController', localAppData: 'C:/User Data/Local', exists: () => true }
test('default launcher uses the integrated installed core and pauses hardware auto-connect', () => {
  const launch = desktopInvocation([], options)
  assert.match(launch.executable.replaceAll('\\', '/'), /Programs\/PCController\/bin\/controller.exe$/)
  assert.deepEqual(launch.args, ['gui', '--no-auto'])
})
test('development and staged packages never select the standalone donor executable', () => {
  assert.match(desktopInvocation(['--development'], options).executable.replaceAll('\\', '/'), /Tools\/Controller\/bin\/controller.exe$/)
  const launch = desktopInvocation(['--package', 'C:/staged package', '--preview', '--page', 'rf'], options)
  assert.deepEqual(launch.args, ['gui', '--preview', '--preview-page', 'rf'])
  assert.match(launch.cwd, /staged package$/)
})
test('explicit attach does not pause or reconnect an existing owner', () => {
  assert.deepEqual(desktopInvocation(['--attach', '127.0.0.1:8788'], options).args, ['gui', '--ipc-addr', '127.0.0.1:8788'])
  assert.deepEqual(desktopInvocation(['--auto-connect'], options).args, ['gui'])
})
test('config paths with spaces and Unicode remain a single argument', () => {
  const launch = desktopInvocation(['--config', 'C:/data folder/آزمایش.json'], options)
  assert.equal(launch.args[0], '--config')
  assert.match(launch.args[1], /آزمایش.json$/)
  assert.equal(launch.args[2], 'gui')
})
test('ambiguous or unsafe combinations fail before any process starts', () => {
  for (const args of [['--installed', '--development'], ['--package'], ['--attach', '--preview'], ['--preview', '--auto-connect'], ['--preview', '--attach', 'host:80'], ['--attach', 'host:80', '--auto-connect'], ['--page', 'rf'], ['--unknown']]) {
    assert.throws(() => desktopInvocation(args, options))
  }
  assert.throws(() => desktopInvocation([], { ...options, exists: () => false }), /not found/)
  assert.throws(() => desktopInvocation([], { ...options, platform: 'linux' }), /Windows/)
})

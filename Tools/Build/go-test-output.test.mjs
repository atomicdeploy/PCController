import test from 'node:test'
import assert from 'node:assert/strict'
import { resolve } from 'node:path'
import { stableTestOutput } from './go-tests.mjs'

const env = { LOCALAPPDATA: 'C:\\Users\\tester\\AppData\\Local' }
const canonical = 'C:\\Users\\tester\\AppData\\Local\\PCController\\test-programs\\go'

test('Windows output is canonical and independent of the current worktree', () => {
 assert.equal(stableTestOutput(undefined, 'win32', env), canonical)
 assert.equal(stableTestOutput(canonical.toUpperCase(), 'win32', env), canonical)
 assert.equal(stableTestOutput(canonical + '\\.', 'win32', env), canonical)
})
test('Windows rejects alternate output roots and missing local app data', () => {
 for (const path of [canonical + '\\task', canonical + '-other', 'D:\\tests']) {
  assert.throws(() => stableTestOutput(path, 'win32', env), /per-task or per-worktree/)
 }
 assert.throws(() => stableTestOutput(undefined, 'win32', {}), /LOCALAPPDATA/)
})
test('non-Windows retains configurable output', () => {
 assert.equal(stableTestOutput('custom-tests', 'linux', {}), resolve('custom-tests'))
})

import test from 'node:test'
import assert from 'node:assert/strict'
import { selectTestPackages, selectedTestCacheName } from './go-tests.mjs'

const packages = ['example/internal/artifacts', 'example/internal/ipcjson', 'example/cmd/controller'].map(importPath => ({ importPath }))

test('default test selection keeps every package', () => {
  assert.deepEqual(selectTestPackages(packages, []), packages)
  assert.equal(selectedTestCacheName([], ''), 'passed.json')
})
test('focused selection accepts exact and module-relative names in stable order', () => {
  assert.deepEqual(selectTestPackages(packages, ['./internal/ipcjson', 'example/internal/artifacts']), packages.slice(0, 2))
  assert.throws(() => selectTestPackages(packages, ['missing']), /matched 0/)
  assert.throws(() => selectTestPackages([{ importPath: 'one/a' }, { importPath: 'two/a' }], ['a']), /matched 2/)
})
test('partial passes never populate the complete-suite cache', () => {
  const full = selectedTestCacheName([], '')
  const focused = selectedTestCacheName(['internal/artifacts'], '')
  assert.notEqual(focused, full)
  assert.notEqual(selectedTestCacheName([], 'TestPeer'), full)
  assert.notEqual(selectedTestCacheName(['internal/artifacts'], 'TestPeer'), focused)
  assert.equal(selectedTestCacheName(['a', 'b'], 'TestPeer'), selectedTestCacheName(['b', 'a'], 'TestPeer'))
})

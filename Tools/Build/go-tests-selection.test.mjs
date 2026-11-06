import test from 'node:test'
import assert from 'node:assert/strict'
import { selectTestPackages, selectedTestCacheName, runTestPlan } from './go-tests.mjs'

const packages = ['example/internal/artifacts', 'example/internal/ipcjson', 'example/cmd/controller'].map(importPath => ({ importPath }))

test('optional failure collection runs every package but cannot succeed with failures', () => {
  const visited = []
  assert.throws(() => runTestPlan(packages, item => {
    visited.push(item.importPath)
    if (item === packages[0] || item === packages[2]) throw new Error('fixture failure')
  }, true), /2 Go test package\(s\) failed/)
  assert.deepEqual(visited, packages.map(item => item.importPath))
  visited.length = 0
  assert.throws(() => runTestPlan(packages, item => {
    visited.push(item.importPath)
    throw new Error('fail fast')
  }), /fail fast/)
  assert.equal(visited.length, 1)
})

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

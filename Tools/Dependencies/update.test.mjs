import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { compareVersions, sameSubstantive, stableParts } from './update.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '..', '..')

test('stable comparison is semantic and rejects prereleases', () => {
  assert.equal(compareVersions('1.10.0', '1.9.9'), 1)
  assert.equal(compareVersions('u8.0', '8.0.0'), 0)
  assert.equal(stableParts('1.5.2-rc.1'), null)
})

test('resolved tool lock comparison ignores timestamp-only churn', () => {
  const current = { format: 'lock/v1', resolved_at_utc: '2026-01-01T00:00:00Z', tool: { version: '1.2.3', sha256: 'abc' } }
  const resolved = { ...current, resolved_at_utc: '2099-01-01T00:00:00Z' }
  assert.equal(sameSubstantive(current, resolved), true)
  resolved.tool = { version: '1.2.4', sha256: 'def' }
  assert.equal(sameSubstantive(current, resolved), false)
})

test('npm projects declare compatibility ranges while each lock stays exact', () => {
  for (const directory of [join(repo, 'Tools', 'Controller', 'web'), join(repo, 'Tools', 'Build')]) {
    const manifest = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'))
    const lock = JSON.parse(readFileSync(join(directory, 'package-lock.json'), 'utf8'))
    for (const [name, value] of Object.entries({ ...manifest.dependencies, ...manifest.devDependencies })) {
      assert.match(value, /^\^\d/, `${name} in ${directory} must use a latest-compatible caret range`)
    }
    assert.deepEqual(lock.packages[''].dependencies ?? {}, manifest.dependencies ?? {})
    assert.deepEqual(lock.packages[''].devDependencies ?? {}, manifest.devDependencies ?? {})
  }
})

test('scheduled updater validates every required candidate gate before PR creation', () => {
  const workflow = readFileSync(join(repo, '.github', 'workflows', 'update-dependencies.yml'), 'utf8')
  const updater = readFileSync(join(here, 'update.mjs'), 'utf8')
  for (const expected of [
    'schedule:', '--apply --validate', 'steps.candidate.outcome == \'success\'',
    'create-pull-request@v8', 'dependency-blocked',
  ]) assert.ok(workflow.includes(expected), `workflow missing ${expected}`)
  for (const expected of [
    '32256', 'Urboot-Custom', 'VirtualBoard tests', 'Generated product identity',
    'Go tests from stable paths', 'Web tests',
    'windowsResources', 'upx', 'function resolveToolchain(mode)',
  ]) assert.ok(updater.includes(expected), `updater missing ${expected}`)
})

test('dependency output uses the shared Chalk and Unicode table renderer', () => {
  const updater = readFileSync(join(here, 'update.mjs'), 'utf8')
  assert.match(updater, /createChalk, renderUnicodeTable/)
  assert.doesNotMatch(updater, /\\u001b|\\x1b|padEnd\(|\.repeat\(/)
})

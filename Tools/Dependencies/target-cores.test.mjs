import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { resolveTargetCores } from './target-cores.mjs'

const targets = JSON.parse(readFileSync(new URL('../Firmware/targets.json', import.meta.url), 'utf8'))
const entry = version => ({ version, architecture: 'avr', url: `https://MCUdude.github.io/MegaCore/MegaCore-${version}.tar.bz2`, checksum: `SHA-256:${'a'.repeat(64)}` })
const index = { packages: [{ name: 'MegaCore', platforms: [entry('3.9.0'), entry('3.10.0'), entry('4.0.0-rc.1'), { ...entry('99.0.0'), architecture: 'arm' }] }] }

test('target core selection uses numeric stable ordering and caches shared indexes', () => {
  let fetches = 0
  const checks = resolveTargetCores({ ...targets, second: targets.mega2560 }, () => { fetches++; return index })
  assert.equal(fetches, 1)
  assert.equal(checks.length, 2)
  assert.equal(checks[0].resolved, '3.10.0')
  assert.equal(checks[0].current, 'minimum 3.0.3')
  assert.equal(checks[0].sha256, 'a'.repeat(64))
})

test('target core selection fails closed for missing versions, hashes and unreviewed sources', () => {
  assert.throws(() => resolveTargetCores(targets, () => ({ packages: [] })), /No compatible stable core/u)
  assert.throws(() => resolveTargetCores(targets, () => ({ packages: [{ name: 'MegaCore', platforms: [{ ...entry('3.10.0'), checksum: 'bad' }] }] })), /Invalid core archive provenance/u)
  assert.throws(() => resolveTargetCores({ mega2560: { ...targets.mega2560, index: 'https://example.test/index.json' } }, () => index), /requires code review/u)
})

test('firmware CI installs the resolved target core rather than pinning to its minimum', () => {
  const source = readFileSync(new URL('../../.github/workflows/firmware.yml', import.meta.url), 'utf8')
  assert.match(source, /node Tools\/Dependencies\/target-cores\.mjs mega2560/u)
  assert.match(source, /TARGET_CORE_VERSION: \$\{\{ steps\.target-core\.outputs\.version \}\}/u)
  assert.match(source, /process\.env\.TARGET_CORE_VERSION/u)
  assert.doesNotMatch(source, /target\.core \+ "@" \+ target\.minimumCoreVersion/u)
  for (const profile of ['constrained', 'expanded-panel', 'expanded-autonomous']) {
    assert.ok(source.includes(`targets.mjs build mega2560 ${profile}`))
  }
})

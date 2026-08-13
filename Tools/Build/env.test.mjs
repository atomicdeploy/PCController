import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { loadProjectEnv, parseEnvFile, resolveProjectEnvFile } from './env.mjs'

test('parses comments, export declarations, and quoted values without shell evaluation', () => {
  const values = parseEnvFile('ALPHA=one # comment\nexport BETA="two words"\nGAMMA=\'three # literal\'\n', 'fixture')
  assert.deepEqual(Object.fromEntries(values), { ALPHA: 'one', BETA: 'two words', GAMMA: 'three # literal' })
})

test('project env preserves inherited values and supports an explicit file', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-env-'))
  try {
    const file = join(directory, 'development.env')
    writeFileSync(file, 'INHERITED=file\nFROM_FILE=present\n', 'utf8')
    const environment = { PCCONTROLLER_ENV_FILE: file, INHERITED: 'process' }
    const result = loadProjectEnv(environment, { cwd: directory })
    assert.equal(result.loaded, true)
    assert.deepEqual(result.applied, ['FROM_FILE'])
    assert.equal(environment.INHERITED, 'process')
    assert.equal(environment.FROM_FILE, 'present')
    assert.equal(resolveProjectEnvFile(environment, { cwd: directory }), file)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('rejects malformed assignments with a source line', () => {
  assert.throws(() => parseEnvFile('NO_EQUALS\n', 'fixture.env'), /fixture\.env:1: expected KEY=VALUE/u)
})

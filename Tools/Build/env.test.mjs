import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv, parseEnvFile, resolveProjectEnvFile } from './env.mjs'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')

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
    assert.equal(environment.PCCONTROLLER_ENV_FILE, file)
    assert.equal(resolveProjectEnvFile(environment, { cwd: directory }), file)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('relative explicit files become absolute for children with another cwd', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-env-'))
  try {
    const child = join(directory, 'child')
    const file = join(directory, 'shared.env')
    writeFileSync(file, 'FROM_SHARED_ENV=yes\n', 'utf8')
    const environment = { PCCONTROLLER_ENV_FILE: 'shared.env' }
    const first = loadProjectEnv(environment, { cwd: directory })
    assert.equal(first.path, file)
    assert.equal(environment.PCCONTROLLER_ENV_FILE, file)
    assert.equal(resolveProjectEnvFile(environment, { cwd: child }), file)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('rejects malformed assignments with a source line', () => {
  assert.throws(() => parseEnvFile('NO_EQUALS\n', 'fixture.env'), /fixture\.env:1: expected KEY=VALUE/u)
})

test('rejects a missing explicitly selected environment file', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-env-'))
  try {
    const environment = { PCCONTROLLER_ENV_FILE: join(directory, 'missing.env') }
    assert.throws(() => loadProjectEnv(environment, { cwd: directory }), /explicit environment file does not exist/u)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('production Node entrypoints load the repository environment contract', () => {
  const entrypoints = new Set([
    'Tools/Build/product-metadata.mjs',
    'Tools/CommandPlan/controller-command.mjs',
    'Tools/Controller/web/vite.config.ts'
  ])
  const visit = (directory) => {
    for (const entry of readdirSync(resolve(repositoryRoot, directory), { withFileTypes: true })) {
      const relative = `${directory}/${entry.name}`
      if (entry.isDirectory()) {
        if (!['.build', '.cache', 'bin', 'dist', 'node_modules'].includes(entry.name)) visit(relative)
        continue
      }
      if (!entry.name.endsWith('.mjs') || entry.name.endsWith('.test.mjs')) continue
      const source = readFileSync(resolve(repositoryRoot, relative), 'utf8')
      if (source.startsWith('#!') || /process\.argv/u.test(source)) entrypoints.add(relative)
    }
  }
  visit('.github/scripts')
  visit('Tools')
  assert.ok(entrypoints.size > 20, 'expected to discover the project Node entrypoint surface')
  for (const entrypoint of [...entrypoints].sort()) {
    const source = readFileSync(resolve(repositoryRoot, entrypoint), 'utf8')
    assert.match(source, /import\s+\{[^}]*\bloadProjectEnv\b[^}]*\}/u, `${entrypoint} must import loadProjectEnv`)
    assert.match(source, /loadProjectEnv\(\)/u, `${entrypoint} must load .env before its main work`)
  }
})

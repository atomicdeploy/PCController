import assert from 'node:assert/strict'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'
import { spawnSync } from 'node:child_process'
import process from 'node:process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { repositoryRoot } from './env.mjs'

const bootstrap = fileURLToPath(new URL('./env-bootstrap.mjs', import.meta.url))

function run(directory, file, ...args) {
  const environment = { ...process.env, PCCONTROLLER_ENV_FILE: file }
  for (const name of ['HTTPS_PROXY', 'https_proxy']) delete environment[name]
  return spawnSync(process.execPath, [bootstrap, ...args], {
    cwd: directory,
    encoding: 'utf8',
    env: environment
  })
}

test('bootstrap title uses APP_NAME before dependency installation', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-bootstrap-'))
  try {
    const file = join(directory, '.env')
    writeFileSync(file, 'APP_NAME=Alpha Lab Console\n', 'utf8')
    const result = run(directory, file, 'title')
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), 'Alpha Lab Console')
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('bootstrap exports proxy values before npm can run', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-bootstrap-'))
  try {
    const file = join(directory, '.env')
    writeFileSync(file, 'HTTPS_PROXY=http://proxy.example:8080\n', 'utf8')
    const result = run(directory, file, 'print', 'HTTPS_PROXY')
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), 'http://proxy.example:8080')
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('bootstrap rejects unsafe or oversized batch-window titles', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-bootstrap-'))
  try {
    const file = join(directory, '.env')
    writeFileSync(file, 'APP_NAME=Alpha&echo injected\n', 'utf8')
    const result = run(directory, file, 'title')
    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), 'Alpha&echo injected')
    const cmd = readFileSync(new URL('../../build.cmd', import.meta.url), 'utf8')
    const firmware = readFileSync(new URL('../../firmware.cmd', import.meta.url), 'utf8')
    assert.match(cmd, /title "%PRODUCT_NAME% project-owned build and packaging"/u)
    assert.match(firmware, /title "%PRODUCT_NAME% AVR firmware studio"/u)
    assert.match(cmd, /setlocal EnableExtensions DisableDelayedExpansion/u)
    assert.match(firmware, /setlocal EnableExtensions DisableDelayedExpansion/u)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('bootstrap rejects a quote that could break out of the batch title argument', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-bootstrap-'))
  try {
    const file = join(directory, '.env')
    writeFileSync(file, 'APP_NAME=Alpha"&echo PCCTR_QUOTE_BREAKOUT&rem \n', 'utf8')
    const result = run(directory, file, 'title')
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /without double quotes/u)
    assert.doesNotMatch(result.stdout, /PCCTR_QUOTE_BREAKOUT/u)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('bootstrap resolves PATH npm without a shell and propagates env plus exact argv', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-bootstrap-'))
  try {
    const file = join(directory, '.env')
    const fakeBin = join(directory, 'npm & fake')
    const fakeCLI = join(fakeBin, 'node_modules', 'npm', 'bin', 'npm-cli.js')
    const capture = join(directory, 'capture.json')
    mkdirSync(join(fakeBin, 'node_modules', 'npm', 'bin'), { recursive: true })
    writeFileSync(fakeCLI, `const { writeFileSync } = require('node:fs')\nwriteFileSync(process.env.PCCONTROLLER_NPM_CAPTURE, JSON.stringify({ argv: process.argv.slice(2), proxy: process.env.HTTPS_PROXY }))\n`, 'utf8')
    if (process.platform === 'win32') {
      writeFileSync(join(fakeBin, 'npm.cmd'), '@exit /b 99\r\n', 'utf8')
    } else {
      const launcher = join(fakeBin, 'npm')
      writeFileSync(launcher, `#!/bin/sh\nexec "${process.execPath}" "${fakeCLI}" "$@"\n`, 'utf8')
      chmodSync(launcher, 0o755)
    }
    writeFileSync(file, 'HTTPS_PROXY=http://proxy.example:8080\n', 'utf8')
    const environment = {
      ...process.env,
      PATH: fakeBin,
      Path: fakeBin,
      PCCONTROLLER_ENV_FILE: file,
      PCCONTROLLER_NPM_CAPTURE: capture
    }
    delete environment.npm_execpath
    delete environment.https_proxy
    delete environment.HTTPS_PROXY
    const result = spawnSync(process.execPath, [bootstrap, 'install-build-dependencies'], {
      cwd: directory, encoding: 'utf8', env: environment
    })
    assert.equal(result.status, 0, result.stderr)
    assert.deepEqual(JSON.parse(readFileSync(capture, 'utf8')), {
      argv: ['--prefix', `${repositoryRoot}/Tools/Build`, 'ci', '--ignore-scripts', '--no-audit', '--no-fund'],
      proxy: 'http://proxy.example:8080'
    })
    const source = readFileSync(bootstrap, 'utf8')
    assert.match(source, /shell:\s*false/u)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('POSIX npm stays PATH-resolved when Node and npm use different prefixes', async () => {
  const { npmInvocation } = await import('./env-bootstrap.mjs')
  assert.deepEqual(npmInvocation({ PATH: '/custom/npm/bin' }, {
    platform: 'linux', node: '/usr/bin/node'
  }), { file: 'npm', prefix: [] })
  assert.equal(delimiter.length, 1)
})

test('clean-checkout wrappers bootstrap dependencies through the env loader', () => {
  const root = new URL('../../', import.meta.url)
  for (const relative of ['build.cmd', 'build.sh', 'update-dependencies.cmd', 'update-dependencies.sh']) {
    const source = readFileSync(new URL(relative, root), 'utf8')
    assert.match(source, /env-bootstrap\.mjs["']?\s+install-build-dependencies/u, relative)
  }
})

#!/usr/bin/env node

// Dependency-free launcher used before Tools/Build/node_modules exists. It
// makes the repository .env contract available to dependency installation and
// to the first visible launcher title, not only after the main tool starts.

import { existsSync, readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import process from 'node:process'
import { delimiter, dirname, extname, join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { loadProjectEnv, repositoryRoot } from './env.mjs'

loadProjectEnv()

function environmentValue(name) {
  for (const [key, value] of Object.entries(process.env)) {
    if (key.toLowerCase() === name.toLowerCase()) return String(value ?? '').trim()
  }
  return ''
}

export function bootstrapTitle(environment = process.env) {
  const lookup = (name) => {
    for (const [key, value] of Object.entries(environment)) {
      if (key.toLowerCase() === name.toLowerCase()) return String(value ?? '').trim()
    }
    return ''
  }
  const configured = lookup('PCCONTROLLER_BUILD_APP_NAME') || lookup('APP_NAME') || lookup('APP_TITLE')
  if (configured) {
    if ([...configured].length > 64 || configured.includes('"') || [...configured].some(character => /\p{Cc}/u.test(character))) {
      throw new Error('application title must be 1..64 printable characters without double quotes')
    }
    return configured
  }
  const metadata = JSON.parse(readFileSync(new URL('../Controller/web/package.json', import.meta.url), 'utf8'))
  return String(metadata.productName || metadata.name || 'Controller').trim() || 'Controller'
}

function environmentValueFrom(environment, wanted) {
  for (const [name, value] of Object.entries(environment)) {
    if (name.toLowerCase() === wanted.toLowerCase()) return String(value ?? '')
  }
  return ''
}

export function npmInvocation(environment = process.env, {
  platform = process.platform,
  node = process.execPath
} = {}) {
  // POSIX npm is an executable script with a shebang and is safe to resolve
  // through PATH without a shell. Distro packages commonly install it outside
  // the Node prefix, so do not assume a node-adjacent npm-cli.js there.
  if (platform !== 'win32') return { file: 'npm', prefix: [] }

  const explicit = environmentValueFrom(environment, 'npm_execpath').trim()
  const pathDirectories = environmentValueFrom(environment, 'PATH').split(delimiter).filter(Boolean)
  const launchers = pathDirectories.flatMap(directory => [
    join(directory, 'npm.cmd'), join(directory, 'npm.exe'), join(directory, 'npm.bat')
  ])
  const launcher = launchers.find(candidate => existsSync(candidate))
  if (launcher && extname(launcher).toLowerCase() === '.exe') return { file: launcher, prefix: [] }

  const candidates = [
    explicit,
    launcher && join(dirname(launcher), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
    join(dirname(node), 'node_modules', 'npm', 'bin', 'npm-cli.js')
  ].filter(candidate => candidate && extname(candidate).toLowerCase() === '.js')
  const cli = candidates.find(candidate => existsSync(candidate))
  if (!cli) throw new Error('npm-cli.js could not be resolved from npm on PATH; install Node.js with npm')
  return { file: node, prefix: [cli] }
}

export function installBuildDependencies({
  environment = process.env,
  platform = process.platform,
  node = process.execPath,
  root = repositoryRoot,
  spawn = spawnSync
} = {}) {
  const npm = npmInvocation(environment, { platform, node })
  const args = [
    ...npm.prefix, '--prefix', `${root}/Tools/Build`, 'ci', '--ignore-scripts', '--no-audit', '--no-fund'
  ]
  const result = spawn(npm.file, args, {
    cwd: root,
    env: environment,
    stdio: 'inherit',
    shell: false,
    windowsHide: true
  })
  if (result.error) throw new Error(`start npm: ${result.error.message}`)
  return result.status ?? 1
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [command, ...args] = process.argv.slice(2)
  switch (command) {
  case 'title':
    process.stdout.write(`${bootstrapTitle()}\n`)
    break
  case 'install-build-dependencies':
    process.exitCode = installBuildDependencies()
    break
  case 'print': {
    const name = args[0]
    if (!name) throw new Error('print requires an environment variable name')
    process.stdout.write(`${environmentValue(name)}\n`)
    break
  }
  case 'npm-cli':
    process.stdout.write(`${JSON.stringify(npmInvocation())}\n`)
    break
  default:
    process.stderr.write('Usage: env-bootstrap.mjs title | install-build-dependencies | print NAME | npm-cli\n')
    process.exitCode = 2
  }
}

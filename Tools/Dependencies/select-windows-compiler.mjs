#!/usr/bin/env node
// Select or provision the exact native Windows compiler from the reviewed host lock.

import { spawnSync } from 'node:child_process'
import { appendFileSync, existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from '../Build/env.mjs'

loadProjectEnv()

const here = dirname(fileURLToPath(import.meta.url))
const repository = resolve(here, '..', '..')
const lock = JSON.parse(readFileSync(join(here, 'resolved-tools-lock.json'), 'utf8'))
const compiler = lock.windows_c_compiler

function invariant(condition, message) {
  if (!condition) throw new Error(message)
}

function environmentValue(name) {
  const key = Object.keys(process.env).find((candidate) => candidate.toLowerCase() === name.toLowerCase())
  return key ? process.env[key] : ''
}

function commandPaths(name) {
  const result = spawnSync('where.exe', [name], { encoding: 'utf8', windowsHide: true })
  if (result.status !== 0) return []
  return result.stdout.split(/\r?\n/u).map((value) => value.trim()).filter(Boolean)
}

function compilerCandidates() {
  const candidates = [
    ...commandPaths('x86_64-w64-mingw32-gcc.exe'),
    ...commandPaths('gcc.exe'),
    'C:\\mingw64\\bin\\gcc.exe',
  ]
  const localAppData = environmentValue('LOCALAPPDATA')
  const packageRoot = localAppData && join(localAppData, 'Microsoft', 'WinGet', 'Packages')
  if (packageRoot && existsSync(packageRoot)) {
    for (const entry of readdirSync(packageRoot, { withFileTypes: true })) {
      if (!entry.isDirectory() || !entry.name.toLowerCase().startsWith(`${compiler.package_id}_`.toLowerCase())) continue
      candidates.push(join(packageRoot, entry.name, 'mingw64', 'bin', 'gcc.exe'))
    }
  }
  return [...new Set(candidates.map((candidate) => resolve(candidate)))].filter((candidate) => existsSync(candidate))
}

function probe(candidate) {
  const options = { encoding: 'utf8', windowsHide: true, timeout: 10_000 }
  const version = spawnSync(candidate, ['-dumpfullversion'], options)
  const target = spawnSync(candidate, ['-dumpmachine'], options)
  if (version.status !== 0 || target.status !== 0) return null
  return { candidate, version: version.stdout.trim(), target: target.stdout.trim() }
}

function selectLockedCompiler() {
  for (const candidate of compilerCandidates()) {
    const identity = probe(candidate)
    if (!identity) continue
    process.stdout.write(`Examined Windows C compiler ${candidate} (${identity.version} / ${identity.target}).\n`)
    if (identity.version === compiler.compiler_version && identity.target === compiler.target) return candidate
  }
  return ''
}

function provisionLockedCompiler() {
  const winget = commandPaths('winget.exe')[0]
  invariant(winget, `locked Windows C compiler ${compiler.compiler_version} is absent and Windows Package Manager is unavailable`)
  const args = [
    'install', '--id', compiler.package_id, '--exact', '--source', 'winget',
    '--scope', 'user', '--architecture', 'x64', '--version', compiler.package_version,
    '--silent', '--accept-package-agreements', '--accept-source-agreements', '--disable-interactivity',
  ]
  const proxy = environmentValue('HTTPS_PROXY') || environmentValue('HTTP_PROXY') || environmentValue('ALL_PROXY')
  if (proxy) args.push('--proxy', proxy)
  process.stdout.write(`Provisioning locked Windows C compiler package ${compiler.package_id} ${compiler.package_version}.\n`)
  const result = spawnSync(winget, args, { env: process.env, stdio: 'inherit', windowsHide: true, timeout: 10 * 60_000 })
  invariant(result.status === 0, `Windows Package Manager failed to provision ${compiler.package_id} ${compiler.package_version}`)
}

function main() {
  invariant(process.platform === 'win32', 'the Windows compiler selector only runs on Windows')
  invariant(compiler?.package_id && compiler.package_version && compiler.compiler_version && compiler.target,
    'the resolved host-tool lock has no complete Windows C compiler identity')
  invariant(resolve(repository, 'Tools', 'Dependencies', 'resolved-tools-lock.json') === join(here, 'resolved-tools-lock.json'),
    'the compiler selector did not resolve the canonical host-tool lock')

  let selected = selectLockedCompiler()
  if (!selected) {
    provisionLockedCompiler()
    selected = selectLockedCompiler()
  }
  invariant(selected, `locked Windows C compiler ${compiler.compiler_version} / ${compiler.target} was not discoverable after provisioning`)

  const githubPath = environmentValue('GITHUB_PATH')
  const githubEnv = environmentValue('GITHUB_ENV')
  invariant(Boolean(githubPath) === Boolean(githubEnv),
    'GitHub Actions compiler exports require both GITHUB_PATH and GITHUB_ENV')
  if (githubPath && githubEnv) {
    appendFileSync(githubPath, `${dirname(selected)}\n`)
    appendFileSync(githubEnv, `CC=${selected}\n`)
  }
  process.stdout.write(`Selected locked Windows C compiler: ${selected}\n`)
  const version = spawnSync(selected, ['--version'], { encoding: 'utf8', windowsHide: true })
  invariant(version.status === 0, 'the selected compiler failed its final version check')
  process.stdout.write(version.stdout)
}

try {
  main()
} catch (error) {
  console.error(`Windows compiler selection failed: ${error.message}`)
  process.exitCode = 1
}

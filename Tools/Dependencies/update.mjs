#!/usr/bin/env node
// Resolve, update, and validate every source-controlled PCController dependency without device I/O.

import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  statSync,
  writeFileSync,
} from 'node:fs'
import { arch, platform } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createChalk, renderUnicodeTable } from '../Build/presentation.mjs'
import { PRODUCT_METADATA, resolveProductTitle } from '../Build/product-metadata.mjs'
import { configuredProxyNames, withDirectFallback } from './network.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '..', '..')
const controller = join(repo, 'Tools', 'Controller')
const web = join(controller, 'web')
const build = join(repo, 'Tools', 'Build')
const policyPath = join(here, 'dependency-policy.json')
const toolsLockPath = join(here, 'resolved-tools-lock.json')
const toolchainPolicyPath = join(controller, 'toolchain-profile.json')
const toolchainLockPath = join(controller, 'toolchain-lock.json')
const buildReportDir = join(repo, '.build', 'dependencies')
const workflowsDirectory = join(repo, '.github', 'workflows')
const defaultReportPath = join(buildReportDir, 'update-report.json')
const policy = JSON.parse(readFileSync(policyPath, 'utf8'))
let cachedGitHubEnvironment

const chalk = createChalk({
  noColor: Boolean(process.env.NO_COLOR) || process.env.TERM === 'dumb',
  forceColor: Boolean(process.env.FORCE_COLOR),
})
const productTitle = resolveProductTitle(process.env)
const productAgent = PRODUCT_METADATA.productName.replace(/[^0-9A-Za-z._-]+/gu, '-') || 'Controller'

// Network and repository identities are code-reviewed here rather than taken
// on trust from a mutable policy, registry response, or redirect field.
const trustedRepositories = new Set([
  'actions/attest-build-provenance',
  'actions/checkout',
  'actions/download-artifact',
  'actions/github-script',
  'actions/setup-go',
  'actions/setup-node',
  'actions/upload-artifact',
  'arduino/arduino-cli',
  'brechtsanders/winlibs_mingw',
  'egor-tensin/setup-mingw',
  'github/codeql-action',
  'mcudude/minicore',
  'microsoft/winget-pkgs',
  'peter-evans/create-pull-request',
  'softprops/action-gh-release',
  'stefanrueger/urboot',
  'tc-hib/go-winres',
  'upx/upx',
])

const trustedArduinoLibraryPaths = new Map([
  ['Adafruit PWM Servo Driver Library', '/libraries/github.com/adafruit/Adafruit_PWM_Servo_Driver_Library-'],
  ['Adafruit INA219', '/libraries/github.com/adafruit/Adafruit_INA219-'],
  ['rc-switch', '/libraries/github.com/sui77/rc_switch-'],
  ['TM1637TinyDisplay', '/libraries/github.com/jasonacox/TM1637TinyDisplay-'],
  ['DallasTemperature', '/libraries/github.com/milesburton/DallasTemperature-'],
  ['OneWire', '/libraries/github.com/PaulStoffregen/OneWire-'],
])

function normalizeRepository(value) {
  const repository = String(value ?? '').trim().replace(/\.git$/iu, '')
  if (!/^[0-9A-Za-z_.-]+\/[0-9A-Za-z_.-]+$/u.test(repository)) return ''
  return repository.toLowerCase()
}

function assertTrustedRepository(value, label = 'dependency repository') {
  const repository = normalizeRepository(value)
  if (!repository || !trustedRepositories.has(repository)) {
    throw new Error(`${label} is not code-review allowlisted: ${value}`)
  }
  return repository
}

function githubRepositoryFromURL(value) {
  const parsed = value instanceof URL ? value : new URL(value)
  const host = parsed.hostname.toLowerCase()
  const parts = parsed.pathname.split('/').filter(Boolean)
  if (host === 'api.github.com' && parts[0]?.toLowerCase() === 'repos') {
    return normalizeRepository(`${parts[1] ?? ''}/${parts[2] ?? ''}`)
  }
  if (host === 'github.com' || host === 'raw.githubusercontent.com') {
    return normalizeRepository(`${parts[0] ?? ''}/${parts[1] ?? ''}`)
  }
  return ''
}

function assertTrustedDependencyURL(value, label = 'dependency source') {
  let parsed
  try {
    parsed = new URL(String(value ?? ''))
  } catch {
    throw new Error(`${label} is not a valid URL: ${value}`)
  }
  if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.port) {
    throw new Error(`${label} must use credential-free HTTPS on its default port: ${value}`)
  }
  const host = parsed.hostname.toLowerCase()
  const path = parsed.pathname
  let trusted = false
  if (host === 'api.github.com' || host === 'github.com' || host === 'raw.githubusercontent.com') {
    const repository = githubRepositoryFromURL(parsed)
    trusted = Boolean(repository && trustedRepositories.has(repository))
  } else if (host === 'downloads.arduino.cc') {
    trusted = path === '/libraries/library_index.json.gz' || path.startsWith('/libraries/github.com/')
  } else if (host === 'mcudude.github.io') {
    trusted = path.startsWith('/MiniCore/')
  } else if (host === 'nodejs.org') {
    trusted = path === '/dist/index.json' || path.startsWith('/dist/v')
  } else if (host === 'go.dev') {
    trusted = path === '/VERSION'
  }
  if (!trusted) throw new Error(`${label} is outside the code-review source allowlist: ${value}`)
  return parsed.href
}

function assertTrustedGitHubURL(value, repository, label = 'GitHub dependency source') {
  const trustedURL = assertTrustedDependencyURL(value, label)
  const expected = assertTrustedRepository(repository, `${label} repository`)
  const actual = githubRepositoryFromURL(trustedURL)
  if (actual !== expected) {
    throw new Error(`${label} redirected to untrusted repository ${actual || '<missing>'}; expected ${expected}`)
  }
  return trustedURL
}

function validateToolchainSourcePolicy(value = readJSON(toolchainPolicyPath)) {
  if (!value) throw new Error('toolchain source policy is missing')
  const cliRepository = assertTrustedRepository(value.cli?.repository, 'firmware CLI repository')
  if (cliRepository !== 'arduino/arduino-cli') throw new Error('firmware CLI repository must be arduino/arduino-cli')
  assertTrustedGitHubURL(value.cli?.release_api, cliRepository, 'firmware CLI release API')
  if (value.core?.id !== 'MiniCore:avr' ||
      assertTrustedDependencyURL(value.core?.index_url, 'MiniCore package index') !==
        'https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json') {
    throw new Error('MiniCore must use its official package and package index')
  }
  if (assertTrustedDependencyURL(value.library_index, 'Arduino library index') !==
      'https://downloads.arduino.cc/libraries/library_index.json.gz') {
    throw new Error('Arduino libraries must use the official library index')
  }
  const bootloaderRepository = assertTrustedRepository(value.bootloader?.repository, 'bootloader repository')
  if (bootloaderRepository !== 'stefanrueger/urboot') throw new Error('bootloader repository must be stefanrueger/urboot')
  assertTrustedGitHubURL(value.bootloader?.tags_api, bootloaderRepository, 'bootloader tags API')
  assertTrustedGitHubURL(value.bootloader?.commits_api, bootloaderRepository, 'bootloader commits API')
  if (assertTrustedDependencyURL(value.go?.version_url, 'Go version source') !== 'https://go.dev/VERSION?m=text') {
    throw new Error('Go versions must use the official version source')
  }
  return value
}

function validateToolchainLockSources(lock = readJSON(toolchainLockPath)) {
  if (!lock) throw new Error('toolchain lock is missing')
  for (const asset of lock.firmware?.cli?.assets ?? []) {
    assertTrustedGitHubURL(asset.url, 'arduino/arduino-cli', `firmware CLI asset ${asset.goos ?? ''}/${asset.goarch ?? ''}`)
  }
  for (const source of lock.firmware?.package_indexes ?? []) {
    if (assertTrustedDependencyURL(source, 'locked MiniCore package index') !==
        'https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json') {
      throw new Error('locked MiniCore package index is not official')
    }
  }
  const coreURL = assertTrustedDependencyURL(lock.core_source?.url, 'locked MiniCore archive')
  if (!coreURL.startsWith('https://mcudude.github.io/MiniCore/MiniCore-')) {
    throw new Error('locked MiniCore archive is not official')
  }
  for (const library of lock.libraries ?? []) {
    const expectedPath = trustedArduinoLibraryPaths.get(library.name)
    const parsed = new URL(assertTrustedDependencyURL(library.url, `${library.name} archive`))
    if (!expectedPath || !parsed.pathname.startsWith(expectedPath)) {
      throw new Error(`${library.name} archive is not code-review allowlisted`)
    }
  }
  const bootloader = assertTrustedRepository(lock.bootloader?.repository, 'locked bootloader repository')
  if (bootloader !== 'stefanrueger/urboot') throw new Error('locked bootloader repository is not official')
  return lock
}

function validateHostSourcePolicy(value = policy) {
  if (assertTrustedDependencyURL(value.node?.index_url, 'Node.js release index') !== 'https://nodejs.org/dist/index.json') {
    throw new Error('Node.js must use its official release index')
  }
  const checksumTemplate = String(value.node?.checksums_url_template ?? '').replace('{version}', '0.0.0')
  assertTrustedDependencyURL(checksumTemplate, 'Node.js checksum source')
  const upxRepository = assertTrustedRepository(value.upx?.repository, 'UPX repository')
  if (upxRepository !== 'upx/upx') throw new Error('UPX repository must be upx/upx')
  assertTrustedGitHubURL(value.upx?.release_api, upxRepository, 'UPX release API')
  const compilerAPI = assertTrustedGitHubURL(
    value.windows_c_compiler?.manifest_api,
    'microsoft/winget-pkgs',
    'WinGet compiler manifest API',
  )
  if (!new URL(compilerAPI).pathname.startsWith('/repos/microsoft/winget-pkgs/contents/manifests/')) {
    throw new Error('WinGet compiler manifest must stay inside the reviewed manifest tree')
  }
  if (value.go_winres?.module !== 'github.com/tc-hib/go-winres') {
    throw new Error('go-winres must use the official module repository')
  }
  return value
}

function parseArgs(argv) {
  const options = {
    mode: 'check', validate: false, requireCurrent: false,
    report: defaultReportPath, directRetry: true,
  }
  for (let index = 0; index < argv.length; index++) {
    const argument = argv[index]
    if (argument === '--check') options.mode = 'check'
    else if (argument === '--apply') options.mode = 'apply'
    else if (argument === '--validate') options.validate = true
    else if (argument === '--require-current') options.requireCurrent = true
    else if (argument === '--no-direct-retry') options.directRetry = false
    else if (argument === '--report') {
      if (!argv[index + 1]) throw new Error('--report requires a JSON path')
      options.report = resolve(repo, argv[++index])
    } else if (argument === '--help' || argument === '-h') {
      console.log(`${chalk.bold.cyan(`${productTitle} dependency updater`)}

  update-dependencies.cmd --check [--require-current]
  update-dependencies.cmd --apply [--validate]

Options:
  --check             Resolve and report; never modify source-controlled files
  --apply             Update exact locks, Go modules, and compatible npm packages
  --validate          Run firmware, Urboot-Custom, VirtualBoard, Go, web, and package tests
  --require-current   Return a failure when check mode finds an update
  --report FILE       Machine-readable report path (default: .build/dependencies/update-report.json)
  --no-direct-retry   Do not retry failed registry reads without proxy variables`)
      process.exit(0)
    } else {
      throw new Error(`unknown option ${argument}`)
    }
  }
  return options
}

function log(icon, label, message, color = chalk.cyan) {
  console.log(`${icon} ${color(chalk.bold(label))} ${message}`)
}

function commandText(file, args) {
  return [file, ...args].map((part) => /\s/.test(part) ? JSON.stringify(part) : part).join(' ')
}

function run(file, args, options = {}) {
  const result = spawnSync(file, args, {
    cwd: options.cwd ?? repo,
    env: options.env ?? process.env,
    encoding: 'utf8',
    windowsHide: true,
    stdio: options.capture === false ? 'inherit' : ['ignore', 'pipe', 'pipe'],
  })
  const accepted = options.accept ?? [0]
  if (!accepted.includes(result.status)) {
    const output = `${result.stdout ?? ''}${result.stderr ?? ''}`.trim()
    throw new Error(`${commandText(file, args)} failed (${result.status ?? result.error?.message ?? 'unknown'}):\n${output}`)
  }
  return { status: result.status, stdout: result.stdout ?? '', stderr: result.stderr ?? '' }
}

function runNetwork(file, args, options = {}, directRetry = true) {
  return withDirectFallback((environment) => run(file, args, { ...options, env: environment }), {
    environment: options.env ?? process.env,
    directRetry,
  }).value
}

function githubEnvironment() {
  if (cachedGitHubEnvironment) return cachedGitHubEnvironment
  if (process.env.GITHUB_TOKEN || process.env.GH_TOKEN) {
    cachedGitHubEnvironment = process.env
    return cachedGitHubEnvironment
  }
  const executable = platform() === 'win32' ? 'gh.exe' : 'gh'
  const credential = spawnSync(executable, ['auth', 'token'], {
    encoding: 'utf8', windowsHide: true, env: process.env,
    stdio: ['ignore', 'pipe', 'ignore'],
  })
  const token = credential.status === 0 ? credential.stdout.trim() : ''
  cachedGitHubEnvironment = token ? { ...process.env, GH_TOKEN: token } : process.env
  return cachedGitHubEnvironment
}

function curlJSON(url, directRetry = true) {
  const trustedURL = assertTrustedDependencyURL(url)
  const parsed = new URL(trustedURL)
  const authenticatedEnvironment = githubEnvironment()
  const haveToken = Boolean(authenticatedEnvironment.GITHUB_TOKEN || authenticatedEnvironment.GH_TOKEN)
  if (parsed.hostname.toLowerCase() === 'api.github.com' && haveToken) {
    const executable = platform() === 'win32' ? 'gh.exe' : 'gh'
    const endpoint = `${parsed.pathname}${parsed.search}`
    const authenticated = spawnSync(executable, ['api', '--method', 'GET', endpoint], {
      encoding: 'utf8', windowsHide: true, env: authenticatedEnvironment,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    if (authenticated.status === 0) return JSON.parse(authenticated.stdout)
  }
  return JSON.parse(curlText(trustedURL, directRetry, 'application/vnd.github+json, application/json'))
}

function curlText(url, directRetry = true, accept = 'text/plain, application/octet-stream') {
  const trustedURL = assertTrustedDependencyURL(url)
  const executable = platform() === 'win32' ? 'curl.exe' : 'curl'
  const base = ['--fail', '--silent', '--show-error', '--location',
    '--proto', '=https', '--proto-redir', '=https',
    '--header', `Accept: ${accept}`,
    '--header', `User-Agent: ${productAgent}-dependency-updater/1`, trustedURL]
  return withDirectFallback((environment, direct) => {
    const args = direct ? ['--noproxy', '*', ...base] : base
    const result = run(executable, args, { env: environment })
    return result.stdout
  }, { directRetry }).value
}

function sha256File(path) {
  return createHash('sha256').update(readFileSync(path)).digest('hex')
}

function stableParts(value) {
  const normalized = String(value).trim().replace(/^go|^v|^u/, '')
  if (!/^\d+(?:\.\d+){1,3}$/.test(normalized)) return null
  return normalized.split('.').map(Number)
}

function compareVersions(left, right) {
  const a = stableParts(left)
  const b = stableParts(right)
  if (!a || !b) return String(left).localeCompare(String(right))
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const delta = (a[index] ?? 0) - (b[index] ?? 0)
    if (delta) return Math.sign(delta)
  }
  return 0
}

function compareCompositeVersions(left, right) {
  const a = String(left).match(/\d+/gu)?.map(Number) ?? []
  const b = String(right).match(/\d+/gu)?.map(Number) ?? []
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const delta = (a[index] ?? 0) - (b[index] ?? 0)
    if (delta) return Math.sign(delta)
  }
  return String(left).localeCompare(String(right))
}

function parseWingetCompilerManifest(text, architecture, target, manifest = {}) {
  const packageID = text.match(/^PackageIdentifier:\s*(\S+)\s*$/mu)?.[1] ?? ''
  const packageVersion = text.match(/^PackageVersion:\s*(\S+)\s*$/mu)?.[1] ?? ''
  const blocks = text.split(/^[ \t]*- Architecture:[ \t]*/gmu).slice(1)
  const block = blocks.find((value) => value.split(/\r?\n/u, 1)[0].trim() === architecture)
  const installerURL = block?.match(/^[ \t]*InstallerUrl:[ \t]*(https:\/\/\S+)[ \t]*$/mu)?.[1] ?? ''
  const installerSHA256 = block?.match(/^[ \t]*InstallerSha256:[ \t]*([0-9A-Fa-f]{64})[ \t]*$/mu)?.[1]?.toLowerCase() ?? ''
  const compilerVersion = packageVersion.match(/^\d+(?:\.\d+){2}/u)?.[0] ?? ''
  if (!packageID || !packageVersion || !compilerVersion || !installerURL || !installerSHA256) {
    throw new Error(`WinGet compiler manifest is incomplete for architecture ${architecture}`)
  }
  return {
    package_id: packageID,
    package_version: packageVersion,
    compiler: 'gcc',
    compiler_version: compilerVersion,
    architecture,
    target,
    provenance: 'winget-community-manifest',
    manifest_url: manifest.url ?? '',
    manifest_git_sha: manifest.sha ?? '',
    installer_url: installerURL,
    installer_sha256: installerSHA256,
  }
}

function resolveWindowsCCompiler(directRetry) {
  const compilerPolicy = policy.windows_c_compiler
  const versions = curlJSON(compilerPolicy.manifest_api, directRetry)
    .filter((entry) => entry.type === 'dir' && /^\d+(?:\.\d+){2}-\d+(?:\.\d+){2}-r\d+$/u.test(entry.name))
    .sort((left, right) => compareCompositeVersions(right.name, left.name))
  if (!versions.length) throw new Error('WinGet registry has no stable native Windows C compiler release')
  const filesURL = assertTrustedGitHubURL(
    versions[0].url,
    'microsoft/winget-pkgs',
    'WinGet version directory API',
  )
  const files = curlJSON(filesURL, directRetry)
  const installer = files.find((entry) => entry.type === 'file' && /\.installer\.yaml$/u.test(entry.name))
  if (!installer?.download_url || !/^[0-9a-f]{40}$/u.test(installer.sha ?? '')) {
    throw new Error(`WinGet compiler ${versions[0].name} has no immutable installer manifest identity`)
  }
  const manifestURL = assertTrustedGitHubURL(
    installer.download_url,
    'microsoft/winget-pkgs',
    'WinGet compiler installer manifest',
  )
  const resolved = parseWingetCompilerManifest(
    curlText(manifestURL, directRetry),
    compilerPolicy.architecture,
    compilerPolicy.target,
    { url: manifestURL, sha: installer.sha },
  )
  assertTrustedGitHubURL(resolved.installer_url, 'brechtsanders/winlibs_mingw', 'Windows compiler installer')
  if (resolved.package_id !== compilerPolicy.package_id ||
      compareVersions(resolved.compiler_version, compilerPolicy.minimum_compiler_version) < 0) {
    throw new Error(`WinGet compiler resolution returned incompatible ${resolved.package_id}@${resolved.package_version}`)
  }
  return resolved
}

function substantive(value) {
  const copy = structuredClone(value)
  delete copy.resolved_at_utc
  return copy
}

function sameSubstantive(left, right) {
  return JSON.stringify(substantive(left)) === JSON.stringify(substantive(right))
}

function readJSON(path, fallback = null) {
  return existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : fallback
}

function workflowActionInventory() {
  const actions = new Map()
  for (const file of readdirSync(workflowsDirectory).filter((name) => /\.ya?ml$/iu.test(name)).sort()) {
    const content = readFileSync(join(workflowsDirectory, file), 'utf8')
    for (const match of content.matchAll(/^\s*uses:\s*([^@\s]+)@([^\s#]+)(?:\s*#\s*(.+))?$/gmu)) {
      const name = match[1]
      if (name.startsWith('./')) continue
      const revision = match[2]
      const version = String(match[3] ?? '').trim()
      if (!/^[0-9a-f]{40}$/iu.test(revision)) throw new Error(`${file}: ${name} must use an immutable 40-character revision`)
      if (!/^v\d+(?:\b|\.)/u.test(version)) throw new Error(`${file}: ${name}@${revision} needs a readable major-version comment`)
      const existing = actions.get(name)
      if (existing && existing.revision !== revision) {
        throw new Error(`${name} uses conflicting immutable revisions ${existing.revision} and ${revision}`)
      }
      const value = existing ?? { name, revision: revision.toLowerCase(), version, workflows: [] }
      if (!value.workflows.includes(file)) value.workflows.push(file)
      actions.set(name, value)
    }
  }
  return [...actions.values()].sort((left, right) => left.name.localeCompare(right.name))
}

function validateHostToolsLock(lock) {
  if (lock?.format !== 'pccontroller-host-tool-lock/v1') throw new Error('unsupported host tool lock format')
  if (!stableParts(lock?.node?.version) || !String(lock.node.lts ?? '').trim()) throw new Error('host tool lock has no stable Node.js LTS identity')
  if (!/^https:\/\//u.test(lock.node.source ?? '') || !/^[0-9a-f]{64}$/u.test(lock.node.checksums_sha256 ?? '') || !Array.isArray(lock.node.assets) || !lock.node.assets.length) {
    throw new Error('host tool lock has no checksum-complete Node.js distribution identity')
  }
  for (const asset of lock.node.assets) {
    if (!/^https:\/\//u.test(asset.url ?? '') || !/^[0-9a-f]{64}$/u.test(asset.sha256 ?? '')) throw new Error(`invalid Node.js asset identity ${asset.name ?? '<missing>'}`)
    assertTrustedDependencyURL(asset.url, `Node.js asset ${asset.name ?? '<missing>'}`)
  }
  if (!stableParts(lock?.upx?.version) || !Array.isArray(lock.upx.assets) || !lock.upx.assets.length) throw new Error('host tool lock has no stable UPX identity')
  for (const asset of lock.upx.assets) {
    if (!/^https:\/\//u.test(asset.url ?? '') || !/^[0-9a-f]{64}$/u.test(asset.sha256 ?? '')) throw new Error(`invalid UPX asset identity ${asset.name ?? '<missing>'}`)
    assertTrustedGitHubURL(asset.url, 'upx/upx', `UPX asset ${asset.name ?? '<missing>'}`)
  }
  if (!stableParts(lock?.go_winres?.version) || !String(lock.go_winres.sum ?? '').startsWith('h1:') || !String(lock.go_winres.go_mod_sum ?? '').startsWith('h1:')) {
    throw new Error('host tool lock has no checksum-complete go-winres identity')
  }
  if (lock.go_winres.module !== 'github.com/tc-hib/go-winres') throw new Error('host tool lock uses an untrusted go-winres repository')
  const compiler = lock?.windows_c_compiler
  if (!compiler?.package_id || !compiler.package_version || !stableParts(compiler.compiler_version) ||
      !/^x86_64-.*(?:mingw(?:32|64)?|windows-gnu)$/iu.test(compiler.target ?? '') ||
      compiler.provenance !== 'winget-community-manifest' ||
      !/^https:\/\//u.test(compiler.manifest_url ?? '') || !/^[0-9a-f]{40}$/u.test(compiler.manifest_git_sha ?? '') ||
      !/^https:\/\//u.test(compiler.installer_url ?? '') || !/^[0-9a-f]{64}$/u.test(compiler.installer_sha256 ?? '')) {
    throw new Error('host tool lock has no checksum-complete native Windows C compiler identity')
  }
  assertTrustedGitHubURL(compiler.manifest_url, 'microsoft/winget-pkgs', 'locked Windows compiler manifest')
  assertTrustedGitHubURL(compiler.installer_url, 'brechtsanders/winlibs_mingw', 'locked Windows compiler installer')
  if (!/^[0-9a-f]{64}$/u.test(lock?.web?.package_lock_sha256 ?? '') || !/^[0-9a-f]{64}$/u.test(lock?.build?.package_lock_sha256 ?? '')) {
    throw new Error('host tool lock has incomplete npm lock hashes')
  }
  if (!Array.isArray(lock?.github_actions?.actions) || !lock.github_actions.actions.length) throw new Error('host tool lock has no immutable GitHub Action inventory')
  for (const action of lock.github_actions.actions) {
    if (!action.name || !/^[0-9a-f]{40}$/u.test(action.revision ?? '') || !/^v\d+/u.test(action.version ?? '')) {
      throw new Error(`invalid GitHub Action identity ${action.name ?? '<missing>'}`)
    }
    assertTrustedRepository(action.name.split('/').slice(0, 2).join('/'), `GitHub Action ${action.name}`)
  }
  return lock
}

function compareHostToolLocks(current, resolved) {
  if (!current) return [
    { area: 'host-tools', name: 'host tool lock', current: '<missing>', resolved: 'latest stable identities' },
  ]
  const changes = []
  const fingerprint = (value) => createHash('sha256').update(JSON.stringify(value ?? null)).digest('hex')
  const compare = (name, currentValue, resolvedValue, area = 'host-tools') => {
    if (JSON.stringify(currentValue) !== JSON.stringify(resolvedValue)) {
      changes.push({ area, name, current: String(currentValue ?? '<missing>'), resolved: String(resolvedValue ?? '<missing>') })
    }
  }
  compare('Node.js LTS', current.node?.version, resolved.node.version)
  compare('Node.js LTS channel', current.node?.lts, resolved.node.lts)
  compare('Node.js distribution inventory', fingerprint([current.node?.source, current.node?.checksums_sha256, current.node?.assets]), fingerprint([resolved.node.source, resolved.node.checksums_sha256, resolved.node.assets]))
  compare('UPX', current.upx?.version, resolved.upx.version)
  compare('UPX release tag', current.upx?.tag, resolved.upx.tag)
  compare('UPX asset inventory', fingerprint(current.upx?.assets), fingerprint(resolved.upx.assets))
  compare('go-winres', current.go_winres?.version, resolved.go_winres.version)
  compare('go-winres module', current.go_winres?.module, resolved.go_winres.module)
  compare('go-winres checksums', fingerprint([current.go_winres?.sum, current.go_winres?.go_mod_sum]), fingerprint([resolved.go_winres.sum, resolved.go_winres.go_mod_sum]))
  compare('Windows C compiler package', current.windows_c_compiler?.package_version, resolved.windows_c_compiler.package_version)
  compare('Windows C compiler identity', fingerprint(current.windows_c_compiler), fingerprint(resolved.windows_c_compiler))
  compare('web package lock', current.web?.package_lock_sha256, resolved.web.package_lock_sha256, 'host-lock')
  compare('build package lock', current.build?.package_lock_sha256, resolved.build.package_lock_sha256, 'host-lock')
  compare('GitHub Actions', fingerprint(current.github_actions?.actions), fingerprint(resolved.github_actions.actions), 'github-actions')
  compare('GitHub Actions updater', current.github_actions?.managed_by, resolved.github_actions.managed_by, 'github-actions')
  return changes
}

function writeJSONAtomic(path, value) {
  mkdirSync(dirname(path), { recursive: true })
  const temporary = `${path}.tmp`
  writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
  renameSync(temporary, path)
}

function resolveHostTools(directRetry) {
  validateHostSourcePolicy()
  const nodeIndex = curlJSON(policy.node.index_url, directRetry)
  const lts = nodeIndex.find((entry) => entry.lts && stableParts(entry.version))
  if (!lts || compareVersions(lts.version, policy.node.minimum_version) < 0) {
    throw new Error('Node registry has no compatible current LTS release')
  }
  const nodeVersion = lts.version.replace(/^v/u, '')
  const nodeChecksumsURL = policy.node.checksums_url_template.replace('{version}', nodeVersion)
  const nodeChecksums = curlText(nodeChecksumsURL, directRetry)
  const nodeChecksumEntries = new Map(nodeChecksums.split(/\r?\n/u).map((line) => {
    const match = line.match(/^([0-9a-f]{64})\s+(.+)$/u)
    return match ? [match[2], match[1]] : null
  }).filter(Boolean))
  const nodeAssets = policy.node.asset_suffixes.map((suffix) => {
    const name = `node-v${nodeVersion}-${suffix}`
    const sha256 = nodeChecksumEntries.get(name)
    if (!sha256) throw new Error(`Node.js ${lts.version} checksum list has no ${name}`)
    return { name, url: `https://nodejs.org/dist/v${nodeVersion}/${name}`, sha256 }
  })

  const upxRelease = curlJSON(policy.upx.release_api, directRetry)
  const upxVersion = String(upxRelease.tag_name ?? '').replace(/^v/, '')
  if (!stableParts(upxVersion) || upxRelease.prerelease || compareVersions(upxVersion, policy.upx.minimum_version) < 0) {
    throw new Error(`UPX registry returned incompatible stable release ${upxRelease.tag_name ?? '<missing>'}`)
  }
  const upxAssets = (upxRelease.assets ?? []).filter((asset) =>
    typeof asset.digest === 'string' && /^sha256:[0-9a-f]{64}$/i.test(asset.digest),
  ).map((asset) => ({
    name: asset.name,
    url: assertTrustedGitHubURL(asset.browser_download_url, 'upx/upx', `UPX asset ${asset.name}`),
    sha256: asset.digest.slice('sha256:'.length).toLowerCase(),
  })).sort((a, b) => a.name.localeCompare(b.name))
  if (!upxAssets.length) throw new Error(`UPX ${upxRelease.tag_name} publishes no hash-bearing assets`)

  const goWinres = JSON.parse(runNetwork('go', ['list', '-m', '-json', `${policy.go_winres.module}@latest`], { cwd: controller }, directRetry).stdout)
  if (!stableParts(goWinres.Version) || compareVersions(goWinres.Version, policy.go_winres.minimum_version) < 0) {
    throw new Error(`go-winres latest response is incomplete: ${goWinres.Version ?? '<missing>'}`)
  }
  const goWinresDownload = JSON.parse(runNetwork('go', ['mod', 'download', '-json', `${policy.go_winres.module}@${goWinres.Version}`], { cwd: controller }, directRetry).stdout)
  if (!goWinresDownload.Sum || !goWinresDownload.GoModSum) throw new Error('go-winres module download omitted checksum identities')
  const windowsCCompiler = resolveWindowsCCompiler(directRetry)

  const resolved = {
    format: 'pccontroller-host-tool-lock/v1',
    policy_name: policy.name,
    resolved_at_utc: new Date().toISOString(),
    node: {
      version: nodeVersion,
      lts: lts.lts,
      source: `https://nodejs.org/dist/v${nodeVersion}/`,
      checksums_url: nodeChecksumsURL,
      checksums_sha256: createHash('sha256').update(nodeChecksums).digest('hex'),
      assets: nodeAssets,
    },
    upx: { version: upxVersion, tag: upxRelease.tag_name, assets: upxAssets },
    go_winres: {
      module: policy.go_winres.module, version: goWinres.Version,
      sum: goWinresDownload.Sum, go_mod_sum: goWinresDownload.GoModSum,
    },
    windows_c_compiler: windowsCCompiler,
    web: {
      package_lock_sha256: sha256File(join(repo, policy.web.lock_file)),
    },
    build: {
      package_lock_sha256: sha256File(join(repo, policy.build.lock_file)),
    },
    github_actions: {
      managed_by: policy.github_actions.managed_by,
      actions: workflowActionInventory(),
    },
  }
  validateHostToolsLock(resolved)
  return resolved
}

function resolvedToolchain(action, extra = [], capture = true) {
  return run('go', ['run', './cmd/toolchain-resolver', action,
    '--policy', toolchainPolicyPath, '--lock', toolchainLockPath, ...extra], {
    cwd: controller, capture, env: githubEnvironment(),
  })
}

function controllerToolchain(action, extra = [], capture = true) {
  return run('go', ['run', './cmd/controller', 'toolchain', action,
    '--policy', toolchainPolicyPath, '--lock', toolchainLockPath, ...extra], {
    cwd: controller, capture, env: githubEnvironment(),
  })
}

function resolveToolchain(mode, directRetry) {
  validateToolchainSourcePolicy()
  validateToolchainLockSources()
  const action = mode === 'apply' ? 'update' : 'check'
  const result = resolvedToolchain(action, ['--include-canary', '--json', `--direct-retry=${directRetry}`])
  validateToolchainLockSources()
  return JSON.parse(result.stdout)
}

function goModuleUpdates(directRetry) {
  const goMod = readFileSync(join(controller, 'go.mod'), 'utf8')
  const explicitlyLocked = new Set(
    [...goMod.matchAll(/^\s*([^\s()]+)\s+v[^\s]+(?:\s+\/\/\s+indirect)?\s*$/gm)].map((match) => match[1]),
  )
  const template = '{{if .Update}}{{.Path}}|{{.Version}}|{{.Update.Version}}{{end}}'
  const output = runNetwork('go', ['list', '-m', '-u', '-f', template, 'all'], { cwd: controller }, directRetry).stdout
  return output.split(/\r?\n/).filter(Boolean).map((line) => {
    const [name, current, resolved] = line.split('|')
    return { name, current, resolved }
  }).filter((item) => explicitlyLocked.has(item.name))
}

function npmRun(args, options = {}) {
  const candidates = [
    process.env.npm_execpath,
    join(dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
    join(dirname(process.execPath), '..', 'lib', 'node_modules', 'npm', 'bin', 'npm-cli.js'),
  ].filter(Boolean)
  const cli = candidates.find((candidate) => existsSync(candidate))
  if (!cli) throw new Error('npm-cli.js could not be resolved beside the active Node.js runtime')
  return run(process.execPath, [cli, ...args], options)
}

function npmNetworkRun(args, options = {}, directRetry = true) {
  return withDirectFallback((environment) => npmRun(args, { ...options, env: environment }), {
    environment: options.env ?? process.env,
    directRetry,
  }).value
}

function npmUpdates(directory, project, directRetry) {
  const result = npmNetworkRun(['outdated', '--json'], {
    cwd: directory, accept: [0, 1],
  }, directRetry)
  const values = result.stdout.trim() ? JSON.parse(result.stdout) : {}
  return Object.entries(values).map(([name, value]) => ({
    project, name, current: value.current, compatible: value.wanted, latest: value.latest,
    update_available: value.current !== value.wanted,
  })).sort((a, b) => a.name.localeCompare(b.name))
}

function npmAudit(directory, project, directRetry) {
  return withDirectFallback((environment) => {
    const result = npmRun(['audit', '--package-lock-only', '--ignore-scripts', '--json'], {
      cwd: directory, accept: [0, 1], env: environment,
    })
    const audit = JSON.parse(result.stdout)
    if (!audit?.metadata?.vulnerabilities) throw new Error(`${project} npm audit returned no vulnerability summary`)
    return { project, vulnerabilities: audit.metadata.vulnerabilities }
  }, { directRetry }).value
}

function currentGoDirective() {
  return readFileSync(join(controller, 'go.mod'), 'utf8').match(/^go\s+(\S+)/m)?.[1] ?? ''
}

function refreshToolchainHostHashes() {
  const lock = readJSON(toolchainLockPath)
  if (!lock?.go) throw new Error('resolved toolchain lock is missing its Go section')
  const goModSHA256 = sha256File(join(controller, 'go.mod'))
  const goSumSHA256 = sha256File(join(controller, 'go.sum'))
  if (lock.go.go_mod_sha256 === goModSHA256 && lock.go.go_sum_sha256 === goSumSHA256) return false
  lock.go.go_mod_sha256 = goModSHA256
  lock.go.go_sum_sha256 = goSumSHA256
  lock.resolved_at_utc = new Date().toISOString()
  writeJSONAtomic(toolchainLockPath, lock)
  return true
}

function updateSourceDependencies(moduleUpdates, npmObservations, directRetry) {
  const goVersion = readJSON(toolchainLockPath).go.version
  log('⬆', 'Go', `updating language directive to ${goVersion} and modules`, chalk.yellow)
  run('go', ['mod', 'edit', '-go', goVersion], { cwd: controller, capture: false })
  if (moduleUpdates.length) {
    runNetwork('go', ['get', ...moduleUpdates.map((item) => `${item.name}@${item.resolved}`)], { cwd: controller, capture: false }, directRetry)
  }
  runNetwork('go', ['mod', 'tidy'], { cwd: controller, capture: false }, directRetry)
  for (const project of [
    { name: 'Web', directory: web, key: 'web' },
    { name: 'Build', directory: build, key: 'build' },
  ]) {
    if (!npmObservations.some((item) => item.project === project.key && item.update_available)) continue
    log('⬆', project.name, 'updating packages within declared compatibility ranges and refreshing exact lock', chalk.yellow)
    npmNetworkRun(['update', '--package-lock-only', '--ignore-scripts'], { cwd: project.directory, capture: false }, directRetry)
    npmNetworkRun(['install', '--package-lock-only', '--ignore-scripts'], { cwd: project.directory, capture: false }, directRetry)
  }
  refreshToolchainHostHashes()
}

function targetKey() {
  const cpu = arch() === 'x64' ? 'amd64' : arch() === 'arm64' ? 'arm64' : arch()
  const os = platform() === 'win32' ? 'windows' : platform()
  return `${os}-${cpu}`
}

function findNamed(root, wanted) {
  for (const entry of readdirSync(root)) {
    const path = join(root, entry)
    const info = statSync(path)
    if (info.isDirectory()) {
      const nested = findNamed(path, wanted)
      if (nested) return nested
    } else if (entry.toLowerCase() === wanted.toLowerCase()) return path
  }
  return ''
}

function installResolvedHostTools(lock, directRetry) {
  validateHostToolsLock(lock)
  const goBin = join(buildReportDir, 'tools', 'go', 'bin')
  mkdirSync(goBin, { recursive: true })
  runNetwork('go', ['install', `${lock.go_winres.module}@${lock.go_winres.version}`], {
    cwd: controller, capture: false, env: { ...process.env, GOBIN: goBin },
  }, directRetry)
  process.env.PATH = `${goBin}${process.platform === 'win32' ? ';' : ':'}${process.env.PATH ?? ''}`

  const key = targetKey()
  const hint = policy.upx.asset_hints[key]
  if (!hint) {
    log('ℹ', 'UPX', `no managed binary mapping for ${key}; packaging check will use PATH if available`, chalk.yellow)
    return
  }
  const asset = lock.upx.assets.find((candidate) => candidate.name.endsWith(hint))
  if (!asset) throw new Error(`UPX ${lock.upx.tag} lacks expected ${key} asset ending ${hint}`)
  const cache = join(buildReportDir, 'cache')
  const extracted = join(buildReportDir, 'tools', 'upx', lock.upx.tag, key)
  mkdirSync(cache, { recursive: true })
  mkdirSync(extracted, { recursive: true })
  const archive = join(cache, asset.name)
  if (!existsSync(archive) || sha256File(archive) !== asset.sha256) {
    const curl = platform() === 'win32' ? 'curl.exe' : 'curl'
    const trustedURL = assertTrustedGitHubURL(asset.url, 'upx/upx', `UPX asset ${asset.name}`)
    const downloadArgs = ['--fail', '--silent', '--show-error', '--location',
      '--proto', '=https', '--proto-redir', '=https', trustedURL, '--output', archive]
    withDirectFallback((environment, direct) => run(curl, direct ? ['--noproxy', '*', ...downloadArgs] : downloadArgs, {
      env: environment,
    }), { directRetry })
  }
  if (sha256File(archive) !== asset.sha256) throw new Error(`UPX ${asset.name} SHA-256 mismatch`)
  run('tar', ['-xf', archive, '-C', extracted], { capture: false })
  const source = findNamed(extracted, platform() === 'win32' ? 'upx.exe' : 'upx')
  if (!source) throw new Error(`UPX executable missing after extracting ${asset.name}`)
  const bin = join(extracted, 'bin')
  mkdirSync(bin, { recursive: true })
  const destination = join(bin, platform() === 'win32' ? 'upx.exe' : 'upx')
  if (resolve(source) !== resolve(destination)) copyFileSync(source, destination)
  chmodSync(destination, 0o755)
  process.env.PATH = `${bin}${process.platform === 'win32' ? ';' : ':'}${process.env.PATH ?? ''}`
  const version = run(destination, ['--version']).stdout.split(/\r?\n/)[0]
  log('✅', 'UPX', `${version} installed from verified ${asset.name}`, chalk.green)
}

function validateEverything(hostTools, directRetry) {
  const validations = []
  const step = (name, action) => {
    log('▶', name, '', chalk.cyan)
    action()
    validations.push({ name, status: 'passed' })
    log('✅', name, 'passed', chalk.green)
  }
  const rootBuild = platform() === 'win32' ? 'build.cmd' : './build.sh'
  // Clean before provisioning managed tools: the root cleaner owns .build,
  // including the dependency-tool staging directory used below.
  step('Clean generated build outputs', () => run(rootBuild, ['--clean'], { capture: false }))
  installResolvedHostTools(hostTools, directRetry)
  step('Exact toolchain bootstrap', () => controllerToolchain('bootstrap', ['--locked'], false))
  step('Generated product identity', () => run('node', [
    'Tools/Controller/internal/productidentity/generate.mjs', '--check',
  ], { capture: false }))
  step('Go tests from stable paths', () => run('node', ['Tools/Build/go-tests.mjs'], { capture: false }))
  step('Go vet', () => run('go', ['vet', './...'], { cwd: controller, capture: false }))
  step('Build dependencies clean install', () => npmNetworkRun(['ci', '--no-audit', '--no-fund'], { cwd: build, capture: false }, directRetry))
  step('Web clean install', () => npmNetworkRun(['ci'], { cwd: web, capture: false }, directRetry))
  step('Web typecheck', () => npmRun(['run', 'typecheck'], { cwd: web, capture: false }))
  step('Web tests', () => npmRun(['test'], { cwd: web, capture: false }))
  step('Web production build', () => npmRun(['run', 'build'], { cwd: web, capture: false }))
  step('Build-system tests', () => run('node', ['--test',
    'Tools/Build/build.test.mjs', 'Tools/Audit/extract-user-turns.test.mjs'], { capture: false }))
  step('Firmware and host build', () => run(rootBuild, ['--all'], { capture: false }))
  const bootBuild = platform() === 'win32'
    ? join(repo, 'Tools', 'Bootloader', 'Urboot-Custom', 'build.cmd')
    : join(repo, 'Tools', 'Bootloader', 'Urboot-Custom', 'build.sh')
  step('Urboot-Custom active patch/build', () => run(bootBuild, [], { capture: false }))
  const virtualBuild = join(repo, '.build', 'virtual-board-dependency-update')
  step('VirtualBoard configure', () => run('cmake', ['-S', join(repo, 'Tools', 'VirtualBoard'), '-B', virtualBuild, '-DBUILD_TESTING=ON'], { capture: false }))
  step('VirtualBoard build', () => run('cmake', ['--build', virtualBuild, '--config', 'Release'], { capture: false }))
  step('VirtualBoard tests', () => run('ctest', ['--test-dir', virtualBuild, '-C', 'Release', '--output-on-failure'], { capture: false }))

  const firmwareManifest = readJSON(join(repo, '.build', 'firmware', 'firmware-manifest.json'))
  const applicationBytes = firmwareManifest?.artifacts?.find((artifact) => artifact.role === 'application')?.dataBytes
  if (!Number.isInteger(applicationBytes) || applicationBytes > 32256) {
    throw new Error(`firmware application ${applicationBytes ?? '<missing>'} exceeds Urboot-Custom ceiling 32256`)
  }
  const bootManifest = readJSON(join(repo, '.build', 'bootloader', 'urboot-custom', 'build-manifest.json'))
  if (bootManifest?.activeUpstream?.tag !== readJSON(toolchainLockPath).bootloader.tag ||
      bootManifest?.custom?.meaningfulBytes > 512 || bootManifest?.custom?.applicationMaximumBytes !== 32256) {
    throw new Error('Urboot-Custom manifest does not match the resolved stable source or 512-byte/32256-byte ceilings')
  }
  let hostManifest = null
  if (platform() === 'win32') {
    hostManifest = readJSON(join(controller, 'bin', 'host-manifest.json'))
    if (hostManifest?.validation?.windowsResources !== 'verified' ||
        !hostManifest?.validation?.upx?.enabled || !hostManifest?.validation?.upx?.tested) {
      throw new Error('Windows host package did not verify resources and UPX compression')
    }
  }
  validations.push({
    name: 'Memory ceilings', status: 'passed',
    firmware_application_bytes: applicationBytes,
    application_maximum_bytes: 32256,
    urboot_custom_bytes: bootManifest.custom.meaningfulBytes,
    urboot_custom_allocated_bytes: 512,
  })
  if (hostManifest) {
    const executable = hostManifest.artifacts?.find((artifact) => /\.exe$/iu.test(artifact.path ?? ''))
    if (!Number.isInteger(executable?.bytes) || executable.bytes <= 0) throw new Error('Windows host package manifest has no executable size')
    validations.push({
      name: 'Host package size', status: 'passed',
      host_executable_bytes: executable.bytes,
      upx_version: hostManifest.validation.upx.version,
    })
  }
  return validations
}

function printChangeTable(rows) {
  if (!rows.length) {
    console.log(chalk.green('✅ No stable dependency changes.'))
    return
  }
  console.log(renderUnicodeTable([
    { label: 'Area' },
    { label: 'Dependency' },
    { label: 'Current' },
    { label: 'Resolved' },
  ], rows.map((row) => [
    row.area ?? '', row.name ?? '', row.current ?? '', row.resolved ?? row.compatible ?? '',
  ]), { chalk }))
}

function main() {
  const options = parseArgs(process.argv.slice(2))
  mkdirSync(buildReportDir, { recursive: true })
  const report = {
    format: 'pccontroller-dependency-update-report/v1',
    mode: options.mode,
    started_at_utc: new Date().toISOString(),
    proxy_variables: configuredProxyNames(),
    updates_available: false,
    updates_applied: false,
    changes: [],
    canary: {},
    validation: [],
  }
  try {
    log('🔎', 'Dependencies', `resolving stable channels (${report.proxy_variables.length ? `proxy variables: ${report.proxy_variables.join(', ')}` : 'direct network'})`)
    const toolchain = resolveToolchain(options.mode, options.directRetry)
    report.canary = toolchain.canary
    const toolchainChanges = toolchain.changes ?? []
    report.changes.push(...toolchainChanges)

    let hostTools = resolveHostTools(options.directRetry)
    const currentTools = readJSON(toolsLockPath)
    const hostToolChanges = compareHostToolLocks(currentTools, hostTools)
    if (hostToolChanges.length) report.changes.push(...hostToolChanges)
    else {
      hostTools.resolved_at_utc = currentTools.resolved_at_utc
    }

    const resolvedGoVersion = readJSON(toolchainLockPath).go.version
    const goDirective = currentGoDirective()
    if (goDirective !== resolvedGoVersion) {
      report.changes.push({ area: 'go-toolchain', name: 'go directive', current: goDirective, resolved: resolvedGoVersion })
    }
    const moduleUpdates = goModuleUpdates(options.directRetry)
    report.changes.push(...moduleUpdates.map((change) => ({ area: 'go-module', ...change })))
    const npm = [
      ...npmUpdates(web, 'web', options.directRetry),
      ...npmUpdates(build, 'build', options.directRetry),
    ]
    report.npm_observations = npm
    report.changes.push(...npm.filter((item) => item.update_available).map((item) => ({
      area: `npm-${item.project}`, name: item.name, current: item.current, resolved: item.compatible,
    })))
    report.updates_available = report.changes.length !== 0
    printChangeTable(report.changes)
    console.log(chalk.dim(`Canary observations (not selected): CLI ${report.canary.cli_release || 'none'}, Urboot ${report.canary.bootloader_ref || 'none'}@${report.canary.bootloader_commit || 'none'}`))

    if (options.mode === 'apply') {
      if (!currentTools || !sameSubstantive(currentTools, hostTools)) writeJSONAtomic(toolsLockPath, hostTools)
      if (goDirective !== resolvedGoVersion || moduleUpdates.length || npm.some((item) => item.update_available)) {
        updateSourceDependencies(moduleUpdates, npm, options.directRetry)
      }
      hostTools = resolveHostTools(options.directRetry)
      const afterTools = readJSON(toolsLockPath)
      if (!afterTools || !sameSubstantive(afterTools, hostTools)) writeJSONAtomic(toolsLockPath, hostTools)
      report.updates_applied = report.updates_available
    }

    report.security = {
      npm: [npmAudit(web, 'web', options.directRetry), npmAudit(build, 'build', options.directRetry)],
      scope: 'candidate npm package locks; non-npm changes retain explicit reviewer confirmation in the generated PR plan',
    }

    if (options.validate) report.validation = validateEverything(readJSON(toolsLockPath) ?? hostTools, options.directRetry)
    report.completed_at_utc = new Date().toISOString()
    report.status = 'passed'
    writeJSONAtomic(options.report, report)
    console.log(`UPDATES_AVAILABLE=${report.updates_available}`)
    console.log(`UPDATES_APPLIED=${report.updates_applied}`)
    log('✅', 'Report', options.report, chalk.green)
    if (options.mode === 'check' && options.requireCurrent && report.updates_available) process.exitCode = 2
  } catch (error) {
    report.completed_at_utc = new Date().toISOString()
    report.status = 'failed'
    report.error = error instanceof Error ? error.message : String(error)
    writeJSONAtomic(options.report, report)
    log('❌', 'Dependency validation failed', report.error, chalk.red)
    console.log('UPDATES_APPLIED=false')
    process.exitCode = 1
  }
}

export {
  assertTrustedDependencyURL,
  assertTrustedGitHubURL,
  assertTrustedRepository,
  compareHostToolLocks,
  compareCompositeVersions,
  compareVersions,
  parseWingetCompilerManifest,
  sameSubstantive,
  stableParts,
  validateHostSourcePolicy,
  validateHostToolsLock,
  validateToolchainLockSources,
  validateToolchainSourcePolicy,
  workflowActionInventory,
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()

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
import { resolveProductTitle } from '../Build/product-metadata.mjs'

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
const defaultReportPath = join(buildReportDir, 'update-report.json')
const policy = JSON.parse(readFileSync(policyPath, 'utf8'))
let cachedGitHubEnvironment

const chalk = createChalk({
  noColor: Boolean(process.env.NO_COLOR) || process.env.TERM === 'dumb',
  forceColor: Boolean(process.env.FORCE_COLOR),
})
const productTitle = resolveProductTitle(process.env)

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
  const parsed = new URL(url)
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
  const executable = platform() === 'win32' ? 'curl.exe' : 'curl'
  const base = ['--fail', '--silent', '--show-error', '--location',
    '--header', 'Accept: application/vnd.github+json, application/json',
    '--header', 'User-Agent: PCController-dependency-updater/1', url]
  let result = spawnSync(executable, base, { encoding: 'utf8', windowsHide: true, env: process.env })
  if (result.status !== 0 && directRetry) {
    result = spawnSync(executable, ['--noproxy', '*', ...base], { encoding: 'utf8', windowsHide: true, env: process.env })
  }
  if (result.status !== 0) throw new Error(`registry request failed for ${url}: ${(result.stderr ?? '').trim()}`)
  return JSON.parse(result.stdout)
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

function writeJSONAtomic(path, value) {
  mkdirSync(dirname(path), { recursive: true })
  const temporary = `${path}.tmp`
  writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 })
  renameSync(temporary, path)
}

function resolveHostTools(directRetry) {
  const nodeIndex = curlJSON(policy.node.index_url, directRetry)
  const lts = nodeIndex.find((entry) => entry.lts && stableParts(entry.version))
  if (!lts || compareVersions(lts.version, policy.node.minimum_version) < 0) {
    throw new Error('Node registry has no compatible current LTS release')
  }

  const upxRelease = curlJSON(policy.upx.release_api, directRetry)
  const upxVersion = String(upxRelease.tag_name ?? '').replace(/^v/, '')
  if (!stableParts(upxVersion) || upxRelease.prerelease || compareVersions(upxVersion, policy.upx.minimum_version) < 0) {
    throw new Error(`UPX registry returned incompatible stable release ${upxRelease.tag_name ?? '<missing>'}`)
  }
  const upxAssets = (upxRelease.assets ?? []).filter((asset) =>
    typeof asset.digest === 'string' && /^sha256:[0-9a-f]{64}$/i.test(asset.digest),
  ).map((asset) => ({
    name: asset.name,
    url: asset.browser_download_url,
    sha256: asset.digest.slice('sha256:'.length).toLowerCase(),
  })).sort((a, b) => a.name.localeCompare(b.name))
  if (!upxAssets.length) throw new Error(`UPX ${upxRelease.tag_name} publishes no hash-bearing assets`)

  const goWinres = JSON.parse(run('go', ['list', '-m', '-json', `${policy.go_winres.module}@latest`], { cwd: controller }).stdout)
  if (!stableParts(goWinres.Version) || compareVersions(goWinres.Version, policy.go_winres.minimum_version) < 0) {
    throw new Error(`go-winres latest response is incomplete: ${goWinres.Version ?? '<missing>'}`)
  }
  const goWinresDownload = JSON.parse(run('go', ['mod', 'download', '-json', `${policy.go_winres.module}@${goWinres.Version}`], { cwd: controller }).stdout)
  if (!goWinresDownload.Sum || !goWinresDownload.GoModSum) throw new Error('go-winres module download omitted checksum identities')

  return {
    format: 'pccontroller-host-tool-lock/v1',
    policy_name: policy.name,
    resolved_at_utc: new Date().toISOString(),
    node: { version: lts.version.replace(/^v/, ''), lts: lts.lts },
    upx: { version: upxVersion, tag: upxRelease.tag_name, assets: upxAssets },
    go_winres: {
      module: policy.go_winres.module, version: goWinres.Version,
      sum: goWinresDownload.Sum, go_mod_sum: goWinresDownload.GoModSum,
    },
    web: {
      package_lock_sha256: sha256File(join(repo, policy.web.lock_file)),
    },
    build: {
      package_lock_sha256: sha256File(join(repo, policy.build.lock_file)),
    },
    github_actions: { managed_by: policy.github_actions.managed_by },
  }
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

function resolveToolchain(mode) {
  const action = mode === 'apply' ? 'update' : 'check'
  const result = resolvedToolchain(action, ['--include-canary', '--json'])
  return JSON.parse(result.stdout)
}

function goModuleUpdates() {
  const goMod = readFileSync(join(controller, 'go.mod'), 'utf8')
  const explicitlyLocked = new Set(
    [...goMod.matchAll(/^\s*([^\s()]+)\s+v[^\s]+(?:\s+\/\/\s+indirect)?\s*$/gm)].map((match) => match[1]),
  )
  const template = '{{if .Update}}{{.Path}}|{{.Version}}|{{.Update.Version}}{{end}}'
  const output = run('go', ['list', '-m', '-u', '-f', template, 'all'], { cwd: controller }).stdout
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

function npmUpdates(directory, project) {
  const result = npmRun(['outdated', '--json'], {
    cwd: directory, accept: [0, 1],
  })
  const values = result.stdout.trim() ? JSON.parse(result.stdout) : {}
  return Object.entries(values).map(([name, value]) => ({
    project, name, current: value.current, compatible: value.wanted, latest: value.latest,
    update_available: value.current !== value.wanted,
  })).sort((a, b) => a.name.localeCompare(b.name))
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

function updateSourceDependencies(moduleUpdates, npmObservations) {
  const goVersion = readJSON(toolchainLockPath).go.version
  log('⬆', 'Go', `updating language directive to ${goVersion} and modules`, chalk.yellow)
  run('go', ['mod', 'edit', '-go', goVersion], { cwd: controller, capture: false })
  if (moduleUpdates.length) {
    run('go', ['get', ...moduleUpdates.map((item) => `${item.name}@${item.resolved}`)], { cwd: controller, capture: false })
  }
  run('go', ['mod', 'tidy'], { cwd: controller, capture: false })
  for (const project of [
    { name: 'Web', directory: web, key: 'web' },
    { name: 'Build', directory: build, key: 'build' },
  ]) {
    if (!npmObservations.some((item) => item.project === project.key && item.update_available)) continue
    log('⬆', project.name, 'updating packages within declared compatibility ranges and refreshing exact lock', chalk.yellow)
    npmRun(['update', '--package-lock-only', '--ignore-scripts'], { cwd: project.directory, capture: false })
    npmRun(['install', '--package-lock-only', '--ignore-scripts'], { cwd: project.directory, capture: false })
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
  const goBin = join(buildReportDir, 'tools', 'go', 'bin')
  mkdirSync(goBin, { recursive: true })
  run('go', ['install', `${lock.go_winres.module}@${lock.go_winres.version}`], {
    cwd: controller, capture: false, env: { ...process.env, GOBIN: goBin },
  })
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
    const downloadArgs = ['--fail', '--silent', '--show-error', '--location', asset.url, '--output', archive]
    let fetched = spawnSync(curl, downloadArgs, { encoding: 'utf8', windowsHide: true, env: process.env })
    if (fetched.status !== 0 && directRetry) {
      fetched = spawnSync(curl, ['--noproxy', '*', ...downloadArgs], { encoding: 'utf8', windowsHide: true, env: process.env })
    }
    if (fetched.status !== 0) throw new Error(`download UPX ${lock.upx.tag}: ${(fetched.stderr ?? '').trim()}`)
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
  installResolvedHostTools(hostTools, directRetry)
  step('Exact toolchain bootstrap', () => controllerToolchain('bootstrap', ['--locked'], false))
  step('Generated product identity', () => run('node', [
    'Tools/Controller/internal/productidentity/generate.mjs', '--check',
  ], { capture: false }))
  step('Go tests from stable paths', () => run('node', ['Tools/Build/go-tests.mjs'], { capture: false }))
  step('Go vet', () => run('go', ['vet', './...'], { cwd: controller, capture: false }))
  step('Build dependencies clean install', () => npmRun(['ci', '--no-audit', '--no-fund'], { cwd: build, capture: false }))
  step('Web clean install', () => npmRun(['ci'], { cwd: web, capture: false }))
  step('Web typecheck', () => npmRun(['run', 'typecheck'], { cwd: web, capture: false }))
  step('Web tests', () => npmRun(['test'], { cwd: web, capture: false }))
  step('Web production build', () => npmRun(['run', 'build'], { cwd: web, capture: false }))
  step('Build-system tests', () => run('node', ['--test',
    'Tools/Build/build.test.mjs', 'Tools/Audit/extract-user-turns.test.mjs'], { capture: false }))
  const rootBuild = platform() === 'win32' ? 'build.cmd' : './build.sh'
  step('Firmware and host build', () => run(rootBuild, ['--all', '--clean'], { capture: false }))
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
  if (platform() === 'win32') {
    const hostManifest = readJSON(join(controller, 'bin', 'host-manifest.json'))
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

function proxyNames() {
  return Object.keys(process.env).filter((name) =>
    /^(?:HTTP|HTTPS|ALL|FTP|NO)_PROXY$/i.test(name) || /^ARDUINO_NETWORK_PROXY$/i.test(name),
  ).filter((name) => String(process.env[name] ?? '').trim()).sort()
}

function main() {
  const options = parseArgs(process.argv.slice(2))
  mkdirSync(buildReportDir, { recursive: true })
  const report = {
    format: 'pccontroller-dependency-update-report/v1',
    mode: options.mode,
    started_at_utc: new Date().toISOString(),
    proxy_variables: proxyNames(),
    updates_available: false,
    updates_applied: false,
    changes: [],
    canary: {},
    validation: [],
  }
  try {
    log('🔎', 'Dependencies', `resolving stable channels (${report.proxy_variables.length ? `proxy variables: ${report.proxy_variables.join(', ')}` : 'direct network'})`)
    const toolchain = resolveToolchain(options.mode)
    report.canary = toolchain.canary
    const toolchainChanges = toolchain.changes ?? []
    report.changes.push(...toolchainChanges)

    let hostTools = resolveHostTools(options.directRetry)
    const currentTools = readJSON(toolsLockPath)
    if (!currentTools || !sameSubstantive(currentTools, hostTools)) {
      report.changes.push({ area: 'host-tools', name: 'resolved tool lock', current: currentTools?.resolved_at_utc ? 'previous' : '', resolved: 'latest stable' })
    } else {
      hostTools.resolved_at_utc = currentTools.resolved_at_utc
    }

    const resolvedGoVersion = readJSON(toolchainLockPath).go.version
    const goDirective = currentGoDirective()
    if (goDirective !== resolvedGoVersion) {
      report.changes.push({ area: 'go-toolchain', name: 'go directive', current: goDirective, resolved: resolvedGoVersion })
    }
    const moduleUpdates = goModuleUpdates()
    report.changes.push(...moduleUpdates.map((change) => ({ area: 'go-module', ...change })))
    const npm = [
      ...npmUpdates(web, 'web'),
      ...npmUpdates(build, 'build'),
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
        updateSourceDependencies(moduleUpdates, npm)
      }
      hostTools = resolveHostTools(options.directRetry)
      const afterTools = readJSON(toolsLockPath)
      if (!afterTools || !sameSubstantive(afterTools, hostTools)) writeJSONAtomic(toolsLockPath, hostTools)
      report.updates_applied = report.updates_available
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

export { compareVersions, sameSubstantive, stableParts }

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()

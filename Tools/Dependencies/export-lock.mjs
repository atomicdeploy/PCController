#!/usr/bin/env node
// Export immutable CI inputs from canonical dependency locks without network access.

import { createHash } from 'node:crypto'
import { appendFileSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repository = resolve(here, '..', '..')
const defaultLockPath = join(repository, 'Tools', 'Controller', 'toolchain-lock.json')
const defaultHostLockPath = join(repository, 'Tools', 'Dependencies', 'resolved-tools-lock.json')
const defaultProductPath = join(repository, 'Tools', 'Controller', 'web', 'package.json')
const requiredLibraries = [
  ['Adafruit PWM Servo Driver Library', 'adafruit_pwm_servo_driver_library_version'],
  ['Adafruit INA219', 'adafruit_ina219_version'],
  ['rc-switch', 'rc_switch_version'],
  ['TM1637TinyDisplay', 'tm1637_tiny_display_version'],
  ['DallasTemperature', 'dallas_temperature_version'],
  ['OneWire', 'one_wire_version'],
]

function readJSON(path) {
  return JSON.parse(readFileSync(path, 'utf8'))
}

function invariant(condition, message) {
  if (!condition) throw new Error(message)
}

function validateLock(lock) {
  invariant(lock?.format === 'pccontroller-toolchain-lock/v1', 'unsupported canonical toolchain lock format')
  invariant(typeof lock?.firmware?.fqbn === 'string' && lock.firmware.fqbn, 'toolchain lock is missing the firmware FQBN')
  invariant(typeof lock?.firmware?.core_id === 'string' && lock.firmware.core_id, 'toolchain lock is missing the firmware core ID')
  invariant(typeof lock?.firmware?.core_version === 'string' && lock.firmware.core_version, 'toolchain lock is missing the firmware core version')
  invariant(Array.isArray(lock?.firmware?.package_indexes) && lock.firmware.package_indexes.length, 'toolchain lock is missing the firmware package index')

  const linux = lock?.firmware?.cli?.assets?.find((asset) => asset.goos === 'linux' && asset.goarch === 'amd64')
  invariant(linux, 'toolchain lock is missing the Linux amd64 dependency-CLI asset')
  invariant(/^https:\/\//u.test(linux.url ?? ''), 'dependency-CLI asset URL must use HTTPS')
  invariant(/^[0-9a-f]{64}$/u.test(linux.sha256 ?? ''), 'dependency-CLI asset must have a lowercase SHA-256')

  const resolved = new Map((lock.libraries ?? []).map((library) => [library.name, library]))
  const installSet = new Map((lock.firmware.libraries ?? []).map((library) => [library.name, library.version]))
  for (const [name] of requiredLibraries) {
    const library = resolved.get(name)
    invariant(library?.version, `canonical toolchain lock is missing required library ${name}`)
    invariant(/^https:\/\//u.test(library.url ?? ''), `${name} source URL must use HTTPS`)
    invariant(/^[0-9a-f]{64}$/u.test(library.sha256 ?? ''), `${name} must have a lowercase SHA-256`)
    invariant(installSet.get(name) === library.version, `${name} install and source versions differ in the canonical lock`)
  }
  return linux
}

function sha256(path) {
  return createHash('sha256').update(readFileSync(path)).digest('hex')
}

function validateHostLock(lock) {
  invariant(lock?.format === 'pccontroller-host-tool-lock/v1', 'unsupported canonical host-tool lock format')
  invariant(/^\d+\.\d+\.\d+$/u.test(lock?.node?.version ?? ''), 'host-tool lock is missing an exact Node.js version')
  invariant(/^https:\/\//u.test(lock?.node?.source ?? ''), 'host-tool lock is missing the Node.js release source')
  invariant(/^[0-9a-f]{64}$/u.test(lock?.node?.checksums_sha256 ?? ''), 'host-tool lock is missing the Node.js checksum-list identity')
  invariant(Array.isArray(lock?.node?.assets) && lock.node.assets.length, 'host-tool lock is missing Node.js distribution assets')
  for (const asset of lock.node.assets) {
    invariant(/^https:\/\//u.test(asset.url ?? ''), `${asset.name ?? 'Node.js asset'} URL must use HTTPS`)
    invariant(/^[0-9a-f]{64}$/u.test(asset.sha256 ?? ''), `${asset.name ?? 'Node.js asset'} must have a lowercase SHA-256`)
  }
  invariant(/^v?\d+\.\d+\.\d+$/u.test(lock?.go_winres?.version ?? ''), 'host-tool lock is missing an exact go-winres version')
  invariant(String(lock?.go_winres?.sum ?? '').startsWith('h1:'), 'host-tool lock is missing the go-winres module checksum')
  invariant(String(lock?.go_winres?.go_mod_sum ?? '').startsWith('h1:'), 'host-tool lock is missing the go-winres go.mod checksum')
  invariant(/^\d+(?:\.\d+){2}-\d+(?:\.\d+){2}-r\d+$/u.test(lock?.windows_c_compiler?.package_version ?? ''), 'host-tool lock is missing an exact Windows C compiler package version')
  invariant(/^\d+(?:\.\d+){2}$/u.test(lock?.windows_c_compiler?.compiler_version ?? ''), 'host-tool lock is missing an exact GCC version')
  invariant(/^x86_64-.*(?:mingw(?:32|64)?|windows-gnu)$/iu.test(lock?.windows_c_compiler?.target ?? ''), 'host-tool lock has an incompatible Windows C compiler target')
  invariant(/^[0-9a-f]{40}$/u.test(lock?.windows_c_compiler?.manifest_git_sha ?? ''), 'host-tool lock is missing the compiler manifest Git identity')
  invariant(/^[0-9a-f]{64}$/u.test(lock?.windows_c_compiler?.installer_sha256 ?? ''), 'host-tool lock is missing the compiler archive SHA-256')
  invariant(Array.isArray(lock?.upx?.assets) && lock.upx.assets.length, 'host-tool lock is missing UPX assets')
  for (const asset of lock.upx.assets) {
    invariant(/^https:\/\//u.test(asset.url ?? ''), `${asset.name ?? 'UPX asset'} URL must use HTTPS`)
    invariant(/^[0-9a-f]{64}$/u.test(asset.sha256 ?? ''), `${asset.name ?? 'UPX asset'} must have a lowercase SHA-256`)
  }
  invariant(Array.isArray(lock?.github_actions?.actions) && lock.github_actions.actions.length, 'host-tool lock is missing immutable GitHub Actions')
  for (const action of lock.github_actions.actions) {
    invariant(/^[0-9a-f]{40}$/u.test(action.revision ?? ''), `${action.name ?? 'GitHub Action'} revision is not immutable`)
    invariant(/^v\d+/u.test(action.version ?? ''), `${action.name ?? 'GitHub Action'} is missing its readable version`)
  }
  invariant(lock.web?.package_lock_sha256 === sha256(join(repository, 'Tools', 'Controller', 'web', 'package-lock.json')), 'web package lock hash differs from the host-tool lock')
  invariant(lock.build?.package_lock_sha256 === sha256(join(repository, 'Tools', 'Build', 'package-lock.json')), 'build package lock hash differs from the host-tool lock')
  return lock
}

function productIdentity(manifest) {
  const name = String(manifest?.productName || manifest?.name || 'Controller').replace(/[\r\n]+/gu, ' ').trim()
  const slug = name.replace(/[^0-9A-Za-z._-]+/gu, '-').replace(/^-+|-+$/gu, '') || 'Controller'
  return { name, slug }
}

function currentOutputs(lock, product) {
  const linux = validateLock(lock)
  const identity = productIdentity(product)
  const libraries = new Map(lock.libraries.map((library) => [library.name, library.version]))
  const outputs = {
    product_name: identity.name,
    product_slug: identity.slug,
    arduino_cli_version: lock.firmware.cli.version,
    arduino_cli_asset: linux.url.split('/').at(-1),
    arduino_cli_download_url: linux.url,
    arduino_cli_linux_64_sha256: linux.sha256,
    minicore_version: lock.firmware.core_version,
    minicore_package: lock.firmware.core_id,
    minicore_index_url: lock.firmware.package_indexes[0],
    toolchain_lock_path: 'Tools/Controller/toolchain-lock.json',
  }
  for (const [name, output] of requiredLibraries) outputs[output] = libraries.get(name)
  return outputs
}

function hostOutputs(lock) {
  validateHostLock(lock)
  return {
    node_version: lock.node.version,
    go_winres_module: lock.go_winres.module,
    go_winres_version: lock.go_winres.version,
    go_winres_sum: lock.go_winres.sum,
    go_winres_go_mod_sum: lock.go_winres.go_mod_sum,
    windows_c_compiler_package: lock.windows_c_compiler.package_id,
    windows_c_compiler_version: lock.windows_c_compiler.package_version,
    windows_c_compiler_target: lock.windows_c_compiler.target,
    windows_c_compiler_sha256: lock.windows_c_compiler.installer_sha256,
    upx_version: lock.upx.version,
    host_tool_lock_path: 'Tools/Dependencies/resolved-tools-lock.json',
  }
}

function writeOutputs(outputs, path) {
  const body = `${Object.entries(outputs).map(([key, value]) => `${key}=${value}`).join('\n')}\n`
  if (path) appendFileSync(path, body)
  else process.stdout.write(body)
}

function parseArguments(argv) {
  const options = {
    command: argv[0] ?? '', lock: defaultLockPath, hostLock: defaultHostLockPath,
    product: defaultProductPath, output: process.env.GITHUB_OUTPUT ?? '',
  }
  for (let index = 1; index < argv.length; index++) {
    const argument = argv[index]
    if (argument === '--lock' || argument === '--host-lock' || argument === '--product' || argument === '--output') {
      if (!argv[index + 1]) throw new Error(`${argument} requires a path`)
      const key = argument === '--host-lock' ? 'hostLock' : argument.slice(2)
      options[key] = resolve(repository, argv[++index])
    } else {
      throw new Error(`unknown option ${argument}`)
    }
  }
  return options
}

function main(argv = process.argv.slice(2)) {
  const options = parseArguments(argv)
  if (!['export', 'validate', 'export-host', 'validate-host'].includes(options.command)) {
    throw new Error('usage: export-lock.mjs export|validate|export-host|validate-host [--lock FILE] [--host-lock FILE] [--product FILE] [--output FILE]')
  }
  if (options.command === 'export-host' || options.command === 'validate-host') {
    const outputs = hostOutputs(readJSON(options.hostLock))
    if (options.command === 'export-host') writeOutputs(outputs, options.output)
    else process.stdout.write(`Canonical host-tool lock is valid: Node.js ${outputs.node_version}, go-winres ${outputs.go_winres_version}, UPX ${outputs.upx_version}, Windows GCC ${outputs.windows_c_compiler_version}.\n`)
    return
  }
  const lock = readJSON(options.lock)
  const product = readJSON(options.product)
  const outputs = currentOutputs(lock, product)
  if (options.command === 'export') writeOutputs(outputs, options.output)
  else process.stdout.write(`Canonical dependency lock is valid for ${outputs.product_name}: six firmware libraries verified.\n`)
}

export { currentOutputs, hostOutputs, productIdentity, requiredLibraries, validateHostLock, validateLock }

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main()
  } catch (error) {
    console.error(`dependency-lock export: ${error.message}`)
    process.exitCode = 1
  }
}

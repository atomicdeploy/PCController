#!/usr/bin/env node
// Export immutable CI inputs from the canonical toolchain lock without network access.

import { appendFileSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repository = resolve(here, '..', '..')
const defaultLockPath = join(repository, 'Tools', 'Controller', 'toolchain-lock.json')
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

function writeOutputs(outputs, path) {
  const body = `${Object.entries(outputs).map(([key, value]) => `${key}=${value}`).join('\n')}\n`
  if (path) appendFileSync(path, body)
  else process.stdout.write(body)
}

function parseArguments(argv) {
  const options = { command: argv[0] ?? '', lock: defaultLockPath, product: defaultProductPath, output: process.env.GITHUB_OUTPUT ?? '' }
  for (let index = 1; index < argv.length; index++) {
    const argument = argv[index]
    if (argument === '--lock' || argument === '--product' || argument === '--output') {
      if (!argv[index + 1]) throw new Error(`${argument} requires a path`)
      options[argument.slice(2)] = resolve(repository, argv[++index])
    } else {
      throw new Error(`unknown option ${argument}`)
    }
  }
  return options
}

function main(argv = process.argv.slice(2)) {
  const options = parseArguments(argv)
  if (options.command !== 'export' && options.command !== 'validate') {
    throw new Error('usage: export-lock.mjs export|validate [--lock FILE] [--product FILE] [--output FILE]')
  }
  const lock = readJSON(options.lock)
  const product = readJSON(options.product)
  const outputs = currentOutputs(lock, product)
  if (options.command === 'export') writeOutputs(outputs, options.output)
  else process.stdout.write(`Canonical dependency lock is valid for ${outputs.product_name}: six firmware libraries verified.\n`)
}

export { currentOutputs, productIdentity, requiredLibraries, validateLock }

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main()
  } catch (error) {
    console.error(`dependency-lock export: ${error.message}`)
    process.exitCode = 1
  }
}

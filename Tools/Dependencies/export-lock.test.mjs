import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { currentOutputs, hostOutputs, productIdentity, requiredLibraries, validateHostLock, validateLock } from './export-lock.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repository = resolve(here, '..', '..')
const lock = JSON.parse(readFileSync(join(repository, 'Tools', 'Controller', 'toolchain-lock.json'), 'utf8'))
const product = JSON.parse(readFileSync(join(repository, 'Tools', 'Controller', 'web', 'package.json'), 'utf8'))
const hostLock = JSON.parse(readFileSync(join(repository, 'Tools', 'Dependencies', 'resolved-tools-lock.json'), 'utf8'))

test('canonical lock exports the complete six-library firmware set', () => {
  validateLock(lock)
  const outputs = currentOutputs(lock, product)
  assert.equal(requiredLibraries.length, 6)
  for (const [, output] of requiredLibraries) assert.match(outputs[output], /^\d+\.\d+/u)
  assert.equal(outputs.product_name, product.productName)
  assert.equal(outputs.minicore_package, lock.firmware.core_id)
  assert.match(outputs.arduino_cli_linux_64_sha256, /^[0-9a-f]{64}$/u)
  assert.deepEqual(
    JSON.parse(outputs.arduino_libraries_json),
    lock.firmware.libraries.map((library) => `${library.name}@${library.version}`),
  )
})

test('canonical export rejects a missing required library', () => {
  const changed = structuredClone(lock)
  changed.libraries = changed.libraries.filter((library) => library.name !== 'OneWire')
  assert.throws(() => validateLock(changed), /missing required library OneWire/u)
})

test('visible product slug derives from package metadata', () => {
  assert.deepEqual(productIdentity({ productName: 'Example Control Center' }), {
    name: 'Example Control Center', slug: 'Example-Control-Center',
  })
})

test('canonical host lock exports exact bootstrap versions and validates source lock hashes', () => {
  validateHostLock(hostLock)
  const outputs = hostOutputs(hostLock)
  assert.equal(outputs.node_version, hostLock.node.version)
  assert.equal(outputs.go_winres_version, hostLock.go_winres.version)
  assert.equal(outputs.go_winres_sum, hostLock.go_winres.sum)
  assert.equal(outputs.upx_version, hostLock.upx.version)
  assert.equal(outputs.windows_c_compiler_version, hostLock.windows_c_compiler.package_version)
  assert.equal(outputs.windows_c_compiler_target, 'x86_64-w64-mingw32')
  assert.equal(outputs.windows_c_compiler_sha256, hostLock.windows_c_compiler.installer_sha256)
})

test('host lock replay rejects a changed npm integrity hash', () => {
  const changed = structuredClone(hostLock)
  changed.web.package_lock_sha256 = '0'.repeat(64)
  assert.throws(() => validateHostLock(changed), /web package lock hash differs/u)
})

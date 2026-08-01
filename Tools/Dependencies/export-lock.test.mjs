import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { currentOutputs, productIdentity, requiredLibraries, validateLock } from './export-lock.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repository = resolve(here, '..', '..')
const lock = JSON.parse(readFileSync(join(repository, 'Tools', 'Controller', 'toolchain-lock.json'), 'utf8'))
const product = JSON.parse(readFileSync(join(repository, 'Tools', 'Controller', 'web', 'package.json'), 'utf8'))

test('canonical lock exports the complete six-library firmware set', () => {
  validateLock(lock)
  const outputs = currentOutputs(lock, product)
  assert.equal(requiredLibraries.length, 6)
  for (const [, output] of requiredLibraries) assert.match(outputs[output], /^\d+\.\d+/u)
  assert.equal(outputs.product_name, product.productName)
  assert.equal(outputs.minicore_package, lock.firmware.core_id)
  assert.match(outputs.arduino_cli_linux_64_sha256, /^[0-9a-f]{64}$/u)
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

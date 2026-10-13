import assert from 'node:assert/strict'
import test from 'node:test'
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { reusableWebDependencies } from './build.mjs'

test('reuse requires matching declarations, locked graph and installed versions', async () => {
  const root = await mkdtemp(join(tmpdir(), 'pccontroller-web-cache-'))
  try {
    const packages = { '': { dependencies: { sample: '1.0.0' } }, 'node_modules/sample': { version: '1.0.0', resolved: 'https://example.invalid/sample.tgz', integrity: 'sha512-fixture' } }
    await mkdir(join(root, 'node_modules/sample'), { recursive: true })
    await writeFile(join(root, 'package.json'), JSON.stringify({ dependencies: { sample: '1.0.0' } }))
    await writeFile(join(root, 'package-lock.json'), JSON.stringify({ lockfileVersion: 3, packages }))
    await writeFile(join(root, 'node_modules/sample/package.json'), JSON.stringify({ version: '1.0.0' }))
    await writeFile(join(root, 'node_modules/.package-lock.json'), JSON.stringify({ lockfileVersion: 3, packages: { 'node_modules/sample': packages['node_modules/sample'] } }))
    assert.equal(reusableWebDependencies(root), true)
    await writeFile(join(root, 'node_modules/sample/package.json'), JSON.stringify({ version: '2.0.0' }))
    assert.equal(reusableWebDependencies(root), false)
    await writeFile(join(root, 'package.json'), JSON.stringify({ dependencies: { sample: '3.0.0' } }))
    assert.equal(reusableWebDependencies(root), false)
  } finally { await rm(root, { recursive: true, force: true }) }
})

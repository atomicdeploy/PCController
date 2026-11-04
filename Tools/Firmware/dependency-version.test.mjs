import assert from 'node:assert/strict'
import test from 'node:test'
import { satisfiesMinimumVersion } from './dependency-version.mjs'

test('firmware dependency compatibility floors admit newer releases, not just the snapshot', () => {
  for (const version of ['2.6.4', '2.6.5', '2.7.0', '3.0.0', '10.0.0']) assert.equal(satisfiesMinimumVersion(version, '2.6.4'), true)
  for (const version of ['2.6.3', '1.20.0', '3.0.0-rc1', '', undefined]) assert.equal(satisfiesMinimumVersion(version, '2.6.4'), false)
})

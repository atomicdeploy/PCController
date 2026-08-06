import assert from 'node:assert/strict'
import test from 'node:test'

import {
  configuredProxyNames,
  environmentWithoutProxy,
  hasConfiguredProxyRoute,
  withDirectFallback,
} from './network.mjs'

test('proxy inventory is case-insensitive and never exposes values', () => {
  const environment = {
    PATH: 'safe', http_proxy: 'http://user:secret@example.test',
    HTTPS_PROXY: 'https://example.test', no_proxy: 'localhost',
  }
  assert.deepEqual(configuredProxyNames(environment), ['http_proxy', 'HTTPS_PROXY', 'no_proxy'])
  assert.equal(hasConfiguredProxyRoute(environment), true)
  assert.equal(JSON.stringify(configuredProxyNames(environment)).includes('secret'), false)
})

test('direct fallback is bounded, strips every proxy spelling, and preserves caller state', () => {
  const environment = {
    PATH: 'safe', HTTP_PROXY: 'http://proxy.test', https_proxy: 'http://proxy.test',
    NO_PROXY: 'localhost', ARDUINO_NETWORK_PROXY: 'http://firmware-proxy.test',
  }
  const attempts = []
  const result = withDirectFallback((candidate, direct) => {
    attempts.push({ candidate, direct })
    if (!direct) throw new Error('proxy unavailable')
    return 'resolved'
  }, { environment, directRetry: true })
  assert.equal(result.value, 'resolved')
  assert.equal(result.usedDirectFallback, true)
  assert.equal(attempts.length, 2)
  assert.deepEqual(configuredProxyNames(attempts[1].candidate), [])
  assert.equal(environment.HTTP_PROXY, 'http://proxy.test')
})

test('network failures do not retry without a configured route or when disabled', () => {
  for (const options of [
    { environment: { PATH: 'safe' }, directRetry: true },
    { environment: { PATH: 'safe', HTTP_PROXY: 'http://proxy.test' }, directRetry: false },
  ]) {
    let attempts = 0
    assert.throws(() => withDirectFallback(() => {
      attempts++
      throw new Error('offline')
    }, options), /offline/u)
    assert.equal(attempts, 1)
  }
})

test('partial failure reports both bounded attempts without leaking proxy secrets', () => {
  const environment = { HTTPS_PROXY: 'https://user:secret@example.test' }
  assert.throws(() => withDirectFallback((_candidate, direct) => {
    throw new Error(direct ? 'registry unavailable' : 'configured route unavailable')
  }, { environment }), (error) => {
    assert.match(error.message, /one direct retry also failed/u)
    assert.equal(error.message.includes('secret'), false)
    return true
  })
})

test('environmentWithoutProxy retains unrelated variables', () => {
  assert.deepEqual(environmentWithoutProxy({ PATH: 'x', ftp_proxy: 'y', HOMELESS: 'z' }), {
    PATH: 'x', HOMELESS: 'z',
  })
})

import { describe, expect, it } from 'vitest'
import {
  AppActionReceiptCache,
  applyWebAppAction,
  parseWebProgress,
  pushedAppAction,
  type AppActionAck,
} from './app-actions'
import type { ControllerEvent } from './types'

function event(kind: string, value: string, target = 'web:one'): ControllerEvent {
  return {
    id: 1,
    time: new Date().toISOString(),
    kind,
    text: value,
    metadata: {
      operation_id: 'operation-1', target_instance: target,
      value, page: value,
    },
  }
}

describe('typed web app actions', () => {
  it('accepts only correlated matching exact-target deliveries', () => {
    expect(pushedAppAction(event('app.title', 'Bench'), 'web:one')).toEqual({
      operationID: 'operation-1', kind: 'app.title', value: 'Bench',
    })
    expect(pushedAppAction(event('app.title', 'Bench', 'web:two'), 'web:one')).toBeNull()
    expect(pushedAppAction({ ...event('app.title', 'Bench'), metadata: {} }, 'web:one')).toBeNull()
    expect(pushedAppAction(event('app.osc', '9;4;1;50'), 'web:one')).toBeNull()
  })

  it('reduces title, page, and progress to factual client effects', () => {
    expect(applyWebAppAction({ operationID: '1', kind: 'app.title', value: 'Bench' }))
      .toEqual({ outcome: 'applied', title: 'Bench' })
    expect(applyWebAppAction({ operationID: '2', kind: 'app.title', value: 'auto' }))
      .toEqual({ outcome: 'applied', title: null })
    expect(applyWebAppAction({ operationID: '3', kind: 'app.page', value: 'events' }))
      .toEqual({ outcome: 'applied', page: 'events' })
    expect(applyWebAppAction({ operationID: '4', kind: 'app.page', value: 'missing' }))
      .toEqual({ outcome: 'rejected', reason: 'unknown_page' })
    expect(applyWebAppAction({ operationID: '5', kind: 'app.progress', value: 'warning 73' }))
      .toEqual({ outcome: 'applied', progress: { state: 'warning', percent: 73 } })
  })

  it('parses only the bounded terminal progress grammar', () => {
    expect(parseWebProgress('normal 42')).toEqual({ state: 'normal', percent: 42 })
    expect(parseWebProgress('3')).toEqual({ state: 'indeterminate' })
    expect(parseWebProgress('clear')).toBeNull()
    expect(parseWebProgress('normal')).toBeUndefined()
    expect(parseWebProgress('warning 101')).toBeUndefined()
  })

  it('deduplicates operation receipts with bounded eviction', () => {
    const cache = new AppActionReceiptCache(2)
    const ack = (id: string): AppActionAck => ({
      operation_id: id, instance_id: 'web:one', state: 'applied',
    })
    cache.remember(ack('one'))
    cache.remember(ack('two'))
    cache.remember(ack('three'))
    expect(cache.get('one')).toBeUndefined()
    expect(cache.get('two')).toEqual(ack('two'))
    expect(cache.get('three')).toEqual(ack('three'))
  })
})

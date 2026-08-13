import { describe, expect, it } from 'vitest'
import {
  AppActionReceiptCache,
  acknowledgeWebAppAction,
  applyWebAppAction,
  parseWebProgress,
  processWebAppAction,
  pushedAppAction,
  type AppActionAck,
} from './app-actions'
import type { ControllerEvent } from './types'

function event(kind: string, value: string, target = 'web:one'): ControllerEvent {
	const expiresAt = new Date(Date.now() + 60_000).toISOString()
  return {
    id: 1,
    time: new Date().toISOString(),
    kind,
    text: value,
    metadata: {
      operation_id: 'operation-1', operation_expires_at: expiresAt, target_instance: target,
      operation_delivery_id: 'delivery-1',
      value, page: value,
    },
  }
}

describe('typed web app actions', () => {
	it('accepts only correlated matching exact-target deliveries', () => {
		const delivery = event('app.title', 'Bench')
		expect(pushedAppAction(delivery, 'web:one')).toEqual({
			operationID: 'operation-1', deliveryID: 'delivery-1',
			receiptKey: 'operation-1\u0000delivery-1',
			kind: 'app.title', value: 'Bench',
		})
    expect(pushedAppAction(event('app.title', 'Bench', 'web:two'), 'web:one')).toBeNull()
    expect(pushedAppAction({ ...event('app.title', 'Bench'), metadata: {} }, 'web:one')).toBeNull()
		const missingDeadline = event('app.title', 'Unbounded')
		delete missingDeadline.metadata!.operation_expires_at
		expect(pushedAppAction(missingDeadline, 'web:one')).toBeNull()
		expect(pushedAppAction(event('app.osc', '9;4;1;50'), 'web:one')).toBeNull()
		const expired = event('app.title', 'Late')
		expired.metadata!.operation_expires_at = new Date(Date.now() - 1_000).toISOString()
		expect(pushedAppAction(expired, 'web:one')).toBeNull()
		const malformed = event('app.title', 'Malformed')
		malformed.metadata!.operation_expires_at = 'not-a-time'
		expect(pushedAppAction(malformed, 'web:one')).toBeNull()
  })

  it('reduces title, page, and progress to factual client effects', () => {
		expect(applyWebAppAction({ operationID: '1', deliveryID: 'd1', receiptKey: '1', kind: 'app.title', value: 'Bench' }))
      .toEqual({ outcome: 'applied', title: 'Bench' })
		expect(applyWebAppAction({ operationID: '2', deliveryID: 'd2', receiptKey: '2', kind: 'app.title', value: 'auto' }))
      .toEqual({ outcome: 'applied', title: null })
		expect(applyWebAppAction({ operationID: '3', deliveryID: 'd3', receiptKey: '3', kind: 'app.page', value: 'events' }))
      .toEqual({ outcome: 'applied', page: 'events' })
		expect(applyWebAppAction({ operationID: '4', deliveryID: 'd4', receiptKey: '4', kind: 'app.page', value: 'missing' }))
      .toEqual({ outcome: 'rejected', reason: 'unknown_page' })
		expect(applyWebAppAction({ operationID: '5', deliveryID: 'd5', receiptKey: '5', kind: 'app.progress', value: 'warning 73' }))
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
			operation_id: id, delivery_id: `delivery-${id}`, instance_id: 'web:one', state: 'applied',
    })
		cache.remember('one:deadline', ack('one'))
		cache.remember('two:deadline', ack('two'))
		cache.remember('three:deadline', ack('three'))
		expect(cache.get('one')).toBeUndefined()
		expect(cache.get('two:deadline')).toEqual(ack('two'))
		expect(cache.get('three:deadline')).toEqual(ack('three'))
		cache.remember('three:later-deadline', ack('three'))
		expect(cache.get('three:later-deadline')).toEqual(ack('three'))
  })

	it('processes event to factual effect and acknowledgement exactly once', () => {
		const cache = new AppActionReceiptCache()
		const pushed = event('app.progress', 'warning 73')
		const first = processWebAppAction(pushed, 'web:one', cache)
		expect(first).toMatchObject({
			duplicate: false,
			effect: { outcome: 'applied', progress: { state: 'warning', percent: 73 } },
			acknowledgement: {
				operation_id: 'operation-1', delivery_id: 'delivery-1',
				instance_id: 'web:one', state: 'applied',
			},
		})
		expect(processWebAppAction(pushed, 'web:one', cache)).toEqual({
			action: first?.action,
			acknowledgement: first?.acknowledgement,
			duplicate: true,
		})

		const invalid = event('app.progress', 'warning 101')
		invalid.metadata!.operation_id = 'invalid-progress'
		expect(processWebAppAction(invalid, 'web:one', cache)).toMatchObject({
			duplicate: false,
			effect: { outcome: 'rejected', reason: 'invalid_progress' },
			acknowledgement: { state: 'rejected', reason: 'invalid_progress' },
		})
	})

	it('retries only the acknowledgement with a bounded silent recovery path', async () => {
		const acknowledgement: AppActionAck = {
			operation_id: 'operation-1', delivery_id: 'delivery-1',
			instance_id: 'web:one', state: 'applied',
		}
		let attempts = 0
		const waits: number[] = []
		await acknowledgeWebAppAction(acknowledgement, async (value) => {
			expect(value).toEqual(acknowledgement)
			attempts += 1
			if (attempts < 3) throw new Error(`temporary-${attempts}`)
		}, async (milliseconds) => { waits.push(milliseconds) })
		expect(attempts).toBe(3)
		expect(waits).toEqual([100, 200])

		attempts = 0
		await expect(acknowledgeWebAppAction(acknowledgement, async () => {
			attempts += 1
			throw new Error('still-offline')
		}, async () => {})).rejects.toThrow('still-offline')
		expect(attempts).toBe(3)
	})
})

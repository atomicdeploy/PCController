import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { ToastStack } from './components'
import { messageActionParams, messageDeliveryParams, messageToast } from './message-presentation'
import type { ControllerEvent, ToastMessage } from './types'

function actionableEvent(): ControllerEvent {
  return {
    id: 42,
    time: '2026-08-12T12:00:00.000Z',
    kind: 'message',
    text: 'Inspect output 3',
    targets: ['web', 'tui'],
    message_type: 'operator.prompt',
    severity: 'warning',
    correlation: 'job-42',
    action: 'relay off',
    metadata: { action_label: 'Stop outputs' },
  }
}

describe('actionable Web message presentation', () => {
  it('retains correlation and an inert explicit action in the rendered toast', () => {
    const value = messageToast(actionableEvent())
    expect(value).toMatchObject({
      messageEventID: 42,
      correlation: 'job-42',
      action: 'relay off',
      actionLabel: 'Stop outputs',
      persistent: true,
    })
    const message = { id: 7, ...value } as ToastMessage
    const markup = renderToStaticMarkup(
      <ToastStack messages={[message]} dismiss={() => undefined} act={() => undefined} />,
    )
    expect(markup).toContain('Stop outputs')
    expect(markup).toContain('#job-42')
    expect(markup).toContain('toast__action')
  })

  it('emits bounded delivery and explicit-action RPC parameters', () => {
    const message = { id: 7, ...messageToast(actionableEvent()) } as ToastMessage
    expect(messageDeliveryParams(message)).toEqual({ event_id: 42, surface: 'web' })
    expect(messageActionParams(message, 'web:tab-1')).toEqual({
      event_id: 42,
      surface: 'web',
      instance_id: 'web:tab-1',
    })
  })

  it('rejects unrelated events instead of rendering them as messages', () => {
    expect(messageToast({ ...actionableEvent(), kind: 'relay.changed' })).toBeNull()
    expect(messageToast({ ...actionableEvent(), id: 0 })).toBeNull()
  })
})

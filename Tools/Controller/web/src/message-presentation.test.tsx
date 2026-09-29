import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { ToastStack } from './components'
import { messageToast } from './message-presentation'
import type { ControllerEvent, ToastMessage } from './types'

function actionableEvent(): ControllerEvent {
  return {
    id: 42,
    time: '2026-08-12T12:00:00.000Z',
    kind: 'message',
    text: 'Inspect output 3',
    targets: ['surface:webui', 'tui'],
    message_type: 'operator.prompt',
    severity: 'warning',
    correlation: 'job-42',
    action: 'relay off',
    metadata: { action_label: 'Stop outputs' },
  }
}

describe('actionable Web message presentation', () => {
  it('retains correlation and descriptive action context without an execution control', () => {
    const value = messageToast(actionableEvent(), 'en')
    expect(value).toMatchObject({
      messageEventID: 42,
      correlation: 'job-42',
      action: 'relay off',
      actionLabel: 'Stop outputs',
      persistent: true,
    })
    const message = { id: 7, ...value } as ToastMessage
    const markup = renderToStaticMarkup(
      <ToastStack messages={[message]} dismiss={() => undefined} />,
    )
    expect(markup).toContain('Stop outputs')
    expect(markup).toContain('relay off')
    expect(markup).toContain('job-42')
    expect(markup).toContain('toast__action-context')
    expect(markup).not.toContain('toast__action-button')
  })

  it('uses localized contextual defaults and severity', () => {
    expect(messageToast({
      ...actionableEvent(), message_type: '', severity: 'error',
      action: 'status', metadata: {}, correlation: '',
    }, 'fa')).toMatchObject({
      title: 'پیام',
      tone: 'danger',
      actionLabel: 'عملیات پیشنهادی',
      persistent: true,
    })
  })

  it('lets transient informational messages expire normally', () => {
    expect(messageToast({
      ...actionableEvent(), action: '', correlation: '', severity: 'info',
    }, 'en')).toMatchObject({ tone: 'info', persistent: false })
  })

  it('rejects unrelated and unidentifiable events', () => {
    expect(messageToast({ ...actionableEvent(), kind: 'relay.changed' }, 'en')).toBeNull()
    expect(messageToast({ ...actionableEvent(), id: 0 }, 'en')).toBeNull()
  })
})

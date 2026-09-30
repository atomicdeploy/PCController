import { describe, expect, it } from 'vitest'
import { updateToastFromEvent } from './update-toast'

describe('update transfer toast presentation', () => {
  it('presents measured byte and percentage progress without raw byte noise', () => {
    const value = updateToastFromEvent({
      kind: 'peer-update.transferring', text: 'transferring verified host artifact to peer', metadata: {
        operation_id: 'peer-op', kind: 'host', state: 'transferring', stage: 'transferring',
        progress_known: 'true', progress_percent: '40', bytes_done: '2567782', bytes_total: '6419456',
      },
    }, 'en')
    expect(value).toMatchObject({ updateOperationID: 'peer-op', progressKnown: true, progressPercent: 40, persistent: true })
    expect(value?.progressLabel).toBe('2.45 MiB / 6.12 MiB')
  })

  it('keeps unknown-duration work indeterminate and terminal work dismissible', () => {
    expect(updateToastFromEvent({ kind: 'update.verifying', text: 'verifying', metadata: {
      operation_id: 'verify', kind: 'firmware', state: 'verifying', progress_known: 'false',
    } }, 'en')).toMatchObject({ progressKnown: false, persistent: true })
    expect(updateToastFromEvent({ kind: 'peer-update.completed', text: 'done', metadata: {
      operation_id: 'peer-op', kind: 'host', state: 'completed', progress_known: 'true', progress_percent: '100',
    } }, 'en')).toMatchObject({ tone: 'success', persistent: false, progressPercent: 100 })
  })
})

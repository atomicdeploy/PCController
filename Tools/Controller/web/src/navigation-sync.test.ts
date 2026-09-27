import { describe, expect, it } from 'vitest'
import {
  defaultNavigationGroup,
  loadNavigationSync,
  NavigationSession,
  navigationKeys,
  navigationSyncStorageKey,
  saveNavigationSync,
} from './navigation-sync'

const clientEpoch = '11111111111111111111111111111111'
const groupEpoch = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'

describe('browser navigation synchronization', () => {
  it('defaults to following and emits monotonic bounded presence and commit identities', () => {
    const session = new NavigationSession(clientEpoch)
    expect(session.nextValues(true)).toEqual({
      [navigationKeys.sync]: 'follow',
      [navigationKeys.group]: defaultNavigationGroup,
      [navigationKeys.epoch]: clientEpoch,
      [navigationKeys.revision]: '1',
    })
    expect(session.nextValues(true, true)[navigationKeys.catchUp]).toBe('true')
    expect(session.nextCommit('tab:browser:1', 'events')).toEqual({
      group: defaultNavigationGroup,
      source: 'tab:browser:1',
      page: 'events',
      operation_id: `${clientEpoch}-1`,
    })
  })

  it('rejects duplicate, stale, malformed, and foreign-epoch pushed actions', () => {
    const session = new NavigationSession(clientEpoch)
    const metadata = {
      [navigationKeys.sync]: 'group',
      [navigationKeys.group]: defaultNavigationGroup,
      [navigationKeys.epoch]: groupEpoch,
      [navigationKeys.revision]: '2',
      [navigationKeys.source]: 'tui:source',
    }
    expect(session.acceptAction(metadata, 'events')).toBe('events')
    expect(session.acceptAction(metadata, 'settings')).toBeNull()
    expect(session.acceptAction({ ...metadata, [navigationKeys.revision]: '1' }, 'settings')).toBeNull()
    expect(session.acceptAction({ ...metadata, [navigationKeys.epoch]: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', [navigationKeys.revision]: '3' }, 'settings')).toBeNull()
    expect(session.acceptAction({ ...metadata, [navigationKeys.revision]: 'NaN' }, 'settings')).toBeNull()
    expect(session.acceptAction({ ...metadata, [navigationKeys.source]: '../bad' }, 'settings')).toBeNull()
  })

  it('settles only the correlated command and permits a new epoch after reconnect', () => {
    const session = new NavigationSession(clientEpoch)
    const commit = session.nextCommit('tab:browser:1', 'controls')
    const outcome = {
      group: defaultNavigationGroup,
      group_epoch: groupEpoch,
      revision: 4,
      operation_id: commit.operation_id,
      page: 'controls',
    }
    expect(session.settle(outcome, 'wrong-operation')).toBeNull()
    expect(session.settle(outcome, commit.operation_id)).toBe('controls')
    session.resetCoordinator()
    expect(session.acceptAction({
      [navigationKeys.sync]: 'group',
      [navigationKeys.group]: defaultNavigationGroup,
      [navigationKeys.epoch]: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      [navigationKeys.revision]: '1',
      [navigationKeys.source]: 'tab:browser:2',
    }, 'updates')).toBe('updates')
  })

  it('rejects actions queued for an older browser presence generation', () => {
    const session = new NavigationSession(clientEpoch)
    session.nextValues(true)
    const stale = {
      [navigationKeys.sync]: 'group',
      [navigationKeys.group]: defaultNavigationGroup,
      [navigationKeys.epoch]: groupEpoch,
      [navigationKeys.revision]: '1',
      [navigationKeys.source]: 'tui:source',
      [navigationKeys.targetEpoch]: clientEpoch,
      [navigationKeys.targetRevision]: '1',
    }
    session.nextValues(true, true)
    expect(session.acceptAction(stale, 'events')).toBeNull()
    expect(session.acceptAction({
      ...stale,
      [navigationKeys.revision]: '2',
      [navigationKeys.targetRevision]: '2',
    }, 'events')).toBe('events')
  })

  it('persists only the current tab opt-out and remains enabled when storage is unavailable', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
    }
    expect(loadNavigationSync(storage)).toBe(true)
    saveNavigationSync(false, storage)
    expect(values.get(navigationSyncStorageKey)).toBe('false')
    expect(loadNavigationSync(storage)).toBe(false)
    expect(loadNavigationSync({ getItem: () => { throw new Error('blocked') } })).toBe(true)
  })
})

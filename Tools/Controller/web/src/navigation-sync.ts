import type { PageID } from './hotkeys'

export const defaultNavigationGroup = 'default'

export const navigationKeys = {
  sync: 'navigation_sync',
  group: 'navigation_group',
  epoch: 'navigation_epoch',
  revision: 'navigation_revision',
  source: 'navigation_source',
  catchUp: 'navigation_catch_up',
  operation: 'navigation_operation_id',
  targetEpoch: 'navigation_target_epoch',
  targetRevision: 'navigation_target_revision',
} as const

const identifierPattern = /^[A-Za-z0-9._-]{1,64}$/
const instancePattern = /^[A-Za-z0-9._:-]{1,180}$/
const epochPattern = /^[a-f0-9]{32}$/

export interface NavigationOutcome {
  group: string
  group_epoch: string
  revision: number
  operation_id: string
  page: string
}

export interface NavigationCommit {
  group: string
  source: string
  page: PageID
  operation_id: string
}

function randomEpoch(): string {
  const bytes = new Uint8Array(16)
  globalThis.crypto.getRandomValues(bytes)
  return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
}

function validRevision(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function parsedRevision(value: string | undefined): number | null {
  if (!value || !/^\d{1,20}$/.test(value)) return null
  const parsed = Number(value)
  return validRevision(parsed) ? parsed : null
}

function validPage(value: string): value is PageID {
  return ['dashboard', 'controls', 'workbench', 'device', 'data', 'updates', 'events', 'settings'].includes(value)
}

/**
 * One browser-tab navigation session. Presence reports establish membership;
 * only correlated commits mutate the coordinator's canonical page.
 */
export class NavigationSession {
  readonly epoch: string
  readonly group: string

  private reportSequence = 0
  private operationSequence = 0
  private coordinatorEpoch = ''
  private coordinatorRevision = 0

  constructor(epoch = randomEpoch(), group = defaultNavigationGroup) {
    const normalizedEpoch = epoch.trim().toLowerCase()
    const normalizedGroup = group.trim().toLowerCase() || defaultNavigationGroup
    if (!epochPattern.test(normalizedEpoch)) throw new TypeError('navigation epoch is invalid')
    if (!identifierPattern.test(normalizedGroup)) throw new TypeError('navigation group is invalid')
    this.epoch = normalizedEpoch
    this.group = normalizedGroup
  }

  nextValues(follow: boolean, catchUp = false): Record<string, string> {
    this.reportSequence += 1
    return {
      [navigationKeys.sync]: follow ? 'follow' : 'independent',
      [navigationKeys.group]: this.group,
      [navigationKeys.epoch]: this.epoch,
      [navigationKeys.revision]: String(this.reportSequence),
      ...(follow && catchUp ? { [navigationKeys.catchUp]: 'true' } : {}),
    }
  }

  nextCommit(source: string, page: PageID): NavigationCommit {
    if (!instancePattern.test(source.trim())) throw new TypeError('navigation source is invalid')
    this.operationSequence += 1
    return {
      group: this.group,
      source: source.trim(),
      page,
      operation_id: `${this.epoch}-${this.operationSequence}`,
    }
  }

  acceptAction(metadata: Record<string, string> | undefined, page: string): PageID | null {
    if (!metadata || metadata[navigationKeys.sync]?.toLowerCase() !== 'group') return null
    if (metadata[navigationKeys.group]?.toLowerCase() !== this.group || !validPage(page)) return null
    const epoch = metadata[navigationKeys.epoch]?.trim().toLowerCase() ?? ''
    const revision = parsedRevision(metadata[navigationKeys.revision])
    const source = metadata[navigationKeys.source]?.trim() ?? ''
    if (!epochPattern.test(epoch) || revision === null || !instancePattern.test(source)) return null
    const targetEpoch = metadata[navigationKeys.targetEpoch]?.trim().toLowerCase() ?? ''
    const targetRevisionText = metadata[navigationKeys.targetRevision]
    if (targetEpoch || targetRevisionText) {
      const targetRevision = parsedRevision(targetRevisionText)
      if (targetEpoch !== this.epoch || targetRevision === null || targetRevision < this.reportSequence) return null
    }
    if (this.coordinatorEpoch && this.coordinatorEpoch !== epoch) return null
    if (this.coordinatorEpoch === epoch && revision <= this.coordinatorRevision) return null
    this.coordinatorEpoch = epoch
    this.coordinatorRevision = revision
    return page
  }

  settle(outcome: NavigationOutcome, operationID: string): PageID | null {
    const group = outcome.group?.trim().toLowerCase()
    const epoch = outcome.group_epoch?.trim().toLowerCase()
    const page = outcome.page?.trim().toLowerCase() ?? ''
    if (group !== this.group || !epochPattern.test(epoch) || !validRevision(outcome.revision) ||
        outcome.operation_id !== operationID || !identifierPattern.test(operationID) || !validPage(page)) {
      return null
    }
    if (this.coordinatorEpoch && this.coordinatorEpoch !== epoch) return null
    if (this.coordinatorEpoch === epoch && outcome.revision < this.coordinatorRevision) return null
    this.coordinatorEpoch = epoch
    this.coordinatorRevision = Math.max(this.coordinatorRevision, outcome.revision)
    return page
  }

  resetCoordinator(): void {
    this.coordinatorEpoch = ''
    this.coordinatorRevision = 0
  }
}

export const navigationSyncStorageKey = 'pccontroller.navigation-sync'

export function loadNavigationSync(storage?: Pick<Storage, 'getItem'>): boolean {
  try {
    return (storage ?? globalThis.sessionStorage).getItem(navigationSyncStorageKey) !== 'false'
  } catch {
    return true
  }
}

export function saveNavigationSync(value: boolean, storage?: Pick<Storage, 'setItem'>): void {
  try {
    (storage ?? globalThis.sessionStorage).setItem(navigationSyncStorageKey, String(value))
  } catch {
    // Sandboxed/private tabs can still use the in-memory default.
  }
}

import type { UIConfig } from './types'

export interface EmbeddedResourceIdentity {
  hostVersion: string
  buildTime: string
}

export interface ResourceReloadEnvironment {
  storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null
  reload?: () => void
  beforeReload?: (identity: string) => void
  embedded?: EmbeddedResourceIdentity
}

export const resourceReloadStorageKey = `${__PRODUCT_PROTOCOL__}.resource-reload`

function normalize(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

export function embeddedResourceIdentity(): EmbeddedResourceIdentity {
  return { hostVersion: __HOST_VERSION__, buildTime: __HOST_BUILD_TIME__ }
}

export function hostResourceIdentity(config: Pick<UIConfig, 'host_version' | 'build_time'>): string {
  return `${normalize(config.host_version)}|${normalize(config.build_time)}`
}

export function embeddedResourcesMismatch(
  config: Pick<UIConfig, 'host_version' | 'build_time'>,
  embedded: EmbeddedResourceIdentity = embeddedResourceIdentity(),
): boolean {
  const hostVersion = normalize(config.host_version)
  const hostBuildTime = normalize(config.build_time)
  const embeddedVersion = normalize(embedded.hostVersion)
  const embeddedBuildTime = normalize(embedded.buildTime)
  if (!hostVersion || !hostBuildTime || !embeddedVersion || !embeddedBuildTime) return false
  if (hostBuildTime === 'unknown' || embeddedBuildTime === 'unknown') return false
  return hostVersion !== embeddedVersion || hostBuildTime !== embeddedBuildTime
}

/**
 * Reloads at most once for each complete host resource identity.
 *
 * The caller must supply an identity obtained directly from the host. A tab
 * channel notification is only a prompt to fetch that authoritative identity;
 * it must never be passed here as the source of truth.
 */
export function reloadForResourceMismatch(
  config: Pick<UIConfig, 'host_version' | 'build_time'>,
  environment: ResourceReloadEnvironment = {},
): boolean {
  const embedded = environment.embedded ?? embeddedResourceIdentity()
  const mismatch = embeddedResourcesMismatch(config, embedded)
  let storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null
  try {
    storage = environment.storage === undefined ? globalThis.sessionStorage : environment.storage
  } catch {
    return false
  }
  if (!mismatch) {
    try { storage?.removeItem(resourceReloadStorageKey) } catch { /* storage may be disabled */ }
    return false
  }
  if (storage === null) return false

  const identity = hostResourceIdentity(config)
  try {
    if (storage.getItem(resourceReloadStorageKey) === identity) return false
    storage.setItem(resourceReloadStorageKey, identity)
  } catch {
    // Persistent per-tab state is required to prove this identity has already
    // reloaded. Refuse to reload when that proof is unavailable.
    return false
  }

  try { environment.beforeReload?.(identity) } catch { /* convergence hints are best-effort */ }
  const reload = environment.reload ?? (() => globalThis.location.reload())
  reload()
  return true
}

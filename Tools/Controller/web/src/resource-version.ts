import type { UIConfig } from './types'

export interface EmbeddedResourceIdentity {
  hostVersion: string
  buildTime: string
}

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

type ResourceConfig = Pick<UIConfig, 'host_version' | 'build_time'>

/** Recheck the serving host after every transport attachment, including an
 * external install that did not emit an update-completed event to this tab. */
export function createResourceReconnectCheck(
  load: (signal: AbortSignal) => Promise<ResourceConfig>,
  reloadIfMismatch: (config: ResourceConfig) => boolean,
  options: { signal?: AbortSignal; onError?: (cause: unknown) => void } = {},
) {
  let stopped = options.signal?.aborted === true
  let open = false
  let generation = 0
  let active: { abort: AbortController; timeout: ReturnType<typeof setTimeout> } | null = null
  let retry: ReturnType<typeof setTimeout> | undefined

  const cancel = () => {
    generation += 1
    if (retry !== undefined) clearTimeout(retry)
    retry = undefined
    if (active) { clearTimeout(active.timeout); active.abort.abort(); active = null }
  }
  const current = (expected: number) => !stopped && open && generation === expected
  const check = (expected: number, attempt: number) => {
    if (!current(expected)) return
    const abort = new AbortController()
    const call = { abort, timeout: setTimeout(() => abort.abort(new Error('Host resource identity check timed out')), 5_000) }
    active = call
    void load(abort.signal).then((config) => {
      if (current(expected) && !abort.signal.aborted) reloadIfMismatch(config)
    }).catch((cause: unknown) => {
      if (!current(expected)) return
      if (attempt === 0) {
        retry = setTimeout(() => { retry = undefined; check(expected, 1) }, 250)
      } else options.onError?.(cause)
    }).finally(() => {
      clearTimeout(call.timeout)
      if (active === call) active = null
    })
  }
  const dispose = () => {
    stopped = true
    open = false
    cancel()
    options.signal?.removeEventListener('abort', dispose)
  }
  options.signal?.addEventListener('abort', dispose, { once: true })

  return {
    state(state: string) {
      if (stopped) return
      if (state !== 'open') { open = false; cancel(); return }
      // controller.error can repeat an "open" state without a new socket.
      if (open) return
      open = true
      cancel()
      check(generation, 0)
    },
    dispose,
  }
}

import type { UIConfig } from './types'

export interface EmbeddedResourceIdentity {
  hostVersion: string
  resourcePath: string
}

function normalize(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

export function embeddedResourceIdentity(): EmbeddedResourceIdentity {
  const entry = typeof document === 'undefined'
    ? null
    : document.querySelector<HTMLScriptElement>('script[type="module"][src*="/assets/app-"]')
  let resourcePath = ''
  if (entry?.src) {
    try { resourcePath = new URL(entry.src, document.baseURI).pathname } catch { /* incomplete development URL */ }
  }
  return { hostVersion: __HOST_VERSION__, resourcePath }
}

export function hostResourceIdentity(config: Pick<UIConfig, 'host_version' | 'web_resource_path'>): string {
  return `${normalize(config.host_version)}|${normalize(config.web_resource_path)}`
}

export function embeddedResourcesMismatch(
  config: Pick<UIConfig, 'host_version' | 'web_resource_path'>,
  embedded: EmbeddedResourceIdentity = embeddedResourceIdentity(),
): boolean {
  const hostVersion = normalize(config.host_version)
  const hostResourcePath = normalize(config.web_resource_path)
  const embeddedVersion = normalize(embedded.hostVersion)
  const embeddedResourcePath = normalize(embedded.resourcePath)
  if (!hostVersion || !hostResourcePath || !embeddedVersion || !embeddedResourcePath) return false
  return hostVersion !== embeddedVersion || hostResourcePath !== embeddedResourcePath
}

type ResourceConfig = Pick<UIConfig, 'host_version' | 'web_resource_path'>

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

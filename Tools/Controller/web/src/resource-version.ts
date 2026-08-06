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

import { rpc } from './api'
import type { ArtifactDescriptor, ArtifactKind } from './updates-api'

export interface ReleaseCandidate {
  id: string
  source: 'github-workflow' | 'github-release' | 'manifest'
  repository?: string
  release_tag?: string
  workflow_run_id?: number
  kind: ArtifactKind
  name: string
  artifact_name?: string
  url: string
  platform?: string
  bytes?: number
  sha256?: string
  archive?: boolean
  archive_bytes?: number
  archive_sha256?: string
  archive_path?: string
  build_hash?: string
  build_timestamp?: string
  packed_timestamp?: number
  created_at?: string
  metadata?: Record<string, string>
}

export interface ReleaseDiscoveryResult {
  source: string
  checked_at: string
  candidates: ReleaseCandidate[]
}

export interface ReleaseStageStatus {
  id: string
  candidate_id?: string
  kind?: ArtifactKind
  state: 'queued' | 'downloading' | 'completed' | 'failed'
  progress_percent: number
  bytes_done?: number
  bytes_total?: number
  detail?: string
  error?: string
  started_at: string
  updated_at: string
  artifact?: ArtifactDescriptor
}

export interface ReleaseCheckResult {
  status: 'same' | 'newer' | 'older' | 'different' | 'unavailable'
  candidate?: ReleaseCandidate
  reason: string
}

interface DiscoveryBase {
  kind: ArtifactKind
  platform?: string
  bearer_token?: string
  packed_timestamp?: number
}

export interface WorkflowDiscoveryRequest extends DiscoveryBase {
  repository: string
  branch?: string
  workflow?: string
  api_base_url?: string
  build_hash?: string
  build_timestamp?: string
}

export interface ReleaseDiscoveryRequest extends DiscoveryBase {
  repository: string
  tag?: string
  include_prerelease?: boolean
  api_base_url?: string
}

export function discoverWorkflow(request: WorkflowDiscoveryRequest, signal?: AbortSignal): Promise<ReleaseDiscoveryResult> {
  return rpc('controller.discovery.github.workflow', request, signal)
}

export function discoverRelease(request: ReleaseDiscoveryRequest, signal?: AbortSignal): Promise<ReleaseDiscoveryResult> {
  return rpc('controller.discovery.github.release', request, signal)
}

export function discoverManifest(url: string, bearerToken?: string, signal?: AbortSignal): Promise<ReleaseDiscoveryResult> {
  return rpc('controller.discovery.manifest', { url, ...(bearerToken ? { bearer_token: bearerToken } : {}) }, signal)
}

export function checkReleaseCandidate(
  current: Pick<ArtifactDescriptor, 'sha256' | 'build_hash' | 'build_timestamp' | 'packed_timestamp'> | undefined,
  candidate: ReleaseCandidate,
  signal?: AbortSignal,
): Promise<ReleaseCheckResult> {
  return rpc('controller.discovery.check', {
    current: current ?? {}, kind: candidate.kind, platform: candidate.platform ?? '', candidates: [candidate],
  }, signal)
}

export function stageReleaseCandidate(
  candidate: ReleaseCandidate,
  bearerToken?: string,
  signal?: AbortSignal,
): Promise<{ operation: ReleaseStageStatus }> {
  const idempotencyKey = `stage:${candidate.id}:${candidate.sha256 || candidate.archive_sha256 || 'unhashed'}`
  return rpc('controller.discovery.stage', {
    candidate, idempotency_key: idempotencyKey,
    ...(bearerToken ? { bearer_token: bearerToken } : {}),
  }, signal)
}

export function getReleaseStageStatus(id?: string, signal?: AbortSignal): Promise<ReleaseStageStatus> {
  return rpc('controller.discovery.status', id ? { id } : {}, signal)
}

export function currentBrowserPlatform(): string {
  const browser = typeof navigator === 'undefined' ? undefined : navigator
  const value = `${browser?.userAgent ?? ''} ${browser?.platform ?? ''}`.toLowerCase()
  const operatingSystem = value.includes('win') ? 'windows' : value.includes('mac') ? 'darwin' : 'linux'
  const architecture = value.includes('arm64') || value.includes('aarch64') ? 'arm64' : 'amd64'
  return `${operatingSystem}/${architecture}`
}

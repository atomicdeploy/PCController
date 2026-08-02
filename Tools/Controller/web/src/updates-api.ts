import { getToken, responseErrorDetail, rpc } from './api'
import { controllerHTTPURL } from './transport-config'

export type ArtifactKind = 'firmware' | 'eeprom' | 'flash-backup' | 'host-executable'
export type ProgrammerMethod = 'urclock' | 'usbasp'
export type UpdateOperationKind = 'artifact-upload' | 'artifact-fetch' | 'device-capture' | 'firmware' | 'flash-restore' | 'eeprom' | 'host'
export type UpdateState =
  | 'queued'
  | 'downloading'
  | 'downloaded'
  | 'reading'
  | 'backing-up'
  | 'programming'
  | 'staging'
  | 'staged'
  | 'verifying'
  | 'completed'
  | 'failed'

export interface ArtifactDescriptor {
  kind: ArtifactKind
  name: string
  sha256: string
  bytes: number
  created_at: string
  source: string
  media_type: string
  download_url?: string
  build_hash?: string
  build_timestamp?: string
  packed_timestamp?: number
  platform?: string
  embedded?: boolean
  current?: boolean
  verified_readback?: boolean
  metadata?: Record<string, string>
}

export interface UpdateStatus {
  id: string
  kind: UpdateOperationKind
  state: UpdateState
  progress_percent: number
  bytes_done?: number
  bytes_total?: number
  started_at?: string
  updated_at?: string
  artifact_sha256?: string
  detail?: string
  error_code?: string
  idempotency_key?: string
  programming_method?: 'none' | 'urclock' | 'usbasp'
  bootloader_outcome?: 'not_attempted' | 'succeeded' | 'failed' | 'timed_out' | 'unavailable'
  isp_fallback_suggested?: boolean
}

export interface ArtifactManifest {
  enabled: boolean
  defaults_enabled: boolean
  defaults: {
    firmware?: ArtifactDescriptor
    eeprom?: ArtifactDescriptor
  }
  current: {
    firmware?: ArtifactDescriptor
    eeprom?: ArtifactDescriptor
    flash_readback?: ArtifactDescriptor
    host?: ArtifactDescriptor
  }
  board: {
    connected: boolean
    build_hash?: string
    build_timestamp?: string
    packed_timestamp?: number
  }
  policy: {
    explicit_authorization_required: boolean
    remote_programming_enabled: boolean
  }
  update?: UpdateStatus
}

export interface ArtifactList {
  artifacts: ArtifactDescriptor[]
}

export interface ArtifactOperationResult {
  operation: UpdateStatus
  artifact?: ArtifactDescriptor
  reused?: boolean
}

export interface RemoteArtifactRequest {
  url: string
  kind: ArtifactKind
  name?: string
  sha256?: string
  bytes?: number
  bearer_token?: string
  build_hash?: string
  build_timestamp?: string
  packed_timestamp?: number
  platform?: string
  idempotency_key?: string
}

export interface ArtifactCaptureRequest {
  components: Array<'flash' | 'eeprom'>
  authorized: true
  method?: ProgrammerMethod
  idempotency_key?: string
}

export interface ArtifactUpdateRequest {
  artifact_sha256: string
  authorized: true
  method?: ProgrammerMethod
  port?: string
  allow_incomplete_backup?: boolean
  /** Development data loss: retain raw EEPROM backup, but do not restore incompatible semantic settings. */
  reinitialize_eeprom?: boolean
  idempotency_key?: string
}

function authorizationHeaders(): Record<string, string> {
  const token = getToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function decode<T>(response: Response): Promise<T> {
  const raw = await response.text()
  let value: unknown = null
  if (raw) {
    try { value = JSON.parse(raw) } catch { value = raw }
  }
  if (!response.ok) {
    const detail = responseErrorDetail(value, response.statusText, response.status)
    throw new Error(detail || `HTTP ${response.status}`)
  }
  return value as T
}

export function getArtifactManifest(signal?: AbortSignal): Promise<ArtifactManifest> {
  return rpc<ArtifactManifest>('controller.artifact.manifest', {}, signal)
}

export function listArtifacts(kind?: ArtifactKind, signal?: AbortSignal): Promise<ArtifactList> {
  return rpc<ArtifactList>('controller.artifact.list', kind ? { kind } : {}, signal)
}

export function fetchRemoteArtifact(request: RemoteArtifactRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.artifact.fetch', request, signal)
}

export function captureDeviceArtifacts(request: ArtifactCaptureRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.artifact.capture', request, signal)
}

export function startFirmwareUpdate(request: ArtifactUpdateRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.update.firmware', request, signal)
}

export function startFlashRestore(request: ArtifactUpdateRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.restore.flash', request, signal)
}

export function startEEPROMUpdate(request: ArtifactUpdateRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.update.eeprom', request, signal)
}

export function startHostUpdate(request: ArtifactUpdateRequest, signal?: AbortSignal): Promise<ArtifactOperationResult> {
  return rpc<ArtifactOperationResult>('controller.update.host', request, signal)
}

export function getUpdateStatus(signal?: AbortSignal): Promise<UpdateStatus> {
  return rpc<UpdateStatus>('controller.update.status', {}, signal)
}

export async function uploadArtifact(
  file: File,
  kind: ArtifactKind,
  sha256: string,
  signal?: AbortSignal,
): Promise<ArtifactOperationResult> {
  const url = new URL(controllerHTTPURL('/api/v1/artifacts/upload'), location.href)
  url.searchParams.set('kind', kind)
  url.searchParams.set('name', file.name)
  url.searchParams.set('sha256', sha256)
  url.searchParams.set('bytes', String(file.size))
  const body = new FormData()
  body.set('artifact', file, file.name)
  const response = await fetch(url.toString(), {
    method: 'POST',
    headers: authorizationHeaders(),
    body,
    signal,
  })
  return decode<ArtifactOperationResult>(response)
}

export async function downloadArtifact(artifact: ArtifactDescriptor, signal?: AbortSignal): Promise<void> {
  const url = controllerHTTPURL(`/api/v1/artifacts/${encodeURIComponent(artifact.kind)}/${encodeURIComponent(artifact.sha256)}`)
  const response = await fetch(url, { headers: authorizationHeaders(), signal })
  if (!response.ok) await decode(response)
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = artifact.name
  link.rel = 'noopener'
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(objectURL)
}

export async function sha256File(file: Blob): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, '0')).join('')
}

export function compareBuildIdentity(
  current: Pick<ArtifactDescriptor, 'sha256' | 'build_timestamp'> | undefined,
  candidate: Pick<ArtifactDescriptor, 'sha256' | 'build_timestamp'> | undefined,
): 'same' | 'newer' | 'older' | 'different' | 'unknown' {
  if (!current || !candidate) return 'unknown'
  if (current.sha256.toLowerCase() === candidate.sha256.toLowerCase()) return 'same'
  if (!current.build_timestamp || !candidate.build_timestamp) return 'different'
  if (candidate.build_timestamp === current.build_timestamp) return 'different'
  return candidate.build_timestamp > current.build_timestamp ? 'newer' : 'older'
}

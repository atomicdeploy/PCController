import type { LocalIntegrationSettings, SegmentScrollSettings } from './types'

const CONTROL_OR_DIRECTIONAL = /[\u0000-\u001f\u007f-\u009f\u200e\u200f\u202a-\u202e\u2066-\u2069]/g
const WHITESPACE = /\s+/g

export interface FieldValidation {
  normalized: string
  error: string
  valid: boolean
}

export interface SegmentPagesValidation extends FieldValidation {
  pages: string[]
}

export interface SegmentTextValidation extends FieldValidation {
  byteLength: number
  availableBytes: number
}

function result(normalized: string, error = ''): FieldValidation {
  return { normalized, error, valid: !error }
}

export function normalizeAppTitle(value: string): string {
  return value
    .normalize('NFKC')
    .replace(CONTROL_OR_DIRECTIONAL, '')
    .replace(WHITESPACE, ' ')
    .slice(0, 64)
}

export function validateAppTitle(value: string): FieldValidation {
  const normalized = normalizeAppTitle(value)
  const trimmed = normalized.trim()
  if (!trimmed) return result(normalized, 'Application title is required.')
  if (trimmed.length > 64) return result(normalized, 'Application title must be 64 characters or fewer.')
  if (!/[\p{L}\p{N}]/u.test(trimmed)) return result(normalized, 'Application title must include a letter or number.')
  return result(trimmed)
}

export function normalizePeripheralNameInput(value: string): string {
  const normalized = value
    .normalize('NFKC')
    .replace(CONTROL_OR_DIRECTIONAL, '')
    .replace(WHITESPACE, ' ')
  return Array.from(normalized).slice(0, 64).join('')
}

export function validatePeripheralName(value: string): FieldValidation {
  const normalized = normalizePeripheralNameInput(value).trim()
  if (Array.from(normalized).length > 64) {
    return result(normalized, 'Peripheral name must be 64 characters or fewer.')
  }
  return result(normalized)
}

export function normalizeSessionToken(value: string): string {
  return value
    .normalize('NFKC')
    .replace(CONTROL_OR_DIRECTIONAL, '')
    .replace(WHITESPACE, '')
    .slice(0, 4096)
}

export function normalizeSegmentPagesInput(value: string): string {
  return value
    .normalize('NFKC')
    .replace(/[\s,]+/g, ' ')
    .replace(CONTROL_OR_DIRECTIONAL, '')
    .trimStart()
    .toLowerCase()
}

export function validateSegmentPages(value: string): SegmentPagesValidation {
  const input = normalizeSegmentPagesInput(value)
  const pages = input.trim().split(' ').filter(Boolean)
  const normalized = pages.join(' ')
  const invalid = pages.some((page) => !/^[\x21-\x7e]+$/.test(page))
  const duplicated = new Set(pages).size !== pages.length
  let error = ''
  if (pages.length > 14) error = 'Use no more than 14 page keys or IDs.'
  else if (invalid) error = 'Page keys or IDs must use printable ASCII characters.'
  else if (duplicated) error = 'Page keys or IDs must be unique.'
  return { normalized, pages, error, valid: !error }
}

export function normalizeSegmentTextInput(value: string): string {
  return value
    .normalize('NFKC')
    .replace(/[\t\r\n\f\v]+/g, ' ')
    .replace(CONTROL_OR_DIRECTIONAL, '')
}

export function validateSegmentText(value: string, gapCells: number): SegmentTextValidation {
  const normalized = normalizeSegmentTextInput(value)
  const byteLength = new TextEncoder().encode(normalized).length
  const availableBytes = Math.max(0, 40 - gapCells)
  let error = ''
  if (!/^[\x20-\x7e]*$/.test(normalized)) error = 'Text must use printable ASCII characters.'
  else if (byteLength < 5) error = 'Text must contain at least 5 printable ASCII bytes.'
  else if (byteLength + gapCells > 40) error = 'Text and repeat gap must fit within 40 bytes.'
  return { normalized, byteLength, availableBytes, error, valid: !error }
}

export function segmentScrollSettingsEqual(left: SegmentScrollSettings, right: SegmentScrollSettings): boolean {
  const leftPages = validateSegmentPages(left.pages.join(' ')).pages
  const rightPages = validateSegmentPages(right.pages.join(' ')).pages
  return left.enabled === right.enabled &&
    left.speed_ms === right.speed_ms &&
    left.gap_cells === right.gap_cells &&
    normalizeSegmentTextInput(left.door_open_text) === normalizeSegmentTextInput(right.door_open_text) &&
    normalizeSegmentTextInput(left.door_closed_text) === normalizeSegmentTextInput(right.door_closed_text) &&
    leftPages.length === rightPages.length &&
    leftPages.every((page, index) => page === rightPages[index])
}

export function normalizeRootURLInput(value: string): string {
  return value
    .normalize('NFKC')
    .replace(CONTROL_OR_DIRECTIONAL, '')
    .replace(WHITESPACE, '')
    .replace(/\\/g, '/')
    .replace(/^([a-z][a-z\d+.-]*):\/(?!\/)/i, '$1://')
    .slice(0, 2048)
}

function parseRootURL(value: string, enabled: boolean): { normalized: string; url?: URL; error?: string } {
  const input = normalizeRootURLInput(value)
  if (!input) return enabled ? { normalized: input, error: 'A root URL is required while this integration is enabled.' } : { normalized: input }
  const candidate = /^[a-z][a-z\d+.-]*:\/\//i.test(input) ? input : `http://${input}`
  let parsed: URL
  try {
    parsed = new URL(candidate)
  } catch {
    return { normalized: input, error: 'Enter a valid HTTP or HTTPS root URL.' }
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return { normalized: input, error: 'Only HTTP and HTTPS are supported.' }
  if (parsed.username || parsed.password) return { normalized: input, error: 'Credentials are not allowed in the URL.' }
  if (parsed.search || parsed.hash) return { normalized: input, error: 'Query strings and fragments are not allowed.' }
  if (parsed.pathname !== '/' && parsed.pathname !== '') return { normalized: input, error: 'Use the service root without a path.' }
  parsed.pathname = '/'
  return { normalized: parsed.toString().replace(/\/$/, ''), url: parsed }
}

function privateIPv4(hostname: string): boolean {
  const parts = hostname.split('.').map(Number)
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) return false
  return parts[0] === 10 ||
    parts[0] === 127 ||
    (parts[0] === 169 && parts[1] === 254) ||
    (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) ||
    (parts[0] === 192 && parts[1] === 168)
}

function validDNSName(host: string): boolean {
  if (!host || host.length > 253) return false
  return host.split('.').every((label) => label.length >= 1 && label.length <= 63 &&
    !label.startsWith('-') && !label.endsWith('-') && /^[a-z\d-]+$/i.test(label))
}

export function localServiceHostname(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (host.includes(':')) return host === '::1' || /^(?:fc|fd)[0-9a-f]{2}:/i.test(host) || /^fe[89ab][0-9a-f]:/i.test(host)
  if (privateIPv4(host)) return true
  if (!validDNSName(host)) return false
  if (host === 'localhost' || !host.includes('.')) return true
  return ['.local', '.lan', '.home.arpa', '.localdomain'].some((suffix) => host.endsWith(suffix) && host.length > suffix.length)
}

function loopbackHostname(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '')
  const parts = host.split('.').map(Number)
  const loopbackIPv4 = parts.length === 4 && parts[0] === 127 && parts.every((part) => Number.isInteger(part) && part >= 0 && part <= 255)
  return host === 'localhost' || host === '::1' || loopbackIPv4
}

export function validateLocalDeviceURL(value: string, enabled: boolean): FieldValidation {
  const parsed = parseRootURL(value, enabled)
  if (parsed.error || !parsed.url) return result(parsed.normalized, parsed.error ?? '')
  if (!localServiceHostname(parsed.url.hostname)) return result(parsed.normalized, 'Use a private, loopback, link-local, or local-DNS host.')
  return result(parsed.normalized)
}

export function validateDataHubURL(value: string, enabled: boolean): FieldValidation {
  const parsed = parseRootURL(value, enabled)
  if (parsed.error || !parsed.url) return result(parsed.normalized, parsed.error ?? '')
  if (!loopbackHostname(parsed.url.hostname)) return result(parsed.normalized, 'The data service must use localhost or a loopback address.')
  return result(parsed.normalized)
}

export function integrationSettingsEqual(
  left: LocalIntegrationSettings,
  right: LocalIntegrationSettings,
): boolean {
  return left.local_device.enabled === right.local_device.enabled &&
    normalizeRootURLInput(left.local_device.base_url ?? '') === normalizeRootURLInput(right.local_device.base_url ?? '') &&
    left.data_hub.enabled === right.data_hub.enabled &&
    normalizeRootURLInput(left.data_hub.base_url ?? '') === normalizeRootURLInput(right.data_hub.base_url ?? '') &&
    left.lifecycle_safety.session_lock === right.lifecycle_safety.session_lock &&
    left.lifecycle_safety.suspend === right.lifecycle_safety.suspend &&
    left.lifecycle_safety.refresh_on_resume === right.lifecycle_safety.refresh_on_resume
}

import { describe, expect, it } from 'vitest'
import {
  integrationSettingsEqual,
  normalizeAppTitle,
  normalizePeripheralNameInput,
  normalizeRootURLInput,
  normalizeSegmentPagesInput,
  normalizeSegmentTextInput,
  normalizeSessionToken,
  segmentScrollSettingsEqual,
  validateAppTitle,
  validatePeripheralName,
  validateDataHubURL,
  validateLocalDeviceURL,
  validateSegmentPages,
  validateSegmentText,
} from './settings-validation'

describe('settings input normalization', () => {
  it('normalizes titles without destroying Persian text', () => {
    expect(normalizeAppTitle('  مرکز\u202e   کنترل\n')).toBe(' مرکز کنترل')
    expect(validateAppTitle('  مرکز کنترل  ')).toMatchObject({ valid: true, normalized: 'مرکز کنترل' })
    expect(validateAppTitle('---')).toMatchObject({ valid: false })
  })

  it('removes whitespace and direction controls from session tokens', () => {
    expect(normalizeSessionToken('  ab\u202ecd \n ef  ')).toBe('abcdef')
  })

  it('normalizes host-side peripheral names while preserving Persian and blank reset', () => {
    expect(normalizePeripheralNameInput('  چراغ\t کارگاه\u202e  ')).toBe(' چراغ کارگاه ')
    expect(validatePeripheralName('  چراغ کارگاه  ')).toMatchObject({ valid: true, normalized: 'چراغ کارگاه' })
    expect(validatePeripheralName('   ')).toMatchObject({ valid: true, normalized: '' })
    expect(Array.from(normalizePeripheralNameInput('x'.repeat(80))).length).toBe(64)
  })

  it('normalizes segment page separators and validates the canonical ordered set', () => {
    expect(normalizeSegmentPagesInput('  DOOR,\n Status\u202e  ')).toBe('door status ')
    expect(validateSegmentPages(' DOOR, status ')).toMatchObject({
      valid: true,
      normalized: 'door status',
      pages: ['door', 'status'],
    })
    expect(validateSegmentPages('door, DOOR')).toMatchObject({ valid: false, error: 'Page keys or IDs must be unique.' })
    expect(validateSegmentPages('door دما')).toMatchObject({ valid: false, error: 'Page keys or IDs must use printable ASCII characters.' })
    expect(validateSegmentPages(Array.from({ length: 15 }, (_, index) => `p${index}`).join(' '))).toMatchObject({ valid: false })
  })

  it('normalizes segment text controls and enforces the firmware byte budget in real time', () => {
    expect(normalizeSegmentTextInput('door\topen\u202e')).toBe('door open')
    expect(validateSegmentText('door open', 3)).toMatchObject({ valid: true, byteLength: 9, availableBytes: 37 })
    expect(validateSegmentText('باز است', 3)).toMatchObject({ valid: false, error: 'Text must use printable ASCII characters.' })
    expect(validateSegmentText('open', 3)).toMatchObject({ valid: false, error: 'Text must contain at least 5 printable ASCII bytes.' })
    expect(validateSegmentText('12345678901234567890123456789012345678', 3)).toMatchObject({ valid: false, error: 'Text and repeat gap must fit within 40 bytes.' })
  })

  it('compares segment-scroll drafts after canonical page and text normalization', () => {
    const baseline = {
      enabled: true,
      pages: ['door', 'status'],
      door_open_text: 'door open',
      door_closed_text: 'door closed',
      speed_ms: 220,
      gap_cells: 3,
    }
    expect(segmentScrollSettingsEqual(
      { ...baseline, pages: [' DOOR ', 'STATUS'], door_open_text: 'door\topen' },
      baseline,
    )).toBe(true)
    expect(segmentScrollSettingsEqual({ ...baseline, speed_ms: 240 }, baseline)).toBe(false)
  })

  it('normalizes URL input without silently accepting paths or credentials', () => {
    expect(normalizeRootURLInput('  http:\\localhost:8787  ')).toBe('http://localhost:8787')
    expect(validateLocalDeviceURL('controller.local:8080', true)).toMatchObject({ valid: true, normalized: 'http://controller.local:8080' })
    expect(validateLocalDeviceURL('https://user:pass@controller.local', true)).toMatchObject({ valid: false })
    expect(validateLocalDeviceURL('http://controller.local/api', true)).toMatchObject({ valid: false })
    expect(validateLocalDeviceURL('https://example.com', true)).toMatchObject({ valid: false })
    expect(validateLocalDeviceURL('controller.lan:8080', true)).toMatchObject({ valid: true })
  })

  it('requires loopback for the data service and permits a disabled blank target', () => {
    expect(validateDataHubURL('127.0.0.1:8080', true)).toMatchObject({ valid: true, normalized: 'http://127.0.0.1:8080' })
    expect(validateDataHubURL('http://[::1]:8080', true)).toMatchObject({ valid: true })
    expect(validateDataHubURL('http://192.168.1.10:8080', true)).toMatchObject({ valid: false })
    expect(validateDataHubURL('http://127.999.0.1:8080', true)).toMatchObject({ valid: false })
    expect(validateDataHubURL('', false)).toMatchObject({ valid: true, normalized: '' })
  })

  it('compares normalized integration drafts', () => {
    const buzzer = { enabled: false, native_enabled: false, web_audio_enabled: true, backend: 'auto' as const, executable: '', driver_directory: '' }
    expect(integrationSettingsEqual(
      { local_device: { enabled: true, base_url: ' controller.local ' }, data_hub: { enabled: false, base_url: '' }, lifecycle_safety: { session_lock: 'stop-motion', suspend: 'all-off', refresh_on_resume: true }, buzzer_mirror: buzzer },
      { local_device: { enabled: true, base_url: 'controller.local' }, data_hub: { enabled: false, base_url: '' }, lifecycle_safety: { session_lock: 'stop-motion', suspend: 'all-off', refresh_on_resume: true }, buzzer_mirror: buzzer },
    )).toBe(true)
    expect(integrationSettingsEqual(
      { local_device: { enabled: false }, data_hub: { enabled: false }, lifecycle_safety: { session_lock: 'stop-motion', suspend: 'stop-motion', refresh_on_resume: true }, buzzer_mirror: buzzer },
      { local_device: { enabled: false }, data_hub: { enabled: false }, lifecycle_safety: { session_lock: 'all-off', suspend: 'stop-motion', refresh_on_resume: true }, buzzer_mirror: buzzer },
    )).toBe(false)
  })
})

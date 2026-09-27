import type { Locale, Snapshot } from './types'

/**
 * Fixed, shareable destinations for the Peripheral Workbench.  This remains a
 * closed set so a pasted URL or browser-console assignment cannot manufacture
 * an unreviewed device route.
 */
export const peripheralDestinationIDs = [
  'overview',
  'lighting-status',
  'lighting-pwm',
  'sensors-ina219',
  'sensors-temperature',
  'interface-displays',
  'interface-audio',
  'automation-radio',
  'automation-macros',
] as const

export type PeripheralDestinationID = (typeof peripheralDestinationIDs)[number]
export type PeripheralDestinationState = 'available' | 'disconnected' | 'pending' | 'unsupported'

export interface PeripheralDestination {
  id: PeripheralDestinationID
  group: 'overview' | 'lighting' | 'sensors' | 'interface' | 'automation'
  path: string
  label: { en: string; fa: string }
  detail: { en: string; fa: string }
  capability?: 'pwm' | 'lcd'
}

export const peripheralDestinations: readonly PeripheralDestination[] = [
  { id: 'overview', group: 'overview', path: '', label: { en: 'Peripheral overview', fa: 'نمای کلی تجهیزات جانبی' }, detail: { en: 'Connection and capability summary', fa: 'خلاصه اتصال و قابلیت‌ها' } },
  { id: 'lighting-status', group: 'lighting', path: 'lighting/status', label: { en: 'Status lighting', fa: 'نور وضعیت' }, detail: { en: 'RGB status and addressable strip', fa: 'RGB وضعیت و نوار آدرس‌پذیر' } },
  { id: 'lighting-pwm', group: 'lighting', path: 'lighting/pwm', label: { en: 'PWM mixer', fa: 'میکسر PWM' }, detail: { en: 'Named dimmer and PWM channels', fa: 'کانال‌های نام‌دار دیمر و PWM' }, capability: 'pwm' },
  { id: 'sensors-ina219', group: 'sensors', path: 'sensors/ina219', label: { en: 'Power monitor', fa: 'پایش توان' }, detail: { en: 'INA219 voltage, current, and power', fa: 'ولتاژ، جریان و توان INA219' } },
  { id: 'sensors-temperature', group: 'sensors', path: 'sensors/temperature', label: { en: 'Temperature probes', fa: 'حسگرهای دما' }, detail: { en: 'DS18B20 identities and values', fa: 'شناسه‌ها و مقادیر DS18B20' } },
  { id: 'interface-displays', group: 'interface', path: 'interface/displays', label: { en: 'Displays', fa: 'نمایشگرها' }, detail: { en: 'Seven-segment and LCD output', fa: 'خروجی سون‌سگمنت و LCD' }, capability: 'lcd' },
  { id: 'interface-audio', group: 'interface', path: 'interface/audio', label: { en: 'Audio cues', fa: 'اعلان‌های صوتی' }, detail: { en: 'Buzzer, melodies, and silent mode', fa: 'بیزر، ملودی‌ها و حالت بی‌صدا' } },
  { id: 'automation-radio', group: 'automation', path: 'automation/radio', label: { en: 'RF controls', fa: 'کنترل‌های RF' }, detail: { en: 'Learned remote keys and mappings', fa: 'کلیدها و نگاشت‌های ریموت آموخته‌شده' } },
  { id: 'automation-macros', group: 'automation', path: 'automation/macros', label: { en: 'Macros', fa: 'ماکروها' }, detail: { en: 'Timed board and host automation', fa: 'خودکارسازی زمان‌بندی‌شده برد و میزبان' } },
]

export const peripheralGroups = [
  { id: 'overview', label: { en: 'Overview', fa: 'نمای کلی' } },
  { id: 'lighting', label: { en: 'Lighting', fa: 'نورپردازی' } },
  { id: 'sensors', label: { en: 'Sensors', fa: 'حسگرها' } },
  { id: 'interface', label: { en: 'Interface', fa: 'رابط' } },
  { id: 'automation', label: { en: 'Automation', fa: 'خودکارسازی' } },
] as const

export function peripheralDestinationByID(id: PeripheralDestinationID): PeripheralDestination {
  return peripheralDestinations.find((destination) => destination.id === id) ?? peripheralDestinations[0]
}

export function peripheralDestinationLabel(destination: PeripheralDestination, locale: Locale): string {
  return destination.label[locale]
}

export function peripheralDestinationDetail(destination: PeripheralDestination, locale: Locale): string {
  return destination.detail[locale]
}

export function canonicalPeripheralHash(id: PeripheralDestinationID): string {
  const destination = peripheralDestinationByID(id)
  return destination.path ? `#/workbench/${destination.path}` : '#/workbench'
}

/** Returns null for every unknown/free-text destination. */
export function peripheralDestinationFromHash(hash: string): PeripheralDestinationID | null {
  const fragment = hash.trim().replace(/^#\/?/, '').replace(/^\/+|\/+$/g, '')
  if (fragment === 'workbench') return 'overview'
  if (!fragment.startsWith('workbench/')) return null
  const path = fragment.slice('workbench/'.length)
  return peripheralDestinations.find((destination) => destination.path === path)?.id ?? null
}

export function peripheralDestinationState(
  destination: PeripheralDestination,
  snapshot: Pick<Snapshot, 'connected' | 'have_status' | 'status'>,
): PeripheralDestinationState {
  if (!snapshot.connected) return 'disconnected'
  if (!destination.capability) return 'available'
  if (!snapshot.have_status) return 'pending'
  if (destination.capability === 'pwm') return snapshot.status.pwm_available ? 'available' : 'unsupported'
  return snapshot.status.lcd_address ? 'available' : 'unsupported'
}

export function peripheralStateLabel(state: PeripheralDestinationState, locale: Locale): string {
  const labels = locale === 'fa'
    ? { available: 'آماده', disconnected: 'برد قطع است', pending: 'در انتظار وضعیت', unsupported: 'در این برد موجود نیست' }
    : { available: 'Ready', disconnected: 'Board disconnected', pending: 'Awaiting status', unsupported: 'Not reported by this board' }
  return labels[state]
}

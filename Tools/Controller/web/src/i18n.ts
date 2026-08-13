import type { Locale } from './types'

const faDigits = new Intl.NumberFormat('fa-IR', { maximumFractionDigits: 2 })
const enDigits = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 })

export const messages = {
  en: {
    dashboard: 'Overview', controls: 'Controller', workbench: 'Peripheral workbench', data: 'Data workspace',
    events: 'Activity', settings: 'Settings', updates: 'Firmware & updates', online: 'Online', offline: 'Offline',
    connecting: 'Connecting', live: 'Live', voltage: 'Supply voltage', current: 'Current',
    power: 'Power', temperature: 'Temperature', outputs: 'Output matrix', quickActions: 'Quick actions',
    emergencyOff: 'All outputs off', reconnect: 'Reconnect', refresh: 'Refresh', command: 'Command',
    run: 'Run', cancel: 'Cancel', confirm: 'Confirm', save: 'Save changes', theme: 'Theme',
    language: 'Language', direction: 'Direction', system: 'System', dark: 'Dark', light: 'Light',
    auto: 'Automatic', english: 'English', persian: 'فارسی', leftToRight: 'Left to right',
    rightToLeft: 'Right to left', appearance: 'Appearance', connection: 'Connection',
    security: 'Security', authToken: 'Session access token', apply: 'Apply', search: 'Search',
    searchCommands: 'Search commands and pages', commandPalette: 'Command palette', closeCommandPalette: 'Close command palette',
    commandPalettePlaceholder: 'Navigate or run a safe command…', matchingPages: 'Matching pages',
    pagesLabel: 'Pages', quickCommandsLabel: 'Quick commands', navigate: 'Navigate', select: 'Select', actions: 'actions',
    primaryNavigation: 'Primary navigation', mobileNavigation: 'Mobile navigation', openNavigation: 'Open navigation',
    dismissNavigation: 'Dismiss navigation', closeNavigation: 'Close navigation', collapseNavigation: 'Collapse sidebar',
    expandNavigation: 'Expand sidebar', dashboardLink: 'dashboard', toggleTheme: 'Toggle theme',
    switchLanguage: 'Switch language', enableAudio: 'Enable interaction audio', muteAudio: 'Mute interaction audio',
    keyboardShortcuts: 'Keyboard shortcuts', notifications: 'Notifications',
    noEvents: 'No activity has arrived yet.', device: 'Device', firmware: 'Firmware', uptime: 'Uptime',
    door: 'Door', bluetooth: 'Bluetooth audio', open: 'Open', closed: 'Closed', unknown: 'Unknown',
    on: 'On', off: 'Off', toggle: 'Toggle', diagnostics: 'Diagnostics', terminal: 'Terminal',
    message: 'Message', beep: 'Beep', send: 'Send', endpoint: 'Endpoint', fetch: 'Fetch',
    products: 'Products', records: 'Records', categories: 'Categories', recentSales: 'Recent sales', export: 'Export',
    operations: 'Operations', sqlTarget: 'SQL target', excelPricing: 'Excel pricing', source: 'Source',
    update: 'Update', fullConsole: 'Open full console', unavailable: 'Service unavailable',
    rangeReady: 'Byte-range downloads are served by the host', integrations: 'Integrations',
    reduceMotion: 'Reduce motion', compactNumbers: 'Compact large numbers', liveTelemetry: 'Live telemetry',
    outputSafety: 'Safety confirmation is required for destructive actions.',
    confirmEmergencyTitle: 'Stop every output?', confirmEmergencyBody: 'This releases all relays and clears PWM output through the same guarded controller command path.',
    noHardware: 'Controller offline — check the connection details below.',
    authenticationDashboard: 'Authentication required — open Settings to connect securely.',
    authenticationDashboardDetail: 'Apply the edge session access token; live board state and telemetry will load automatically.',
    demoMode: 'Visual demonstration data', eventStream: 'Event stream', status: 'Status',
  },
  fa: {
    dashboard: 'نمای کلی', controls: 'کنترلر', workbench: 'میزکار تجهیزات', data: 'فضای داده',
    events: 'رویدادها', settings: 'تنظیمات', updates: 'میان‌افزار و به‌روزرسانی', online: 'آنلاین', offline: 'آفلاین',
    connecting: 'در حال اتصال', live: 'زنده', voltage: 'ولتاژ تغذیه', current: 'جریان',
    power: 'توان', temperature: 'دما', outputs: 'ماتریس خروجی‌ها', quickActions: 'دسترسی سریع',
    emergencyOff: 'توقف همه خروجی‌ها', reconnect: 'اتصال دوباره', refresh: 'به‌روزرسانی', command: 'فرمان',
    run: 'اجرا', cancel: 'انصراف', confirm: 'تأیید', save: 'ذخیره تغییرات', theme: 'پوسته',
    language: 'زبان', direction: 'جهت نمایش', system: 'سیستم', dark: 'تیره', light: 'روشن',
    auto: 'خودکار', english: 'English', persian: 'فارسی', leftToRight: 'چپ به راست',
    rightToLeft: 'راست به چپ', appearance: 'ظاهر', connection: 'اتصال',
    security: 'امنیت', authToken: 'توکن دسترسی این نشست', apply: 'اعمال', search: 'جست‌وجو',
    searchCommands: 'جستجوی فرمان‌ها و صفحه‌ها', commandPalette: 'پالت فرمان', closeCommandPalette: 'بستن پالت فرمان',
    commandPalettePlaceholder: 'رفتن به صفحه یا اجرای فرمان امن…', matchingPages: 'صفحه‌های منطبق',
    pagesLabel: 'صفحه‌ها', quickCommandsLabel: 'فرمان‌های سریع', navigate: 'پیمایش', select: 'انتخاب', actions: 'عملیات',
    primaryNavigation: 'پیمایش اصلی', mobileNavigation: 'پیمایش موبایل', openNavigation: 'بازکردن پیمایش',
    dismissNavigation: 'بستن لایه پیمایش', closeNavigation: 'بستن پیمایش', collapseNavigation: 'جمع‌کردن نوار کناری',
    expandNavigation: 'بازکردن نوار کناری', dashboardLink: 'داشبورد', toggleTheme: 'تغییر پوسته',
    switchLanguage: 'تغییر زبان', enableAudio: 'فعال‌کردن صدای تعامل', muteAudio: 'بی‌صداکردن صدای تعامل',
    keyboardShortcuts: 'میان‌برهای صفحه‌کلید', notifications: 'اعلان‌ها',
    noEvents: 'هنوز رویدادی دریافت نشده است.', device: 'دستگاه', firmware: 'میان‌افزار', uptime: 'زمان کارکرد',
    door: 'درب', bluetooth: 'صدای بلوتوث', open: 'باز', closed: 'بسته', unknown: 'نامشخص',
    on: 'روشن', off: 'خاموش', toggle: 'تغییر وضعیت', diagnostics: 'عیب‌یابی', terminal: 'ترمینال',
    message: 'پیام', beep: 'بوق', send: 'ارسال', endpoint: 'مسیر', fetch: 'دریافت',
    products: 'کالاها', records: 'رکوردها', categories: 'دسته‌بندی‌ها', recentSales: 'فروش اخیر', export: 'خروجی',
    operations: 'عملیات', sqlTarget: 'مقصد SQL', excelPricing: 'قیمت‌گذاری اکسل', source: 'منبع',
    update: 'به‌روزرسانی', fullConsole: 'بازکردن کنسول کامل', unavailable: 'سرویس در دسترس نیست',
    rangeReady: 'دانلودهای بایتی به‌درستی توسط میزبان ارائه می‌شوند', integrations: 'یکپارچه‌سازی‌ها',
    reduceMotion: 'کاهش حرکت‌ها', compactNumbers: 'نمایش فشرده اعداد بزرگ', liveTelemetry: 'تله‌متری زنده',
    outputSafety: 'برای عملیات مخرب تأیید ایمنی لازم است.',
    confirmEmergencyTitle: 'همه خروجی‌ها متوقف شوند؟', confirmEmergencyBody: 'این کار همه رله‌ها را آزاد و PWM را از همان مسیر امن فرمان کنترلر پاک می‌کند.',
    noHardware: 'کنترلر آفلاین است — جزئیات اتصال را در ادامه بررسی کنید.',
    authenticationDashboard: 'احراز هویت لازم است — برای اتصال امن، تنظیمات را باز کنید.',
    authenticationDashboardDetail: 'توکن دسترسی نشست لبه را اعمال کنید؛ وضعیت زنده برد و تله‌متری به‌طور خودکار بارگیری می‌شود.',
    demoMode: 'داده نمایشی رابط', eventStream: 'جریان رویدادها', status: 'وضعیت',
  },
} as const

export type MessageKey = keyof typeof messages.en

export function translator(locale: Locale): (key: MessageKey) => string {
  return (key) => messages[locale][key] ?? messages.en[key]
}

export function formatNumber(locale: Locale, value: number, digits = 1): string {
  const formatter = new Intl.NumberFormat(locale === 'fa' ? 'fa-IR' : 'en-US', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
  return formatter.format(Number.isFinite(value) ? value : 0)
}

export function formatCompact(locale: Locale, value: number): string {
  return new Intl.NumberFormat(locale === 'fa' ? 'fa-IR' : 'en-US', {
    notation: 'compact', maximumFractionDigits: 1,
  }).format(value)
}

export function localizeDigits(locale: Locale, value: number): string {
  return (locale === 'fa' ? faDigits : enDigits).format(value)
}

export function formatDuration(locale: Locale, milliseconds: number): string {
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return '—'
  const seconds = Math.floor(milliseconds / 1000)
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const pieces = days > 0 ? [`${days}d`, `${hours}h`] : hours > 0 ? [`${hours}h`, `${minutes}m`] : [`${minutes}m`]
  const value = pieces.join(' ')
  if (locale !== 'fa') return value
  return value.replace(/\d+/g, (digits) => localizeDigits(locale, Number(digits))).replace('d', 'ر').replace('h', 'س').replace('m', 'د')
}

export function formatClock(locale: Locale, value?: string | number | Date): string {
  if (!value) return '—'
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(locale === 'fa' ? 'fa-IR' : 'en-US', {
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(date)
}

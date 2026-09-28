import type { HardwareProblem, Locale } from './types'

export interface HardwareProblemPresentation {
  title: string
  guidance: string
  impact?: string
}

export function hardwareProblemPresentation(problem: HardwareProblem, locale: Locale): HardwareProblemPresentation {
  const fa = locale === 'fa'
  let title = fa ? 'خرابی سخت‌افزار کنترلر' : 'Controller hardware failure'
  let guidance = fa
    ? 'کابل داده، برق و درگاه USB کنترلر را بررسی کنید.'
    : 'Check the controller data cable, power, and USB port.'
  switch (problem.code) {
    case 'usb_descriptor_failure':
      title = fa ? 'خرابی اتصال USB کنترلر' : 'Controller USB connection failed'
      guidance = fa
        ? 'دریافت شناسهٔ دستگاه USB ناموفق بود. کابل داده، برق و درگاه USB کنترلر را بررسی کنید.'
        : 'The USB device descriptor failed. Check the controller data cable, power, or try another USB port.'
      break
    case 'device_cannot_start':
    case 'device_reported_problem':
      title = fa ? 'دستگاه USB کنترلر راه‌اندازی نشد' : 'Controller USB device could not start'
      guidance = fa
        ? 'کنترلر را دوباره متصل کنید یا از کابل و درگاه USB دیگری استفاده کنید.'
        : 'Reconnect the controller or use another data cable and USB port.'
      break
    case 'device_disabled':
      title = fa ? 'دستگاه کنترلر غیرفعال است' : 'Controller device is disabled'
      guidance = fa
        ? 'دستگاه را در «مدیریت دستگاه‌ها» فعال کنید و سپس دوباره متصل شوید.'
        : 'Enable the device in Device Manager, then reconnect.'
      break
    case 'device_driver_missing':
    case 'device_driver_failure':
      title = fa ? 'درایور USB کنترلر خراب است' : 'Controller USB driver problem'
      guidance = fa
        ? 'درایور USB کنترلر را نصب یا تعمیر کنید و سپس دستگاه را دوباره متصل کنید.'
        : 'Install or repair the controller USB driver, then reconnect the device.'
      break
    case 'device_removal_pending':
      title = fa ? 'جداسازی کنترلر در انتظار است' : 'Controller removal is pending'
      guidance = fa
        ? 'کنترلر را جدا کنید و دوباره وصل کنید.'
        : 'Unplug the controller and reconnect it.'
      break
  }
  const impact = problem.impact === 'active_operation_outcome_unknown'
    ? (fa
        ? 'ارتباط هنگام یک عملیات فعال قطع شد و نتیجهٔ آن نامشخص است. پیش از ادامه، وضعیت سخت‌افزار را بررسی کنید.'
        : 'Communication was lost during an active operation and its outcome is unknown. Verify the hardware state before continuing.')
    : undefined
  return { title, guidance, impact }
}

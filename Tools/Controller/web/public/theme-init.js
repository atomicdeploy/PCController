(() => {
  try {
    const value = JSON.parse(localStorage.getItem('pccontroller.appearance') || '{}')
    const theme = value.theme === 'light' || value.theme === 'dark'
      ? value.theme
      : matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
    const locale = value.locale === 'fa' ? 'fa' : 'en'
    const direction = value.direction === 'rtl' || value.direction === 'ltr'
      ? value.direction
      : locale === 'fa' ? 'rtl' : 'ltr'
    document.documentElement.dataset.theme = theme
    document.querySelector('meta[data-runtime-theme]')?.setAttribute('content', theme === 'dark' ? '#0b0a0e' : '#f5f3f6')
    document.documentElement.lang = locale === 'fa' ? 'fa-IR' : 'en'
    document.documentElement.dir = direction
  } catch {
    // The markup's dark/LTR defaults are usable if storage is unavailable.
  }
})()

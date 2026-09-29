// Loaded before modules so a deployment cannot strand an old lazy-loaded UI.
(() => {
  const key = 'pccontroller.bundle-recovery'
  let recovering = false
  function showRetry() {
    if (document.getElementById('bundle-recovery')) return
    const panel = document.createElement('section')
    panel.id = 'bundle-recovery'
    panel.setAttribute('role', 'alert')
    panel.style.cssText = 'position:fixed;inset:0;z-index:10000;display:grid;place-content:center;gap:20px;padding:32px;background:#18141e;color:#f4f0f8;font:16px system-ui;text-align:center'
    const title = document.createElement('h1')
    const button = document.createElement('button')
    const fa = document.documentElement.lang.startsWith('fa')
    title.textContent = fa ? 'رابط کاربری بارگیری نشد' : 'The interface could not load'
    button.textContent = fa ? 'بارگیری دوباره' : 'Reload interface'
    button.style.cssText = 'padding:12px 24px;border:1px solid #b99bff;border-radius:10px;background:#7950c8;color:white;font:inherit;cursor:pointer'
    button.onclick = () => window.location.reload()
    panel.append(title, button)
    document.body.append(panel)
    button.focus()
  }
  function recover(event) {
    event.preventDefault()
    if (recovering) return
    recovering = true
    try {
      const previous = Number(sessionStorage.getItem(key))
      const now = Date.now()
      if (previous > 0 && now - previous < 60000) { showRetry(); return }
      sessionStorage.setItem(key, String(now))
    } catch {
      // Without durable tab storage, automatic reload could loop forever.
      showRetry()
      return
    }
    // Navigation only: never retry a command, upload, macro or board mutation.
    window.location.reload()
  }
  window.addEventListener('vite:preloadError', recover)
  window.addEventListener('error', event => {
    const target = event.target
    if (target instanceof HTMLScriptElement && target.type === 'module' && target.src) recover(event)
  }, true)
  window.addEventListener('unhandledrejection', event => {
    const message = String(event.reason?.message || '')
    if (/Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(message)) recover(event)
  })
})()

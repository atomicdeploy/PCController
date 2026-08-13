import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './app'
import './styles.css'
import { installImmediateTouchActivation } from './touch-activation'

const root = document.getElementById('root')
if (!root) throw new Error('Controller UI root is missing')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// Pointer Events give touch screens a true touch-down activation path for
// ordinary controls. Hold/motion controls opt out with data-touch-mode="hold".
const removeImmediateTouchActivation = installImmediateTouchActivation()
window.addEventListener('pagehide', removeImmediateTouchActivation, { once: true })

// The HTML shell is intentionally removed only after React has painted a real
// accessible application surface; it covers the network/module-start interval.
window.requestAnimationFrame(() => document.getElementById('startup-preloader')?.remove())

// The worker caches only the versioned UI shell. Live controller/API traffic
// remains network-only so a PWA never pretends an offline board is connected.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/service-worker.js', { scope: '/' }).catch(() => undefined)
  }, { once: true })
}

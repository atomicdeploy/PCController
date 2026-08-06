import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './app'
import './styles.css'

const root = document.getElementById('root')
if (!root) throw new Error('Controller UI root is missing')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// The worker intentionally uses the network for every request. It provides an
// installable app shell without hiding host shutdown behind an obsolete cache.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/service-worker.js', { scope: '/' }).catch(() => undefined)
  }, { once: true })
}

/* PCController PWA shell worker. Live board state is always network-only. */
const shellCache = 'pccontroller-shell-v2'
const runtimeCache = 'pccontroller-runtime-v2'
const shell = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/theme-init.js',
  '/favicon.svg',
  '/favicon.ico',
]

function sameOrigin(url) {
  return url.origin === self.location.origin
}

// Only same-origin pages that actually control this worker may request lifecycle actions.
function trustedMessage(event) {
  if (event.origin !== self.location.origin) return false
  const source = event.source
  if (!source || !('url' in source) || typeof source.url !== 'string' || source.url === '') return false
  try {
    return sameOrigin(new URL(source.url))
  } catch {
    return false
  }
}

function isLiveControllerPath(pathname) {
  return pathname === '/ipc' || pathname.startsWith('/ipc/') ||
    pathname === '/api' || pathname.startsWith('/api/') ||
    pathname === '/healthz' || pathname.startsWith('/healthz/') ||
    pathname === '/controller-config.js'
}

async function cacheResponse(cacheName, request, response) {
  if (!response || !response.ok || response.type === 'opaque') return response
  const cache = await caches.open(cacheName)
  await cache.put(request, response.clone())
  return response
}

async function navigation(request) {
  try {
    return await cacheResponse(shellCache, request, await fetch(request))
  } catch {
    return (await caches.match('/index.html')) || new Response(
      '<!doctype html><title>PCController unavailable</title><p>The UI shell is not cached yet. Reconnect once to install it.</p>',
      { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } },
    )
  }
}

async function versionedAsset(request) {
  const cached = await caches.match(request)
  if (cached) return cached
  return cacheResponse(runtimeCache, request, await fetch(request))
}

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(shellCache).then((cache) => cache.addAll(shell)))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keep = new Set([shellCache, runtimeCache])
    await Promise.all((await caches.keys())
      .filter((name) => name.startsWith('pccontroller-') && !keep.has(name))
      .map((name) => caches.delete(name)))
    await self.clients.claim()
  })())
})

self.addEventListener('message', (event) => {
  if (trustedMessage(event) && event.data?.type === 'SKIP_WAITING') self.skipWaiting()
})

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (!sameOrigin(url) || isLiveControllerPath(url.pathname) || url.pathname === '/service-worker.js') return
  if (request.mode === 'navigate') {
    event.respondWith(navigation(request))
    return
  }
  if (url.pathname.startsWith('/assets/') || request.destination === 'style' ||
      request.destination === 'script' || request.destination === 'font' || request.destination === 'image') {
    event.respondWith(versionedAsset(request))
  }
})

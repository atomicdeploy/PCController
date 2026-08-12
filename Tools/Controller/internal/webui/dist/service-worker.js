/* PCController installability worker: deliberately network-only. */
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))
self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') return
  const request = event.request.mode === 'navigate'
    ? new Request(event.request, { cache: 'no-store' })
    : event.request
  event.respondWith(fetch(request))
})

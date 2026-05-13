const CACHE_NAME = 'odin-operator-shell-v1';
const SHELL_ASSETS = [
  '/app/',
  '/app/index.html',
  '/app/styles.css',
  '/app/app.js',
  '/app/offline.html',
  '/app/manifest.webmanifest',
  '/app/icons/icon-192.svg',
  '/app/icons/icon-512.svg'
];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(SHELL_ASSETS)));
});

self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') {
    return;
  }
  event.respondWith(
    fetch(event.request).catch(() => caches.match(event.request).then((cached) => {
      return cached || caches.match('/app/offline.html');
    }))
  );
});

// shell-only: runtime data and approval actions are never queued offline.

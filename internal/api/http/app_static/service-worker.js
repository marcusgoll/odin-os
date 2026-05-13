const cacheName = 'odin-operator-shell-only-v1';
const shellAssets = [
  '/app/',
  '/app/index.html',
  '/app/styles.css',
  '/app/app.js',
  '/app/manifest.webmanifest',
  '/app/offline.html',
  '/app/icons/icon-192.svg',
  '/app/icons/icon-512.svg',
];

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(cacheName).then((cache) => cache.addAll(shellAssets)));
});

self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') {
    return;
  }
  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request).catch(() => caches.match('/app/offline.html')))
  );
});

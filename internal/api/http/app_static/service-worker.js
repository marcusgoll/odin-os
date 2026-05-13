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
  const url = new URL(event.request.url);
  if (event.request.method === 'POST' && url.pathname === '/app/share') {
    event.respondWith(captureShare(event.request));
    return;
  }
  if (event.request.method !== 'GET') {
    return;
  }
  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request).catch(() => caches.match('/app/offline.html')))
  );
});

async function captureShare(request) {
  const form = await request.formData();
  const id = crypto.randomUUID ? crypto.randomUUID() : String(Date.now());
  await putPendingShare(id, form);
  return Response.redirect('/app/share?share_id=' + encodeURIComponent(id), 303);
}

function openShareDB() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('odin-share-target', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('pending-shares');
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function putPendingShare(id, form) {
  const files = form.getAll('files');
  const payload = {
    status: 'pending_upload',
    created_at: new Date().toISOString(),
    payload: {
      title: form.get('title') || '',
      text: form.get('text') || '',
      url: form.get('url') || '',
      source: 'web-share-target',
      files
    }
  };
  const db = await openShareDB();
  await new Promise((resolve, reject) => {
    const tx = db.transaction('pending-shares', 'readwrite');
    tx.objectStore('pending-shares').put(payload, id);
    tx.oncomplete = resolve;
    tx.onerror = () => reject(tx.error);
  });
}

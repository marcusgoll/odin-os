import { readFile } from 'node:fs/promises';

const root = new URL('../../internal/api/http/app_static/', import.meta.url);

const checks = [
  ['index.html', ['<link rel="manifest" href="/app/manifest.webmanifest">', 'id="home-title"', 'Action Required', 'Browser Needs Help', 'data-capture-kind="note"', 'id="voice-record"', 'id="failed-uploads"']],
  ['manifest.webmanifest', ['"name":"Odin Operator"', '"start_url":"/app/"', '"display":"standalone"', '"icons"']],
  ['service-worker.js', ['shell-only', "event.request.method !== 'GET'", '/app/offline.html']],
  ['offline.html', ['Odin Offline', 'Runtime projections and uploads are unavailable']],
  ['app.js', ['/mobile/status', '/mobile/overview', '/mobile/review-queue', '/mobile/approvals', '/mobile/browser/status', '/mobile/notifications/preferences', 'decision_by']]
];

for (const [file, expected] of checks) {
  const body = await readFile(new URL(file, root), 'utf8');
  for (const token of expected) {
    if (!body.includes(token)) {
      throw new Error(`${file} missing ${token}`);
    }
  }
}

const manifest = JSON.parse(await readFile(new URL('manifest.webmanifest', root), 'utf8'));
if (manifest.start_url !== '/app/' || manifest.scope !== '/app/' || manifest.display !== 'standalone') {
  throw new Error(`manifest is not installable shell scoped: ${JSON.stringify(manifest)}`);
}
if (!Array.isArray(manifest.icons) || manifest.icons.length < 2) {
  throw new Error('manifest needs placeholder icons');
}

console.log('odin PWA static checks passed');

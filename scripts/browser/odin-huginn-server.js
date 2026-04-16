#!/usr/bin/env node
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { accessSync, constants as fsConstants, mkdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import net from 'node:net';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const REPO_ROOT = join(__dirname, '..', '..');
const ODIN_DIR = process.env.ODIN_DIR || join(REPO_ROOT, '.odin-browser');
const BROWSER_STATE_DIR = join(ODIN_DIR, 'browser-state');
const LOG_DIR = join(ODIN_DIR, 'logs', new Date().toISOString().slice(0, 10));
const PORT = Number.parseInt(process.env.ODIN_BROWSER_PORT || '19227', 10);
const HOST = '127.0.0.1';
const ENGINE = 'chromium';

mkdirSync(BROWSER_STATE_DIR, { recursive: true });
mkdirSync(LOG_DIR, { recursive: true });

let browserProcess = null;
let debugPort = null;
let cdp = null;
let currentUrl = null;
let currentTitle = null;

function json(res, status, body) {
  const payload = JSON.stringify(body);
  res.writeHead(status, { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload) });
  res.end(payload);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      if (!raw) {
        resolve({});
        return;
      }
      try {
        resolve(JSON.parse(raw));
      } catch {
        reject(new Error('Invalid JSON body'));
      }
    });
    req.on('error', reject);
  });
}

function browserHostIsLocalService(host) {
  let normalized = String(host || '').toLowerCase();
  while (normalized.endsWith('.')) {
    normalized = normalized.slice(0, -1);
  }
  if (normalized === 'localhost' || normalized.endsWith('.localhost')) {
    return true;
  }
  if (normalized === '127.0.0.1' || normalized === '::1') {
    return true;
  }
  if (normalized.startsWith('::ffff:')) {
    const tail = normalized.slice('::ffff:'.length).replace(/\.+$/, '');
    if (tail === '127.0.0.1') {
      return true;
    }
    const mapped = tail.match(/^([0-9a-f]{1,4}):([0-9a-f]{1,4})$/i);
    if (mapped) {
      const hi = Number.parseInt(mapped[1], 16);
      const lo = Number.parseInt(mapped[2], 16);
      const ipv4 = (hi << 16) | lo;
      return ((ipv4 >>> 24) & 255) === 127;
    }
  }
  return false;
}

function assertBrowserTargetAllowed(target) {
  const url = new URL(target);
  const scheme = url.protocol.slice(0, -1).toLowerCase();
  if (scheme === 'javascript' || scheme === 'chrome') {
    throw new Error('Blocked browser URL');
  }
  if (browserHostIsLocalService(url.hostname)) {
    throw new Error('Blocked browser URL');
  }
}

function isExecutableFile(filePath) {
  try {
    const stats = statSync(filePath);
    if (!stats.isFile()) return false;
    accessSync(filePath, fsConstants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function findChromeBinary() {
  const candidates = [
    process.env.CHROME_BIN,
    'google-chrome',
    'google-chrome-stable',
    'chromium',
    'chromium-browser',
    'chrome',
  ].filter(Boolean);
  const pathDirs = (process.env.PATH || '').split(':').filter(Boolean);
  for (const candidate of candidates) {
    if (candidate.includes('/')) {
      if (isExecutableFile(candidate)) return candidate;
      continue;
    }
    for (const dir of pathDirs) {
      const full = join(dir, candidate);
      if (isExecutableFile(full)) return full;
    }
  }
  return null;
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, HOST, () => {
      const addr = srv.address();
      const port = typeof addr === 'object' && addr ? addr.port : null;
      srv.close(() => resolve(port));
    });
  });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForHttp(url, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      if (res.ok) return res;
    } catch {
      // keep polling
    }
    await sleep(250);
  }
  throw new Error('Timed out waiting for ' + url);
}

function createCdp(wsUrl) {
  const ws = new WebSocket(wsUrl);
  const pending = new Map();
  const listeners = new Map();
  let nextId = 1;

  const ready = new Promise((resolve, reject) => {
    ws.addEventListener('open', () => resolve());
    ws.addEventListener('error', reject);
  });

  ws.addEventListener('message', (event) => {
    const msg = JSON.parse(event.data);
    if (msg.id && pending.has(msg.id)) {
      const entry = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) {
        entry.reject(new Error(msg.error.message || 'CDP error'));
      } else {
        entry.resolve(msg.result || {});
      }
      return;
    }
    const callbacks = listeners.get(msg.method) || [];
    for (const callback of callbacks) {
      callback(msg.params || {});
    }
  });

  ws.addEventListener('close', () => {
    for (const entry of pending.values()) {
      entry.reject(new Error('CDP socket closed'));
    }
    pending.clear();
  });

  return {
    ready,
    async call(method, params = {}) {
      await ready;
      const id = nextId++;
      ws.send(JSON.stringify({ id, method, params }));
      return await new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
      });
    },
    once(method, timeoutMs = 15000) {
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          cleanup();
          reject(new Error('Timed out waiting for ' + method));
        }, timeoutMs);
        const handler = (params) => {
          cleanup();
          resolve(params);
        };
        const cleanup = () => {
          clearTimeout(timer);
          const callbacks = listeners.get(method) || [];
          const idx = callbacks.indexOf(handler);
          if (idx >= 0) callbacks.splice(idx, 1);
          listeners.set(method, callbacks);
        };
        const callbacks = listeners.get(method) || [];
        callbacks.push(handler);
        listeners.set(method, callbacks);
      });
    },
    close() {
      ws.close();
    },
  };
}

async function connectPage(url) {
  const versionRes = await fetch('http://' + HOST + ':' + debugPort + '/json/version');
  if (!versionRes.ok) throw new Error('Failed to query browser version');
  const version = await versionRes.json();
  if (!version.webSocketDebuggerUrl) throw new Error('Missing browser websocket debugger url');

  const browserClient = createCdp(version.webSocketDebuggerUrl);
  await browserClient.ready;
  const created = await browserClient.call('Target.createTarget', { url: 'about:blank' });
  browserClient.close();

  const targetId = created.targetId;
  const deadline = Date.now() + 10000;
  let pageWsUrl = null;
  while (Date.now() < deadline) {
    const listRes = await fetch('http://' + HOST + ':' + debugPort + '/json/list');
    if (listRes.ok) {
      const targets = await listRes.json();
      const match = targets.find((target) => target.id === targetId || target.targetId === targetId);
      if (match && match.webSocketDebuggerUrl) {
        pageWsUrl = match.webSocketDebuggerUrl;
        break;
      }
    }
    await sleep(250);
  }
  if (!pageWsUrl) throw new Error('Failed to resolve page websocket debugger url');

  if (cdp) {
    try { cdp.close(); } catch {}
  }
  cdp = createCdp(pageWsUrl);
  await cdp.ready;
  await cdp.call('Page.enable');
  await cdp.call('Runtime.enable');

  if (url && url !== 'about:blank') {
    await navigate(url);
  } else {
    currentUrl = 'about:blank';
    currentTitle = '';
  }
}

async function navigate(url) {
  if (!cdp) throw new Error('No page open');
  const load = cdp.once('Page.loadEventFired', 15000).catch(() => null);
  await cdp.call('Page.navigate', { url });
  await load;
  try {
    const state = await cdp.call('Runtime.evaluate', {
      expression: '({ url: location.href, title: document.title })',
      returnByValue: true,
    });
    currentUrl = state?.result?.value?.url || url;
    currentTitle = state?.result?.value?.title || '';
  } catch {
    currentUrl = url;
    currentTitle = '';
  }
}

async function launchBrowser(body) {
  if (body.browser && body.browser !== 'chromium' && body.browser !== 'chrome') {
    throw new Error('Only Chromium is supported');
  }
  if (body.url) {
    assertBrowserTargetAllowed(body.url);
  }

  await stopBrowser();
  const chrome = findChromeBinary();
  if (!chrome) throw new Error('Chromium binary not found');

  debugPort = await freePort();
  const profileDir = join(BROWSER_STATE_DIR, 'chromium-profile');
  mkdirSync(profileDir, { recursive: true });

  const args = [
    '--remote-debugging-port=' + debugPort,
    '--user-data-dir=' + profileDir,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-dev-shm-usage',
    '--disable-gpu',
    '--no-sandbox',
  ];
  if (body.headless !== false) {
    args.push('--headless=new');
  }
  args.push('about:blank');

  browserProcess = spawn(chrome, args, { stdio: ['ignore', 'ignore', 'pipe'] });
  browserProcess.stderr?.on('data', () => {});
  browserProcess.on('exit', () => {
    browserProcess = null;
    debugPort = null;
    if (cdp) {
      try { cdp.close(); } catch {}
      cdp = null;
    }
  });

  let cleanupStartupWatchers = () => {};
  const startupFailure = new Promise((_, reject) => {
    const onError = (error) => reject(error);
    const onExit = () => reject(new Error('Chromium exited before launch completed'));
    cleanupStartupWatchers = () => {
      browserProcess?.off('error', onError);
      browserProcess?.off('exit', onExit);
    };
    browserProcess.once('error', onError);
    browserProcess.once('exit', onExit);
  });
  const startupReady = Promise.race([
    waitForHttp('http://' + HOST + ':' + debugPort + '/json/version'),
    startupFailure,
  ]).finally(() => {
    cleanupStartupWatchers();
  });

  try {
    await startupReady;
    await connectPage(body.url || 'about:blank');
    return { ok: true, engine: ENGINE, url: currentUrl };
  } catch (error) {
    await stopBrowser();
    throw error;
  }
}

async function snapshotPage() {
  if (!cdp) throw new Error('No page open');
  let bodyText = '';
  try {
    const result = await cdp.call('Runtime.evaluate', {
      expression: 'document.body ? (document.body.innerText || document.body.textContent || "") : ""',
      returnByValue: true,
    });
    bodyText = String(result?.result?.value || '');
  } catch {
    bodyText = '';
  }
  return [currentTitle || '', bodyText].filter(Boolean).join('\n').trim();
}

async function stopBrowser() {
  if (cdp) {
    try { cdp.close(); } catch {}
    cdp = null;
  }
  if (browserProcess) {
    const proc = browserProcess;
    browserProcess = null;
    try { proc.kill('SIGTERM'); } catch {}
    await Promise.race([
      new Promise((resolve) => proc.once('exit', resolve)),
      sleep(1500),
    ]);
    try { proc.kill('SIGKILL'); } catch {}
  }
  currentUrl = null;
  currentTitle = null;
}

const server = createServer(async (req, res) => {
  try {
    if (req.method === 'GET' && req.url === '/health') {
      json(res, 200, { ok: true, engine: ENGINE, browser: !!browserProcess, page: !!cdp, url: currentUrl, title: currentTitle });
      return;
    }

    if (req.method === 'POST' && req.url === '/launch') {
      const body = await readBody(req);
      const result = await launchBrowser(body);
      json(res, 200, result);
      return;
    }

    if (req.method === 'POST' && req.url === '/navigate') {
      const body = await readBody(req);
      if (body.action === 'reload') {
        await navigate(currentUrl || 'about:blank');
      } else if (body.url) {
        assertBrowserTargetAllowed(body.url);
        await navigate(body.url);
      } else {
        throw new Error('url or action is required');
      }
      json(res, 200, { ok: true, url: currentUrl, title: currentTitle });
      return;
    }

    if (req.method === 'GET' && req.url.startsWith('/snapshot')) {
      const snapshot = await snapshotPage();
      json(res, 200, { ok: true, snapshot, url: currentUrl, title: currentTitle });
      return;
    }

    if (req.method === 'POST' && req.url === '/stop') {
      await stopBrowser();
      json(res, 200, { ok: true });
      return;
    }

    json(res, 404, { ok: false, error: 'not found' });
  } catch (error) {
    json(res, 500, { ok: false, error: error.message || 'internal error' });
  }
});

server.listen(PORT, HOST, () => {
  console.log('odin-huginn-server listening on ' + HOST + ':' + PORT + ' (engine: ' + ENGINE + ')');
});

async function shutdown() {
  try {
    await stopBrowser();
  } finally {
    server.close(() => process.exit(0));
  }
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);

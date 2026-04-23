#!/usr/bin/env node
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { accessSync, constants as fsConstants, mkdirSync, statSync, writeFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import net from 'node:net';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const REPO_ROOT = join(__dirname, '..', '..');
const ODIN_DIR = process.env.ODIN_DIR || process.env.ODIN_ROOT || join(REPO_ROOT, '.odin-browser');
const BROWSER_STATE_DIR = join(ODIN_DIR, 'browser-state');
const LOG_DIR = join(ODIN_DIR, 'logs', new Date().toISOString().slice(0, 10));
const PORT = Number.parseInt(process.env.ODIN_BROWSER_PORT || '19227', 10);
const HOST = '127.0.0.1';
const ENGINE = 'chromium';

mkdirSync(BROWSER_STATE_DIR, { recursive: true });
mkdirSync(LOG_DIR, { recursive: true });

let browserProcess = null;
let debugPort = null;
let debugBaseUrl = null;
let cdp = null;
let currentUrl = null;
let currentTitle = null;

const ALLOWED_TARGET_SCHEMES = new Set([
  'http',
  'https',
  'data',
  'about',
]);

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

function normalizeHostForMatching(host) {
  let normalized = String(host || '').toLowerCase();
  if (normalized.startsWith('[') && normalized.endsWith(']')) {
    normalized = normalized.slice(1, -1);
  }
  normalized = normalized.replace(/%2e/gi, '.');
  while (normalized.endsWith('.')) {
    normalized = normalized.slice(0, -1);
  }
  return normalized;
}

function stripIpv6ZoneId(host) {
  return String(host || '').replace(/%25.*$/, '');
}

function browserDomainDenylist() {
  return String(process.env.ODIN_BROWSER_DOMAIN_DENYLIST || 'localhost,127.0.0.1,::1,*.local')
    .split(',')
    .map((entry) => entry.trim().toLowerCase())
    .filter(Boolean);
}

function ipv4IsLocalService(host) {
  const normalized = normalizeHostForMatching(host);
  const match = normalized.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (!match) {
    return false;
  }

  const a = Number.parseInt(match[1], 10);
  const b = Number.parseInt(match[2], 10);
  const c = Number.parseInt(match[3], 10);
  const d = Number.parseInt(match[4], 10);
  if ([a, b, c, d].some((part) => Number.isNaN(part) || part < 0 || part > 255)) {
    return false;
  }

  return a === 127 || a === 10 || (a === 192 && b === 168) || (a === 172 && b >= 16 && b <= 31);
}

function ipv4ValueIsLocalService(value) {
  const a = (value >>> 24) & 255;
  const b = (value >>> 16) & 255;
  return a === 127 || a === 10 || (a === 192 && b === 168) || (a === 172 && b >= 16 && b <= 31);
}

function ipv4MappedIpv6IsLocalService(host) {
  const normalized = stripIpv6ZoneId(normalizeHostForMatching(host));
  if (!normalized.startsWith('::ffff:')) {
    return false;
  }

  const tail = normalized.slice('::ffff:'.length).replace(/\.+$/, '');
  if (ipv4IsLocalService(tail)) {
    return true;
  }

  const mapped = tail.match(/^([0-9a-f]{1,4}):([0-9a-f]{1,4})$/i);
  if (!mapped) {
    return false;
  }

  const hi = Number.parseInt(mapped[1], 16);
  const lo = Number.parseInt(mapped[2], 16);
  const ipv4 = ((hi << 16) | lo) >>> 0;
  return ipv4ValueIsLocalService(ipv4);
}

function ipv6IsPrivateService(host) {
  const normalized = stripIpv6ZoneId(normalizeHostForMatching(host));
  if (!normalized.includes(':')) {
    return false;
  }

  if (normalized === '::1') {
    return true;
  }
  if (normalized.startsWith('fd')) {
    return true;
  }
  if (/^fe[89ab]/.test(normalized)) {
    return true;
  }
  return false;
}

function browserHostIsLocalService(host) {
  const normalized = stripIpv6ZoneId(normalizeHostForMatching(host));
  if (normalized === 'localhost' || normalized.endsWith('.localhost')) {
    return true;
  }
  if (normalized === '::1') {
    return true;
  }
  if (ipv4IsLocalService(normalized)) {
    return true;
  }
  if (ipv6IsPrivateService(normalized)) {
    return true;
  }
  if (ipv4MappedIpv6IsLocalService(normalized)) {
    return true;
  }
  return false;
}

function browserHostMatchesDenylist(host) {
  const normalized = normalizeHostForMatching(host);
  if (!normalized) {
    return false;
  }

  for (const entry of browserDomainDenylist()) {
    if (entry.startsWith('*.')) {
      const suffix = entry.slice(2);
      if (normalized === suffix || normalized.endsWith('.' + suffix)) {
        return true;
      }
    } else if (normalized === entry) {
      return true;
    }
  }

  return false;
}

function assertBrowserTargetAllowed(target) {
  const url = new URL(target);
  const scheme = url.protocol.slice(0, -1).toLowerCase();
  if (!ALLOWED_TARGET_SCHEMES.has(scheme)) {
    throw new Error('Blocked browser URL');
  }
  if (scheme === 'data' || scheme === 'about') {
    return;
  }
  if (browserHostIsLocalService(url.hostname) || browserHostMatchesDenylist(url.hostname)) {
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
  const baseUrl = debugBaseUrl || ('http://' + HOST + ':' + debugPort);
  const versionRes = await fetch(baseUrl + '/json/version');
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
    const listRes = await fetch(baseUrl + '/json/list');
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
  debugBaseUrl = 'http://' + HOST + ':' + debugPort;
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

async function connectBrowser(body) {
  const cdpUrl = String(body?.cdpUrl || '').trim().replace(/\/+$/, '');
  if (!cdpUrl) {
    throw new Error('cdpUrl is required');
  }

  await stopBrowser();
  debugBaseUrl = cdpUrl;
  await waitForHttp(debugBaseUrl + '/json/version');
  await connectPage(body.url || 'about:blank');
  return { ok: true, engine: ENGINE, url: currentUrl };
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

async function screenshotPage(body) {
  if (!cdp) throw new Error('No page open');

  const result = await cdp.call('Page.captureScreenshot', {
    format: 'png',
    fromSurface: true,
  });
  const data = String(result?.data || '');
  if (!data) {
    throw new Error('Screenshot capture failed');
  }

  const screenshotPath = body?.path ? String(body.path) : join(BROWSER_STATE_DIR, 'browser.png');
  mkdirSync(dirname(screenshotPath), { recursive: true });
  writeFileSync(screenshotPath, Buffer.from(data, 'base64'));
  return { ok: true, screenshot_path: screenshotPath, url: currentUrl, title: currentTitle };
}

async function evaluatePage(body) {
  if (!cdp) throw new Error('No page open');
  const fn = String(body?.fn || '').trim();
  if (!fn) {
    throw new Error('fn is required');
  }

  const result = await cdp.call('Runtime.evaluate', {
    expression: fn,
    awaitPromise: true,
    returnByValue: true,
  });

  let value = null;
  if (Object.prototype.hasOwnProperty.call(result?.result || {}, 'value')) {
    value = result.result.value;
  } else if (result?.result?.description) {
    value = result.result.description;
  }

  try {
    const state = await cdp.call('Runtime.evaluate', {
      expression: '({ url: location.href, title: document.title })',
      returnByValue: true,
    });
    currentUrl = state?.result?.value?.url || currentUrl;
    currentTitle = state?.result?.value?.title || currentTitle;
  } catch {
    // best effort only
  }

  return { ok: true, result: value, url: currentUrl, title: currentTitle };
}

async function typeIntoSelector(selector, text, submit) {
  if (!cdp) throw new Error('No page open');
  const selectorExpr = JSON.stringify(String(selector || ''));
  const focusResult = await cdp.call('Runtime.evaluate', {
    expression: `(() => {
      const el = document.querySelector(${selectorExpr});
      if (!el) return { ok: false, reason: 'selector_not_found' };
      el.focus();
      if (typeof el.select === 'function') {
        try { el.select(); } catch {}
      } else if (typeof el.setSelectionRange === 'function') {
        try {
          const length = typeof el.value === 'string' ? el.value.length : 0;
          el.setSelectionRange(0, length);
        } catch {}
      }
      return { ok: true };
    })()`,
    awaitPromise: true,
    returnByValue: true,
  });
  const focusValue = focusResult?.result?.value || {};
  if (!focusValue?.ok) {
    throw new Error(focusValue?.reason || 'selector_not_found');
  }

  await cdp.call('Input.insertText', { text: String(text || '') });

  if (submit) {
    const enterParams = {
      key: 'Enter',
      code: 'Enter',
      windowsVirtualKeyCode: 13,
      nativeVirtualKeyCode: 13,
    };
    await cdp.call('Input.dispatchKeyEvent', { type: 'rawKeyDown', ...enterParams });
    await cdp.call('Input.dispatchKeyEvent', { type: 'keyUp', ...enterParams });
  }

  try {
    const state = await cdp.call('Runtime.evaluate', {
      expression: '({ url: location.href, title: document.title })',
      returnByValue: true,
    });
    currentUrl = state?.result?.value?.url || currentUrl;
    currentTitle = state?.result?.value?.title || currentTitle;
  } catch {
    // best effort only
  }

  return { ok: true, url: currentUrl, title: currentTitle };
}

async function clickSelector(selector) {
  if (!cdp) throw new Error('No page open');
  const selectorExpr = JSON.stringify(String(selector || ''));
  const targetResult = await cdp.call('Runtime.evaluate', {
    expression: `(() => {
      const el = document.querySelector(${selectorExpr});
      if (!el) return { ok: false, reason: 'selector_not_found' };
      el.scrollIntoView({ block: 'center', inline: 'center' });
      const rect = el.getBoundingClientRect();
      if (!rect || rect.width <= 0 || rect.height <= 0) {
        return { ok: false, reason: 'selector_not_interactable' };
      }
      return {
        ok: true,
        x: rect.left + (rect.width / 2),
        y: rect.top + (rect.height / 2),
      };
    })()`,
    awaitPromise: true,
    returnByValue: true,
  });
  const targetValue = targetResult?.result?.value || {};
  if (!targetValue?.ok) {
    throw new Error(targetValue?.reason || 'selector_not_found');
  }

  const x = Number(targetValue.x);
  const y = Number(targetValue.y);
  if (!Number.isFinite(x) || !Number.isFinite(y)) {
    throw new Error('selector_not_interactable');
  }

  const clickParams = { x, y, button: 'left', clickCount: 1 };
  await cdp.call('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y, button: 'none' });
  await cdp.call('Input.dispatchMouseEvent', { type: 'mousePressed', ...clickParams });
  await cdp.call('Input.dispatchMouseEvent', { type: 'mouseReleased', ...clickParams });

  try {
    const state = await cdp.call('Runtime.evaluate', {
      expression: '({ url: location.href, title: document.title })',
      returnByValue: true,
    });
    currentUrl = state?.result?.value?.url || currentUrl;
    currentTitle = state?.result?.value?.title || currentTitle;
  } catch {
    // best effort only
  }

  return { ok: true, url: currentUrl, title: currentTitle };
}

async function handleAct(body) {
  if (!cdp) throw new Error('No page open');
  const kind = String(body?.kind || '');
  if (!kind) throw new Error('kind is required');

  if (kind === 'type') {
    const selector = String(body?.selector || '').trim();
    if (!selector) throw new Error('selector is required for type');
    const submit = body?.submit === true;
    const text = String(body?.text || '');
    return await typeIntoSelector(selector, text, submit);
  }

  if (kind === 'click') {
    const selector = String(body?.selector || '').trim();
    if (!selector) throw new Error('selector is required for click');
    return await clickSelector(selector);
  }

  throw new Error('unsupported action');
}

async function stopBrowser() {
  if (cdp) {
    try { cdp.close(); } catch {}
    cdp = null;
  }
  debugBaseUrl = null;
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

    if (req.method === 'POST' && req.url === '/connect') {
      const body = await readBody(req);
      const result = await connectBrowser(body);
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

    if (req.method === 'POST' && req.url === '/screenshot') {
      const body = await readBody(req);
      const result = await screenshotPage(body);
      json(res, 200, result);
      return;
    }

    if (req.method === 'POST' && req.url === '/evaluate') {
      const body = await readBody(req);
      const result = await evaluatePage(body);
      json(res, 200, result);
      return;
    }

    if (req.method === 'POST' && req.url === '/act') {
      const body = await readBody(req);
      const result = await handleAct(body);
      json(res, 200, result);
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

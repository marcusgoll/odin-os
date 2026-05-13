const csrfKey = 'odin.mobile.csrf';
const failedKey = 'odin.mobile.failedUploads';
const form = document.querySelector('#capture-form');
const statusEl = document.querySelector('#capture-status');
const imageInput = document.querySelector('#image-input');
const mediaState = document.querySelector('#media-state');
const failedList = document.querySelector('#failed-uploads');
const dashboardError = document.querySelector('#dashboard-error');
const offlineState = document.querySelector('#offline-state');
const homeSummary = document.querySelector('#home-summary');
let voiceBlob = null;
let recorder = null;
let chunks = [];
let pendingApprovalDecision = null;

function csrfToken() {
  return sessionStorage.getItem(csrfKey) || '';
}

function failedUploads() {
  try {
    return JSON.parse(localStorage.getItem(failedKey) || '[]');
  } catch {
    return [];
  }
}

function saveFailed(items) {
  localStorage.setItem(failedKey, JSON.stringify(items.slice(-20)));
  renderFailed();
}

function setStatus(message) {
  statusEl.textContent = message;
}

function showError(message) {
  dashboardError.hidden = !message;
  dashboardError.textContent = message || '';
}

function setCount(id, value) {
  const node = document.querySelector(id);
  if (node) node.textContent = String(value);
}

function clearNode(node) {
  node.textContent = '';
}

function emptyCard(message, detail) {
  const article = document.createElement('article');
  article.className = 'status-card empty';
  const title = document.createElement('p');
  title.className = 'card-title';
  title.textContent = message;
  article.appendChild(title);
  if (detail) {
    const body = document.createElement('p');
    body.className = 'card-body';
    body.textContent = detail;
    article.appendChild(body);
  }
  return article;
}

function projectionCard(kind, title, meta, body) {
  const article = document.createElement('article');
  article.className = `status-card ${kind || ''}`.trim();
  const row = document.createElement('div');
  row.className = 'card-row';
  const h3 = document.createElement('h3');
  h3.textContent = title || 'Untitled';
  row.appendChild(h3);
  if (meta) {
    const badge = document.createElement('span');
    badge.className = 'badge';
    badge.textContent = meta;
    row.appendChild(badge);
  }
  article.appendChild(row);
  if (body) {
    const text = document.createElement('p');
    text.className = 'card-body';
    text.textContent = body;
    article.appendChild(text);
  }
  return article;
}

function renderFailed() {
  const items = failedUploads();
  failedList.innerHTML = '';
  if (items.length === 0) {
    const li = document.createElement('li');
    li.textContent = 'No failed local captures.';
    failedList.appendChild(li);
    return;
  }
  for (const item of items) {
    const li = document.createElement('li');
    const media = item.attachment ? ` (${item.attachment.kind})` : '';
    li.textContent = `${item.title || item.kind || 'capture'}${media}: ${item.error || 'upload failed'}`;
    failedList.appendChild(li);
  }
}

function selectedKind() {
  return new FormData(form).get('kind') || 'note';
}

function textPayload() {
  return {
    kind: selectedKind(),
    title: document.querySelector('#capture-title').value,
    content: document.querySelector('#capture-body').value,
    transcript: document.querySelector('#capture-transcript').value,
    source_app: document.querySelector('#source-app').value,
    share_url: document.querySelector('#share-url').value,
  };
}

async function mobileFetch(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  const csrf = csrfToken();
  if (csrf) headers['X-Odin-CSRF'] = csrf;
  const response = await fetch(path, { ...options, headers, credentials: 'same-origin' });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  return response.json();
}

async function sendCapture(payload, attachment) {
  let body;
  const headers = {};
  if (attachment) {
    body = new FormData();
    Object.entries(payload).forEach(([key, value]) => body.append(key, value || ''));
    body.set('kind', attachment.kind);
    body.append('attachment', attachment.blob, attachment.name);
  } else {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(payload);
  }
  return mobileFetch('/mobile/intake/raw', { method: 'POST', headers, body });
}

async function refreshDashboard() {
  showError('');
  try {
    const [status, overview, reviewQueue, approvals, browser, notifications] = await Promise.all([
      mobileFetch('/mobile/status'),
      mobileFetch('/mobile/overview'),
      mobileFetch('/mobile/review-queue'),
      mobileFetch('/mobile/approvals'),
      mobileFetch('/mobile/browser/status'),
      mobileFetch('/mobile/notifications/preferences'),
    ]);
    renderDashboard({ status, overview, reviewQueue, approvals, browser, notifications });
  } catch (error) {
    renderAuthRequired();
    showError(`Could not load Odin projections: ${error.message}`);
  }
}

function renderAuthRequired() {
  homeSummary.textContent = 'Register this device to load live projections.';
  for (const id of [
    '#action-required-list',
    '#approvals-list',
    '#failed-blocked-list',
    '#today-list',
    '#inbox-list',
    '#running-work-list',
    '#browser-list',
    '#quiet-list',
  ]) {
    const node = document.querySelector(id);
    clearNode(node);
    node.appendChild(emptyCard('Device registration required', 'Use Register. No production mock data is shown.'));
  }
}

function renderDashboard({ status, overview, reviewQueue, approvals, browser, notifications }) {
  const reviewItems = reviewQueue.items || [];
  const approvalItems = approvals.items || [];
  const actionCount = overview.actual_use?.action_required_count ?? reviewItems.length;
  homeSummary.textContent = actionCount > 0
    ? `${actionCount} action-required item${actionCount === 1 ? '' : 's'} from live projections.`
    : 'No action-required rows in current projections.';

  renderActionRequired(overview, reviewItems, browser);
  renderApprovals(approvalItems);
  renderFailedBlocked(overview, reviewItems);
  renderToday(status, overview, notifications);
  renderInbox(overview);
  renderRunningWork(overview);
  renderBrowser(browser);
  renderQuietLater(overview, notifications);
}

function renderActionRequired(overview, reviewItems, browser) {
  const node = document.querySelector('#action-required-list');
  clearNode(node);
  const items = [];
  for (const item of reviewItems.slice(0, 4)) {
    items.push(projectionCard('urgent', item.title || item.object_key, item.source_type, item.reason || item.status));
  }
  const loginNeeds = (browser.login_requests || []).filter((item) => item.status !== 'completed');
  for (const item of loginNeeds.slice(0, 2)) {
    items.push(projectionCard('warn', `Browser login request ${item.id}`, item.status, `Session ${item.session_id} expires ${item.expires_at}`));
  }
  if (items.length === 0) {
    items.push(emptyCard('No action-required rows', 'Odin overview has no review, blocked, failed, or browser intervention rows right now.'));
  }
  setCount('#action-count', overview.actual_use?.action_required_count ?? items.length);
  items.forEach((item) => node.appendChild(item));
}

function renderApprovals(items) {
  const node = document.querySelector('#approvals-list');
  clearNode(node);
  setCount('#approvals-count', items.length);
  if (items.length === 0) {
    node.appendChild(emptyCard('No pending approvals', 'New approval requests will appear here with resolver and consequence details.'));
    return;
  }
  for (const item of items) {
    const card = projectionCard('approval', item.task_key || `Approval ${item.approval_id}`, item.resolver_support || item.status, `Risk: resolver=${item.resolver_support || 'unknown'} on governed work. Consequence: approve lets the resolver continue; deny keeps the work blocked and records the reason.`);
    const row = document.createElement('div');
    row.className = 'button-row';
    for (const action of ['approve', 'deny']) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = action === 'approve' ? 'primary small' : 'danger-button small';
      button.textContent = action === 'approve' ? 'Approve' : 'Deny';
      button.setAttribute('aria-label', `${button.textContent} approval ${item.approval_id}`);
      button.addEventListener('click', () => openApprovalConfirmation(item, action));
      row.appendChild(button);
    }
    card.appendChild(row);
    node.appendChild(card);
  }
}

function openApprovalConfirmation(item, action) {
  pendingApprovalDecision = { item, action };
  document.querySelector('#approval-confirmation-summary').textContent =
    `${action.toUpperCase()} approval ${item.approval_id} for ${item.task_key}. This writes an approval audit event through Odin.`;
  document.querySelector('#approval-reason').value = '';
  document.querySelector('#approval-confirmation').hidden = false;
  document.querySelector('#approval-reason').focus();
}

async function confirmApprovalDecision() {
  if (!pendingApprovalDecision) return;
  const reason = document.querySelector('#approval-reason').value.trim();
  if (!reason) {
    setStatus('Approval reason is required.');
    return;
  }
  const { item, action } = pendingApprovalDecision;
  await mobileFetch(`/mobile/approvals/${item.approval_id}/decision`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action, reason, decision_by: 'odin-pwa' }),
  });
  document.querySelector('#approval-confirmation').hidden = true;
  pendingApprovalDecision = null;
  setStatus(`Approval ${action} recorded.`);
  await refreshDashboard();
}

function renderFailedBlocked(overview, reviewItems) {
  const node = document.querySelector('#failed-blocked-list');
  clearNode(node);
  const failed = reviewItems.filter((item) => item.source_type === 'failed_work');
  const blocked = overview.observability?.blocked_work || [];
  const recovery = overview.observability?.recovery_guidance || [];
  setCount('#failed-blocked-count', failed.length + blocked.length + recovery.length);
  for (const item of failed.slice(0, 4)) {
    node.appendChild(projectionCard('danger', item.title || item.object_key, 'failed work', `Allowed actions: ${(item.allowed_actions || []).join(', ') || 'inspect'}`));
  }
  for (const item of blocked.slice(0, 4)) {
    node.appendChild(projectionCard('warn', item.work_item_key, item.reason || item.source, `${item.project_key || 'workspace'} remains blocked.`));
  }
  for (const item of recovery.slice(0, 4)) {
    node.appendChild(projectionCard('warn', item.work_item_key, item.decision, item.recovery_recommendation || item.last_error || 'Inspect failed work before retry.'));
  }
  if (!node.childElementCount) {
    node.appendChild(emptyCard('No failed or blocked rows', 'Failed work and blocked work stay visible here until runtime projections clear them.'));
  }
}

function renderToday(status, overview, notifications) {
  const node = document.querySelector('#today-list');
  clearNode(node);
  node.appendChild(projectionCard('info', `Readiness: ${overview.readiness?.status || 'unknown'}`, status.health_status || 'health unknown', overview.readiness?.note || 'Readiness comes from Odin runtime status.'));
  node.appendChild(projectionCard('info', `Review queue: ${overview.review_queue?.total_count || 0}`, 'today', `${overview.actual_use?.open_work_item_count || 0} open work items, ${overview.actual_use?.active_run_count || 0} active run attempts.`));
  node.appendChild(projectionCard('info', `Notifications: ${notifications.status || overview.notifications?.wiring || 'projection'}`, overview.notifications?.notifications_enabled ? 'enabled' : 'not enabled', `${overview.notifications?.in_app_unread_count || 0} unread in-app notifications.`));
}

function renderInbox(overview) {
  const node = document.querySelector('#inbox-list');
  clearNode(node);
  const inbox = overview.intake_inbox || {};
  setCount('#inbox-count', inbox.raw_item_count || 0);
  node.appendChild(projectionCard('info', `Raw intake: ${inbox.raw_item_count || 0}`, inbox.status || 'empty', inbox.note || 'Raw intake rows appear before governed work exists.'));
  for (const item of (inbox.raw_items || []).slice(0, 4)) {
    node.appendChild(projectionCard('info', item.subject || `Intake ${item.id}`, item.status, `${item.source_family || item.source || 'intake'} ${item.event_kind || item.intake_type || ''}`.trim()));
  }
}

function renderRunningWork(overview) {
  const node = document.querySelector('#running-work-list');
  clearNode(node);
  const activeRuns = overview.observability?.active_runs || [];
  setCount('#running-work-count', activeRuns.length);
  for (const run of activeRuns.slice(0, 5)) {
    node.appendChild(projectionCard('info', run.work_item_key, run.status, `Run ${run.run_id}, attempt ${run.attempt}, ${run.executor}`));
  }
  if (!activeRuns.length) {
    node.appendChild(emptyCard('No active run attempts', 'Running work appears only when Odin reports active run attempts.'));
  }
}

function renderBrowser(browser) {
  const node = document.querySelector('#browser-list');
  clearNode(node);
  const requests = (browser.login_requests || []).filter((item) => item.status !== 'completed');
  const runners = (browser.runners || []).filter((item) => item.error_code || ['failed', 'expired'].includes(item.status));
  setCount('#browser-count', requests.length + runners.length);
  for (const item of requests) {
    node.appendChild(projectionCard('warn', `Login request ${item.id}`, item.status, `Session ${item.session_id}; expires ${item.expires_at}`));
  }
  for (const item of runners) {
    node.appendChild(projectionCard('danger', `Browser runner ${item.id}`, item.status, item.error_message || item.error_code || 'Browser handoff needs inspection.'));
  }
  if (!node.childElementCount) {
    node.appendChild(emptyCard('No browser intervention rows', 'Login, MFA, CAPTCHA, and runner failures appear here from browser status projections.'));
  }
}

function renderQuietLater(overview, notifications) {
  const node = document.querySelector('#quiet-list');
  clearNode(node);
  const notificationLane = overview.notifications || {};
  node.appendChild(projectionCard('quiet', 'Notification posture', notificationLane.notifications_enabled ? 'enabled' : 'not enabled', `Quiet hours: ${notificationLane.quiet_hours || notifications.status || 'not configured'}. Batching: ${notificationLane.batching || 'none'}.`));
  const triggers = overview.automation_triggers || {};
  node.appendChild(projectionCard('quiet', `Automation triggers: ${triggers.trigger_count || 0}`, `${triggers.enabled_count || 0} enabled`, 'Due and quiet follow-up work stays projection-only here.'));
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  const payload = textPayload();
  const image = imageInput.files[0];
  const selected = selectedKind();
  const attachment = voiceBlob
    ? { kind: 'voice_note', blob: voiceBlob, name: 'voice-note.webm' }
    : image
      ? { kind: 'photo', blob: image, name: image.name || 'photo.jpg' }
      : null;
  if (!attachment && !payload.content.trim()) {
    setStatus('Capture body is required unless a photo or voice note is attached.');
    return;
  }
  if (selected === 'voice_note' && !voiceBlob) {
    setStatus('Record a voice note before submitting voice capture.');
    return;
  }
  if (selected === 'photo' && !image) {
    setStatus('Attach a photo before submitting photo capture.');
    return;
  }
  try {
    await sendCapture(payload, attachment);
    form.reset();
    voiceBlob = null;
    mediaState.textContent = 'No media attached';
    setStatus('Captured as raw intake.');
    await refreshDashboard();
  } catch (error) {
    const failed = { ...payload, error: error.message, retryable: true };
    if (attachment) {
      failed.attachment = {
        kind: attachment.kind,
        name: attachment.name,
        type: attachment.blob.type,
        data_url: await blobToDataURL(attachment.blob),
      };
    }
    saveFailed([...failedUploads(), failed]);
    setStatus('Upload failed. It is in the retry list.');
  }
});

imageInput.addEventListener('change', () => {
  const file = imageInput.files[0];
  mediaState.textContent = file ? `Photo ready: ${file.name}` : 'No media attached';
});

document.querySelector('#register-device').addEventListener('click', async () => {
  const value = window.prompt('One-time Odin admin token');
  if (value === null || value.trim() === '') {
    return;
  }
  const response = await fetch('/mobile/devices/register', {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      Authorization: `Bearer ${value.trim()}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ device_name: navigator.userAgent || 'mobile browser' }),
  });
  if (!response.ok) {
    setStatus('Device registration failed.');
    return;
  }
  const payload = await response.json();
  sessionStorage.setItem(csrfKey, payload.csrf_token || '');
  setStatus('Device registered for this browser session.');
  await refreshDashboard();
});

document.querySelector('#refresh-dashboard').addEventListener('click', refreshDashboard);

document.querySelector('#capture-fab').addEventListener('click', () => {
  document.querySelector('#capture-body').focus();
  document.querySelector('.capture-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
});

document.querySelector('#approval-cancel').addEventListener('click', () => {
  pendingApprovalDecision = null;
  document.querySelector('#approval-confirmation').hidden = true;
});

document.querySelector('#approval-confirm').addEventListener('click', () => {
  confirmApprovalDecision().catch((error) => setStatus(`Approval decision failed: ${error.message}`));
});

document.querySelector('#voice-record').addEventListener('click', async () => {
  if (recorder && recorder.state === 'recording') {
    recorder.stop();
    return;
  }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    chunks = [];
    recorder = new MediaRecorder(stream, { mimeType: 'audio/webm' });
    recorder.ondataavailable = (event) => chunks.push(event.data);
    recorder.onstop = () => {
      voiceBlob = new Blob(chunks, { type: 'audio/webm' });
      stream.getTracks().forEach((track) => track.stop());
      mediaState.textContent = 'Voice note ready';
    };
    recorder.start();
    mediaState.textContent = 'Recording voice... tap Voice again to stop.';
  } catch (error) {
    mediaState.textContent = `Voice unavailable: ${error.message}`;
  }
});

document.querySelector('#retry-failed').addEventListener('click', async () => {
  const pending = failedUploads();
  const remaining = [];
  for (const item of pending) {
    try {
      const attachment = item.attachment
        ? {
            kind: item.attachment.kind,
            name: item.attachment.name,
            blob: dataURLToBlob(item.attachment.data_url, item.attachment.type),
          }
        : null;
      await sendCapture(item, attachment);
    } catch (error) {
      remaining.push({ ...item, error: error.message, retryable: true });
    }
  }
  saveFailed(remaining);
  setStatus(remaining.length ? 'Some captures still need retry.' : 'Failed queue cleared.');
  await refreshDashboard();
});

function updateOnlineState() {
  offlineState.hidden = navigator.onLine;
}

function blobToDataURL(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

function dataURLToBlob(dataURL, fallbackType) {
  const [prefix, encoded] = dataURL.split(',');
  const match = /data:([^;]+);base64/.exec(prefix);
  const type = match ? match[1] : fallbackType || 'application/octet-stream';
  const binary = atob(encoded || '');
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new Blob([bytes], { type });
}

window.addEventListener('online', updateOnlineState);
window.addEventListener('offline', updateOnlineState);
updateOnlineState();
renderFailed();
refreshDashboard();

if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/app/service-worker.js');
}

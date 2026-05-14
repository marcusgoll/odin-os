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
const registerButton = document.querySelector('#register-device');
const captureDetails = document.querySelector('#capture-details');
let voiceBlob = null;
let recorder = null;
let chunks = [];
let pendingApprovalDecision = null;
let pendingReviewDecision = null;
let registeredSession = false;

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

function setRegisteredState(authenticated) {
  registeredSession = authenticated;
  registerButton.disabled = authenticated;
  registerButton.classList.toggle('registered', authenticated);
  registerButton.textContent = authenticated ? 'Registered' : 'Register';
  registerButton.setAttribute(
    'aria-label',
    authenticated ? 'Mobile device registered for this browser session' : 'Register this mobile device',
  );
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

function humanizeToken(value) {
  return String(value || '')
    .replace(/[_-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
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

function appendCardFacts(card, facts) {
  const values = (facts || []).filter((value) => value !== undefined && value !== null && String(value).trim() !== '');
  if (!values.length) return;
  const dl = document.createElement('dl');
  dl.className = 'fact-grid';
  for (const fact of values) {
    const [label, ...rest] = String(fact).split('=');
    const dt = document.createElement('dt');
    const dd = document.createElement('dd');
    dt.textContent = humanizeToken(label || 'detail');
    dd.textContent = rest.length ? rest.join('=') : String(fact);
    dl.append(dt, dd);
  }
  card.appendChild(dl);
}

function appendActionRow(card, controls) {
  const active = (controls || []).filter(Boolean);
  if (!active.length) return;
  const row = document.createElement('div');
  row.className = 'button-row action-row';
  for (const control of active) row.appendChild(control);
  card.appendChild(row);
}

function actionButton(label, className, onClick) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = className || 'ghost-button small';
  button.textContent = label;
  button.addEventListener('click', onClick);
  return button;
}

function actionLink(label, href) {
  if (!href) return null;
  const link = document.createElement('a');
  link.className = 'link-button small';
  link.href = href;
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  link.textContent = label;
  return link;
}

function handoffLink(item) {
  const deepLink = String(item?.deep_link || '');
  if (!deepLink) return '';
  const separator = deepLink.includes('?') ? '&' : '?';
  return deepLink.includes('format=') ? deepLink : `${deepLink}${separator}format=html`;
}

function compactID(label, value) {
  if (value === undefined || value === null || value === '') return '';
  return `${label}=${value}`;
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
    throw await responseError(response);
  }
  return response.json();
}

async function responseError(response) {
  const text = await response.text();
  let code = '';
  let message = text || `HTTP ${response.status}`;
  try {
    const payload = JSON.parse(text);
    code = payload?.error?.code || '';
    message = payload?.error?.message || message;
  } catch {
    // Plain-text responses such as 405 still use the raw body as the message.
  }
  const error = new Error(message);
  error.code = code;
  error.status = response.status;
  return error;
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

async function hasRegisteredSession() {
  try {
    const response = await fetch('/app/session', { credentials: 'same-origin' });
    if (!response.ok) return true;
    const payload = await response.json();
    return Boolean(payload.authenticated);
  } catch {
    return true;
  }
}

function normalizeAdminTokenInput(value) {
  let token = String(value || '').trim();
  const assignment = token.match(/^ODIN_ADMIN_TOKEN\s*=\s*(.*)$/);
  if (assignment) token = assignment[1].trim();
  if ((token.startsWith('"') && token.endsWith('"')) || (token.startsWith("'") && token.endsWith("'"))) {
    token = token.slice(1, -1).trim();
  }
  return token.replace(/[\u200B-\u200D\uFEFF]/g, '').replace(/\s+/g, '');
}

function registrationFailureMessage(error) {
  if (error.code === 'admin_disabled') {
    return 'Odin admin token is not configured on this server.';
  }
  if (error.code === 'admin_auth_failed') {
    return 'Admin token did not match this Odin server. Run odin mobile token on the server and paste that value.';
  }
  return error.message;
}

async function refreshDashboard() {
  showError('');
  const authenticated = await hasRegisteredSession();
  setRegisteredState(authenticated);
  if (!authenticated) {
    renderAuthRequired();
    setStatus('Register this device to load Odin projections.');
    return;
  }
  renderLoading();
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
    if (error.code === 'admin_auth_required') {
      setRegisteredState(false);
      setStatus('Register this device to load Odin projections.');
      showError('');
      return;
    }
    showError(`Could not load Odin projections: ${error.message}`);
  }
}

function renderAuthRequired() {
  homeSummary.textContent = 'Register this device to load live projections.';
  setCount('#action-count', 0);
  setCount('#approvals-count', 0);
  setCount('#failed-blocked-count', 0);
  setCount('#inbox-count', 0);
  setCount('#running-work-count', 0);
  setCount('#browser-count', 0);
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

function renderLoading() {
  homeSummary.textContent = 'Loading live Odin projections...';
  setStatus(registeredSession ? 'Device registered. Loading projections...' : 'Loading projections...');
  setCount('#action-count', '...');
  setCount('#approvals-count', '...');
  setCount('#failed-blocked-count', '...');
  setCount('#inbox-count', '...');
  setCount('#running-work-count', '...');
  setCount('#browser-count', '...');
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
    node.appendChild(emptyCard('Loading live projections', 'Odin is reading the current operator queues.'));
  }
}

function renderDashboard({ status, overview, reviewQueue, approvals, browser, notifications }) {
  const reviewItems = reviewQueue.items || [];
  const approvalItems = approvals.items || [];
  const actionCount = workbenchActionCount(reviewItems, approvalItems, browser);
  const projectionCount = overview.actual_use?.action_required_count ?? reviewItems.length;
  setStatus('Device registered for this browser session.');
  homeSummary.textContent = actionCount > 0
    ? `${actionCount} live action card${actionCount === 1 ? '' : 's'} ready. Odin overview reports ${projectionCount} action-required row${projectionCount === 1 ? '' : 's'}.`
    : 'No action-required rows in current projections.';

  renderActionRequired(overview, reviewItems, approvalItems, browser);
  renderApprovals(approvalItems);
  renderFailedBlocked(overview, reviewItems);
  renderToday(status, overview, notifications);
  renderInbox(overview);
  renderRunningWork(overview);
  renderBrowser(browser);
  renderQuietLater(overview, notifications);
}

function workbenchActionCount(reviewItems, approvalItems, browser) {
  const approvalCount = approvalItems.filter((item) => (item.actions || []).length || item.status === 'pending').length;
  const reviewActionCount = reviewItems.filter((item) => {
    if (item.source_type === 'approval') return false;
    if (item.source_type === 'browser_attended_login') return true;
    if (item.source_type === 'intake_review') return true;
    if (item.source_type === 'browser_run_failed') return true;
    if (item.source_type === 'failed_work') return true;
    return false;
  }).length;
  const browserRunnerCount = (browser.runners || []).filter((item) => item.error_code || ['failed', 'expired'].includes(item.status)).length;
  return approvalCount + reviewActionCount + browserRunnerCount;
}

function renderActionRequired(overview, reviewItems, approvalItems, browser) {
  const node = document.querySelector('#action-required-list');
  clearNode(node);
  const items = [];
  for (const item of approvalItems.slice(0, 3)) {
    items.push(approvalWorkbenchCard(item));
  }
  for (const item of reviewItems) {
    if (items.length >= 6) break;
    if (item.source_type === 'approval') continue;
    if (item.source_type === 'browser_attended_login') {
      items.push(browserLoginWorkbenchCard(item));
    } else if (item.source_type === 'intake_review') {
      items.push(intakeReviewWorkbenchCard(item));
    } else if (item.source_type === 'browser_run_failed' || item.source_type === 'failed_work') {
      items.push(inspectOnlyWorkbenchCard(item, 'danger'));
    } else if (item.browser_event) {
      items.push(inspectOnlyWorkbenchCard(item, 'warn'));
    }
  }
  const runners = (browser.runners || []).filter((item) => item.error_code || ['failed', 'expired'].includes(item.status));
  for (const item of runners) {
    if (items.length >= 6) break;
    const card = projectionCard('danger', `Browser runner ${item.id}`, humanizeToken(item.status), item.error_message || item.error_code || 'Browser handoff needs inspection.');
    appendCardFacts(card, [compactID('login_request', item.login_request_id), compactID('session', item.session_id), compactID('status_code', item.error_code)]);
    appendActionRow(card, [actionButton('Refresh', 'ghost-button small', refreshDashboard)]);
    items.push(card);
  }
  if (items.length < 6) {
    const blocked = overview.observability?.blocked_work || [];
    for (const item of blocked.slice(0, 6 - items.length)) {
      items.push(projectionCard('warn', item.work_item_key, humanizeToken(item.reason || item.source), `${item.project_key || 'workspace'} remains blocked.`));
    }
  }
  if (items.length < 6) {
    const recovery = overview.observability?.recovery_guidance || [];
    for (const item of recovery.slice(0, 6 - items.length)) {
      items.push(projectionCard('warn', item.work_item_key, humanizeToken(item.decision), item.recovery_recommendation || item.last_error || 'Inspect failed work before retry.'));
    }
  }
  if (items.length === 0) {
    items.push(emptyCard('No action-required rows', 'Odin overview has no review, blocked, failed, or browser intervention rows right now.'));
  }
  setCount('#action-count', items.length);
  items.forEach((item) => node.appendChild(item));
}

function approvalWorkbenchCard(item) {
  const actions = Array.isArray(item.actions) && item.actions.length ? item.actions : ['approve', 'deny', 'clarify'];
  const risk = humanizeToken(item.risk_level || item.resolver_support || 'unknown');
  const card = projectionCard('urgent action-card', item.title || item.task_key || `Approval ${item.approval_id}`, `approval · ${risk}`, item.requested_action || item.required_reason || 'Approval is required before Odin continues.');
  appendCardFacts(card, [
    compactID('approval', item.approval_id),
    compactID('task', item.task_key),
    compactID('resolver', item.resolver_support || 'unknown'),
    compactID('policy', item.policy_snapshot_hash),
    compactID('runtime', item.runtime_snapshot_hash),
  ]);
  appendActionRow(card, actions.map((action) => {
    const className = action === 'approve' ? 'primary small' : action === 'clarify' ? 'ghost-button small' : 'danger-button small';
    return actionButton(actionLabel(action), className, () => openApprovalConfirmation(item, action));
  }));
  return card;
}

function browserLoginWorkbenchCard(item) {
  const card = projectionCard('warn action-card', item.title || item.object_key || 'Browser login required', 'browser handoff', item.reason || 'Manual login or MFA is blocking browser work.');
  appendCardFacts(card, [
    compactID('queue', item.queue_id),
    compactID('login_request', item.object_id),
    compactID('status', item.status),
    compactID('allowed', (item.allowed_actions || []).join(',')),
  ]);
  appendActionRow(card, [
    actionLink('Open handoff', handoffLink(item)),
    actionButton('Mark login complete', 'primary small', () => openReviewDecision(item, 'complete')),
  ]);
  return card;
}

function intakeReviewWorkbenchCard(item) {
  const card = projectionCard('urgent action-card', item.title || item.object_key || 'Intake review', 'intake review', item.reason || item.status || 'Operator decision required.');
  appendCardFacts(card, [
    compactID('queue', item.queue_id),
    compactID('intake', item.object_id),
    compactID('project', item.project_key || 'default'),
    compactID('status', item.status),
  ]);
  appendActionRow(card, [
    actionButton('Reject', 'danger-button small', () => openReviewDecision(item, 'reject')),
    actionButton('Clarify', 'ghost-button small', () => openReviewDecision(item, 'clarify')),
    actionButton('Archive', 'ghost-button small', () => openReviewDecision(item, 'archive')),
  ]);
  return card;
}

function inspectOnlyWorkbenchCard(item, kind) {
  const card = projectionCard(`${kind || 'warn'} action-card`, item.title || item.object_key || 'Review item', humanizeToken(item.source_type), item.reason || item.status || 'Inspect in Odin before acting.');
  appendCardFacts(card, [
    compactID('queue', item.queue_id),
    compactID('object', item.object_key || item.object_id),
    compactID('status', item.status),
    compactID('allowed', (item.allowed_actions || []).join(',')),
  ]);
  appendActionRow(card, [actionButton('Refresh', 'ghost-button small', refreshDashboard)]);
  return card;
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
    const actions = Array.isArray(item.actions) && item.actions.length ? item.actions : ['approve', 'deny'];
    const actionText = actions.join(', ');
    const risk = humanizeToken(item.risk_level || item.resolver_support || 'unknown');
    const detail = item.requested_action || item.required_reason || 'governed work';
    const card = projectionCard('approval', item.title || item.task_key || `Approval ${item.approval_id}`, risk, `Action: ${detail}. Allowed decisions: ${actionText}.`);
    const row = document.createElement('div');
    row.className = 'button-row';
    for (const action of actions) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = action === 'approve' ? 'primary small' : 'danger-button small';
      button.setAttribute('data-approval-action', action);
      button.textContent = actionLabel(action);
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
  const prompt = item.confirmation_prompt || '';
  document.querySelector('#approval-confirmation-summary').textContent =
    `${action.toUpperCase()} approval ${item.approval_id} for ${item.task_key}. This writes an approval audit event through Odin.`;
  document.querySelector('#approval-reason').value = '';
  const confirmationField = document.querySelector('#approval-confirmation-text-field');
  const confirmationInput = document.querySelector('#approval-confirmation-text');
  confirmationField.hidden = !prompt || action !== 'approve';
  confirmationInput.value = '';
  confirmationInput.placeholder = prompt || 'Required confirmation text';
  document.querySelector('#approval-confirmation').hidden = false;
  document.querySelector('#approval-reason').focus();
}

function openReviewDecision(item, action) {
  pendingReviewDecision = { item, action };
  const label = action === 'complete'
    ? 'Mark attended browser login complete'
    : `${actionLabel(action)} ${humanizeToken(item.source_type)}`;
  document.querySelector('#review-confirmation-summary').textContent =
    `${label} for ${item.queue_id}. Odin records this against the governed review queue.`;
  document.querySelector('#review-reason').value = '';
  document.querySelector('#review-confirmation').hidden = false;
  document.querySelector('#review-reason').focus();
}

async function confirmApprovalDecision() {
  if (!pendingApprovalDecision) return;
  const reason = document.querySelector('#approval-reason').value.trim();
  if (!reason) {
    setStatus('Approval reason is required.');
    return;
  }
  const { item, action } = pendingApprovalDecision;
  const confirmationText = document.querySelector('#approval-confirmation-text').value.trim();
  await mobileFetch(`/mobile/approvals/${item.approval_id}/decision`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      action,
      reason,
      actor: 'pwa',
      decision_by: 'odin-pwa',
      confirmation_text: confirmationText,
      expected_policy_snapshot_hash: item.policy_snapshot_hash || '',
      expected_runtime_snapshot_hash: item.runtime_snapshot_hash || '',
    }),
  });
  document.querySelector('#approval-confirmation').hidden = true;
  pendingApprovalDecision = null;
  setStatus(`Approval ${action} recorded.`);
  await refreshDashboard();
}

async function confirmReviewDecision() {
  if (!pendingReviewDecision) return;
  const reason = document.querySelector('#review-reason').value.trim();
  if (!reason) {
    setStatus('Review reason is required.');
    return;
  }
  const { item, action } = pendingReviewDecision;
  await mobileFetch(`/mobile/review-queue/${encodeURIComponent(item.queue_id)}/decision`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      action,
      reason,
      actor: 'odin-pwa',
    }),
  });
  document.querySelector('#review-confirmation').hidden = true;
  pendingReviewDecision = null;
  setStatus(`Review action ${actionLabel(action)} recorded.`);
  await refreshDashboard();
}

function actionLabel(action) {
  switch (action) {
    case 'approve':
      return 'Approve';
    case 'deny':
      return 'Deny';
    case 'clarify':
      return 'Clarify';
    case 'archive':
      return 'Archive';
    case 'reject':
      return 'Reject';
    case 'complete':
    case 'mark-complete':
      return 'Complete';
    default:
      return humanizeToken(action);
  }
}

function renderFailedBlocked(overview, reviewItems) {
  const node = document.querySelector('#failed-blocked-list');
  clearNode(node);
  const failed = reviewItems.filter((item) => item.source_type === 'failed_work');
  const blocked = overview.observability?.blocked_work || [];
  const recovery = overview.observability?.recovery_guidance || [];
  setCount('#failed-blocked-count', failed.length + blocked.length + recovery.length);
  for (const item of failed.slice(0, 4)) {
    node.appendChild(inspectOnlyWorkbenchCard(item, 'danger'));
  }
  for (const item of blocked.slice(0, 4)) {
    const card = projectionCard('warn', item.work_item_key, humanizeToken(item.reason || item.source), `${item.project_key || 'workspace'} remains blocked.`);
    appendCardFacts(card, [
      compactID('project', item.project_key || 'workspace'),
      compactID('source', item.source),
      compactID('reason', item.reason),
    ]);
    node.appendChild(card);
  }
  for (const item of recovery.slice(0, 4)) {
    const card = projectionCard('warn', item.work_item_key, humanizeToken(item.decision), item.recovery_recommendation || item.last_error || 'Inspect failed work before retry.');
    appendCardFacts(card, [
      compactID('decision', item.decision),
      compactID('status_code', item.status_code || item.error_code),
      compactID('source', item.source),
    ]);
    node.appendChild(card);
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
    const card = projectionCard('warn', `Login request ${item.id}`, humanizeToken(item.status), `Session ${item.session_id}; expires ${item.expires_at}`);
    appendCardFacts(card, [
      compactID('login_request', item.id),
      compactID('session', item.session_id),
      compactID('expires', item.expires_at),
    ]);
    node.appendChild(card);
  }
  for (const item of runners) {
    const card = projectionCard('danger', `Browser runner ${item.id}`, humanizeToken(item.status), item.error_message || item.error_code || 'Browser handoff needs inspection.');
    appendCardFacts(card, [
      compactID('runner', item.id),
      compactID('login_request', item.login_request_id),
      compactID('status_code', item.error_code),
    ]);
    node.appendChild(card);
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
  if (registeredSession) return;
  const value = window.prompt('Current Odin admin token');
  const token = normalizeAdminTokenInput(value);
  if (value === null || token === '') {
    return;
  }
  let response;
  try {
    response = await fetch('/mobile/devices/register', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ device_name: navigator.userAgent || 'mobile browser' }),
    });
  } catch (error) {
    setStatus(`Device registration failed: ${error.message}`);
    showError(`Device registration failed: ${error.message}`);
    return;
  }
  if (!response.ok) {
    const error = await responseError(response);
    const detail = registrationFailureMessage(error);
    setStatus(`Device registration failed: ${detail}`);
    showError(`Device registration failed: ${detail}`);
    return;
  }
  const payload = await response.json();
  sessionStorage.setItem(csrfKey, payload.csrf_token || '');
  setRegisteredState(true);
  setStatus('Device registered for this browser session.');
  await refreshDashboard();
});

document.querySelector('#refresh-dashboard').addEventListener('click', refreshDashboard);

document.querySelector('#capture-fab').addEventListener('click', () => {
  captureDetails.open = true;
  document.querySelector('.capture-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
  document.querySelector('#capture-body').focus({ preventScroll: true });
});

document.querySelector('#approval-cancel').addEventListener('click', () => {
  pendingApprovalDecision = null;
  document.querySelector('#approval-confirmation').hidden = true;
});

document.querySelector('#approval-confirm').addEventListener('click', () => {
  confirmApprovalDecision().catch((error) => setStatus(`Approval decision failed: ${error.message}`));
});

document.querySelector('#review-cancel').addEventListener('click', () => {
  pendingReviewDecision = null;
  document.querySelector('#review-confirmation').hidden = true;
});

document.querySelector('#review-confirm').addEventListener('click', () => {
  confirmReviewDecision().catch((error) => setStatus(`Review decision failed: ${error.message}`));
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

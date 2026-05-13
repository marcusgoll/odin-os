const state = { active: 'dashboard' };
const endpoints = {
  summary: '/mobile/summary',
  approvals: '/mobile/approvals',
  review: '/mobile/review',
  work: '/mobile/work',
  inbox: '/mobile/inbox',
  settings: '/mobile/settings'
};

const runtimePill = document.getElementById('runtime-pill');
const dashboardState = document.getElementById('dashboard-state');
const dashboardMetrics = document.getElementById('dashboard-metrics');
const dashboardPanels = document.getElementById('dashboard-panels');
const approvalsList = document.getElementById('approvals-list');
const reviewList = document.getElementById('review-list');
const workList = document.getElementById('work-list');
const inboxList = document.getElementById('inbox-list');
const settingsList = document.getElementById('settings-list');
const capturePolicy = document.getElementById('capture-policy');

document.querySelectorAll('.nav-item').forEach((button) => {
  button.addEventListener('click', () => setScreen(button.dataset.target));
});
document.querySelectorAll('[data-refresh]').forEach((button) => {
  button.addEventListener('click', () => loadAll());
});
document.getElementById('capture-form').addEventListener('submit', (event) => event.preventDefault());
window.addEventListener('online', () => loadAll());
window.addEventListener('offline', () => {
  setRuntimePill('offline', 'not-ready');
  dashboardState.textContent = 'Offline shell loaded. Runtime data requires Odin API.';
});

function setScreen(name) {
  state.active = name;
  document.querySelectorAll('.screen').forEach((screen) => {
    screen.classList.toggle('is-active', screen.dataset.screen === name);
  });
  document.querySelectorAll('.nav-item').forEach((button) => {
    button.classList.toggle('is-active', button.dataset.target === name);
  });
}

async function loadAll() {
  setRuntimePill('loading', '');
  renderLoading();
  try {
    const [summary, approvals, review, work, inbox, settings] = await Promise.all([
      getJSON(endpoints.summary),
      getJSON(endpoints.approvals),
      getJSON(endpoints.review),
      getJSON(endpoints.work),
      getJSON(endpoints.inbox),
      getJSON(endpoints.settings)
    ]);
    Object.assign(state, { summary, approvals, review, work, inbox, settings });
    renderAll();
  } catch (error) {
    setRuntimePill('api error', 'error');
    const message = `Odin API error: ${error.message}`;
    dashboardState.textContent = message;
    [approvalsList, reviewList, workList, inboxList, settingsList].forEach((target) => {
      target.innerHTML = panelHTML('Error', message, 'error');
    });
  }
}

async function getJSON(url) {
  const response = await fetch(url, { headers: { 'Accept': 'application/json' } });
  if (!response.ok) throw new Error(`${url} returned ${response.status}`);
  return response.json();
}

function renderLoading() {
  dashboardState.textContent = 'Loading Odin API...';
  dashboardMetrics.innerHTML = '';
  [dashboardPanels, approvalsList, reviewList, workList, inboxList, settingsList].forEach((target) => {
    target.innerHTML = panelHTML('Loading', 'Waiting for Odin API response.', 'pending');
  });
}

function renderAll() {
  renderDashboard();
  renderApprovals();
  renderReview();
  renderWork();
  renderInbox();
  renderSettings();
}

function renderDashboard() {
  const summary = state.summary || {};
  const counts = summary.counts || {};
  const readiness = summary.readiness || {};
  setRuntimePill(readiness.ready ? 'ready' : 'not ready', readiness.ready ? 'ready' : 'not-ready');
  dashboardState.textContent = `Health ${readiness.health_status || 'unknown'}; runtime ${summary.runtime?.status || 'unknown'}; generated ${formatTime(summary.generated_at)}.`;
  dashboardMetrics.innerHTML = [
    metricHTML('Approvals', counts.approvals),
    metricHTML('Review', counts.review_queue),
    metricHTML('Work', counts.work_items),
    metricHTML('Runs', counts.run_attempts),
    metricHTML('Triggers', counts.automation_triggers),
    metricHTML('Intake', counts.intake_items)
  ].join('');
  dashboardPanels.innerHTML = [
    panelHTML('Readiness', readiness.ready ? 'Odin API reports ready.' : 'Odin API reports not ready.', readiness.ready ? 'ready' : 'pending'),
    panelHTML('Offline Policy', summary.offline?.policy_statement || 'Offline policy unavailable.', summary.offline?.mode || 'unknown'),
    panelHTML('Runtime Source', 'Data is loaded from Odin API endpoints under /mobile. No sample rows are rendered by the shell.', 'api')
  ].join('');
}

function renderApprovals() {
  const items = state.approvals?.approvals || state.approvals?.items || [];
  approvalsList.innerHTML = items.length
    ? items.map((item) => panelHTML(item.title || item.task_key || `Approval ${item.approval_id || item.id}`, `${item.project_key || 'project unknown'} · ${item.required_reason || item.status || 'pending'} · task ${item.task_id || 'unknown'}`, item.status || item.risk_level)).join('')
    : panelHTML('Empty', 'No pending approvals returned by Odin API.', 'empty');
}

function renderReview() {
  const items = state.review?.items || [];
  reviewList.innerHTML = items.length
    ? items.map((item) => panelHTML(item.title || item.queue_id, `${item.queue_id} · ${item.source} · ${item.project_key || 'no project'}${item.reason ? ` · ${item.reason}` : ''}`, item.status)).join('')
    : panelHTML('Empty', 'No review queue items returned by Odin API.', 'empty');
}

function renderWork() {
  const workItems = state.work?.work_items || [];
  const runs = state.work?.runs || [];
  const workHTML = workItems.length
    ? workItems.map((item) => panelHTML(item.title || item.work_item_key, `${item.project_key} · ${item.status} · intent ${item.execution_intent || 'unknown'}${item.current_run_id ? ` · run ${item.current_run_id}` : ''}`, item.status)).join('')
    : panelHTML('Empty', 'No work items returned by Odin API.', 'empty');
  const runHTML = runs.length
    ? runs.map((item) => panelHTML(`Run ${item.run_id}`, `${item.project_key} · ${item.executor} · ${item.status} · attempt ${item.attempt}`, item.status)).join('')
    : panelHTML('Empty', 'No run attempts returned by Odin API.', 'empty');
  workList.innerHTML = workHTML + runHTML;
}

function renderInbox() {
  const raw = state.inbox?.raw_items || [];
  const linked = state.inbox?.linked_items || [];
  if (state.inbox?.capture?.policy_statement) capturePolicy.textContent = state.inbox.capture.policy_statement;
  const rawHTML = raw.length
    ? raw.map((item) => panelHTML(item.subject || `Intake ${item.id}`, `${item.source_family} · ${item.status} · received ${formatTime(item.received_at)}`, item.status)).join('')
    : panelHTML('Empty', 'No raw intake items returned by Odin API.', 'empty');
  const linkedHTML = linked.length
    ? linked.map((item) => panelHTML(item.work_item_key || `Linked intake ${item.intake_id}`, `${item.source} · ${item.intake_type} · ${item.work_item_status}`, item.work_item_status)).join('')
    : panelHTML('Empty', 'No linked intake evidence returned by Odin API.', 'empty');
  inboxList.innerHTML = rawHTML + linkedHTML;
}

function renderSettings() {
  const settings = state.settings || {};
  settingsList.innerHTML = [
    panelHTML('Runtime', settings.runtime_source || 'unknown', 'api'),
    panelHTML('Admin Actions', settings.admin_actions?.policy_statement || 'Admin policy unavailable.', settings.admin_actions?.enabled ? 'enabled' : 'disabled'),
    panelHTML('Offline', settings.offline?.policy_statement || 'Offline policy unavailable.', settings.offline?.mode || 'unknown'),
    panelHTML('Endpoints', (settings.endpoints || []).join(' · ') || 'No endpoint list returned.', 'api')
  ].join('');
}

function metricHTML(label, value) {
  return `<div class="metric"><span>${escapeHTML(label)}</span><strong>${Number.isFinite(value) ? value : 0}</strong></div>`;
}

function panelHTML(title, meta, tag) {
  return `<article class="panel"><div class="panel-header"><h3>${escapeHTML(title)}</h3><span class="tag ${className(tag)}">${escapeHTML(tag || 'status')}</span></div><p class="panel-meta">${escapeHTML(meta || '')}</p></article>`;
}

function setRuntimePill(text, classNameValue) {
  runtimePill.textContent = text;
  runtimePill.className = `runtime-pill ${classNameValue || ''}`.trim();
}

function formatTime(value) {
  if (!value) return 'unknown time';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function className(value) {
  return String(value || '').toLowerCase().replace(/[^a-z0-9_-]+/g, '_');
}

function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
}

loadAll();

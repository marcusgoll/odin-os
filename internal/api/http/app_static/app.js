const csrfKey = 'odin.mobile.csrf';
const failedKey = 'odin.mobile.failedUploads';
const form = document.querySelector('#capture-form');
const statusEl = document.querySelector('#capture-status');
const imageInput = document.querySelector('#image-input');
const mediaState = document.querySelector('#media-state');
const failedList = document.querySelector('#failed-uploads');
const approvalList = document.querySelector('#approval-list');
const reviewList = document.querySelector('#review-list');
let voiceBlob = null;
let recorder = null;
let chunks = [];

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

function renderFailed() {
  const items = failedUploads();
  failedList.innerHTML = '';
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

async function sendCapture(payload, attachment) {
  const headers = { 'X-Odin-CSRF': csrfToken() };
  let body;
  if (attachment) {
    body = new FormData();
    Object.entries(payload).forEach(([key, value]) => body.append(key, value || ''));
    body.set('kind', attachment.kind);
    body.append('attachment', attachment.blob, attachment.name);
  } else {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(payload);
  }
  const response = await fetch('/mobile/intake/raw', { method: 'POST', headers, body, credentials: 'same-origin' });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  return response.json();
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  const payload = textPayload();
  const image = imageInput.files[0];
  const attachment = voiceBlob
    ? { kind: 'voice_note', blob: voiceBlob, name: 'voice-note.webm' }
    : image
      ? { kind: 'photo', blob: image, name: image.name || 'photo.jpg' }
      : null;
  try {
    await sendCapture(payload, attachment);
    form.reset();
    voiceBlob = null;
    mediaState.textContent = 'No media attached';
    setStatus('Captured as raw intake');
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
});

document.querySelector('#voice-record').addEventListener('click', async () => {
  if (recorder && recorder.state === 'recording') {
    recorder.stop();
    return;
  }
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
  mediaState.textContent = 'Recording voice...';
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
});

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

renderFailed();


if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/app/service-worker.js');
}

async function mobileJSON(path, options = {}) {
  const headers = { 'X-Odin-CSRF': csrfToken(), ...(options.headers || {}) };
  const response = await fetch(path, { ...options, headers, credentials: 'same-origin' });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  return response.json();
}

function renderList(target, items, emptyText) {
  target.innerHTML = '';
  if (!items.length) {
    const li = document.createElement('li');
    li.textContent = emptyText;
    target.appendChild(li);
    return;
  }
  for (const item of items) {
    const li = document.createElement('li');
    const title = item.title || item.task_key || item.object_key || 'review item';
    const event = item.browser_event ? ` [${item.browser_event}]` : '';
    li.textContent = `${title}${event}: ${item.status}`;
    if (item.deep_link) {
      const link = document.createElement('a');
      link.href = item.deep_link;
      link.textContent = ' Open';
      li.appendChild(link);
    }
    if (item.approval_id && item.status === 'pending') {
      li.appendChild(decisionButton(item.approval_id, 'approve'));
      li.appendChild(decisionButton(item.approval_id, 'deny'));
    }
    target.appendChild(li);
  }
}

function decisionButton(approvalID, decision) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = decision;
  button.addEventListener('click', async () => {
    const reason = window.prompt(`Reason to ${decision}`, `${decision} from mobile`);
    if (!reason) return;
    await mobileJSON(`/mobile/approvals/${approvalID}/decision`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ decision, reason, decision_by: 'mobile-pwa' }),
    });
    await refreshOperatorQueues();
  });
  return button;
}

async function refreshApprovals() {
  const data = await mobileJSON('/mobile/approvals');
  renderList(approvalList, data.items || [], 'No approvals pending.');
}

async function refreshReview() {
  await mobileJSON('/mobile/notifications');
  const data = await mobileJSON('/mobile/review-queue');
  renderList(reviewList, data.items || [], 'No browser review items.');
}

async function refreshOperatorQueues() {
  try {
    await Promise.all([refreshApprovals(), refreshReview()]);
  } catch (error) {
    setStatus(`Review refresh failed: ${error.message}`);
  }
}

document.querySelector('#refresh-approvals').addEventListener('click', refreshApprovals);
document.querySelector('#refresh-review').addEventListener('click', refreshReview);

const params = new URLSearchParams(window.location.search);
if (params.get('approval_id') || params.get('queue_id')) {
  refreshOperatorQueues();
}

}

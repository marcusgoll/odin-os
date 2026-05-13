const tokenKey = 'odin.mobile.adminToken';
const failedKey = 'odin.mobile.failedUploads';
const form = document.querySelector('#capture-form');
const statusEl = document.querySelector('#capture-status');
const imageInput = document.querySelector('#image-input');
const mediaState = document.querySelector('#media-state');
const failedList = document.querySelector('#failed-uploads');
let voiceBlob = null;
let recorder = null;
let chunks = [];

function token() {
  return localStorage.getItem(tokenKey) || '';
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
  const headers = { Authorization: `Bearer ${token()}` };
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
  const response = await fetch('/mobile/intake/raw', { method: 'POST', headers, body });
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

document.querySelector('#save-token').addEventListener('click', () => {
  const value = window.prompt('Odin admin token', token());
  if (value !== null) {
    localStorage.setItem(tokenKey, value.trim());
    setStatus('Token saved locally in this browser.');
  }
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

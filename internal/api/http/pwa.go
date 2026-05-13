package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	mobileShareMaxBodyBytes = 8 << 20
	mobileShareMaxFileBytes = 5 << 20
)

func handlePWASharePost(writer http.ResponseWriter, request *http.Request) {
	payload, _, _, err := decodePWAShareRequest(writer, request)
	if request.MultipartForm != nil {
		_ = request.MultipartForm.RemoveAll()
	}
	if err != nil {
		writePWASharePage(writer, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	writePWASharePage(writer, map[string]any{
		"status":  "pending_upload",
		"payload": payload,
	})
}

func handleMobileShareIntake(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	payload, facts, dedupeKey, err := decodePWAShareRequest(writer, request)
	if request.MultipartForm != nil {
		_ = request.MultipartForm.RemoveAll()
	}
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, shareErrorCode(err), err.Error())
		return
	}
	item, err := createMobileIntakeItem(request.Context(), deps, mobileCreateIntakeInput{
		Kind:                "pwa_share",
		Title:               defaultPWAShareTitle(payload),
		DedupeKey:           dedupeKey,
		ProjectKey:          strings.TrimSpace(payload.ProjectKey),
		SourceFamily:        "pwa_share",
		DedupeRecipeVersion: "pwa-share-v1",
		Facts:               facts,
		Now:                 now,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "pwa_share_intake_create_failed", err.Error())
		return
	}
	recordMobileIntakeAudit(request.Context(), deps, request, item)
	writeMobileJSON(writer, http.StatusAccepted, mobileIntakeResponse{IntakeItem: mobileIntakeView(item)})
}

type pwaSharePayload struct {
	Title      string             `json:"title,omitempty"`
	Text       string             `json:"text,omitempty"`
	URL        string             `json:"url,omitempty"`
	Source     string             `json:"source,omitempty"`
	ProjectKey string             `json:"project_key,omitempty"`
	Files      []pwaShareFileFact `json:"files,omitempty"`
}

type pwaShareFileFact struct {
	Filename      string `json:"filename"`
	ContentType   string `json:"content_type"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"content_base64"`
	StoragePolicy string `json:"storage_policy"`
}

type pwaShareDecodeError struct {
	code string
	err  string
}

func (err pwaShareDecodeError) Error() string {
	return err.err
}

func decodePWAShareRequest(writer http.ResponseWriter, request *http.Request) (pwaSharePayload, map[string]any, string, error) {
	payload, err := parsePWASharePayload(writer, request)
	if err != nil {
		return pwaSharePayload{}, nil, "", err
	}
	if strings.TrimSpace(payload.Title) == "" && strings.TrimSpace(payload.Text) == "" && strings.TrimSpace(payload.URL) == "" && len(payload.Files) == 0 {
		return pwaSharePayload{}, nil, "", pwaShareDecodeError{code: "empty_pwa_share", err: "shared title, text, url, or file is required"}
	}
	payload.Title = strings.TrimSpace(payload.Title)
	payload.Text = strings.TrimSpace(payload.Text)
	payload.URL = strings.TrimSpace(payload.URL)
	payload.Source = strings.TrimSpace(payload.Source)
	payload.ProjectKey = strings.TrimSpace(payload.ProjectKey)

	canonical, err := json.Marshal(payload)
	if err != nil {
		return pwaSharePayload{}, nil, "", err
	}
	facts := map[string]any{
		"source":           "pwa_share_target",
		"kind":             "pwa_share",
		"title":            defaultPWAShareTitle(payload),
		"requested_by":     "pwa_share_target",
		"received_via":     "odin_pwa_share_target",
		"payload_policy":   "stored_in_source_facts_json",
		"upload_state":     "uploaded",
		"content_sha256":   mobileHash(string(canonical)),
		"platform_limits":  "share_target support varies by browser and operating system",
		"processing_state": "raw_intake_only",
		"payload":          payload,
	}
	return payload, facts, "pwa-share:" + mobileHash(string(canonical)), nil
}

func parsePWASharePayload(writer http.ResponseWriter, request *http.Request) (pwaSharePayload, error) {
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var payload pwaSharePayload
		if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, mobileShareMaxBodyBytes)).Decode(&payload); err != nil {
			return pwaSharePayload{}, pwaShareDecodeError{code: "invalid_pwa_share_json", err: err.Error()}
		}
		return payload, nil
	}
	if strings.Contains(contentType, "multipart/form-data") {
		request.Body = http.MaxBytesReader(writer, request.Body, mobileShareMaxBodyBytes)
		if err := request.ParseMultipartForm(mobileShareMaxBodyBytes); err != nil {
			return pwaSharePayload{}, pwaShareDecodeError{code: "invalid_pwa_share_form", err: err.Error()}
		}
		payload := pwaSharePayload{
			Title:      request.FormValue("title"),
			Text:       request.FormValue("text"),
			URL:        request.FormValue("url"),
			Source:     firstNonEmpty(request.FormValue("source"), request.FormValue("share_source")),
			ProjectKey: request.FormValue("project_key"),
		}
		if request.MultipartForm != nil {
			for _, fileHeader := range request.MultipartForm.File["files"] {
				fileFact, err := readPWAShareFile(fileHeader)
				if err != nil {
					return pwaSharePayload{}, err
				}
				payload.Files = append(payload.Files, fileFact)
			}
		}
		return payload, nil
	}
	return pwaSharePayload{}, pwaShareDecodeError{code: "unsupported_pwa_share_media_type", err: "share intake requires application/json or multipart/form-data"}
}

func readPWAShareFile(fileHeader *multipart.FileHeader) (pwaShareFileFact, error) {
	if fileHeader.Size > mobileShareMaxFileBytes {
		return pwaShareFileFact{}, pwaShareDecodeError{code: "share_file_too_large", err: fmt.Sprintf("shared file %q exceeds %d bytes", fileHeader.Filename, mobileShareMaxFileBytes)}
	}
	file, err := fileHeader.Open()
	if err != nil {
		return pwaShareFileFact{}, pwaShareDecodeError{code: "invalid_share_file", err: err.Error()}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, mobileShareMaxFileBytes+1))
	if err != nil {
		return pwaShareFileFact{}, pwaShareDecodeError{code: "invalid_share_file", err: err.Error()}
	}
	if int64(len(data)) > mobileShareMaxFileBytes {
		return pwaShareFileFact{}, pwaShareDecodeError{code: "share_file_too_large", err: fmt.Sprintf("shared file %q exceeds %d bytes", fileHeader.Filename, mobileShareMaxFileBytes)}
	}
	filename := sanitizePWAShareFilename(fileHeader.Filename)
	contentType := detectPWAShareContentType(filename, fileHeader.Header.Get("Content-Type"), data)
	if !allowedPWAShareContentType(filename, contentType) {
		return pwaShareFileFact{}, pwaShareDecodeError{code: "unsupported_share_file_type", err: fmt.Sprintf("shared file %q has unsupported content type %q", filename, contentType)}
	}
	return pwaShareFileFact{
		Filename:      filename,
		ContentType:   contentType,
		SizeBytes:     int64(len(data)),
		SHA256:        mobileHash(string(data)),
		ContentBase64: base64.StdEncoding.EncodeToString(data),
		StoragePolicy: "stored_in_source_facts_json_base64",
	}, nil
}

func sanitizePWAShareFilename(filename string) string {
	filename = strings.TrimSpace(filepath.Base(filename))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "shared-file"
	}
	return filename
}

func detectPWAShareContentType(filename string, header string, data []byte) string {
	detected := http.DetectContentType(data)
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".md", ".csv", ".log":
		return "text/plain"
	}
	if strings.HasPrefix(detected, "image/") || detected == "application/pdf" {
		return detected
	}
	header = strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if header != "" && header != "application/octet-stream" {
		return header
	}
	return detected
}

func allowedPWAShareContentType(filename string, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/heic", "image/heif", "application/pdf":
		return true
	case "text/plain":
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".txt", ".md", ".csv", ".log":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func defaultPWAShareTitle(payload pwaSharePayload) string {
	if title := strings.TrimSpace(payload.Title); title != "" {
		return mobileDefaultTitle("share", title)
	}
	if url := strings.TrimSpace(payload.URL); url != "" {
		return mobileDefaultTitle("share", url)
	}
	if text := strings.TrimSpace(payload.Text); text != "" {
		return mobileDefaultTitle("share", text)
	}
	if len(payload.Files) == 1 {
		return payload.Files[0].Filename
	}
	if len(payload.Files) > 1 {
		return fmt.Sprintf("%d shared files", len(payload.Files))
	}
	return "Shared content"
}

func shareErrorCode(err error) string {
	if decoded, ok := err.(pwaShareDecodeError); ok {
		return decoded.code
	}
	return "invalid_pwa_share"
}

func writePWASharePage(writer http.ResponseWriter, preloaded map[string]any) {
	preloadedJSON := "{}"
	if preloaded != nil {
		if encoded, err := json.Marshal(preloaded); err == nil {
			preloadedJSON = string(encoded)
		}
	}
	page := strings.Replace(pwaShareHTML, "__PRELOADED_SHARE__", template.JSEscapeString(preloadedJSON), 1)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, page)
}

const pwaShareHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="manifest" href="/app/manifest.webmanifest">
  <title>Share to Odin</title>
  <style>
    body { margin: 0; font-family: ui-serif, Georgia, serif; background: #f4efe6; color: #1f2d2a; }
    main { max-width: 760px; margin: 0 auto; padding: 28px; }
    section { border: 1px solid #c9b99a; border-radius: 18px; background: #fffaf0; padding: 20px; margin: 16px 0; box-shadow: 0 18px 40px rgba(31,45,42,.08); }
    label { display: block; font-weight: 700; margin-top: 14px; }
    input, textarea { box-sizing: border-box; width: 100%; margin-top: 6px; padding: 12px; border: 1px solid #b9aa8d; border-radius: 10px; font: inherit; }
    button { margin-top: 16px; border: 0; border-radius: 999px; padding: 12px 18px; background: #1f3d36; color: white; font-weight: 800; }
    .status { font-weight: 800; }
  </style>
</head>
<body>
<main>
  <h1>Share to Odin</h1>
  <section>
    <p class="status" id="status">No pending upload.</p>
    <p>This app stores shared content as raw intake first. It does not auto-process shared content into work without review.</p>
    <p>Share target support varies by browser and OS; iOS and Android do not expose identical capabilities.</p>
    <button id="retry">Retry upload</button>
  </section>
  <section>
    <h2>copy/paste fallback</h2>
    <form id="fallback">
      <label>Admin token<input id="token" name="token" autocomplete="current-password"></label>
      <label>Title<input name="title"></label>
      <label>URL<input name="url" type="url"></label>
      <label>Text<textarea name="text" rows="5"></textarea></label>
      <label>File<input name="files" type="file" accept="image/*,application/pdf,text/plain" multiple></label>
      <button type="submit">Capture raw intake</button>
    </form>
  </section>
</main>
<script id="preloaded-share" type="application/json">__PRELOADED_SHARE__</script>
<script>
const statusEl = document.getElementById('status');
const retryEl = document.getElementById('retry');
const form = document.getElementById('fallback');
const tokenInput = document.getElementById('token');
tokenInput.value = localStorage.getItem('odinAdminToken') || '';
let pending = JSON.parse(document.getElementById('preloaded-share').textContent || '{}');
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/app/service-worker.js');
function openShareDB() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('odin-share-target', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('pending-shares');
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
async function getPendingShare(id) {
  const db = await openShareDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction('pending-shares', 'readonly');
    const request = tx.objectStore('pending-shares').get(id);
    request.onsuccess = () => resolve(request.result || {});
    request.onerror = () => reject(request.error);
  });
}
async function uploadFromForm(formData) {
  const token = tokenInput.value.trim();
  if (!token) throw new Error('admin token required before upload');
  localStorage.setItem('odinAdminToken', token);
  statusEl.textContent = 'pending upload';
  const res = await fetch('/mobile/intake/share', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'X-Odin-Admin-Token': token }, body: formData });
  if (!res.ok) throw new Error(await res.text());
  statusEl.textContent = 'success: shared content captured as raw intake';
}
function formDataFromPayload(payload) {
  const data = new FormData();
  const body = payload && payload.payload ? payload.payload : payload;
  for (const key of ['title', 'text', 'url', 'source', 'project_key']) {
    if (body && body[key]) data.set(key, body[key]);
  }
  if (body && Array.isArray(body.files)) {
    for (const file of body.files) data.append('files', file, file.name || 'shared-file');
  }
  return data;
}
retryEl.addEventListener('click', async () => {
  try { await uploadFromForm(formDataFromPayload(pending)); }
  catch (err) { statusEl.textContent = 'failure: ' + err.message; }
});
form.addEventListener('submit', async (event) => {
  event.preventDefault();
  try { await uploadFromForm(new FormData(form)); }
  catch (err) { statusEl.textContent = 'failure: ' + err.message; }
});
const shareId = new URL(location.href).searchParams.get('share_id');
(async () => {
  if (shareId) pending = await getPendingShare(shareId);
  if (pending && pending.status === 'pending_upload') {
    statusEl.textContent = 'pending upload';
  } else {
    statusEl.textContent = 'No pending upload. Use the fallback form or share from another app.';
  }
})().catch(err => { statusEl.textContent = 'failure: ' + err.message; });
</script>
</body>
</html>`

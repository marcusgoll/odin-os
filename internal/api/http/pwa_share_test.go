package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/store/sqlite"
)

func TestPWAManifestRegistersShareTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{}))
	defer server.Close()

	res, err := http.Get(server.URL + "/app/manifest.webmanifest")
	if err != nil {
		t.Fatalf("GET manifest error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET manifest status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	var manifest struct {
		Name        string `json:"name"`
		Display     string `json:"display"`
		ShareTarget struct {
			Action  string `json:"action"`
			Method  string `json:"method"`
			Enctype string `json:"enctype"`
			Params  struct {
				Title string `json:"title"`
				Text  string `json:"text"`
				URL   string `json:"url"`
				Files []struct {
					Name   string   `json:"name"`
					Accept []string `json:"accept"`
				} `json:"files"`
			} `json:"params"`
		} `json:"share_target"`
	}
	if err := json.NewDecoder(res.Body).Decode(&manifest); err != nil {
		t.Fatalf("Decode manifest error = %v", err)
	}
	if manifest.Name == "" || manifest.Display != "standalone" {
		t.Fatalf("manifest = %+v, want installable standalone PWA", manifest)
	}
	if manifest.ShareTarget.Action != "/app/share" || manifest.ShareTarget.Method != http.MethodPost || manifest.ShareTarget.Enctype != "multipart/form-data" {
		t.Fatalf("share_target = %+v, want POST multipart /app/share", manifest.ShareTarget)
	}
	if manifest.ShareTarget.Params.Title != "title" || manifest.ShareTarget.Params.Text != "text" || manifest.ShareTarget.Params.URL != "url" {
		t.Fatalf("share_target params = %+v, want title/text/url", manifest.ShareTarget.Params)
	}
	if len(manifest.ShareTarget.Params.Files) != 1 || manifest.ShareTarget.Params.Files[0].Name != "files" {
		t.Fatalf("share_target files = %+v, want files field", manifest.ShareTarget.Params.Files)
	}
	if !containsString(manifest.ShareTarget.Params.Files[0].Accept, "image/*") {
		t.Fatalf("share_target file accepts = %+v, want image/*", manifest.ShareTarget.Params.Files[0].Accept)
	}
}

func TestPWAShareRouteServesPendingUploadUI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{}))
	defer server.Close()

	res, err := http.Get(server.URL + "/app/share")
	if err != nil {
		t.Fatalf("GET share route error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET share route status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	raw := string(body)
	for _, want := range []string{"pending upload", "Retry upload", "copy/paste fallback", "/mobile/intake/share"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("share route body missing %q:\n%s", want, raw)
		}
	}
}

func TestPWAServiceWorkerCapturesSharePosts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{}))
	defer server.Close()

	res, err := http.Get(server.URL + "/app/service-worker.js")
	if err != nil {
		t.Fatalf("GET service worker error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET service worker status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	raw := string(body)
	for _, want := range []string{"event.request.method === 'POST'", "url.pathname === '/app/share'", "pending_upload", "pending-shares"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("service worker missing %q:\n%s", want, raw)
		}
	}
}

func TestMobileShareIntakeCreatesRawItemForTextLinkAndImage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "title", "Shared checklist")
	mustWriteField(t, writer, "text", "Review this before tomorrow")
	mustWriteField(t, writer, "url", "https://example.com/checklist")
	mustWriteField(t, writer, "source", "android-share-sheet")
	fileWriter, err := writer.CreateFormFile("files", "checklist.png")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write([]byte("\x89PNG\r\n\x1a\nodin-share-image")); err != nil {
		t.Fatalf("Write(file) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer error = %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/intake/share", &body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST share intake error = %v", err)
	}
	defer res.Body.Close()
	rawResponse, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(response) error = %v", err)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST share intake status = %d body=%s, want %d", res.StatusCode, string(rawResponse), http.StatusAccepted)
	}
	if strings.Contains(string(rawResponse), "Review this before tomorrow") {
		t.Fatalf("share response echoed raw shared text: %s", string(rawResponse))
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("intake items len = %d, want 1: %+v", len(items), items)
	}
	item := items[0]
	if item.Status != "received" || item.SourceFamily != "pwa_share" || item.EventKind != "pwa_share" || item.Subject != "Shared checklist" {
		t.Fatalf("intake item = %+v, want received pwa_share raw item", item)
	}

	var facts struct {
		Source        string `json:"source"`
		RequestedBy   string `json:"requested_by"`
		PayloadPolicy string `json:"payload_policy"`
		UploadState   string `json:"upload_state"`
		Payload       struct {
			Title  string `json:"title"`
			Text   string `json:"text"`
			URL    string `json:"url"`
			Source string `json:"source"`
			Files  []struct {
				Filename      string `json:"filename"`
				ContentType   string `json:"content_type"`
				SizeBytes     int64  `json:"size_bytes"`
				SHA256        string `json:"sha256"`
				ContentBase64 string `json:"content_base64"`
				StoragePolicy string `json:"storage_policy"`
			} `json:"files"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(item.SourceFactsJSON), &facts); err != nil {
		t.Fatalf("source facts unmarshal error = %v\n%s", err, item.SourceFactsJSON)
	}
	if facts.Source != "pwa_share_target" || facts.RequestedBy != "pwa_share_target" || facts.PayloadPolicy != "stored_in_source_facts_json" || facts.UploadState != "uploaded" {
		t.Fatalf("source facts provenance = %+v, want uploaded PWA share provenance", facts)
	}
	if facts.Payload.Title != "Shared checklist" || facts.Payload.Text != "Review this before tomorrow" || facts.Payload.URL != "https://example.com/checklist" || facts.Payload.Source != "android-share-sheet" {
		t.Fatalf("source facts payload = %+v, want shared title/text/url/source", facts.Payload)
	}
	if len(facts.Payload.Files) != 1 {
		t.Fatalf("payload files len = %d, want 1", len(facts.Payload.Files))
	}
	file := facts.Payload.Files[0]
	if file.Filename != "checklist.png" || file.ContentType != "image/png" || file.SizeBytes == 0 || file.SHA256 == "" || file.ContentBase64 == "" || file.StoragePolicy != "stored_in_source_facts_json_base64" {
		t.Fatalf("payload file = %+v, want captured image metadata and content", file)
	}
}

func TestMobileShareIntakeRejectsUnsupportedFiles(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "title", "Unsafe file")
	fileWriter, err := writer.CreateFormFile("files", "notes.exe")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write([]byte("MZ")); err != nil {
		t.Fatalf("Write(file) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer error = %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/intake/share", &body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST share intake error = %v", err)
	}
	defer res.Body.Close()
	rawResponse, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST unsupported share file status = %d body=%s, want %d", res.StatusCode, string(rawResponse), http.StatusBadRequest)
	}
	if !strings.Contains(string(rawResponse), "unsupported_share_file_type") {
		t.Fatalf("unsupported file response = %s, want unsupported_share_file_type", string(rawResponse))
	}

	items, err := store.ListIntakeItems(context.Background(), sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("intake items = %+v, want no item for rejected file", items)
	}
}

func mustWriteField(t *testing.T, writer *multipart.Writer, field string, value string) {
	t.Helper()
	if err := writer.WriteField(field, value); err != nil {
		t.Fatalf("WriteField(%s) error = %v", field, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

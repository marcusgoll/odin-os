package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/store/sqlite"
)

func TestMobileRawIntakeSupportsPromptTaskBugAndProjectNoteKinds(t *testing.T) {
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

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "quick text note", body: `{"kind":"note","content":"Remember gate change pattern","source_app":"share-sheet"}`, want: "note"},
		{name: "raw prompt", body: `{"kind":"prompt","prompt":"Draft a safe deployment checklist"}`, want: "prompt"},
		{name: "task", body: `{"kind":"task","title":"Check backup","content":"Review failed backup log"}`, want: "task"},
		{name: "bug", body: `{"kind":"bug","title":"PWA sync failed","content":"Capture retry loop is stuck"}`, want: "bug"},
		{name: "project note", body: `{"kind":"project_note","title":"Odin mobile","content":"Keep capture raw until reviewed"}`, want: "project_note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := mustMobileRequest(t, server, http.MethodPost, "/mobile/intake/raw", "secret", strings.NewReader(tc.body))
			defer res.Body.Close()
			if res.StatusCode != http.StatusAccepted {
				raw, _ := io.ReadAll(res.Body)
				t.Fatalf("POST /mobile/intake/raw status = %d body=%s, want %d", res.StatusCode, string(raw), http.StatusAccepted)
			}
			var response struct {
				IntakeItem struct {
					IntakeType string `json:"intake_type"`
				} `json:"intake_item"`
			}
			decodeJSON(t, res.Body, &response)
			if response.IntakeItem.IntakeType != tc.want {
				t.Fatalf("intake_type = %q, want %q", response.IntakeItem.IntakeType, tc.want)
			}
		})
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("intake items len = %d, want 5", len(items))
	}
	for _, item := range items {
		if item.Status != "received" {
			t.Fatalf("mobile capture status = %q, want received", item.Status)
		}
		if strings.Contains(item.SourceFactsJSON, "sensitive_memory") {
			t.Fatalf("source facts stored sensitive-memory marker: %s", item.SourceFactsJSON)
		}
	}
}

func TestOperationalHandlerServesMobileCapturePWAShell(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	html := getURLText(t, server.URL+"/app/")
	for _, want := range []string{
		`<link rel="manifest" href="/app/manifest.webmanifest">`,
		`<link rel="icon" href="/app/icons/icon-192.svg" type="image/svg+xml">`,
		`What needs me now?`,
		`Action Required`,
		`Approvals`,
		`Failed/Blocked`,
		`Today`,
		`Inbox`,
		`Running Work`,
		`Browser Needs Help`,
		`Quiet Later`,
		`Critical confirmation`,
		`Review action`,
		`data-capture-kind="note"`,
		`data-capture-kind="voice_note"`,
		`data-capture-kind="photo"`,
		`accept="image/*"`,
		`capture="environment"`,
		`id="voice-record"`,
		`id="failed-uploads"`,
		`id="capture-fab"`,
		`id="register-device"`,
		`aria-label="Capture a note"`,
		`aria-label="Register this mobile device"`,
		`/mobile/intake/raw`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("/app/ missing %q:\n%s", want, html)
		}
	}

	manifest := getURLText(t, server.URL+"/app/manifest.webmanifest")
	if !strings.Contains(manifest, `"name":"Odin Operator"`) || !strings.Contains(manifest, `"start_url":"/app/"`) {
		t.Fatalf("manifest = %s, want installable Odin Operator manifest", manifest)
	}

	serviceWorker := getURLText(t, server.URL+"/app/service-worker.js")
	for _, want := range []string{`/app/offline.html`, `event.request.method !== 'GET'`, `shell-only`} {
		if !strings.Contains(serviceWorker, want) {
			t.Fatalf("service worker missing %q:\n%s", want, serviceWorker)
		}
	}

	appJS := getURLText(t, server.URL+"/app/app.js")
	for _, want := range []string{
		`navigator.serviceWorker.register('/app/service-worker.js')`,
		`/app/session`,
		`/mobile/devices/register`,
		`/mobile/status`,
		`/mobile/overview`,
		`/mobile/review-queue`,
		`/mobile/review-queue/${encodeURIComponent(item.queue_id)}/decision`,
		`/mobile/approvals`,
		`/mobile/browser/status`,
		`/mobile/notifications/preferences`,
		`/mobile/approvals/${item.approval_id}/decision`,
		`Odin admin token is not configured on this server.`,
		`No production mock data is shown.`,
		`No action-required rows in current projections.`,
		`Approval reason is required.`,
		`Review reason is required.`,
		`Mark attended browser login complete`,
		`Allowed decisions:`,
		`confirmation_text`,
		`expected_policy_snapshot_hash`,
		`expected_runtime_snapshot_hash`,
	} {
		if !strings.Contains(appJS, want) {
			t.Fatalf("/app/app.js missing %q:\n%s", want, appJS)
		}
	}

	styles := getURLText(t, server.URL+"/app/styles.css")
	for _, want := range []string{
		`prefers-color-scheme: dark`,
		`.capture-fab`,
		`.status-card`,
		`.confirmation-panel`,
		`@media (max-width: 560px)`,
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("/app/styles.css missing visual smoke marker %q:\n%s", want, styles)
		}
	}

	session := getURLText(t, server.URL+"/app/session")
	if !strings.Contains(session, `"authenticated":false`) {
		t.Fatalf("/app/session = %s, want unauthenticated public session status", session)
	}

	faviconResponse, err := http.Get(server.URL + "/favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico error = %v", err)
	}
	defer faviconResponse.Body.Close()
	if faviconResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET /favicon.ico status = %d, want redirected icon success", faviconResponse.StatusCode)
	}
}

func TestMobileReviewQueueDecisionRejectsClarifiesAndArchivesIntake(t *testing.T) {
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

	cases := []struct {
		action string
		status string
	}{
		{action: "reject", status: "rejected"},
		{action: "clarify", status: "needs_clarification"},
		{action: "archive", status: "archived"},
	}
	for _, tc := range cases {
		item, err := store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
			WorkspaceID:         "default",
			SourceFamily:        "mobile-test",
			ExternalObjectID:    "review-" + tc.action,
			EventKind:           "operator_review",
			Subject:             "Review " + tc.action,
			DedupeKey:           "review-" + tc.action,
			DedupeRecipeVersion: "test-v1",
			SourceFactsJSON:     `{}`,
			Status:              "review_required",
			Scope:               "workspace",
			ScopeKey:            "default",
		})
		if err != nil {
			t.Fatalf("CreateIntakeItem(%s) error = %v", tc.action, err)
		}
		queueID := "intake-review:" + int64String(item.ID)
		res := mustMobileRequest(t, server, http.MethodPost, "/mobile/review-queue/"+url.PathEscape(queueID)+"/decision", "secret", strings.NewReader(`{"action":"`+tc.action+`","reason":"operator decided from PWA"}`))
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(res.Body)
			t.Fatalf("POST review decision %s status = %d body=%s, want %d", tc.action, res.StatusCode, string(raw), http.StatusOK)
		}
		updated, err := store.GetIntakeItem(ctx, item.ID)
		if err != nil {
			t.Fatalf("GetIntakeItem(%s) error = %v", tc.action, err)
		}
		if updated.Status != tc.status {
			t.Fatalf("intake status after %s = %q, want %q", tc.action, updated.Status, tc.status)
		}
	}
}

func getURLText(t *testing.T, url string) string {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(%s) error = %v", url, err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s, want %d", url, res.StatusCode, string(body), http.StatusOK)
	}
	return string(body)
}

func TestMobileImageAndAudioCaptureStoreAttachmentMetadata(t *testing.T) {
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

	for _, tc := range []struct {
		name        string
		kind        string
		filename    string
		contentType string
		data        string
		transcript  string
	}{
		{name: "image", kind: "photo", filename: "panel.jpg", contentType: "image/jpeg", data: "jpeg bytes", transcript: ""},
		{name: "audio", kind: "voice_note", filename: "voice.webm", contentType: "audio/webm", data: "webm bytes", transcript: "transcript pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := multipartMobileCapture(t, map[string]string{
				"kind":       tc.kind,
				"title":      tc.filename,
				"content":    "operator mobile upload",
				"transcript": tc.transcript,
				"source_app": "ios-share-sheet",
			}, tc.filename, tc.contentType, []byte(tc.data))

			req, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/intake/raw", body)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			req.Header.Set("Authorization", "Bearer secret")
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST multipart mobile intake error = %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusAccepted {
				raw, _ := io.ReadAll(res.Body)
				t.Fatalf("POST multipart mobile intake status = %d body=%s, want %d", res.StatusCode, string(raw), http.StatusAccepted)
			}
		})
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("intake items len = %d, want 2", len(items))
	}
	for _, item := range items {
		attachments, err := store.ListIntakeAttachments(ctx, sqlite.ListIntakeAttachmentsParams{IntakeItemID: item.ID})
		if err != nil {
			t.Fatalf("ListIntakeAttachments(%d) error = %v", item.ID, err)
		}
		if len(attachments) != 1 {
			t.Fatalf("attachments for intake %d len = %d, want 1", item.ID, len(attachments))
		}
		if attachments[0].Status != "stored" || attachments[0].SizeBytes == 0 || attachments[0].SHA256 == "" {
			t.Fatalf("attachment = %+v, want stored size and sha256", attachments[0])
		}
		if !json.Valid([]byte(item.SourceFactsJSON)) || !strings.Contains(item.SourceFactsJSON, `"attachments"`) {
			t.Fatalf("source facts = %s, want valid JSON with attachment metadata", item.SourceFactsJSON)
		}
	}
}

func TestMobileCaptureRejectsInvalidAttachmentTypeWithoutCreatingIntake(t *testing.T) {
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

	body, contentType := multipartMobileCapture(t, map[string]string{
		"kind":    "photo",
		"title":   "bad upload",
		"content": "not an allowed upload",
	}, "payload.txt", "text/plain", []byte("plain text"))

	req, err := http.NewRequest(http.MethodPost, server.URL+"/mobile/intake/raw", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST multipart mobile intake error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("invalid attachment status = %d body=%s, want %d", res.StatusCode, string(raw), http.StatusBadRequest)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(raw), "attachment_type_not_allowed") || !strings.Contains(string(raw), "retry") {
		t.Fatalf("invalid attachment response = %s, want stable retryable error", string(raw))
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("intake items len = %d, want no invalid attachment intake", len(items))
	}
}

func TestMobileRawIntakePreservesDedupeKey(t *testing.T) {
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

	for range 2 {
		res := mustMobileRequest(t, server, http.MethodPost, "/mobile/intake/raw", "secret", strings.NewReader(`{"kind":"idea","title":"Same idea","content":"same body","dedup_key":"mobile-share:abc123"}`))
		res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("POST /mobile/intake/raw status = %d, want %d", res.StatusCode, http.StatusAccepted)
		}
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("intake items len = %d, want duplicate raw arrivals preserved", len(items))
	}
	if items[0].DedupeKey != "mobile-share:abc123" || items[1].DedupeKey != "mobile-share:abc123" {
		t.Fatalf("dedupe keys = %q %q, want supplied dedupe key preserved", items[0].DedupeKey, items[1].DedupeKey)
	}
}

func multipartMobileCapture(t *testing.T, fields map[string]string, filename string, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField(%s) error = %v", key, err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="attachment"; filename="`+filename+`"`)
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("attachment Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart Close() error = %v", err)
	}
	return body, writer.FormDataContentType()
}

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
	"strconv"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/core/workspaces"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
)

func TestOdinPhoneReleaseCheck(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")
	project := seedPhoneReleaseProject(t, ctx, store)
	approvalTask, approvalID := seedPhoneReleaseApproval(t, ctx, store, project.ID)
	seedPhoneReleaseBrowserEvidence(t, ctx, store, approvalTask.ID)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      "phone-secret",
	}))
	defer server.Close()

	assertPhoneGET(t, server.URL+"/healthz", http.StatusOK, `"status":"healthy"`)
	assertPhoneGET(t, server.URL+"/readyz", http.StatusOK, `"status":"healthy"`)

	sessionCookie, csrfToken, deviceID := registerMobileDevice(t, server, "phone-secret")
	sessionHeaders := map[string]string{
		"Cookie":      sessionCookie.String(),
		"X-Odin-CSRF": csrfToken,
	}
	assertPhoneGETWithHeader(t, server.URL+"/mobile/status", sessionHeaders, http.StatusOK, `"runtime"`)
	assertPhoneGETWithHeader(t, server.URL+"/mobile/overview", sessionHeaders, http.StatusOK, `"workspace"`)

	assertPhoneGET(t, server.URL+"/app/", http.StatusOK, `/mobile/intake/raw`)
	assertPhoneGET(t, server.URL+"/app/manifest.webmanifest", http.StatusOK, `"share_target"`)
	assertPhoneGET(t, server.URL+"/app/service-worker.js", http.StatusOK, `pending-shares`)
	assertPhoneGET(t, server.URL+"/app/share", http.StatusOK, `/mobile/intake/share`)

	postPhoneJSON(t, server.URL+"/mobile/intake/raw", sessionHeaders, `{"kind":"idea","title":"Phone text","content":"capture from phone"}`, http.StatusAccepted)
	postPhoneMultipartRaw(t, server.URL+"/mobile/intake/raw", sessionHeaders, map[string]string{
		"kind":    "photo",
		"title":   "panel.jpg",
		"content": "phone image capture",
	}, "panel.jpg", "image/jpeg", []byte("jpeg bytes"), http.StatusAccepted)
	postPhoneMultipartRaw(t, server.URL+"/mobile/intake/raw", sessionHeaders, map[string]string{
		"kind":       "voice_note",
		"title":      "voice.webm",
		"content":    "phone voice capture",
		"transcript": "transcript pending",
	}, "voice.webm", "audio/webm", []byte("webm bytes"), http.StatusAccepted)
	postPhoneShare(t, server.URL+"/mobile/intake/share", sessionHeaders, http.StatusAccepted)

	subscriptionID := postPhoneNotificationSubscription(t, server.URL+"/mobile/notifications/subscriptions", sessionHeaders)
	postPhoneJSON(t, server.URL+"/mobile/notifications/subscriptions/"+int64String(subscriptionID)+"/revoke", sessionHeaders, `{"reason":"release check cleanup"}`, http.StatusOK)

	approvals := assertPhoneGETWithHeader(t, server.URL+"/mobile/approvals", sessionHeaders, http.StatusOK, `"resolver_support":"supported"`)
	if !strings.Contains(approvals, `"approval_id":`+int64String(approvalID)) {
		t.Fatalf("mobile approvals = %s, want seeded approval %d", approvals, approvalID)
	}
	reviewQueue := assertPhoneGETWithHeader(t, server.URL+"/mobile/review-queue", sessionHeaders, http.StatusOK, `"source_type":"browser_evidence"`)
	if !strings.Contains(reviewQueue, `"approval-`+int64String(approvalID)+`"`) {
		t.Fatalf("mobile review queue = %s, want seeded approval", reviewQueue)
	}
	if !strings.Contains(reviewQueue, `"browser_event":"browser_evidence_ready"`) {
		t.Fatalf("mobile review queue = %s, want Huginn browser evidence event", reviewQueue)
	}
	postPhoneJSON(t, server.URL+"/mobile/approvals/"+int64String(approvalID)+"/decision", sessionHeaders, `{"action":"approve","reason":"phone release check","decision_by":"mobile-release-check"}`, http.StatusOK)

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("intake items len = %d, want text image audio share and notification evidence", len(items))
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	for eventType, wantAtLeast := range map[runtimeevents.Type]int{
		runtimeevents.EventMobileLogin:                   1,
		runtimeevents.EventMobileIntakeCreated:           5,
		runtimeevents.EventMobileApprovalResolved:        1,
		runtimeevents.EventMobilePushSubscriptionRevoked: 1,
		runtimeevents.EventApprovalRequested:             1,
		runtimeevents.EventApprovalResolved:              1,
	} {
		if counts[eventType] < wantAtLeast {
			t.Fatalf("%s events = %d, want at least %d", eventType, counts[eventType], wantAtLeast)
		}
	}

	if strings.Contains(reviewQueue, "profile_path") || strings.Contains(reviewQueue, "handoff_id") || strings.Contains(reviewQueue, "secret") {
		t.Fatalf("mobile review queue leaked browser/session secret material: %s", reviewQueue)
	}
	if deviceID == "" {
		t.Fatal("registered mobile device id is empty")
	}
}

func seedPhoneReleaseProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "phone-release",
		Name:          "Phone Release",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}

func seedPhoneReleaseApproval(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64) (sqlite.Task, int64) {
	t.Helper()

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         "phone-release-approval",
		Title:       "Approve phone release check",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "phone-release-check",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	task, err = store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: task.ID,
		Reason: "approval_required",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(blocked) error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		Status:      "pending",
		RequestedBy: "phone-release-check",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	return task, approval.ID
}

func seedPhoneReleaseBrowserEvidence(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64) {
	t.Helper()

	run, err := store.StartRun(ctx, sqlite.StartRunParams{TaskID: taskID, Executor: "huginn_browser", Attempt: 1, Status: "running"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: "browser_evidence",
		Summary:      "Huginn stub browser evidence for phone release check",
		DetailsJSON:  `{"adapter_kind":"stub_local","action_log":["no_external_mutation_performed"],"confidence":"deterministic_stub","selected_links":[{"text":"Odin","url":"https://example.com/odin"}]}`,
	}); err != nil {
		t.Fatalf("RecordRunArtifact() error = %v", err)
	}
	if _, _, err := store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:          run.ID,
		RunStatus:      "completed",
		TaskStatus:     "blocked",
		Summary:        "Huginn browser evidence recorded",
		TerminalReason: "approval_required",
		ArtifactsJSON:  `[{"type":"browser_evidence","summary":"Huginn stub browser evidence for phone release check"}]`,
	}); err != nil {
		t.Fatalf("FinishRunAndSetTaskStatus() error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: taskID,
		Reason: "approval_required",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(reblock) error = %v", err)
	}
}

func assertPhoneGET(t *testing.T, target string, wantStatus int, wantBody string) string {
	t.Helper()
	return assertPhoneGETWithHeader(t, target, nil, wantStatus, wantBody)
}

func assertPhoneGETWithHeader(t *testing.T, target string, headers map[string]string, wantStatus int, wantBody string) string {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("NewRequest(GET %s) error = %v", target, err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s error = %v", target, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(GET %s) error = %v", target, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("GET %s status = %d body=%s, want %d", target, response.StatusCode, string(body), wantStatus)
	}
	if wantBody != "" && !strings.Contains(string(body), wantBody) {
		t.Fatalf("GET %s body = %s, want %q", target, string(body), wantBody)
	}
	return string(body)
}

func postPhoneJSON(t *testing.T, target string, headers map[string]string, body string, wantStatus int) string {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest(POST %s) error = %v", target, err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", target, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(POST %s) error = %v", target, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("POST %s status = %d body=%s, want %d", target, response.StatusCode, string(raw), wantStatus)
	}
	return string(raw)
}

func postPhoneMultipartRaw(t *testing.T, target string, headers map[string]string, fields map[string]string, filename string, contentType string, data []byte, wantStatus int) {
	t.Helper()

	body, multipartContentType := multipartMobileCapture(t, fields, filename, contentType, data)
	request, err := http.NewRequest(http.MethodPost, target, body)
	if err != nil {
		t.Fatalf("NewRequest(POST multipart %s) error = %v", target, err)
	}
	request.Header.Set("Content-Type", multipartContentType)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST multipart %s error = %v", target, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != wantStatus {
		t.Fatalf("POST multipart %s status = %d body=%s, want %d", target, response.StatusCode, string(raw), wantStatus)
	}
}

func postPhoneShare(t *testing.T, target string, headers map[string]string, wantStatus int) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "title", "Phone share")
	mustWriteField(t, writer, "text", "Shared from phone release check")
	mustWriteField(t, writer, "url", "https://example.com/phone")
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="files"; filename="share.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart(share) error = %v", err)
	}
	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\nphone-share")); err != nil {
		t.Fatalf("Write(share file) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(share multipart) error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatalf("NewRequest(share) error = %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST share error = %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != wantStatus {
		t.Fatalf("POST share status = %d body=%s, want %d", response.StatusCode, string(raw), wantStatus)
	}
}

func postPhoneNotificationSubscription(t *testing.T, target string, headers map[string]string) int64 {
	t.Helper()

	body := postPhoneJSON(t, target, headers, `{"endpoint":"https://push.example.test/release-check","user_agent":"phone-release-check","platform":"test"}`, http.StatusAccepted)
	var response struct {
		SubscriptionID int64 `json:"subscription_id"`
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("Decode notification subscription response error = %v body=%s", err, body)
	}
	if response.SubscriptionID <= 0 {
		t.Fatalf("notification subscription response = %s, want subscription id", body)
	}
	return response.SubscriptionID
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

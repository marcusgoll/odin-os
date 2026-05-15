package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"odin-os/internal/adapters/huginnbrowser"
	httpapi "odin-os/internal/api/http"
	browserexec "odin-os/internal/executors/browser"
	"odin-os/internal/store/sqlite"
)

const browserMobileToken = "browser-mobile-token"

type browserMobileResponse struct {
	Items []map[string]any `json:"items"`
}

type browserApprovalResponse struct {
	Items []map[string]any `json:"items"`
}

func TestMobileBrowserMutationApprovalAppearsAndApproveRequeuesTask(t *testing.T) {
	ctx := context.Background()
	store := openMobileBrowserStore(t)
	readModels := store.DB()
	project := createMobileBrowserProject(t, ctx, store)
	task := createMobileBrowserTask(t, ctx, store, project.ID, "browser-mutation-approval")

	svc := browserexec.Service{Store: store}
	result, err := svc.RunPlugin(ctx, browserexec.PluginRequest{
		TaskID:             task.ID,
		Objective:          "Submit browser mutation after approval",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/form"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"submit_form"},
		RequestedBy:        "browser_executor",
	})
	if err != nil {
		t.Fatalf("run plugin: %v", err)
	}
	if result.ApprovalID == 0 {
		t.Fatalf("expected approval id")
	}

	server := startMobileBrowserServer(t, store, readModels)
	approvals := mobileBrowserGet(t, server, "/mobile/approvals")
	approval := findMobileItem(t, approvals.Items, "approval_id", float64(result.ApprovalID))
	if approval["browser_event"] != "browser_mutation_approval_required" {
		t.Fatalf("approval browser_event = %v", approval["browser_event"])
	}
	if approval["resolver_support"] != "supported" {
		t.Fatalf("approval resolver_support = %v", approval["resolver_support"])
	}
	if !strings.Contains(asString(approval["deep_link"]), "approval_id=") {
		t.Fatalf("missing approval deep link: %#v", approval)
	}
	assertNoBrowserSecrets(t, approval)

	review := mobileBrowserGet(t, server, "/mobile/review-queue")
	reviewItem := findMobileItem(t, review.Items, "object_id", float64(result.ApprovalID))
	if reviewItem["browser_event"] != "browser_mutation_approval_required" {
		t.Fatalf("review browser_event = %v", reviewItem["browser_event"])
	}
	assertNoBrowserSecrets(t, reviewItem)

	decision := []byte(`{"decision":"approved","reason":"operator approved browser mutation"}`)
	mobileBrowserPost(t, server, "/mobile/approvals/"+intString(result.ApprovalID)+"/decision", decision)

	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.Status != "queued" || updated.BlockedReason != "" {
		t.Fatalf("task after approval = status %q blocked_reason %q", updated.Status, updated.BlockedReason)
	}
}

func TestMobileBrowserAttendedLoginAppearsInReviewQueue(t *testing.T) {
	ctx := context.Background()
	store := openMobileBrowserStore(t)
	readModels := store.DB()
	_ = createMobileBrowserProject(t, ctx, store)
	store.BrowserSessionHandoffID = func() (string, error) { return "abc123", nil }
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "Example login",
		Domain:         "example.com",
		AccountHint:    "operator",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	request, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create login request: %v", err)
	}

	server := startMobileBrowserServer(t, store, readModels)
	review := mobileBrowserGet(t, server, "/mobile/review-queue")
	item := findMobileItem(t, review.Items, "queue_id", "browser-login:"+intString(request.ID))
	if item["browser_event"] != "browser_attended_login_required" {
		t.Fatalf("login browser_event = %v", item["browser_event"])
	}
	if !strings.Contains(asString(item["deep_link"]), "handoff_id=abc123") {
		t.Fatalf("missing handoff deep link: %#v", item)
	}
	assertNoBrowserSecrets(t, item)
}

func TestMobileReviewQueueDecisionCompletesBrowserAttendedLogin(t *testing.T) {
	ctx := context.Background()
	store := openMobileBrowserStore(t)
	readModels := store.DB()
	store.BrowserSessionHandoffID = func() (string, error) { return "complete-mobile-handoff", nil }
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "Example login",
		Domain:         "example.com",
		AccountHint:    "operator",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	request, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create login request: %v", err)
	}

	server := startMobileBrowserServer(t, store, readModels)
	sessionCookie, csrfToken, _ := registerMobileDevice(t, server, browserMobileToken)
	path := "/mobile/review-queue/" + url.PathEscape("browser-login:"+intString(request.ID)) + "/decision"
	response := postJSON(t, server.URL+path, sessionCookie.String(), csrfToken, `{"action":"complete","reason":"manual login completed in attended handoff"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("mobile session browser complete status = %d body=%s, want %d", response.StatusCode, body, http.StatusOK)
	}

	updatedSession, err := store.GetBrowserSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updatedSession.Status != sqlite.BrowserSessionStatusVerified {
		t.Fatalf("session status = %q, want verified", updatedSession.Status)
	}
	updatedRequest, err := store.GetBrowserSessionLoginRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("get login request: %v", err)
	}
	if updatedRequest.Status != sqlite.BrowserSessionLoginRequestStatusCompleted {
		t.Fatalf("login request status = %q, want completed", updatedRequest.Status)
	}
}

func TestMobileBrowserEvidenceReadyDetailAndFailedRetryable(t *testing.T) {
	ctx := context.Background()
	store := openMobileBrowserStore(t)
	readModels := store.DB()
	project := createMobileBrowserProject(t, ctx, store)
	evidenceTask := createMobileBrowserTask(t, ctx, store, project.ID, "browser-evidence")
	failedTask := createMobileBrowserTask(t, ctx, store, project.ID, "browser-failed")

	svc := browserexec.Service{Store: store, Adapter: browserMobileAdapter{status: "success", summary: "Browser evidence ready"}}
	if _, err := svc.RunWorkEvidence(ctx, browserexec.WorkEvidenceTask{
		TaskID:             evidenceTask.ID,
		Objective:          "collect evidence",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
	}); err != nil {
		t.Fatalf("run evidence: %v", err)
	}

	failingSvc := browserexec.Service{Store: store, Adapter: browserMobileAdapter{status: "failed", summary: "Manual retry required", errorCode: "timeout"}}
	if _, err := failingSvc.RunWorkEvidence(ctx, browserexec.WorkEvidenceTask{
		TaskID:             failedTask.ID,
		Objective:          "collect failing evidence",
		AllowedDomains:     []string{"example.com"},
		StartURLs:          []string{"https://example.com/slow"},
		MaxPages:           1,
		MaxDurationSeconds: 30,
	}); err != nil {
		t.Fatalf("run failing evidence: %v", err)
	}

	server := startMobileBrowserServer(t, store, readModels)
	review := mobileBrowserGet(t, server, "/mobile/review-queue")
	evidence := findMobileItemByPrefix(t, review.Items, "queue_id", "browser-evidence:")
	if evidence["browser_event"] != "browser_evidence_ready" {
		t.Fatalf("evidence browser_event = %v", evidence["browser_event"])
	}
	assertNoBrowserSecrets(t, evidence)

	detail := mobileBrowserGet(t, server, "/mobile/review-queue/detail?queue_id="+asString(evidence["queue_id"]))
	if !strings.Contains(string(mustJSON(t, detail)), "Browser evidence ready") {
		t.Fatalf("detail missing evidence summary: %#v", detail)
	}
	assertNoBrowserSecrets(t, detail)

	failed := findMobileItem(t, review.Items, "queue_id", "browser-run-failed:"+intString(failedTask.ID))
	if failed["browser_event"] != "browser_run_failed_retryable" {
		t.Fatalf("failed browser_event = %v", failed["browser_event"])
	}
	assertNoBrowserSecrets(t, failed)
}

func TestPWAShellContainsMobileApprovalReviewClient(t *testing.T) {
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{}))
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/app/")
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	body := buf.String()
	for _, expected := range []string{"Action Required", "Approvals", "Browser Needs Help", "app.js"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("app shell missing %q", expected)
		}
	}

	resp, err = http.Get(server.URL + "/app/app.js")
	if err != nil {
		t.Fatalf("get app js: %v", err)
	}
	defer resp.Body.Close()
	buf.Reset()
	_, _ = buf.ReadFrom(resp.Body)
	js := buf.String()
	for _, expected := range []string{"/mobile/approvals", "/mobile/review-queue", "/mobile/browser/status", "/mobile/notifications", "approval_id", "decision"} {
		if !strings.Contains(js, expected) {
			t.Fatalf("app js missing %q", expected)
		}
	}
}

type browserMobileAdapter struct {
	status    string
	summary   string
	errorCode string
}

func (a browserMobileAdapter) Run(ctx context.Context, request huginnbrowser.Request) (huginnbrowser.Response, error) {
	status := a.status
	if status == "" {
		status = "success"
	}
	response := huginnbrowser.Response{
		Status:                    status,
		ExtractedTextSummary:      a.summary,
		VisitedURLs:               []string{"https://example.com"},
		PageResults:               []huginnbrowser.PageResult{{URL: "https://example.com", Title: "Example", Summary: a.summary}},
		Screenshots:               []string{"stub://screenshot/1"},
		ScreenshotMetadata:        []huginnbrowser.ScreenshotMetadata{{URL: "https://example.com", Path: "stub://screenshot/1", CapturedAt: time.Now().UTC().Format(time.RFC3339)}},
		SelectedLinks:             []huginnbrowser.SelectedLink{{URL: "https://example.com/next", Text: "Next", Reason: "relevant"}},
		DownloadedFiles:           []huginnbrowser.DownloadedFileMetadata{{Name: "report.pdf", Path: "stub://report.pdf", SizeBytes: 123, ContentType: "application/pdf", SHA256: "abc123"}},
		FormStateSummary:          "1 form inspected; no sensitive fields included.",
		BrowserErrorRecoveryNotes: []string{"retry if stale"},
		Confidence:                "0.9",
		Limitations:               []string{"stub"},
	}
	if a.errorCode != "" {
		response.ErrorCode = a.errorCode
		response.ErrorMessage = a.summary
	}
	return response, nil
}

func openMobileBrowserStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store := openStore(t)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createMobileBrowserProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{Key: "odin-core", Name: "Odin Core"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return project
}

func createMobileBrowserTask(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64, key string) sqlite.Task {
	t.Helper()
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         key,
		Title:       key,
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func startMobileBrowserServer(t *testing.T, store *sqlite.Store, readModels *sql.DB) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: readModels,
		AdminToken: browserMobileToken,
	}))
	t.Cleanup(server.Close)
	return server
}

func mobileBrowserGet(t *testing.T, server *httptest.Server, path string) browserMobileResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+browserMobileToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s status %d", path, resp.StatusCode)
	}
	var out browserMobileResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func mobileBrowserPost(t *testing.T, server *httptest.Server, path string, body []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+browserMobileToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post %s status %d", path, resp.StatusCode)
	}
}

func findMobileItem(t *testing.T, items []map[string]any, key string, value any) map[string]any {
	t.Helper()
	for _, item := range items {
		if item[key] == value {
			return item
		}
	}
	t.Fatalf("missing item %s=%v in %#v", key, value, items)
	return nil
}

func findMobileItemByPrefix(t *testing.T, items []map[string]any, key string, prefix string) map[string]any {
	t.Helper()
	for _, item := range items {
		if strings.HasPrefix(asString(item[key]), prefix) {
			return item
		}
	}
	t.Fatalf("missing item %s prefix %s in %#v", key, prefix, items)
	return nil
}

func assertNoBrowserSecrets(t *testing.T, value any) {
	t.Helper()
	encoded := strings.ToLower(string(mustJSON(t, value)))
	for _, forbidden := range []string{"cookie", "token", "secret", "profile_path"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("mobile browser payload leaks %q: %s", forbidden, encoded)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return encoded
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intString(id int64) string {
	return strings.TrimSpace(strings.Trim(strings.ReplaceAll(jsonNumber(id), "\"", ""), " "))
}

func jsonNumber(id int64) string {
	encoded, _ := json.Marshal(id)
	return string(encoded)
}

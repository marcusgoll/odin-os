package lifecycle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/runtime/browserprofileartifacts"
	"odin-os/internal/runtime/browserprofilecrypto"
	"odin-os/internal/runtime/browserprofilekeys"
	"odin-os/internal/store/sqlite"
)

func TestRunBrowserRunRecordsGoalEvidenceAndKeepsGoalStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeGoalEnvelope(t, []byte(run("goal", "create", "--title", "Collect browser evidence", "--json")))
	browserRun := run("browser", "run", "--goal-id", int64String(created.ID), "--url", "https://example.com/research", "--objective", "Collect public documentation", "--allowed-domain", "example.com", "--max-pages", "2", "--max-duration-seconds", "30", "--evidence-required", "--json")
	for _, want := range []string{
		`"status": "recorded"`,
		`"goal_id": 1`,
		`"evidence_id": 1`,
		`"adapter_kind": "stub_local"`,
		`"browser_proof_kind": "stub_contract_only"`,
		`"real_browser_evidence": false`,
		`"page_results":`,
		`"status": "visited"`,
		`"no_live_browser_launched"`,
	} {
		if !strings.Contains(browserRun, want) {
			t.Fatalf("browser run output = %s, want %s", browserRun, want)
		}
	}

	shown := decodeGoalEnvelope(t, []byte(run("goal", "show", "--id", int64String(created.ID), "--json")))
	if shown.Status != string(sqlite.GoalStatusCreated) {
		t.Fatalf("goal status = %q, want unchanged created", shown.Status)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "goal.evidence_recorded"`) || !strings.Contains(logs, `"evidence_type": "browser_readonly"`) {
		t.Fatalf("logs output = %s, want browser evidence audit event", logs)
	}
}

func TestRunBrowserRunTaskRecordsWorkEvidence(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "browser-work",
		Name:          "Browser Work",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "browser-evidence-task",
		Title:       "Collect browser evidence for work",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runBrowser(ctx, app, []string{"run", "--task-id", int64String(task.ID), "--url", "https://example.com/research", "--allowed-domain", "example.com", "--objective", "Collect browser evidence", "--worker-mode", "browser", "--json"}, &stdout); err != nil {
		t.Fatalf("runBrowser(task) error = %v\n%s", err, stdout.String())
	}
	for _, want := range []string{
		`"status": "recorded"`,
		`"task_id": 1`,
		`"run_id": 1`,
		`"run_artifact_id": 1`,
		`"browser_proof_kind": "stub_contract_only"`,
		`"real_browser_evidence": false`,
		`"selected_links":`,
		`"downloaded_files":`,
		`"form_state_summary":`,
		`"confidence":`,
		`"limitations":`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("browser run output = %s, want %s", stdout.String(), want)
		}
	}
	shown, err := app.Store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if shown.Status != "queued" {
		t.Fatalf("task status = %q, want queued after successful evidence capture", shown.Status)
	}
}

func TestRunBrowserTimeoutCaptureCreatesRecoveryRecommendation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "huginn-timeout.sh")
	if err := os.WriteFile(fixture, []byte("#!/usr/bin/env bash\ncat >/dev/null\nprintf '{\"status\":\"timeout\",\"adapter_kind\":\"huginn_live\",\"error_code\":\"command_timeout\",\"error_message\":\"browser capture timed out\",\"extracted_text_summary\":\"Huginn live browser adapter timed out before evidence was complete.\"}'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("ODIN_BROWSER_ADAPTER", "live")
	t.Setenv("ODIN_HUGINN_BROWSER_COMMAND", fixture)
	t.Setenv("ODIN_HUGINN_BROWSER_ALLOWED_COMMANDS", fixture)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("work", "start", "--project", "odin-core", "--title", "Capture timing-out browser evidence", "--intent", "read_only")
	run("browser", "run", "--task-id", "1", "--url", "https://example.com/research", "--allowed-domain", "example.com", "--json")
	reviewList := run("review", "list", "--json")
	for _, want := range []string{
		`"queue_id": "failed-work:1"`,
		`"source_type": "failed_work"`,
		`"recovery_recommendation": "Browser evidence capture failed.`,
	} {
		if !strings.Contains(reviewList, want) {
			t.Fatalf("review list output = %s, want %s", reviewList, want)
		}
	}
	overview := run("overview", "--json")
	if !strings.Contains(overview, `"source": "browser_evidence"`) || !strings.Contains(overview, `"recovery_recommendation": "Browser evidence capture failed.`) {
		t.Fatalf("overview output = %s, want browser timeout recovery guidance", overview)
	}
}

func TestRunBrowserRunRejectsUnsafeInputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var createOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "create", "--title", "Collect browser evidence", "--json"}, strings.NewReader(""), &createOut); err != nil {
		t.Fatalf("Run(goal create) error = %v", err)
	}
	created := decodeGoalEnvelope(t, createOut.Bytes())

	var domainOut bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "run", "--goal-id", int64String(created.ID), "--url", "https://not-example.test/research", "--objective", "Collect public documentation", "--allowed-domain", "example.com", "--json"}, strings.NewReader(""), &domainOut)
	if err == nil || !strings.Contains(err.Error(), "disallowed domain") {
		t.Fatalf("Run(browser disallowed domain) error = %v output=%s, want disallowed domain", err, domainOut.String())
	}

	var actionOut bytes.Buffer
	err = Run(context.Background(), root, []string{"browser", "run", "--goal-id", int64String(created.ID), "--url", "https://example.com/research", "--objective", "Collect public documentation", "--action", "submit_form", "--json"}, strings.NewReader(""), &actionOut)
	if err == nil || !strings.Contains(err.Error(), "mutation action") {
		t.Fatalf("Run(browser mutation action) error = %v output=%s, want mutation action rejection", err, actionOut.String())
	}
}

func TestRunBrowserRunRejectsBrowserSessionAttachUntilImplemented(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	goal := decodeGoalEnvelope(t, []byte(run("goal", "create", "--title", "Collect browser evidence", "--json")))
	session := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--account-hint", "marcus",
		"--json",
	)))
	verified := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "status", "--id", int64String(session.ID), "--status", "verified", "--json")))
	if verified.Status != "verified" {
		t.Fatalf("verified.Status = %q, want verified", verified.Status)
	}

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "run", "--goal-id", int64String(goal.ID), "--url", "https://example.com/account", "--objective", "Collect account evidence", "--allowed-domain", "example.com", "--session-id", int64String(session.ID), "--worker-mode", "browser", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "authenticated browser session attachment is not implemented") {
		t.Fatalf("Run(browser session attach) error = %v output=%s, want fail-closed attach boundary", err, output.String())
	}

	logs := run("logs", "--json")
	if strings.Contains(logs, `"type": "goal.evidence_recorded"`) {
		t.Fatalf("logs output = %s, want no evidence event for unsupported browser session attach", logs)
	}
}

func TestRunBrowserSessionCreateListShowStatusAndRevoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--account-hint", "marcus",
		"--json",
	)))
	if created.ID != 1 || created.Name != "google-main" || created.Domain != "google.com" || created.PermissionTier != "authenticated_readonly" || created.Status != "created" {
		t.Fatalf("created session = %+v, want created google session metadata", created)
	}
	if created.ProfilePath != "browser-sessions/profiles/google-main" {
		t.Fatalf("created.ProfilePath = %q, want default profile metadata path", created.ProfilePath)
	}
	if created.ProfileStoragePolicy != "encrypted_required" {
		t.Fatalf("created.ProfileStoragePolicy = %q, want encrypted_required", created.ProfileStoragePolicy)
	}
	if created.ProfilePathExists {
		t.Fatalf("created.ProfilePathExists = true, want false before profile directory exists")
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after create err=%v, want no profile directory allocation side effects", err)
	}

	list := run("browser", "session", "list", "--json")
	for _, want := range []string{`"sessions":`, `"name": "google-main"`, `"domain": "google.com"`, `"profile_storage_policy": "encrypted_required"`, `"profile_path_exists": false`} {
		if !strings.Contains(list, want) {
			t.Fatalf("list output = %s, want %s", list, want)
		}
	}

	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(created.ProfilePath)), 0o755); err != nil {
		t.Fatalf("mkdir profile path: %v", err)
	}
	shown := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "show", "--id", int64String(created.ID), "--json")))
	if shown.ID != created.ID || shown.Name != created.Name {
		t.Fatalf("shown session = %+v, want created session", shown)
	}
	if shown.ProfileStoragePolicy != "encrypted_required" {
		t.Fatalf("shown.ProfileStoragePolicy = %q, want encrypted_required", shown.ProfileStoragePolicy)
	}
	if !shown.ProfilePathExists {
		t.Fatalf("shown.ProfilePathExists = false, want true after empty profile directory exists")
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(created.ProfilePath)))
	if err != nil {
		t.Fatalf("ReadDir(profile path) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("profile path entries = %v, want empty directory only", entries)
	}

	requiresLogin := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "status", "--id", int64String(created.ID), "--status", "requires_attended_login", "--json")))
	if requiresLogin.Status != "requires_attended_login" {
		t.Fatalf("requiresLogin.Status = %q, want requires_attended_login", requiresLogin.Status)
	}

	verified := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "status", "--id", int64String(created.ID), "--status", "verified", "--json")))
	if verified.Status != "verified" || verified.LastVerifiedAt == "" {
		t.Fatalf("verified session = %+v, want verified with timestamp", verified)
	}

	revoked := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "revoke", "--id", int64String(created.ID), "--json")))
	if revoked.Status != "revoked" || revoked.RevokedAt == "" {
		t.Fatalf("revoked session = %+v, want revoked with timestamp", revoked)
	}

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.session_created"`, `"type": "browser.session_status_changed"`, `"type": "browser.session_revoked"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionValidationErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "create", "--domain", "google.com", "--permission-tier", "authenticated_read", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "--name is required") {
		t.Fatalf("Run(browser session create missing name) error = %v output=%s, want name required", err, output.String())
	}

	output.Reset()
	err = Run(context.Background(), root, []string{"browser", "session", "status", "--id", "1", "--status", "invalid", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "--status must be") {
		t.Fatalf("Run(browser session status invalid) error = %v output=%s, want status validation", err, output.String())
	}

	output.Reset()
	err = Run(context.Background(), root, []string{"browser", "session", "create", "--name", "unsafe", "--domain", "google.com", "--permission-tier", "authenticated_read", "--profile-path", "../escape", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "stay under ODIN_ROOT") {
		t.Fatalf("Run(browser session create unsafe profile path) error = %v output=%s, want traversal rejection", err, output.String())
	}
}

func TestRunBrowserSessionLoginRequestCreateAndList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequestOutput := run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")
	if !strings.Contains(loginRequestOutput, `"handoff_url": null`) {
		t.Fatalf("login-request output = %s, want explicit null handoff_url", loginRequestOutput)
	}
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(loginRequestOutput))
	if loginRequest.ID != 1 || loginRequest.SessionID != created.ID || loginRequest.Status != "requested" || loginRequest.ExpiresAt == "" || loginRequest.HandoffID == "" || loginRequest.HandoffURL != nil {
		t.Fatalf("login request = %+v, want requested metadata with handoff id and nil handoff URL", loginRequest)
	}

	loginWithURL := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run(
		"browser", "session", "login-request",
		"--id", int64String(created.ID),
		"--handoff-base-url", "https://odin-handoff.tailnet.local/manual-login",
		"--json",
	)))
	if loginWithURL.HandoffID == "" || loginWithURL.HandoffURL == nil || !strings.HasPrefix(*loginWithURL.HandoffURL, "https://odin-handoff.tailnet.local/manual-login?handoff_id=") {
		t.Fatalf("loginWithURL = %+v, want metadata handoff URL with opaque id", loginWithURL)
	}
	if loginWithURL.HandoffID == int64String(created.ID) || strings.Contains(*loginWithURL.HandoffURL, "session_id=") {
		t.Fatalf("loginWithURL = %+v, must not expose session id in handoff metadata", loginWithURL)
	}

	loginRequestsOutput := run("browser", "session", "login-requests", "--id", int64String(created.ID), "--json")
	if !strings.Contains(loginRequestsOutput, `"login_requests":`) || !strings.Contains(loginRequestsOutput, `"status": "requested"`) || !strings.Contains(loginRequestsOutput, `"handoff_id":`) {
		t.Fatalf("login-requests output = %s, want requested login metadata list", loginRequestsOutput)
	}

	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after login request err=%v, want metadata-only request", err)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.session_login_requested"`) {
		t.Fatalf("logs output = %s, want browser.session_login_requested audit event", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionLoginRequestUsesConfiguredHandoffBaseURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ODIN_BROWSER_HANDOFF_BASE_URL", "https://odin.marcusgoll.com/browser/session/handoff")
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "x-profile-bio-stress",
		"--domain", "x.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run(
		"browser", "session", "login-request",
		"--id", int64String(created.ID),
		"--json",
	)))
	if loginRequest.HandoffURL == nil || !strings.HasPrefix(*loginRequest.HandoffURL, "https://odin.marcusgoll.com/browser/session/handoff?handoff_id=") {
		t.Fatalf("loginRequest.HandoffURL = %v, want configured operator handoff URL", loginRequest.HandoffURL)
	}
	if strings.Contains(*loginRequest.HandoffURL, "localhost") || strings.Contains(*loginRequest.HandoffURL, "127.0.0.1") || strings.Contains(*loginRequest.HandoffURL, "session_id=") {
		t.Fatalf("loginRequest.HandoffURL = %q, must be routed metadata URL without session id", *loginRequest.HandoffURL)
	}
}

func TestRunBrowserSessionRunnerMetadataCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))

	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))
	if runner.ID != 1 || runner.SessionID != created.ID || runner.LoginRequestID != loginRequest.ID || runner.HandoffID != loginRequest.HandoffID {
		t.Fatalf("runner = %+v, want linked runner metadata", runner)
	}
	if runner.Status != "requested" || runner.ViewerURL != nil || runner.RunnerID != nil || runner.ProcessID != nil || runner.ExpiresAt == "" {
		t.Fatalf("runner = %+v, want requested metadata-only runner with nil process fields", runner)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after runner create err=%v, want metadata-only runner", err)
	}

	listOutput := run("browser", "session", "runner", "list", "--login-request-id", int64String(loginRequest.ID), "--json")
	if !strings.Contains(listOutput, `"runners":`) || !strings.Contains(listOutput, `"status": "requested"`) || !strings.Contains(listOutput, `"viewer_url": null`) {
		t.Fatalf("runner list output = %s, want requested runner metadata list", listOutput)
	}

	shown := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "show", "--id", int64String(runner.ID), "--json")))
	if shown.ID != runner.ID || shown.HandoffID != runner.HandoffID {
		t.Fatalf("shown runner = %+v, want created runner", shown)
	}

	started := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "status", "--id", int64String(runner.ID), "--status", "started", "--json")))
	if started.Status != "started" || started.StartedAt == "" {
		t.Fatalf("started runner = %+v, want started with timestamp", started)
	}

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "status", "--id", int64String(runner.ID), "--status", "completed", "--json")))
	if completed.Status != "completed" || completed.CompletedAt == "" || completed.ExitedAt == "" {
		t.Fatalf("completed runner = %+v, want completed with completion and exit timestamps", completed)
	}

	cancelRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	cancelRunner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(cancelRequest.ID), "--json")))
	cancelled := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "cancel", "--id", int64String(cancelRunner.ID), "--json")))
	if cancelled.Status != "cancelled" || cancelled.CancelledAt == "" || cancelled.ExitedAt == "" {
		t.Fatalf("cancelled runner = %+v, want cancelled with cancellation and exit timestamps", cancelled)
	}

	verifiedRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	run("browser", "session", "verify", "--id", int64String(created.ID), "--login-request-id", int64String(verifiedRequest.ID), "--json")
	var completedRequestOutput bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "runner", "create", "--login-request-id", int64String(verifiedRequest.ID), "--json"}, strings.NewReader(""), &completedRequestOutput)
	if err == nil || !strings.Contains(err.Error(), `status "completed"`) {
		t.Fatalf("Run(runner create completed request) error = %v output=%s, want completed request rejection", err, completedRequestOutput.String())
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("Open store error = %v", err)
	}
	defer store.Close()
	expiredRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	if _, err := store.DB().ExecContext(context.Background(), `UPDATE browser_session_login_requests SET expires_at = ? WHERE id = ?`, "2000-01-01T00:00:00.000000000Z", expiredRequest.ID); err != nil {
		t.Fatalf("expire login request metadata error = %v", err)
	}
	var expiredRequestOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"browser", "session", "runner", "create", "--login-request-id", int64String(expiredRequest.ID), "--json"}, strings.NewReader(""), &expiredRequestOutput)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Run(runner create expired request) error = %v output=%s, want expired request rejection", err, expiredRequestOutput.String())
	}

	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "browser.handoff_runner_requested"`,
		`"type": "browser.handoff_runner_started"`,
		`"type": "browser.handoff_runner_completed"`,
		`"type": "browser.handoff_runner_cancelled"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartUsesStubRunnerSafely(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "stub-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	started := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if started.Status != "failed" {
		t.Fatalf("started runner status = %q, want failed for StubRunner not_implemented", started.Status)
	}
	if started.ErrorCode == nil || *started.ErrorCode != "not_implemented" {
		t.Fatalf("started runner error_code = %v, want not_implemented", started.ErrorCode)
	}
	if started.ViewerURL != nil || started.RunnerID != nil || started.ProcessID != nil {
		t.Fatalf("started runner = %+v, want no viewer/process metadata from StubRunner", started)
	}
	if started.StartedAt != "" {
		t.Fatalf("started.StartedAt = %q, want empty because StubRunner did not start", started.StartedAt)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after runner start err=%v, want metadata-only stub start", err)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.handoff_runner_failed"`) || !strings.Contains(logs, `"error_code": "not_implemented"`) {
		t.Fatalf("logs output = %s, want failed not_implemented runner audit event", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}

	completedRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	completedRunner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(completedRequest.ID), "--json")))
	run("browser", "session", "verify", "--id", int64String(created.ID), "--login-request-id", int64String(completedRequest.ID), "--json")
	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "runner", "start", "--id", int64String(completedRunner.ID), "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), `status "completed"`) {
		t.Fatalf("Run(runner start completed request) error = %v output=%s, want completed request rejection", err, output.String())
	}
}

func TestRunBrowserSessionRunnerStartUsesNoVNCFixtureLaunchSafely(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", commandPath)
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "300")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "novnc-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	started := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if started.Status != "completed" {
		t.Fatalf("started runner status = %q, want completed for harmless NoVNC fixture launch", started.Status)
	}
	if started.ErrorCode != nil || started.ErrorMessage != nil {
		t.Fatalf("started runner error metadata = %v/%v, want empty after completion", started.ErrorCode, started.ErrorMessage)
	}
	if started.ViewerURL == nil || !strings.HasPrefix(*started.ViewerURL, "https://odin-handoff.tailnet.local/session/novnc-") {
		t.Fatalf("started runner viewer_url = %v, want private NoVNC fixture viewer URL", started.ViewerURL)
	}
	if started.RunnerID == nil || !strings.HasPrefix(*started.RunnerID, "novnc-") || started.ProcessID == nil || *started.ProcessID <= 0 || started.StartedAt == "" || started.CompletedAt == "" {
		t.Fatalf("started runner = %+v, want runner/process and started/completed metadata", started)
	}
	if started.BindAddr == nil || *started.BindAddr != "127.0.0.1:6080" || started.PrivateBaseURL == nil || *started.PrivateBaseURL != "https://odin-handoff.tailnet.local" {
		t.Fatalf("started runner network metadata = bind %v base %v, want validated private metadata", started.BindAddr, started.PrivateBaseURL)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after runner start err=%v, want fixture process proof only", err)
	}
	assertNoBrowserSessionArtifacts(t, root)

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.handoff_runner_started"`) || !strings.Contains(logs, `"type": "browser.handoff_runner_completed"`) {
		t.Fatalf("logs output = %s, want started and completed runner audit events", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartUsesRealWebsockifyGateWithFakeBackends(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")
	markerPath := filepath.Join(t.TempDir(), "websockify.marker")
	websockifyPath := writeLifecycleExecutable(t, "websockify", "#!/bin/sh\nprintf ran > "+shellQuote(markerPath)+"\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", websockifyPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{commandPath, websockifyPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_WEBSOCKIFY", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "websockify-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if completed.Status != "completed" {
		t.Fatalf("completed runner status = %q, want completed for real websockify role with fake backends", completed.Status)
	}
	if completed.ViewerURL == nil || !strings.HasPrefix(*completed.ViewerURL, "https://odin-handoff.tailnet.local/session/novnc-") {
		t.Fatalf("completed runner viewer_url = %v, want private NoVNC viewer URL", completed.ViewerURL)
	}
	if payload, err := os.ReadFile(markerPath); err != nil || string(payload) != "ran" {
		t.Fatalf("websockify marker payload=%q err=%v, want real websockify role executable to run", string(payload), err)
	}
	assertNoBrowserSessionArtifacts(t, root)

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.handoff_runner_started"`, `"type": "browser.handoff_runner_completed"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartUsesRealDisplayGateWithFakeBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")
	markerPath := filepath.Join(t.TempDir(), "display.marker")
	displayPath := writeLifecycleExecutable(t, "display-vnc", "#!/bin/sh\nprintf ran > "+shellQuote(markerPath)+"\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", displayPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{commandPath, displayPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_DISPLAY", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "display-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if completed.Status != "completed" {
		t.Fatalf("completed runner status = %q, want completed for real display role with fake browser", completed.Status)
	}
	if completed.ViewerURL == nil || !strings.HasPrefix(*completed.ViewerURL, "https://odin-handoff.tailnet.local/session/novnc-") {
		t.Fatalf("completed runner viewer_url = %v, want private NoVNC viewer URL", completed.ViewerURL)
	}
	if payload, err := os.ReadFile(markerPath); err != nil || string(payload) != "ran" {
		t.Fatalf("display marker payload=%q err=%v, want real display role executable to run", string(payload), err)
	}
	assertNoBrowserSessionArtifacts(t, root)

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.handoff_runner_started"`, `"type": "browser.handoff_runner_completed"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartUsesRealBrowserGateWithFakeBackends(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")
	markerPath := filepath.Join(t.TempDir(), "browser.marker")
	browserPath := writeLifecycleExecutable(t, "browser", "#!/bin/sh\nprintf ran > "+shellQuote(markerPath)+"\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", browserPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{commandPath, browserPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_BROWSER", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "browser-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if completed.Status != "completed" {
		t.Fatalf("completed runner status = %q, want completed for real browser role with fake backends", completed.Status)
	}
	if completed.ViewerURL == nil || !strings.HasPrefix(*completed.ViewerURL, "https://odin-handoff.tailnet.local/session/novnc-") {
		t.Fatalf("completed runner viewer_url = %v, want private NoVNC viewer URL", completed.ViewerURL)
	}
	if payload, err := os.ReadFile(markerPath); err != nil || string(payload) != "ran" {
		t.Fatalf("browser marker payload=%q err=%v, want real browser role executable to run", string(payload), err)
	}
	assertNoBrowserSessionArtifacts(t, root)

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.handoff_runner_started"`, `"type": "browser.handoff_runner_completed"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartRealBrowserDoesNotReusePreparedProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")
	markerPath := filepath.Join(t.TempDir(), "browser.marker")
	browserPath := writeLifecycleExecutable(t, "browser", "#!/bin/sh\nprintf ran > "+shellQuote(markerPath)+"\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", browserPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", commandPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{commandPath, browserPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_BROWSER", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "browser-prepared-profile",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	run("browser", "session", "prepare-profile", "--id", int64String(created.ID), "--json")
	profileDir := filepath.Join(root, filepath.FromSlash(created.ProfilePath))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if completed.Status != "completed" {
		t.Fatalf("completed runner status = %q, want completed for real browser role with prepared profile metadata", completed.Status)
	}
	if payload, err := os.ReadFile(markerPath); err != nil || string(payload) != "ran" {
		t.Fatalf("browser marker payload=%q err=%v, want real browser role executable to run", string(payload), err)
	}
	entries, err := os.ReadDir(profileDir)
	if err != nil {
		t.Fatalf("ReadDir(profileDir) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("profile directory entries = %v, want browser runner to leave prepared profile empty", entries)
	}
	for _, path := range []string{"cookies", "credentials"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("%s exists after runner start err=%v, want no credential/session artifact", path, err)
		}
	}

	logs := run("logs", "--json")
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartFullRealNoVNCRecordsStartedEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	displayMarker := filepath.Join(t.TempDir(), "display.marker")
	browserMarker := filepath.Join(t.TempDir(), "browser.marker")
	websockifyMarker := filepath.Join(t.TempDir(), "websockify.marker")
	displayPath := writeLifecycleExecutable(t, "display-vnc", "#!/bin/sh\nprintf ran > "+shellQuote(displayMarker)+"\nsleep 2\n")
	browserPath := writeLifecycleExecutable(t, "browser", "#!/bin/sh\nprintf ran > "+shellQuote(browserMarker)+"\nsleep 2\n")
	websockifyPath := writeLifecycleExecutable(t, "websockify", "#!/bin/sh\nprintf ran > "+shellQuote(websockifyMarker)+"\nsleep 2\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", browserPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", displayPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", websockifyPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{displayPath, browserPath, websockifyPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_DISPLAY", "true")
	t.Setenv("ODIN_NOVNC_REAL_BROWSER", "true")
	t.Setenv("ODIN_NOVNC_REAL_WEBSOCKIFY", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "real-novnc-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	started := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if started.Status != "started" {
		t.Fatalf("started runner status = %q, want started for full real NoVNC handoff", started.Status)
	}
	if !started.RealBrowserEvidence {
		t.Fatalf("started runner = %+v, want real_browser_evidence marker", started)
	}
	if started.ViewerURL == nil || !strings.HasPrefix(*started.ViewerURL, "https://odin-handoff.tailnet.local/session/novnc-real-") {
		t.Fatalf("started runner viewer_url = %v, want private real NoVNC viewer URL", started.ViewerURL)
	}
	if started.RunnerID == nil || !strings.HasPrefix(*started.RunnerID, "novnc-real-") || started.ProcessID == nil || *started.ProcessID <= 0 || started.StartedAt == "" || started.CompletedAt != "" {
		t.Fatalf("started runner = %+v, want real runner/process started metadata without completion", started)
	}
	for _, marker := range []string{displayMarker, browserMarker, websockifyMarker} {
		waitForBrowserSessionMarker(t, marker, "ran")
	}
	assertNoBrowserSessionArtifacts(t, root)

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.handoff_runner_started"`) || !strings.Contains(logs, `"real_browser_evidence": true`) {
		t.Fatalf("logs output = %s, want started runner audit event with real browser evidence marker", logs)
	}
	if strings.Contains(logs, `"type": "browser.handoff_runner_completed"`) {
		t.Fatalf("logs output = %s, want full real NoVNC runner to remain started for attended login", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartFullRealNoVNCFailedChildPersistsEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	displayPath := writeLifecycleExecutable(t, "display-vnc", "#!/bin/sh\nsleep 2\n")
	browserPath := writeLifecycleExecutable(t, "browser", "#!/bin/sh\necho browser failed during startup >&2\nexit 7\n")
	websockifyPath := writeLifecycleExecutable(t, "websockify", "#!/bin/sh\nsleep 2\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", browserPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", displayPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", websockifyPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{displayPath, browserPath, websockifyPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_DISPLAY", "true")
	t.Setenv("ODIN_NOVNC_REAL_BROWSER", "true")
	t.Setenv("ODIN_NOVNC_REAL_WEBSOCKIFY", "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "real-novnc-failed-child",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	failed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if failed.Status != "failed" {
		t.Fatalf("runner status = %q, want failed for startup child failure", failed.Status)
	}
	if failed.ErrorCode == nil || *failed.ErrorCode != "novnc_process_failed" {
		t.Fatalf("runner error_code = %v, want novnc_process_failed", failed.ErrorCode)
	}
	if failed.ErrorMessage == nil || !strings.Contains(*failed.ErrorMessage, "browser failed during startup") {
		t.Fatalf("runner error_message = %v, want browser stderr evidence", failed.ErrorMessage)
	}
	if failed.RunnerID == nil || !strings.HasPrefix(*failed.RunnerID, "novnc-real-") || failed.ProcessID == nil || *failed.ProcessID <= 0 || failed.StartedAt == "" || failed.ExitedAt == "" {
		t.Fatalf("failed runner = %+v, want runner/process started and exited metadata", failed)
	}

	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "browser.handoff_runner_started"`,
		`"type": "browser.handoff_runner_failed"`,
		`"error_code": "novnc_process_failed"`,
		`"child_processes"`,
		`"role": "browser"`,
		`"stderr_excerpt": "browser failed during startup`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want evidence marker %s", logs, want)
		}
	}
}

func TestRunBrowserSessionProveStartsSavedProfileRunnerAndRecordsProof(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	displayPath := writeLifecycleExecutable(t, "display-vnc", "#!/bin/sh\nsleep 2\n")
	browserPath := writeLifecycleExecutable(t, "browser", "#!/bin/sh\nsleep 2\n")
	websockifyPath := writeLifecycleExecutable(t, "websockify", "#!/bin/sh\nsleep 2\n")
	titlePath := writeLifecycleExecutable(t, "prove-title", "#!/bin/sh\nprintf 'Home / X - Google Chrome\\n'\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "novnc")
	t.Setenv("ODIN_NOVNC_BROWSER_COMMAND", browserPath)
	t.Setenv("ODIN_NOVNC_DISPLAY_COMMAND", displayPath)
	t.Setenv("ODIN_NOVNC_WEBSOCKIFY_COMMAND", websockifyPath)
	t.Setenv("ODIN_NOVNC_ALLOWED_COMMANDS", strings.Join([]string{displayPath, browserPath, websockifyPath}, ","))
	t.Setenv("ODIN_NOVNC_BIND_ADDR", "127.0.0.1:6080")
	t.Setenv("ODIN_NOVNC_PRIVATE_BASE_URL", "https://odin-handoff.tailnet.local")
	t.Setenv("ODIN_NOVNC_TIMEOUT_SECONDS", "2")
	t.Setenv("ODIN_NOVNC_REAL_DISPLAY", "true")
	t.Setenv("ODIN_NOVNC_REAL_BROWSER", "true")
	t.Setenv("ODIN_NOVNC_REAL_WEBSOCKIFY", "true")
	t.Setenv("ODIN_BROWSER_PROOF_TITLE_COMMAND", titlePath)
	t.Setenv("ODIN_BROWSER_PROOF_TITLE_ALLOWED_COMMANDS", titlePath)

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	session := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "x-main",
		"--domain", "x.com",
		"--permission-tier", "authenticated_read",
		"--profile-path", "browser-sessions/profiles/x-main",
		"--json",
	)))
	sourceDir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(filepath.Join(sourceDir, "Default"), 0o700); err != nil {
		t.Fatalf("MkdirAll profile source error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Default", "Preferences"), []byte("saved x session"), 0o600); err != nil {
		t.Fatalf("WriteFile profile source error = %v", err)
	}
	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "create-directory",
		"--session-id", int64String(session.ID),
		"--name", "x-proof-profile",
		"--source-dir", sourceDir,
		"--json",
	)))

	proof := decodeBrowserSessionProofEnvelope(t, []byte(run(
		"browser", "session", "prove",
		"--id", int64String(session.ID),
		"--url", "https://x.com/",
		"--expect-title", "Home / X",
		"--json",
	)))
	if proof.Status != "passed" || proof.ArtifactID != artifact.ID || proof.RunnerID <= 0 || proof.LoginRequestID <= 0 || proof.HandoffID == "" {
		t.Fatalf("proof = %+v, want passed proof linked to artifact, runner, and login request", proof)
	}
	if proof.ObservedTitle != "Home / X - Google Chrome" || proof.TitleSource != titlePath {
		t.Fatalf("proof title = %q source=%q, want allowlisted title command output", proof.ObservedTitle, proof.TitleSource)
	}
	for _, want := range []string{"encrypted_profile_materialized", "protected_viewer_route", "no_direct_novnc_exposure", "saved_session_login_skip", "mutation_requires_approval"} {
		if !browserSessionProofHasPassedCheck(proof, want) {
			t.Fatalf("proof checks = %+v, want passed %s", proof.Checks, want)
		}
	}
	materialized := filepath.Join(root, "runtime", "browser-profile-materializations", "runner-"+int64String(proof.RunnerID)+"-artifact-"+int64String(artifact.ID))
	if info, err := os.Stat(materialized); err != nil || !info.IsDir() {
		t.Fatalf("materialized profile stat err=%v info=%v, want directory", err, info)
	}
	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.session_proof_recorded"`, `"saved_session_login_skip"`, `"mutation_requires_approval"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want proof evidence %s", logs, want)
		}
	}
}

func TestRunBrowserSessionApplyXBioRequiresApprovalAndRecordsEvidence(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	app.RuntimeRoot = t.TempDir()
	app.RepoRoot = testRepoRoot(t)
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 7)
	}
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))

	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "x-bio",
		Name:          "X Bio",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "x-bio-task",
		Title:       "Apply approved X bio",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := app.Store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      task.ID,
		RequestedBy: "operator",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runBrowser(ctx, app, []string{"session", "create", "--name", "x-main", "--domain", "x.com", "--permission-tier", "authenticated_read", "--profile-path", "browser-sessions/profiles/x-main", "--json"}, &stdout); err != nil {
		t.Fatalf("runBrowser(session create) error = %v", err)
	}
	session := decodeBrowserSessionEnvelope(t, stdout.Bytes())
	stdout.Reset()
	if err := runBrowser(ctx, app, []string{"session", "status", "--id", int64String(session.ID), "--status", "verified", "--json"}, &stdout); err != nil {
		t.Fatalf("runBrowser(session verify) error = %v", err)
	}
	sourceDir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(filepath.Join(sourceDir, "Default"), 0o700); err != nil {
		t.Fatalf("MkdirAll profile source error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Default", "Preferences"), []byte("saved x session"), 0o600); err != nil {
		t.Fatalf("WriteFile profile source error = %v", err)
	}
	stdout.Reset()
	if err := runBrowser(ctx, app, []string{"session", "profile", "artifact", "create-directory", "--session-id", int64String(session.ID), "--name", "x-bio-profile", "--source-dir", sourceDir, "--json"}, &stdout); err != nil {
		t.Fatalf("runBrowser(artifact create-directory) error = %v\n%s", err, stdout.String())
	}
	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, stdout.Bytes())

	approval, err := app.Store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	stdout.Reset()
	err = runBrowser(ctx, app, []string{"session", "apply-x-bio", "--id", int64String(session.ID), "--task-id", int64String(task.ID), "--approval-id", int64String(approval.ID), "--bio", "Daily autonomy proof via Odin OS.", "--json"}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "must be approved") {
		t.Fatalf("runBrowser(pending approval) error = %v output=%s, want approved approval rejection", err, stdout.String())
	}
	if _, err := app.Store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "approved X bio mutation",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	failDriverPath := writeLifecycleExecutable(t, "x-bio-driver-fail", `#!/bin/sh
cat >/dev/null
test -d "$ODIN_CHROME_PROFILE_DIR" || exit 12
printf '{"status":"failed","tool_key":"browser_x_profile_bio_update","summary":"simulated X save failure","artifacts":{"reason":"test_failure","save_clicked":true,"post_save_url":"https://x.com/home","profile_url":"https://x.com/marcusgoll","bio_verified":false,"observed_title":"Marcus / X"}}'
`)
	t.Setenv("ODIN_X_BIO_DRIVER", failDriverPath)
	t.Setenv("ODIN_X_BIO_ALLOWED_DRIVERS", failDriverPath)

	stdout.Reset()
	err = runBrowser(ctx, app, []string{"session", "apply-x-bio", "--id", int64String(session.ID), "--task-id", int64String(task.ID), "--approval-id", int64String(approval.ID), "--bio", "Daily autonomy proof via Odin OS.", "--json"}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "x_bio_mutation_failed") {
		t.Fatalf("runBrowser(apply-x-bio failure) error = %v\n%s, want failed run error", err, stdout.String())
	}
	failedTask, err := app.Store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() after failed X bio error = %v", err)
	}
	if failedTask.Status != "failed" {
		t.Fatalf("task status after failed X bio = %q, want failed", failedTask.Status)
	}

	driverPath := writeLifecycleExecutable(t, "x-bio-driver", `#!/bin/sh
cat >/dev/null
test -d "$ODIN_CHROME_PROFILE_DIR" || exit 12
printf '{"status":"completed","tool_key":"browser_x_profile_bio_update","summary":"Applied approved X profile bio change and verified it on the X profile page.","artifacts":{"target_url":"https://x.com/settings/profile","post_save_url":"https://x.com/home","profile_url":"https://x.com/marcusgoll","current_url":"https://x.com/marcusgoll","observed_title":"Marcus / X","save_clicked":true,"bio_verified":true,"bio":"Daily autonomy proof via Odin OS."}}'
`)
	t.Setenv("ODIN_X_BIO_DRIVER", driverPath)
	t.Setenv("ODIN_X_BIO_ALLOWED_DRIVERS", driverPath)

	stdout.Reset()
	if err := runBrowser(ctx, app, []string{"session", "apply-x-bio", "--id", int64String(session.ID), "--task-id", int64String(task.ID), "--approval-id", int64String(approval.ID), "--bio", "Daily autonomy proof via Odin OS.", "--json"}, &stdout); err != nil {
		t.Fatalf("runBrowser(apply-x-bio) error = %v\n%s", err, stdout.String())
	}
	result := decodeBrowserSessionXBioApplyEnvelope(t, stdout.Bytes())
	if result.Status != "completed" || result.TaskID != task.ID || result.ApprovalID != approval.ID || result.SessionID != session.ID || result.ArtifactID != artifact.ID || result.RunID <= 0 || result.RunArtifactID <= 0 {
		t.Fatalf("apply result = %+v, want completed task/run/artifact evidence", result)
	}
	updated, err := app.Store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if updated.Status != "completed" || updated.BlockedReason != "" {
		t.Fatalf("task status=%q blocked_reason=%q, want completed with cleared block", updated.Status, updated.BlockedReason)
	}
	run, err := app.Store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Status != "completed" || !strings.Contains(run.ArtifactsJSON, "browser_x_bio") {
		t.Fatalf("run = %+v, want completed browser_x_bio artifact", run)
	}
	artifacts, err := app.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.RunID})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("run artifacts len = %d, want 1", len(artifacts))
	}
	for _, want := range []string{`"save_clicked":true`, `"post_save_url":"https://x.com/home"`, `"profile_url":"https://x.com/marcusgoll"`, `"bio_verified":true`, `"observed_title":"Marcus / X"`} {
		if !strings.Contains(artifacts[0].DetailsJSON, want) {
			t.Fatalf("artifact details missing %s: %s", want, artifacts[0].DetailsJSON)
		}
	}
}

func waitForBrowserSessionMarker(t *testing.T, path string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var payload []byte
	var err error
	for time.Now().Before(deadline) {
		payload, err = os.ReadFile(path)
		if err == nil && string(payload) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("marker %s payload=%q err=%v, want real role executable to run", path, string(payload), err)
}

func TestRunBrowserSessionRunnerPlanNoVNCIsReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	commandPath := testLifecycleExecutablePath(t, "true")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "novnc-plan",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))
	logsBefore := run("logs", "--json")

	plan := decodeBrowserSessionRunnerPlanEnvelope(t, []byte(run(novncPlanArgs(runner.ID, commandPath, "127.0.0.1:6080")...)))
	if plan.ID != runner.ID || plan.SessionID != created.ID || plan.LoginRequestID != loginRequest.ID || plan.HandoffID != loginRequest.HandoffID {
		t.Fatalf("plan = %+v, want linked runner/session/request metadata", plan)
	}
	if plan.ViewerURL != "https://odin-handoff.tailnet.local/session/dry-run-"+loginRequest.HandoffID {
		t.Fatalf("plan.ViewerURL = %q, want private dry-run viewer URL", plan.ViewerURL)
	}
	if plan.BindAddr != "127.0.0.1:6080" || plan.PrivateBaseURL != "https://odin-handoff.tailnet.local" || plan.TimeoutSeconds != 300 {
		t.Fatalf("plan config = %+v, want validated bind/base/timeout", plan)
	}
	if len(plan.Commands) != 3 {
		t.Fatalf("plan.Commands = %+v, want planned display/browser/novnc commands", plan.Commands)
	}

	shown := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "show", "--id", int64String(runner.ID), "--json")))
	if shown.Status != "requested" || shown.ViewerURL != nil || shown.RunnerID != nil || shown.ProcessID != nil || shown.StartedAt != "" {
		t.Fatalf("shown runner after plan = %+v, want unmutated requested metadata", shown)
	}
	logsAfter := run("logs", "--json")
	if logsAfter != logsBefore {
		t.Fatalf("logs changed after plan-novnc\nbefore=%s\nafter=%s", logsBefore, logsAfter)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after plan-novnc err=%v, want dry-run only", err)
	}

	var unsafeOutput bytes.Buffer
	err := Run(context.Background(), root, novncPlanArgs(runner.ID, commandPath, "0.0.0.0:6080"), strings.NewReader(""), &unsafeOutput)
	if err == nil || !strings.Contains(err.Error(), "bind_addr") {
		t.Fatalf("Run(plan-novnc unsafe bind) error = %v output=%s, want bind_addr rejection", err, unsafeOutput.String())
	}
	if logsAfterUnsafe := run("logs", "--json"); logsAfterUnsafe != logsBefore {
		t.Fatalf("logs changed after rejected plan-novnc\nbefore=%s\nafter=%s", logsBefore, logsAfterUnsafe)
	}
}

func TestRunBrowserSessionRunnerStartFixtureCompletesThroughSupervisor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	fixtureCommand := testLifecycleExecutablePath(t, "true")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "fixture")
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_COMMAND", fixtureCommand)
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_ALLOWED_COMMANDS", fixtureCommand)
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_TIMEOUT_SECONDS", "2")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "fixture-start",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	completed := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if completed.Status != "completed" || completed.StartedAt == "" || completed.CompletedAt == "" || completed.ExitedAt == "" {
		t.Fatalf("completed runner = %+v, want supervisor start and completion metadata", completed)
	}
	if completed.RunnerID == nil || *completed.RunnerID == "" || completed.ProcessID == nil || *completed.ProcessID <= 0 {
		t.Fatalf("completed runner = %+v, want fixture runner id and process id", completed)
	}
	if completed.ViewerURL != nil {
		t.Fatalf("completed runner viewer_url = %v, want null fixture viewer URL", completed.ViewerURL)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after fixture runner start err=%v, want metadata-only fixture start", err)
	}

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.handoff_runner_started"`, `"type": "browser.handoff_runner_completed"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionRunnerStartFixtureTimeoutThroughSupervisor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	fixtureCommand := testLifecycleExecutablePath(t, "sleep")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "fixture")
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_COMMAND", fixtureCommand)
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_ARGS", "5")
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_ALLOWED_COMMANDS", fixtureCommand)
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_TIMEOUT_SECONDS", "1")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "fixture-timeout",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	expired := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if expired.Status != "expired" || expired.StartedAt == "" || expired.ExitedAt == "" {
		t.Fatalf("expired runner = %+v, want supervisor timeout metadata", expired)
	}
	if expired.ErrorCode == nil || *expired.ErrorCode != "fixture_timeout" {
		t.Fatalf("expired runner error_code = %v, want fixture_timeout", expired.ErrorCode)
	}
	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.handoff_runner_started"`, `"type": "browser.handoff_runner_expired"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
}

func TestRunBrowserSessionRunnerStartFixtureRejectsDisallowedCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	markerPath := filepath.Join(t.TempDir(), "marker")
	fixtureCommand := writeLifecycleExecutable(t, "mark.sh", "#!/bin/sh\nprintf ran > "+shellQuote(markerPath)+"\n")
	t.Setenv("ODIN_BROWSER_HANDOFF_RUNNER", "fixture")
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_COMMAND", fixtureCommand)
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_ALLOWED_COMMANDS", testLifecycleExecutablePath(t, "true"))
	t.Setenv("ODIN_BROWSER_HANDOFF_FIXTURE_TIMEOUT_SECONDS", "2")

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "fixture-disallowed",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	runner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(loginRequest.ID), "--json")))

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("Run(disallowed fixture start) error = %v output=%s, want allowlist rejection", err, output.String())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("marker stat error = %v, want no marker because disallowed command was not executed", err)
	}
	shown := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "show", "--id", int64String(runner.ID), "--json")))
	if shown.Status != "requested" || shown.StartedAt != "" || shown.ExitedAt != "" {
		t.Fatalf("shown runner after rejected start = %+v, want unchanged requested runner", shown)
	}
}

func TestRunBrowserSessionHandoffShowValidatesReadOnlyMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--account-hint", "marcus",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run(
		"browser", "session", "login-request",
		"--id", int64String(created.ID),
		"--handoff-base-url", "https://odin-handoff.tailnet.local/manual-login",
		"--json",
	)))

	logsBefore := run("logs", "--json")
	handoffOutput := run("browser", "session", "handoff", "show", "--handoff-id", loginRequest.HandoffID, "--json")
	handoff := decodeBrowserSessionHandoffEnvelope(t, []byte(handoffOutput))
	if handoff.HandoffID != loginRequest.HandoffID || handoff.LoginRequestID != loginRequest.ID || handoff.SessionID != created.ID {
		t.Fatalf("handoff = %+v, want linked handoff/request/session", handoff)
	}
	if handoff.SessionName != "google-main" || handoff.Domain != "google.com" || handoff.AccountHint != "marcus" || handoff.Status != "requested" || handoff.ExpiresAt == "" {
		t.Fatalf("handoff = %+v, want safe session metadata", handoff)
	}
	if handoff.AllowedActions != "manual_login_only" {
		t.Fatalf("AllowedActions = %q, want manual_login_only", handoff.AllowedActions)
	}

	logsAfter := run("logs", "--json")
	if strings.Count(logsAfter, `"type": "browser.session_`) != strings.Count(logsBefore, `"type": "browser.session_`) {
		t.Fatalf("handoff lookup changed browser session audit events before=%s after=%s", logsBefore, logsAfter)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after handoff lookup err=%v, want read-only metadata lookup", err)
	}

	verified := run("browser", "session", "verify", "--id", int64String(created.ID), "--login-request-id", int64String(loginRequest.ID), "--json")
	if !strings.Contains(verified, `"status": "verified"`) {
		t.Fatalf("verify output = %s, want verified session", verified)
	}
	var completedOutput bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "handoff", "show", "--handoff-id", loginRequest.HandoffID, "--json"}, strings.NewReader(""), &completedOutput)
	if err == nil || !strings.Contains(err.Error(), `status "completed"`) {
		t.Fatalf("Run(handoff show completed) error = %v output=%s, want completed rejection", err, completedOutput.String())
	}

	revoked := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "github-main",
		"--domain", "github.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	revokedRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(revoked.ID), "--json")))
	run("browser", "session", "revoke", "--id", int64String(revoked.ID), "--json")
	var revokedOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"browser", "session", "handoff", "show", "--handoff-id", revokedRequest.HandoffID, "--json"}, strings.NewReader(""), &revokedOutput)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Run(handoff show revoked session) error = %v output=%s, want revoked rejection", err, revokedOutput.String())
	}
}

func TestRunBrowserSessionLoginRequestRejectsInvalidBaseURLAndRevokedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "login-request", "--id", int64String(created.ID), "--handoff-base-url", "ssh://odin-handoff.tailnet.local/manual-login", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "handoff base URL must use http or https") {
		t.Fatalf("Run(login-request invalid base URL) error = %v output=%s, want http/https rejection", err, output.String())
	}

	run("browser", "session", "revoke", "--id", int64String(created.ID), "--json")
	output.Reset()
	err = Run(context.Background(), root, []string{"browser", "session", "login-request", "--id", int64String(created.ID), "--handoff-base-url", "https://odin-handoff.tailnet.local/manual-login", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "revoked browser session cannot create login request") {
		t.Fatalf("Run(login-request revoked) error = %v output=%s, want revoked rejection", err, output.String())
	}
}

func TestRunBrowserSessionVerifyCompletesLoginRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	loginRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))

	verifiedOutput := run("browser", "session", "verify", "--id", int64String(created.ID), "--login-request-id", int64String(loginRequest.ID), "--json")
	verified := decodeBrowserSessionEnvelope(t, []byte(verifiedOutput))
	if verified.Status != "verified" || verified.LastVerifiedAt == "" {
		t.Fatalf("verified session = %+v, want verified with last_verified_at", verified)
	}

	loginRequestsOutput := run("browser", "session", "login-requests", "--id", int64String(created.ID), "--json")
	if !strings.Contains(loginRequestsOutput, `"status": "completed"`) || !strings.Contains(loginRequestsOutput, `"completed_at":`) {
		t.Fatalf("login-requests output = %s, want completed request metadata", loginRequestsOutput)
	}

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.session_status_changed"`, `"type": "browser.session_verified"`, `"type": "browser.session_login_completed"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionVerifyRejectsRevokedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	run("browser", "session", "revoke", "--id", int64String(created.ID), "--json")

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "verify", "--id", int64String(created.ID), "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Run(browser session verify revoked) error = %v output=%s, want revoked rejection", err, output.String())
	}

	output.Reset()
	err = Run(context.Background(), root, []string{"browser", "session", "status", "--id", int64String(created.ID), "--status", "verified", "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Run(browser session status verified after revoke) error = %v output=%s, want revoked rejection", err, output.String())
	}
}

func TestRunBrowserSessionPrepareProfileCreatesEmptyDirectoryAndAudits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	profileDir := filepath.Join(root, filepath.FromSlash(created.ProfilePath))
	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Fatalf("profile directory exists before prepare err=%v, want absent", err)
	}

	first := decodeBrowserSessionPrepareProfileEnvelope(t, []byte(run("browser", "session", "prepare-profile", "--id", int64String(created.ID), "--json")))
	if first.ProfilePath != created.ProfilePath || !first.ProfilePathExists || !first.Created {
		t.Fatalf("first prepare-profile = %+v, want created empty profile directory", first)
	}
	info, err := os.Stat(profileDir)
	if err != nil {
		t.Fatalf("Stat(profileDir) error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("profile path mode = %v, want directory", info.Mode())
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("profile directory permissions = %v, want no group/other permissions", info.Mode().Perm())
	}
	entries, err := os.ReadDir(profileDir)
	if err != nil {
		t.Fatalf("ReadDir(profileDir) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("profile directory entries = %v, want empty directory only", entries)
	}

	second := decodeBrowserSessionPrepareProfileEnvelope(t, []byte(run("browser", "session", "prepare-profile", "--id", int64String(created.ID), "--json")))
	if second.ProfilePath != created.ProfilePath || !second.ProfilePathExists || second.Created {
		t.Fatalf("second prepare-profile = %+v, want idempotent existing directory", second)
	}
	entries, err = os.ReadDir(profileDir)
	if err != nil {
		t.Fatalf("ReadDir(profileDir after second prepare) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("profile directory entries after second prepare = %v, want empty directory only", entries)
	}

	shown := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "show", "--id", int64String(created.ID), "--json")))
	if !shown.ProfilePathExists {
		t.Fatalf("shown.ProfilePathExists = false, want true after prepare")
	}
	if shown.ProfileStoragePolicy != "encrypted_required" {
		t.Fatalf("shown.ProfileStoragePolicy = %q, want prepare-profile to leave writes denied", shown.ProfileStoragePolicy)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.session_profile_prepared"`) {
		t.Fatalf("logs output = %s, want browser.session_profile_prepared audit event", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden credential/profile byte token %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionPrepareProfileRejectsRevokedAndUnsafeMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	run("browser", "session", "revoke", "--id", int64String(created.ID), "--json")
	var output bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "session", "prepare-profile", "--id", int64String(created.ID), "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("Run(browser session prepare-profile revoked) error = %v output=%s, want revoked rejection", err, output.String())
	}

	unsafe := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "unsafe",
		"--domain", "unsafe.example",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("Open store error = %v", err)
	}
	defer store.Close()
	if _, err := store.DB().ExecContext(context.Background(), `UPDATE browser_session_profiles SET profile_path = ? WHERE id = ?`, "../escape", unsafe.ID); err != nil {
		t.Fatalf("update unsafe profile path error = %v", err)
	}

	output.Reset()
	err = Run(context.Background(), root, []string{"browser", "session", "prepare-profile", "--id", int64String(unsafe.ID), "--json"}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "stay under ODIN_ROOT") {
		t.Fatalf("Run(browser session prepare-profile unsafe path) error = %v output=%s, want unsafe path rejection", err, output.String())
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe escape path stat error = %v, want no directory outside runtime root", err)
	}
}

func TestRunBrowserSessionProfileRetentionCleanupDryRunAndApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "retention-cli",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))

	store := openLifecycleBrowserStore(t, root)
	active := createLifecycleBrowserArtifact(t, context.Background(), store, root, created, "active-cli.enc")
	revoked := createLifecycleBrowserArtifact(t, context.Background(), store, root, created, "revoked-cli.enc")
	expired := createLifecycleBrowserArtifact(t, context.Background(), store, root, created, "expired-cli.enc")
	if _, err := store.MarkBrowserEncryptedProfileArtifactRevoked(context.Background(), sqlite.MarkBrowserEncryptedProfileArtifactRevokedParams{
		ID:     revoked.ID,
		Actor:  "test",
		Reason: "retention cli test revoke",
	}); err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactRevoked() error = %v", err)
	}
	if _, err := store.MarkBrowserEncryptedProfileArtifactExpired(context.Background(), sqlite.MarkBrowserEncryptedProfileArtifactExpiredParams{
		ID:     expired.ID,
		Actor:  "test",
		Reason: "retention cli test expire",
	}); err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactExpired() error = %v", err)
	}
	protectedPaths := []string{
		"browser-sessions/encrypted-profiles/not-an-artifact.txt",
	}
	for _, rel := range protectedPaths {
		writeLifecycleRetentionMarker(t, filepath.Join(root, filepath.FromSlash(rel)))
	}
	closeLifecycleBrowserStore(t, store)

	dryRun := decodeBrowserSessionProfileRetentionEnvelope(t, []byte(run("browser", "session", "profile", "retention", "cleanup", "--json")))
	if !dryRun.DryRun || dryRun.Apply || dryRun.Eligible != 2 || dryRun.Cleaned != 0 || dryRun.Failed != 0 || dryRun.Skipped != 1 {
		t.Fatalf("dry-run retention = %+v, want two eligible, one skipped, no mutation", dryRun)
	}
	if len(dryRun.Artifacts) != 2 {
		t.Fatalf("dry-run artifacts = %d, want 2", len(dryRun.Artifacts))
	}
	for _, item := range dryRun.Artifacts {
		if item.Action != "would_clean" || item.Removed {
			t.Fatalf("dry-run item = %+v, want would_clean without removal", item)
		}
	}
	for _, artifact := range []sqlite.BrowserEncryptedProfileArtifact{active, revoked, expired} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(artifact.EncryptedArtifactPath))); err != nil {
			t.Fatalf("artifact %d missing after dry-run: %v", artifact.ID, err)
		}
	}

	afterDryRun := openLifecycleBrowserStore(t, root)
	assertLifecycleBrowserArtifactStatus(t, afterDryRun, active.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	assertLifecycleBrowserArtifactStatus(t, afterDryRun, revoked.ID, sqlite.BrowserEncryptedProfileArtifactStatusRevoked)
	assertLifecycleBrowserArtifactStatus(t, afterDryRun, expired.ID, sqlite.BrowserEncryptedProfileArtifactStatusExpired)
	closeLifecycleBrowserStore(t, afterDryRun)

	applied := decodeBrowserSessionProfileRetentionEnvelope(t, []byte(run(
		"browser", "session", "profile", "retention", "cleanup",
		"--session-id", int64String(created.ID),
		"--apply",
		"--json",
	)))
	if applied.DryRun || !applied.Apply || applied.Eligible != 2 || applied.Cleaned != 2 || applied.Failed != 0 || applied.Skipped != 1 {
		t.Fatalf("apply retention = %+v, want two cleaned and one skipped", applied)
	}
	for _, item := range applied.Artifacts {
		if item.Action != "cleaned" || !item.Removed {
			t.Fatalf("apply item = %+v, want cleaned removal", item)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(revoked.EncryptedArtifactPath))); !os.IsNotExist(err) {
		t.Fatalf("revoked artifact still exists after apply: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(expired.EncryptedArtifactPath))); !os.IsNotExist(err) {
		t.Fatalf("expired artifact still exists after apply: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(active.EncryptedArtifactPath))); err != nil {
		t.Fatalf("active artifact missing after apply: %v", err)
	}
	for _, rel := range protectedPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("protected path %q was touched by retention cleanup: %v", rel, err)
		}
	}
	for _, rel := range []string{created.ProfilePath, "cookies", "credentials"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("path %q exists after retention cleanup err=%v, want no profile/cookie/credential writes", rel, err)
		}
	}

	afterApply := openLifecycleBrowserStore(t, root)
	assertLifecycleBrowserArtifactStatus(t, afterApply, active.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	assertLifecycleBrowserArtifactStatus(t, afterApply, revoked.ID, sqlite.BrowserEncryptedProfileArtifactStatusCleaned)
	assertLifecycleBrowserArtifactStatus(t, afterApply, expired.ID, sqlite.BrowserEncryptedProfileArtifactStatusCleaned)
	closeLifecycleBrowserStore(t, afterApply)

	logs := run("logs", "--json")
	if strings.Count(logs, `"type": "browser.profile_cleaned"`) != 2 {
		t.Fatalf("logs output = %s, want two browser.profile_cleaned audit events", logs)
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden marker %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionProfileArtifactCreateFixtureEncryptsAndRecordsMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	key := bytes.Repeat([]byte{0x52}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	plaintext := []byte("fixture plaintext sentinel for cli artifact")
	plaintextPath := filepath.Join(t.TempDir(), "fixture-profile.txt")
	if err := os.WriteFile(plaintextPath, plaintext, 0o600); err != nil {
		t.Fatalf("WriteFile(plaintext fixture) error = %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "fixture-cli",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	output := run(
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "fixture-one",
		"--plaintext-file", plaintextPath,
		"--json",
	)
	if strings.Contains(output, string(plaintext)) || strings.Contains(output, plaintextPath) {
		t.Fatalf("create-fixture output leaked plaintext/source path: %s", output)
	}
	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(output))
	if artifact.ID <= 0 || artifact.SessionID != created.ID || artifact.Status != "encrypted" {
		t.Fatalf("artifact = %+v, want encrypted artifact linked to session", artifact)
	}
	if artifact.ArtifactPath != "browser-sessions/encrypted-profiles/fixture-one.enc" {
		t.Fatalf("artifact.ArtifactPath = %q, want safe encrypted artifact path", artifact.ArtifactPath)
	}
	if artifact.ProfilePath != created.ProfilePath {
		t.Fatalf("artifact.ProfilePath = %q, want session profile path %q", artifact.ProfilePath, created.ProfilePath)
	}
	if artifact.EncryptionKeyRef != "env:"+browserprofilekeys.EnvKeyB64 {
		t.Fatalf("artifact.EncryptionKeyRef = %q, want env key ref", artifact.EncryptionKeyRef)
	}
	artifactBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.ArtifactPath)))
	if err != nil {
		t.Fatalf("ReadFile(encrypted artifact) error = %v", err)
	}
	if bytes.Contains(artifactBytes, plaintext) {
		t.Fatalf("encrypted artifact contains fixture plaintext")
	}
	readPlaintext, err := browserprofileartifacts.Read(browserprofileartifacts.ReadParams{
		ODINRoot:     root,
		ArtifactPath: artifact.ArtifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Read(encrypted artifact) error = %v", err)
	}
	if !bytes.Equal(readPlaintext, plaintext) {
		t.Fatalf("decrypted artifact plaintext = %q, want fixture plaintext", readPlaintext)
	}

	store := openLifecycleBrowserStore(t, root)
	persisted := assertLifecycleBrowserArtifactStatus(t, store, artifact.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	closeLifecycleBrowserStore(t, store)
	if persisted.EncryptedArtifactPath != artifact.ArtifactPath || persisted.EncryptionKeyRef != artifact.EncryptionKeyRef {
		t.Fatalf("persisted artifact = %+v, want CLI metadata %+v", persisted, artifact)
	}

	retention := decodeBrowserSessionProfileRetentionEnvelope(t, []byte(run("browser", "session", "profile", "retention", "cleanup", "--json")))
	if retention.Eligible != 0 || retention.Skipped != 1 || retention.Cleaned != 0 {
		t.Fatalf("retention = %+v, want active encrypted artifact skipped by cleanup", retention)
	}
	for _, rel := range []string{created.ProfilePath, "cookies", "credentials"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("path %q exists after create-fixture err=%v, want no profile/cookie/credential writes", rel, err)
		}
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.profile_encrypted"`) {
		t.Fatalf("logs output = %s, want browser.profile_encrypted audit event", logs)
	}
	for _, forbidden := range []string{string(plaintext), plaintextPath, "password", "totp", "backup_code", "profile_bytes"} {
		if strings.Contains(strings.ToLower(logs), strings.ToLower(forbidden)) {
			t.Fatalf("logs output contains forbidden marker %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionProfileArtifactCreateFixtureFailsClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	plaintextPath := filepath.Join(t.TempDir(), "fixture-profile.txt")
	if err := os.WriteFile(plaintextPath, []byte("fixture plaintext"), 0o600); err != nil {
		t.Fatalf("WriteFile(plaintext fixture) error = %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "fixture-fail-closed",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "missing-key",
		"--plaintext-file", plaintextPath,
		"--json",
	}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), browserprofilekeys.EnvKeyB64) {
		t.Fatalf("Run(create-fixture missing key) error = %v output=%s, want missing key rejection", err, output.String())
	}
	if _, statErr := os.Stat(filepath.Join(root, "browser-sessions", "encrypted-profiles", "missing-key.enc")); !os.IsNotExist(statErr) {
		t.Fatalf("artifact exists after missing-key rejection: err=%v", statErr)
	}

	key := bytes.Repeat([]byte{0x61}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	output.Reset()
	err = Run(context.Background(), root, []string{
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "cookies",
		"--plaintext-file", plaintextPath,
		"--json",
	}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		t.Fatalf("Run(create-fixture credential-looking name) error = %v output=%s, want forbidden name rejection", err, output.String())
	}

	output.Reset()
	err = Run(context.Background(), root, []string{
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "missing-file",
		"--plaintext-file", filepath.Join(t.TempDir(), "missing.txt"),
		"--json",
	}, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "read plaintext fixture") {
		t.Fatalf("Run(create-fixture missing file) error = %v output=%s, want missing file rejection", err, output.String())
	}
}

func TestRunBrowserSessionProfileArtifactCreateDirectoryAndMaterializeDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	key := bytes.Repeat([]byte{0x62}, browserprofilecrypto.KeySize)
	encodedKey := base64.StdEncoding.EncodeToString(key)
	t.Setenv(browserprofilekeys.EnvKeyB64, encodedKey)

	sourceDir := filepath.Join(t.TempDir(), "chrome-profile")
	if err := os.MkdirAll(filepath.Join(sourceDir, "Default"), 0o700); err != nil {
		t.Fatalf("MkdirAll(source profile) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Default", "Preferences"), []byte("saved-profile-state"), 0o600); err != nil {
		t.Fatalf("WriteFile(source profile) error = %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "directory-artifact",
		"--domain", "x.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	createOutput := run(
		"browser", "session", "profile", "artifact", "create-directory",
		"--session-id", int64String(created.ID),
		"--name", "x-managed-profile",
		"--source-dir", sourceDir,
		"--json",
	)
	for _, forbidden := range []string{"saved-profile-state", sourceDir, encodedKey} {
		if strings.Contains(createOutput, forbidden) {
			t.Fatalf("create-directory output leaked forbidden marker %q: %s", forbidden, createOutput)
		}
	}
	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(createOutput))
	targetDir := "runtime/browser-profile-materializations/directory-cli-proof"
	materializeOutput := run(
		"browser", "session", "profile", "artifact", "materialize-directory",
		"--id", int64String(artifact.ID),
		"--target-dir", targetDir,
		"--json",
	)
	if strings.Contains(materializeOutput, "saved-profile-state") || strings.Contains(materializeOutput, sourceDir) {
		t.Fatalf("materialize-directory output leaked source/profile state: %s", materializeOutput)
	}
	materialization := decodeBrowserSessionProfileMaterializationEnvelope(t, []byte(materializeOutput))
	if materialization.MaterializationPath != targetDir || materialization.ReadOnly {
		t.Fatalf("materialization = %+v, want writable directory materialization", materialization)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(targetDir), "Default", "Preferences"))
	if err != nil {
		t.Fatalf("ReadFile(materialized preferences) error = %v", err)
	}
	if string(got) != "saved-profile-state" {
		t.Fatalf("materialized preferences = %q", got)
	}
}

func TestRunBrowserSessionProfileArtifactMaterializeAndCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	key := bytes.Repeat([]byte{0x6d}, browserprofilecrypto.KeySize)
	encodedKey := base64.StdEncoding.EncodeToString(key)
	t.Setenv(browserprofilekeys.EnvKeyB64, encodedKey)
	secretText := "fixture plaintext sentinel for materialization"
	plaintextPath := filepath.Join(t.TempDir(), "materialize-profile.txt")
	if err := os.WriteFile(plaintextPath, []byte(secretText), 0o600); err != nil {
		t.Fatalf("WriteFile(plaintext fixture) error = %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "materialize-cli",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "materialize-flow",
		"--plaintext-file", plaintextPath,
		"--json",
	)))
	materializationDir := "runtime/browser-profile-materializations/cli-proof"
	materializeOutput := run(
		"browser", "session", "profile", "artifact", "materialize",
		"--id", int64String(artifact.ID),
		"--target-dir", materializationDir,
		"--json",
	)
	for _, forbidden := range []string{secretText, plaintextPath, encodedKey} {
		if strings.Contains(materializeOutput, forbidden) {
			t.Fatalf("materialize output leaked forbidden marker %q: %s", forbidden, materializeOutput)
		}
	}
	materialization := decodeBrowserSessionProfileMaterializationEnvelope(t, []byte(materializeOutput))
	if materialization.ArtifactID != artifact.ID || materialization.SessionID != created.ID || materialization.MaterializationPath != materializationDir || materialization.MaterializedFilePath == "" {
		t.Fatalf("materialization = %+v, want artifact materialized under requested temp path", materialization)
	}
	materializedAbs := filepath.Join(root, filepath.FromSlash(materialization.MaterializedFilePath))
	materializedBytes, err := os.ReadFile(materializedAbs)
	if err != nil {
		t.Fatalf("ReadFile(materialized) error = %v", err)
	}
	if string(materializedBytes) != secretText {
		t.Fatalf("materialized bytes = %q, want fixture text", materializedBytes)
	}
	artifactAbs := filepath.Join(root, filepath.FromSlash(artifact.ArtifactPath))
	if _, err := os.Stat(artifactAbs); err != nil {
		t.Fatalf("encrypted artifact missing after materialize: %v", err)
	}
	for _, rel := range []string{created.ProfilePath, "cookies", "cookie", "credentials", "profile-bytes"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("forbidden path %q exists after materialize err=%v", rel, err)
		}
	}

	cleanupOutput := run(
		"browser", "session", "profile", "artifact", "cleanup-materialization",
		"--id", int64String(artifact.ID),
		"--target-dir", materializationDir,
		"--json",
	)
	cleanup := decodeBrowserSessionProfileMaterializationEnvelope(t, []byte(cleanupOutput))
	if cleanup.ArtifactID != artifact.ID || cleanup.MaterializationPath != materializationDir || !cleanup.Removed {
		t.Fatalf("cleanup = %+v, want materialization removed", cleanup)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(materializationDir))); !os.IsNotExist(err) {
		t.Fatalf("materialization dir exists after cleanup err=%v", err)
	}
	if _, err := os.Stat(artifactAbs); err != nil {
		t.Fatalf("encrypted artifact missing after cleanup: %v", err)
	}
	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.profile_materialized"`, `"type": "browser.profile_materialization_cleaned"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range []string{secretText, plaintextPath, encodedKey} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("logs output leaked forbidden marker %q: %s", forbidden, logs)
		}
	}
}

func TestRunBrowserSessionProfileArtifactStatusRevokeAndRetentionFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	key := bytes.Repeat([]byte{0x73}, browserprofilecrypto.KeySize)
	encodedKey := base64.StdEncoding.EncodeToString(key)
	t.Setenv(browserprofilekeys.EnvKeyB64, encodedKey)
	secretText := "fixture plaintext sentinel for artifact revoke flow"
	activeText := "fixture plaintext sentinel for active artifact"
	plaintextPath := filepath.Join(t.TempDir(), "revoked-profile.txt")
	activePlaintextPath := filepath.Join(t.TempDir(), "active-profile.txt")
	if err := os.WriteFile(plaintextPath, []byte(secretText), 0o600); err != nil {
		t.Fatalf("WriteFile(revoked plaintext) error = %v", err)
	}
	if err := os.WriteFile(activePlaintextPath, []byte(activeText), 0o600); err != nil {
		t.Fatalf("WriteFile(active plaintext) error = %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeBrowserSessionEnvelope(t, []byte(run(
		"browser", "session", "create",
		"--name", "artifact-status-cli",
		"--domain", "example.com",
		"--permission-tier", "authenticated_read",
		"--json",
	)))
	revokedArtifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "revoke-flow",
		"--plaintext-file", plaintextPath,
		"--json",
	)))
	activeArtifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "active-flow",
		"--plaintext-file", activePlaintextPath,
		"--json",
	)))

	listOutput := run(
		"browser", "session", "profile", "artifact", "list",
		"--session-id", int64String(created.ID),
		"--json",
	)
	forbiddenJSONMarkers := []string{secretText, activeText, plaintextPath, activePlaintextPath, encodedKey}
	for _, forbidden := range forbiddenJSONMarkers {
		if strings.Contains(listOutput, forbidden) {
			t.Fatalf("artifact list output leaked forbidden marker %q: %s", forbidden, listOutput)
		}
	}
	list := decodeBrowserSessionProfileArtifactListEnvelope(t, []byte(listOutput))
	if len(list) != 2 || list[0].ID != revokedArtifact.ID || list[1].ID != activeArtifact.ID {
		t.Fatalf("artifact list = %+v, want two persisted artifacts in id order", list)
	}

	showOutput := run(
		"browser", "session", "profile", "artifact", "show",
		"--id", int64String(revokedArtifact.ID),
		"--json",
	)
	for _, forbidden := range forbiddenJSONMarkers {
		if strings.Contains(showOutput, forbidden) {
			t.Fatalf("artifact show output leaked forbidden marker %q: %s", forbidden, showOutput)
		}
	}
	show := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(showOutput))
	if show.ID != revokedArtifact.ID || show.SessionID != created.ID || show.Status != "encrypted" || show.ArtifactPath == "" || show.EncryptionKeyRef != "env:"+browserprofilekeys.EnvKeyB64 {
		t.Fatalf("show artifact = %+v, want metadata-only encrypted artifact", show)
	}

	revoke := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "revoke",
		"--id", int64String(revokedArtifact.ID),
		"--json",
	)))
	if revoke.ID != revokedArtifact.ID || revoke.Status != "revoked" || revoke.RevokedAt == "" {
		t.Fatalf("revoke artifact = %+v, want revoked artifact with timestamp", revoke)
	}
	revokedArtifactAbsPath := filepath.Join(root, filepath.FromSlash(revokedArtifact.ArtifactPath))
	activeArtifactAbsPath := filepath.Join(root, filepath.FromSlash(activeArtifact.ArtifactPath))
	if _, err := os.Stat(revokedArtifactAbsPath); err != nil {
		t.Fatalf("revoked artifact file missing after revoke: %v", err)
	}
	if _, err := os.Stat(activeArtifactAbsPath); err != nil {
		t.Fatalf("active artifact file missing after revoke: %v", err)
	}

	dryRun := decodeBrowserSessionProfileRetentionEnvelope(t, []byte(run(
		"browser", "session", "profile", "retention", "cleanup",
		"--session-id", int64String(created.ID),
		"--json",
	)))
	if !dryRun.DryRun || dryRun.Apply || dryRun.Eligible != 1 || dryRun.Cleaned != 0 || dryRun.Failed != 0 || dryRun.Skipped != 1 {
		t.Fatalf("dry-run retention = %+v, want one revoked eligible and one active skipped", dryRun)
	}
	if len(dryRun.Artifacts) != 1 || dryRun.Artifacts[0].ArtifactID != revokedArtifact.ID || dryRun.Artifacts[0].Action != "would_clean" || dryRun.Artifacts[0].Removed {
		t.Fatalf("dry-run retention artifacts = %+v, want revoked artifact would_clean", dryRun.Artifacts)
	}
	if _, err := os.Stat(revokedArtifactAbsPath); err != nil {
		t.Fatalf("revoked artifact file missing after dry-run: %v", err)
	}

	applied := decodeBrowserSessionProfileRetentionEnvelope(t, []byte(run(
		"browser", "session", "profile", "retention", "cleanup",
		"--session-id", int64String(created.ID),
		"--apply",
		"--json",
	)))
	if applied.DryRun || !applied.Apply || applied.Eligible != 1 || applied.Cleaned != 1 || applied.Failed != 0 || applied.Skipped != 1 {
		t.Fatalf("apply retention = %+v, want one revoked cleaned and one active skipped", applied)
	}
	if _, err := os.Stat(revokedArtifactAbsPath); !os.IsNotExist(err) {
		t.Fatalf("revoked artifact file exists after apply: err=%v", err)
	}
	if _, err := os.Stat(activeArtifactAbsPath); err != nil {
		t.Fatalf("active artifact missing after apply: %v", err)
	}

	store := openLifecycleBrowserStore(t, root)
	assertLifecycleBrowserArtifactStatus(t, store, revokedArtifact.ID, sqlite.BrowserEncryptedProfileArtifactStatusCleaned)
	assertLifecycleBrowserArtifactStatus(t, store, activeArtifact.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	closeLifecycleBrowserStore(t, store)

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.profile_encrypted"`, `"type": "browser.profile_revoked"`, `"type": "browser.profile_cleaned"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want audit event %s", logs, want)
		}
	}
	for _, forbidden := range forbiddenJSONMarkers {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("logs output leaked forbidden marker %q: %s", forbidden, logs)
		}
	}
	for _, rel := range []string{created.ProfilePath, "cookies", "credentials"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("path %q exists after artifact status flow err=%v, want no profile/cookie/credential writes", rel, err)
		}
	}
}

type browserSessionJSON struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Domain               string `json:"domain"`
	AccountHint          string `json:"account_hint"`
	PermissionTier       string `json:"permission_tier"`
	Status               string `json:"status"`
	ProfileStoragePolicy string `json:"profile_storage_policy"`
	ProfilePath          string `json:"profile_path"`
	ProfilePathExists    bool   `json:"profile_path_exists"`
	LastVerifiedAt       string `json:"last_verified_at,omitempty"`
	RevokedAt            string `json:"revoked_at,omitempty"`
}

type browserSessionPrepareProfileJSON struct {
	SessionID         int64  `json:"session_id"`
	ProfilePath       string `json:"profile_path"`
	ProfilePathExists bool   `json:"profile_path_exists"`
	Created           bool   `json:"created"`
}

type browserSessionProfileRetentionJSON struct {
	DryRun    bool                                         `json:"dry_run"`
	Apply     bool                                         `json:"apply"`
	Eligible  int                                          `json:"eligible"`
	Cleaned   int                                          `json:"cleaned"`
	Failed    int                                          `json:"failed"`
	Skipped   int                                          `json:"skipped"`
	Artifacts []browserSessionProfileRetentionArtifactJSON `json:"artifacts"`
}

type browserSessionProfileRetentionArtifactJSON struct {
	ArtifactID   int64  `json:"artifact_id"`
	SessionID    int64  `json:"session_id"`
	Status       string `json:"status"`
	ArtifactPath string `json:"artifact_path"`
	Action       string `json:"action"`
	Removed      bool   `json:"removed"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type browserSessionProfileArtifactJSON struct {
	ID               int64  `json:"id"`
	SessionID        int64  `json:"session_id"`
	Status           string `json:"status"`
	ProfilePath      string `json:"profile_path"`
	ArtifactPath     string `json:"artifact_path"`
	EncryptionKeyRef string `json:"encryption_key_ref"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	RevokedAt        string `json:"revoked_at,omitempty"`
	CleanedAt        string `json:"cleaned_at,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

type browserSessionProfileMaterializationJSON struct {
	ArtifactID           int64  `json:"artifact_id"`
	SessionID            int64  `json:"session_id"`
	ArtifactPath         string `json:"artifact_path"`
	MaterializationPath  string `json:"materialization_path"`
	MaterializedFilePath string `json:"materialized_file_path,omitempty"`
	ReadOnly             bool   `json:"read_only,omitempty"`
	Removed              bool   `json:"removed,omitempty"`
}

type browserSessionLoginRequestJSON struct {
	ID          int64   `json:"id"`
	SessionID   int64   `json:"session_id"`
	Status      string  `json:"status"`
	HandoffID   string  `json:"handoff_id"`
	HandoffURL  *string `json:"handoff_url"`
	ExpiresAt   string  `json:"expires_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type browserSessionHandoffJSON struct {
	HandoffID      string `json:"handoff_id"`
	LoginRequestID int64  `json:"login_request_id"`
	SessionID      int64  `json:"session_id"`
	SessionName    string `json:"session_name"`
	Domain         string `json:"domain"`
	AccountHint    string `json:"account_hint"`
	ExpiresAt      string `json:"expires_at"`
	Status         string `json:"status"`
	AllowedActions string `json:"allowed_actions"`
}

type browserSessionRunnerJSON struct {
	ID                  int64   `json:"id"`
	SessionID           int64   `json:"session_id"`
	LoginRequestID      int64   `json:"login_request_id"`
	HandoffID           string  `json:"handoff_id"`
	Status              string  `json:"status"`
	RealBrowserEvidence bool    `json:"real_browser_evidence"`
	ViewerURL           *string `json:"viewer_url"`
	RunnerID            *string `json:"runner_id"`
	ProcessID           *int64  `json:"process_id"`
	BindAddr            *string `json:"bind_addr"`
	PrivateBaseURL      *string `json:"private_base_url"`
	PublicBaseURL       *string `json:"public_base_url"`
	ExpiresAt           string  `json:"expires_at"`
	StartedAt           string  `json:"started_at,omitempty"`
	ExitedAt            string  `json:"exited_at,omitempty"`
	CompletedAt         string  `json:"completed_at,omitempty"`
	CancelledAt         string  `json:"cancelled_at,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
	ErrorCode           *string `json:"error_code"`
	ErrorMessage        *string `json:"error_message"`
}

type browserSessionRunnerPlanJSON struct {
	ID             int64                                 `json:"id"`
	SessionID      int64                                 `json:"session_id"`
	LoginRequestID int64                                 `json:"login_request_id"`
	HandoffID      string                                `json:"handoff_id"`
	Commands       []browserSessionRunnerPlanCommandJSON `json:"commands"`
	BindAddr       string                                `json:"bind_addr"`
	PrivateBaseURL string                                `json:"private_base_url"`
	ViewerURL      string                                `json:"viewer_url"`
	TimeoutSeconds int                                   `json:"timeout_seconds"`
}

type browserSessionRunnerPlanCommandJSON struct {
	Role string   `json:"role"`
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type browserSessionProofJSON struct {
	SessionID      int64                          `json:"session_id"`
	Status         string                         `json:"status"`
	URL            string                         `json:"url"`
	ExpectedTitle  string                         `json:"expected_title"`
	ObservedTitle  string                         `json:"observed_title"`
	TitleSource    string                         `json:"title_source"`
	ArtifactID     int64                          `json:"artifact_id"`
	RunnerID       int64                          `json:"runner_id"`
	LoginRequestID int64                          `json:"login_request_id"`
	HandoffID      string                         `json:"handoff_id"`
	ViewerURL      string                         `json:"viewer_url"`
	BindAddr       string                         `json:"bind_addr"`
	PrivateBaseURL string                         `json:"private_base_url"`
	Checks         []browserSessionProofCheckJSON `json:"checks"`
}

type browserSessionXBioApplyJSON struct {
	Status              string `json:"status"`
	TaskID              int64  `json:"task_id"`
	RunID               int64  `json:"run_id"`
	RunArtifactID       int64  `json:"run_artifact_id"`
	ApprovalID          int64  `json:"approval_id"`
	SessionID           int64  `json:"session_id"`
	ArtifactID          int64  `json:"artifact_id"`
	ProfilePath         string `json:"profile_path"`
	MaterializationPath string `json:"materialization_path"`
	Summary             string `json:"summary"`
	ErrorCode           string `json:"error_code,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

type browserSessionProofCheckJSON struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

func decodeBrowserSessionPrepareProfileEnvelope(t *testing.T, payload []byte) browserSessionPrepareProfileJSON {
	t.Helper()
	var envelope struct {
		Profile browserSessionPrepareProfileJSON `json:"profile"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session prepare profile json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Profile
}

func decodeBrowserSessionProfileRetentionEnvelope(t *testing.T, payload []byte) browserSessionProfileRetentionJSON {
	t.Helper()
	var envelope struct {
		Retention browserSessionProfileRetentionJSON `json:"retention"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session profile retention json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Retention
}

func decodeBrowserSessionProfileArtifactEnvelope(t *testing.T, payload []byte) browserSessionProfileArtifactJSON {
	t.Helper()
	var envelope struct {
		Artifact browserSessionProfileArtifactJSON `json:"artifact"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session profile artifact json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Artifact
}

func decodeBrowserSessionProfileArtifactListEnvelope(t *testing.T, payload []byte) []browserSessionProfileArtifactJSON {
	t.Helper()
	var envelope struct {
		Artifacts []browserSessionProfileArtifactJSON `json:"artifacts"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session profile artifact list json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Artifacts
}

func decodeBrowserSessionProfileMaterializationEnvelope(t *testing.T, payload []byte) browserSessionProfileMaterializationJSON {
	t.Helper()
	var envelope struct {
		Materialization browserSessionProfileMaterializationJSON `json:"materialization"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session profile materialization json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Materialization
}

func decodeBrowserSessionEnvelope(t *testing.T, payload []byte) browserSessionJSON {
	t.Helper()
	var envelope struct {
		Session browserSessionJSON `json:"session"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Session
}

func decodeBrowserSessionLoginRequestEnvelope(t *testing.T, payload []byte) browserSessionLoginRequestJSON {
	t.Helper()
	var envelope struct {
		LoginRequest browserSessionLoginRequestJSON `json:"login_request"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session login request json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.LoginRequest
}

func decodeBrowserSessionHandoffEnvelope(t *testing.T, payload []byte) browserSessionHandoffJSON {
	t.Helper()
	var envelope struct {
		Handoff browserSessionHandoffJSON `json:"handoff"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session handoff json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Handoff
}

func decodeBrowserSessionRunnerEnvelope(t *testing.T, payload []byte) browserSessionRunnerJSON {
	t.Helper()
	var envelope struct {
		Runner browserSessionRunnerJSON `json:"runner"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session runner json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Runner
}

func decodeBrowserSessionRunnerPlanEnvelope(t *testing.T, payload []byte) browserSessionRunnerPlanJSON {
	t.Helper()
	var envelope struct {
		Plan browserSessionRunnerPlanJSON `json:"plan"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session runner plan json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Plan
}

func decodeBrowserSessionProofEnvelope(t *testing.T, payload []byte) browserSessionProofJSON {
	t.Helper()
	var envelope struct {
		Proof browserSessionProofJSON `json:"proof"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session proof json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Proof
}

func decodeBrowserSessionXBioApplyEnvelope(t *testing.T, payload []byte) browserSessionXBioApplyJSON {
	t.Helper()
	var envelope struct {
		Result browserSessionXBioApplyJSON `json:"result"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("browser session X bio apply json decode error = %v; output=%s", err, string(payload))
	}
	return envelope.Result
}

func browserSessionProofHasPassedCheck(proof browserSessionProofJSON, name string) bool {
	for _, check := range proof.Checks {
		if check.Name == name && check.Status == "passed" {
			return true
		}
	}
	return false
}

func novncPlanArgs(runnerID int64, commandPath string, bindAddr string) []string {
	return []string{
		"browser", "session", "runner", "plan-novnc",
		"--id", int64String(runnerID),
		"--browser-command", commandPath,
		"--browser-allowed-command", commandPath,
		"--display-command", commandPath,
		"--display-allowed-command", commandPath,
		"--novnc-command", commandPath,
		"--novnc-allowed-command", commandPath,
		"--bind-addr", bindAddr,
		"--private-base-url", "https://odin-handoff.tailnet.local",
		"--timeout-seconds", "300",
		"--json",
	}
}

func testLifecycleExecutablePath(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range []string{"/usr/bin", "/bin"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	t.Fatalf("required fixture executable %q not found", name)
	return ""
}

func assertNoBrowserSessionArtifacts(t *testing.T, root string) {
	t.Helper()
	for _, relativePath := range []string{
		"browser-sessions",
		"cookies",
		"cookie",
		"credentials",
		"profile-bytes",
	} {
		if _, err := os.Stat(filepath.Join(root, relativePath)); !os.IsNotExist(err) {
			t.Fatalf("%s exists after NoVNC fixture launch err=%v, want absent", relativePath, err)
		}
	}
}

func openLifecycleBrowserStore(t *testing.T, root string) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("Open store error = %v", err)
	}
	return store
}

func closeLifecycleBrowserStore(t *testing.T, store *sqlite.Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("Close store error = %v", err)
	}
}

func createLifecycleBrowserArtifact(t *testing.T, ctx context.Context, store *sqlite.Store, root string, session browserSessionJSON, name string) sqlite.BrowserEncryptedProfileArtifact {
	t.Helper()
	path := filepath.ToSlash(filepath.Join("browser-sessions", "encrypted-profiles", name))
	writeLifecycleRetentionMarker(t, filepath.Join(root, filepath.FromSlash(path)))
	artifact, err := store.CreateBrowserEncryptedProfileArtifact(ctx, sqlite.CreateBrowserEncryptedProfileArtifactParams{
		SessionID:             session.ID,
		ProfilePath:           session.ProfilePath,
		EncryptedArtifactPath: path,
		EncryptionKeyRef:      "test-key:v1",
	})
	if err != nil {
		t.Fatalf("CreateBrowserEncryptedProfileArtifact(%s) error = %v", name, err)
	}
	return artifact
}

func assertLifecycleBrowserArtifactStatus(t *testing.T, store *sqlite.Store, id int64, want sqlite.BrowserEncryptedProfileArtifactStatus) sqlite.BrowserEncryptedProfileArtifact {
	t.Helper()
	artifact, err := store.GetBrowserEncryptedProfileArtifact(context.Background(), id)
	if err != nil {
		t.Fatalf("GetBrowserEncryptedProfileArtifact(%d) error = %v", id, err)
	}
	if artifact.Status != want {
		t.Fatalf("artifact %d status = %q, want %q", id, artifact.Status, want)
	}
	return artifact
}

func writeLifecycleRetentionMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte("fixture marker"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func writeLifecycleExecutable(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

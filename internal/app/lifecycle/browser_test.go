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

func TestRunBrowserRunFromWorkSurfacesEvidenceInReviewAndOverview(t *testing.T) {
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

	run("work", "start", "--project", "odin-core", "--title", "Collect browser evidence for docs", "--intent", "read_only")
	browserRun := run("browser", "run", "--task-id", "1", "--url", "https://example.com/research", "--objective", "Collect public documentation", "--allowed-domain", "example.com", "--max-pages", "2", "--max-duration-seconds", "30", "--evidence-required", "--json")
	for _, want := range []string{
		`"status": "recorded"`,
		`"task_id": 1`,
		`"run_id": 1`,
		`"adapter_kind": "stub_local"`,
		`"screenshot_metadata":`,
		`"selected_links":`,
		`"form_state_summary":`,
		`"confidence": "medium"`,
	} {
		if !strings.Contains(browserRun, want) {
			t.Fatalf("browser run output = %s, want %s", browserRun, want)
		}
	}

	reviewList := run("review", "list", "--json")
	for _, want := range []string{`"queue_id": "browser-evidence:1"`, `"source_type": "browser_evidence"`, `"allowed_actions": [`} {
		if !strings.Contains(reviewList, want) {
			t.Fatalf("review list output = %s, want %s", reviewList, want)
		}
	}
	reviewShow := run("review", "show", "browser-evidence:1", "--json")
	for _, want := range []string{`"source_type": "browser_evidence"`, `"evidence": [`, `"adapter_status": "completed"`, `"selected_links":`} {
		if !strings.Contains(reviewShow, want) {
			t.Fatalf("review show output = %s, want %s", reviewShow, want)
		}
	}
	overview := run("overview", "--json")
	for _, want := range []string{`"blocked_reason": "browser_evidence_review"`, `"browser_evidence": [`, `"evidence_type": "browser_readonly"`} {
		if !strings.Contains(overview, want) {
			t.Fatalf("overview output = %s, want %s", overview, want)
		}
	}
}

func TestRunBrowserFailedCaptureCreatesRecoveryRecommendation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "huginn-fail.sh")
	if err := os.WriteFile(fixture, []byte("#!/usr/bin/env bash\ncat >/dev/null\nprintf '{\"status\":\"failed\",\"adapter_kind\":\"huginn_live\",\"error_code\":\"selector_missing\",\"error_message\":\"selector missing\",\"extracted_text_summary\":\"Huginn live browser adapter did not produce browsing evidence.\"}'\n"), 0o755); err != nil {
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

	run("work", "start", "--project", "odin-core", "--title", "Capture failing browser evidence", "--intent", "read_only")
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
		t.Fatalf("overview output = %s, want browser recovery guidance", overview)
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

func TestRunBrowserSessionProfileArtifactMaterializeAndCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ODIN_BROWSER_PROFILE_KEY_B64", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
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
	plaintextPath := filepath.Join(t.TempDir(), "profile-state.json")
	if err := os.WriteFile(plaintextPath, []byte(`{"state":"safe fixture"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(plaintext fixture) error = %v", err)
	}

	artifact := decodeBrowserSessionProfileArtifactEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "create-fixture",
		"--session-id", int64String(created.ID),
		"--name", "google-main",
		"--plaintext-file", plaintextPath,
		"--json",
	)))
	if artifact.SessionID != created.ID || artifact.Status != "encrypted" || artifact.ArtifactPath != "browser-sessions/encrypted-profiles/google-main.enc" {
		t.Fatalf("artifact = %+v, want encrypted artifact metadata for created session", artifact)
	}
	encryptedBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.ArtifactPath)))
	if err != nil {
		t.Fatalf("ReadFile(encrypted artifact) error = %v", err)
	}
	if strings.Contains(string(encryptedBytes), "safe fixture") {
		t.Fatalf("encrypted artifact contains plaintext fixture bytes: %s", string(encryptedBytes))
	}

	targetDir := "runtime/browser-profile-materializations/google-main"
	materialization := decodeBrowserSessionProfileMaterializationEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "materialize",
		"--id", int64String(artifact.ID),
		"--target-dir", targetDir,
		"--json",
	)))
	if materialization.ArtifactID != artifact.ID || materialization.SessionID != created.ID || materialization.MaterializationPath != targetDir || materialization.MaterializedFilePath == "" || !materialization.ReadOnly {
		t.Fatalf("materialization = %+v, want read-only materialized profile artifact", materialization)
	}
	materializedBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(materialization.MaterializedFilePath)))
	if err != nil {
		t.Fatalf("ReadFile(materialized file) error = %v", err)
	}
	if string(materializedBytes) != `{"state":"safe fixture"}` {
		t.Fatalf("materialized bytes = %q, want original fixture plaintext", string(materializedBytes))
	}

	cleanup := decodeBrowserSessionProfileMaterializationEnvelope(t, []byte(run(
		"browser", "session", "profile", "artifact", "cleanup-materialization",
		"--id", int64String(artifact.ID),
		"--target-dir", targetDir,
		"--json",
	)))
	if cleanup.ArtifactID != artifact.ID || !cleanup.Removed {
		t.Fatalf("cleanup = %+v, want materialization removed", cleanup)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(targetDir))); !os.IsNotExist(err) {
		t.Fatalf("materialization dir stat error = %v, want removed", err)
	}

	logs := run("logs", "--json")
	for _, want := range []string{`"type": "browser.profile_encrypted"`, `"type": "browser.profile_materialized"`, `"type": "browser.profile_materialization_cleaned"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
	for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes", "safe fixture"} {
		if strings.Contains(strings.ToLower(logs), forbidden) {
			t.Fatalf("logs output contains forbidden token %q: %s", forbidden, logs)
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

type browserSessionProfileArtifactJSON struct {
	ID               int64  `json:"id"`
	SessionID        int64  `json:"session_id"`
	Status           string `json:"status"`
	ProfilePath      string `json:"profile_path"`
	ArtifactPath     string `json:"artifact_path"`
	EncryptionKeyRef string `json:"encryption_key_ref"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
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

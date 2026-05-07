package lifecycle

import (
	"bytes"
	"context"
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
	if completed.Status != "completed" || completed.CompletedAt == "" {
		t.Fatalf("completed runner = %+v, want completed with timestamp", completed)
	}

	cancelRequest := decodeBrowserSessionLoginRequestEnvelope(t, []byte(run("browser", "session", "login-request", "--id", int64String(created.ID), "--json")))
	cancelRunner := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "create", "--login-request-id", int64String(cancelRequest.ID), "--json")))
	cancelled := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "cancel", "--id", int64String(cancelRunner.ID), "--json")))
	if cancelled.Status != "cancelled" || cancelled.CancelledAt == "" {
		t.Fatalf("cancelled runner = %+v, want cancelled with timestamp", cancelled)
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

func TestRunBrowserSessionRunnerStartFixtureUpdatesMetadataSafely(t *testing.T) {
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

	started := decodeBrowserSessionRunnerEnvelope(t, []byte(run("browser", "session", "runner", "start", "--id", int64String(runner.ID), "--json")))
	if started.Status != "started" || started.StartedAt == "" {
		t.Fatalf("started runner = %+v, want started metadata", started)
	}
	if started.RunnerID == nil || *started.RunnerID == "" || started.ProcessID == nil || *started.ProcessID <= 0 {
		t.Fatalf("started runner = %+v, want fixture runner id and process id", started)
	}
	if started.ViewerURL != nil {
		t.Fatalf("started runner viewer_url = %v, want null fixture viewer URL", started.ViewerURL)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser-sessions directory exists after fixture runner start err=%v, want metadata-only fixture start", err)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "browser.handoff_runner_started"`) {
		t.Fatalf("logs output = %s, want started runner audit event", logs)
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

type browserSessionRunnerJSON struct {
	ID             int64   `json:"id"`
	SessionID      int64   `json:"session_id"`
	LoginRequestID int64   `json:"login_request_id"`
	HandoffID      string  `json:"handoff_id"`
	Status         string  `json:"status"`
	ViewerURL      *string `json:"viewer_url"`
	RunnerID       *string `json:"runner_id"`
	ProcessID      *int64  `json:"process_id"`
	BindAddr       *string `json:"bind_addr"`
	PrivateBaseURL *string `json:"private_base_url"`
	PublicBaseURL  *string `json:"public_base_url"`
	ExpiresAt      string  `json:"expires_at"`
	StartedAt      string  `json:"started_at,omitempty"`
	CompletedAt    string  `json:"completed_at,omitempty"`
	CancelledAt    string  `json:"cancelled_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	ErrorCode      *string `json:"error_code"`
	ErrorMessage   *string `json:"error_message"`
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

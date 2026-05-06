package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
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

	list := run("browser", "session", "list", "--json")
	for _, want := range []string{`"sessions":`, `"name": "google-main"`, `"domain": "google.com"`} {
		if !strings.Contains(list, want) {
			t.Fatalf("list output = %s, want %s", list, want)
		}
	}

	shown := decodeBrowserSessionEnvelope(t, []byte(run("browser", "session", "show", "--id", int64String(created.ID), "--json")))
	if shown.ID != created.ID || shown.Name != created.Name {
		t.Fatalf("shown session = %+v, want created session", shown)
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
	if loginRequest.ID != 1 || loginRequest.SessionID != created.ID || loginRequest.Status != "requested" || loginRequest.ExpiresAt == "" || loginRequest.HandoffURL != nil {
		t.Fatalf("login request = %+v, want requested metadata with nil handoff URL", loginRequest)
	}

	loginRequestsOutput := run("browser", "session", "login-requests", "--id", int64String(created.ID), "--json")
	if !strings.Contains(loginRequestsOutput, `"login_requests":`) || !strings.Contains(loginRequestsOutput, `"status": "requested"`) {
		t.Fatalf("login-requests output = %s, want requested login metadata list", loginRequestsOutput)
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

type browserSessionJSON struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	AccountHint    string `json:"account_hint"`
	PermissionTier string `json:"permission_tier"`
	Status         string `json:"status"`
	ProfilePath    string `json:"profile_path"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	RevokedAt      string `json:"revoked_at,omitempty"`
}

type browserSessionLoginRequestJSON struct {
	ID          int64   `json:"id"`
	SessionID   int64   `json:"session_id"`
	Status      string  `json:"status"`
	HandoffURL  *string `json:"handoff_url"`
	ExpiresAt   string  `json:"expires_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
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

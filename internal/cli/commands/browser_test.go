package commands

import (
	"strings"
	"testing"
)

func TestParseBrowserRunDefaultsAllowedDomainFromURL(t *testing.T) {
	command, err := ParseBrowser([]string{"run", "--goal-id", "42", "--url", "https://example.com/research", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser() error = %v", err)
	}
	if command.Name != "run" || command.GoalID != 42 || command.URL != "https://example.com/research" || command.AllowedDomains[0] != "example.com" || !command.JSON {
		t.Fatalf("command = %+v, want parsed browser run command with default allowed domain", command)
	}
}

func TestParseBrowserRunAcceptsTaskIDForWorkEvidence(t *testing.T) {
	command, err := ParseBrowser([]string{"run", "--task-id", "77", "--url", "https://example.com/research", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser() error = %v", err)
	}
	if command.TaskID != 77 || command.GoalID != 0 {
		t.Fatalf("TaskID/GoalID = %d/%d, want 77/0", command.TaskID, command.GoalID)
	}
}

func TestParseBrowserRunAcceptsSafetyOptions(t *testing.T) {
	command, err := ParseBrowser([]string{
		"run",
		"--goal-id", "42",
		"--url", "https://docs.example.com/research",
		"--objective", "Collect public docs",
		"--allowed-domain", "example.com",
		"--max-pages", "3",
		"--max-duration-seconds", "45",
		"--worker-mode", "browser",
		"--evidence-required",
		"--action", "read",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser() error = %v", err)
	}
	if command.Objective != "Collect public docs" || command.AllowedDomains[0] != "example.com" || command.MaxPages != 3 || command.MaxDurationSeconds != 45 || command.WorkerMode != "browser" || !command.EvidenceRequired || command.Actions[0] != "read" {
		t.Fatalf("command = %+v, want parsed safety options", command)
	}
}

func TestParseBrowserRunAcceptsBrowserSessionReference(t *testing.T) {
	command, err := ParseBrowser([]string{
		"run",
		"--goal-id", "42",
		"--url", "https://docs.example.com/research",
		"--allowed-domain", "example.com",
		"--session-id", "7",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser() error = %v", err)
	}
	if command.SessionID != 7 {
		t.Fatalf("command.SessionID = %d, want 7", command.SessionID)
	}
}

func TestParseBrowserSessionCreateAndShow(t *testing.T) {
	create, err := ParseBrowser([]string{
		"session",
		"create",
		"--name", "google-main",
		"--domain", "google.com",
		"--permission-tier", "authenticated_read",
		"--account-hint", "marcus",
		"--profile-path", "browser-sessions/profiles/google-main",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(session create) error = %v", err)
	}
	if create.Name != "session" || create.SessionAction != "create" || create.SessionName != "google-main" || create.SessionDomain != "google.com" || create.PermissionTier != "authenticated_read" || create.AccountHint != "marcus" || create.ProfilePath != "browser-sessions/profiles/google-main" || !create.JSON {
		t.Fatalf("create command = %+v, want parsed browser session create", create)
	}

	show, err := ParseBrowser([]string{"session", "show", "--id", "42", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session show) error = %v", err)
	}
	if show.Name != "session" || show.SessionAction != "show" || show.ID != 42 || !show.JSON {
		t.Fatalf("show command = %+v, want parsed browser session show", show)
	}
}

func TestParseBrowserSessionValidatesRequiredFields(t *testing.T) {
	if _, err := ParseBrowser([]string{"session", "create", "--domain", "google.com", "--permission-tier", "authenticated_read", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session create missing name) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "create", "--name", "google-main", "--permission-tier", "authenticated_read", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session create missing domain) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "create", "--name", "google-main", "--domain", "google.com", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session create missing permission tier) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "status", "--id", "42", "--status", "unknown", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session status unknown status) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "status", "--id", "42", "--status", "requires_attended_login", "--json"}); err != nil {
		t.Fatalf("ParseBrowser(session status requires_attended_login) error = %v", err)
	}
}

func TestParseBrowserSessionLoginRequestCommands(t *testing.T) {
	request, err := ParseBrowser([]string{"session", "login-request", "--id", "42", "--handoff-base-url", "https://odin-handoff.tailnet.local/manual-login", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session login-request) error = %v", err)
	}
	if request.Name != "session" || request.SessionAction != "login-request" || request.ID != 42 || request.HandoffBaseURL != "https://odin-handoff.tailnet.local/manual-login" || !request.JSON {
		t.Fatalf("login-request command = %+v, want parsed request command", request)
	}

	list, err := ParseBrowser([]string{"session", "login-requests", "--id", "42", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session login-requests) error = %v", err)
	}
	if list.Name != "session" || list.SessionAction != "login-requests" || list.ID != 42 || !list.JSON {
		t.Fatalf("login-requests command = %+v, want parsed list command", list)
	}
}

func TestParseBrowserSessionHandoffShowCommand(t *testing.T) {
	command, err := ParseBrowser([]string{"session", "handoff", "show", "--handoff-id", "opaque-handoff-id", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session handoff show) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "handoff" || command.HandoffAction != "show" || command.HandoffID != "opaque-handoff-id" || !command.JSON {
		t.Fatalf("command = %+v, want parsed handoff show command", command)
	}

	if _, err := ParseBrowser([]string{"session", "handoff", "show", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session handoff show missing handoff id) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "handoff", "approve", "--handoff-id", "opaque-handoff-id", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session handoff unsupported action) error = nil, want error")
	}
	if _, err := ParseBrowser([]string{"session", "handoff", "show", "--handoff-id", "opaque-handoff-id", "--id", "42", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session handoff show with id) error = nil, want read-only lookup flag rejection")
	}
}

func TestParseBrowserSessionRunnerCommands(t *testing.T) {
	create, err := ParseBrowser([]string{"session", "runner", "create", "--login-request-id", "7", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner create) error = %v", err)
	}
	if create.Name != "session" || create.SessionAction != "runner" || create.RunnerAction != "create" || create.LoginRequestID != 7 || !create.JSON {
		t.Fatalf("create command = %+v, want parsed runner create", create)
	}

	list, err := ParseBrowser([]string{"session", "runner", "list", "--login-request-id", "7", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner list) error = %v", err)
	}
	if list.SessionAction != "runner" || list.RunnerAction != "list" || list.LoginRequestID != 7 || !list.JSON {
		t.Fatalf("list command = %+v, want parsed runner list", list)
	}

	show, err := ParseBrowser([]string{"session", "runner", "show", "--id", "3", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner show) error = %v", err)
	}
	if show.SessionAction != "runner" || show.RunnerAction != "show" || show.ID != 3 || !show.JSON {
		t.Fatalf("show command = %+v, want parsed runner show", show)
	}

	start, err := ParseBrowser([]string{"session", "runner", "start", "--id", "3", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner start) error = %v", err)
	}
	if start.SessionAction != "runner" || start.RunnerAction != "start" || start.ID != 3 || !start.JSON {
		t.Fatalf("start command = %+v, want parsed runner start", start)
	}

	status, err := ParseBrowser([]string{"session", "runner", "status", "--id", "3", "--status", "started", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner status) error = %v", err)
	}
	if status.SessionAction != "runner" || status.RunnerAction != "status" || status.ID != 3 || status.Status != "started" || !status.JSON {
		t.Fatalf("status command = %+v, want parsed runner status", status)
	}

	cancel, err := ParseBrowser([]string{"session", "runner", "cancel", "--id", "3", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner cancel) error = %v", err)
	}
	if cancel.SessionAction != "runner" || cancel.RunnerAction != "cancel" || cancel.ID != 3 || !cancel.JSON {
		t.Fatalf("cancel command = %+v, want parsed runner cancel", cancel)
	}

	plan, err := ParseBrowser([]string{
		"session", "runner", "plan-novnc",
		"--id", "3",
		"--browser-command", "/usr/bin/chromium",
		"--browser-allowed-command", "/usr/bin/chromium",
		"--display-command", "/usr/bin/x11vnc",
		"--display-allowed-command", "/usr/bin/x11vnc",
		"--novnc-command", "/usr/bin/websockify",
		"--novnc-allowed-command", "/usr/bin/websockify",
		"--bind-addr", "127.0.0.1:6080",
		"--private-base-url", "https://odin-handoff.tailnet.local",
		"--timeout-seconds", "300",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(session runner plan-novnc) error = %v", err)
	}
	if plan.SessionAction != "runner" || plan.RunnerAction != "plan-novnc" || plan.ID != 3 || plan.NoVNCBrowserCommand != "/usr/bin/chromium" || plan.NoVNCDisplayCommand != "/usr/bin/x11vnc" || plan.NoVNCCommand != "/usr/bin/websockify" || plan.NoVNCBindAddr != "127.0.0.1:6080" || plan.NoVNCPrivateBaseURL != "https://odin-handoff.tailnet.local" || plan.NoVNCTimeoutSeconds != 300 || !plan.JSON {
		t.Fatalf("plan command = %+v, want parsed NoVNC dry-run plan config", plan)
	}
	if len(plan.NoVNCBrowserAllowedCommands) != 1 || plan.NoVNCBrowserAllowedCommands[0] != "/usr/bin/chromium" || len(plan.NoVNCDisplayAllowedCommands) != 1 || plan.NoVNCDisplayAllowedCommands[0] != "/usr/bin/x11vnc" || len(plan.NoVNCAllowedCommands) != 1 || plan.NoVNCAllowedCommands[0] != "/usr/bin/websockify" {
		t.Fatalf("plan allowed commands = browser %+v display %+v novnc %+v, want parsed allowlists", plan.NoVNCBrowserAllowedCommands, plan.NoVNCDisplayAllowedCommands, plan.NoVNCAllowedCommands)
	}

	if _, err := ParseBrowser([]string{"session", "runner", "create", "--id", "3", "--login-request-id", "7", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner create with id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "runner", "status", "--id", "3", "--status", "requested", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner status requested) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "runner", "start", "--id", "3", "--status", "started", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner start with status) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "runner", "plan-novnc", "--id", "3", "--status", "started", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner plan-novnc with status) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "runner", "approve", "--id", "3", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner unsupported action) error = nil, want rejection")
	}
}

func TestParseBrowserSessionVerifyCommand(t *testing.T) {
	command, err := ParseBrowser([]string{"session", "verify", "--id", "42", "--login-request-id", "7", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session verify) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "verify" || command.ID != 42 || command.LoginRequestID != 7 || !command.JSON {
		t.Fatalf("verify command = %+v, want parsed verify request", command)
	}

	_, err = ParseBrowser([]string{"session", "verify", "--id", "42", "--status", "verified", "--json"})
	if err == nil || !strings.Contains(err.Error(), "only accepts --id, --login-request-id, and --json") {
		t.Fatalf("ParseBrowser(session verify with status) error = %v, want rejected status flag", err)
	}
}

func TestParseBrowserSessionProveCommand(t *testing.T) {
	command, err := ParseBrowser([]string{"session", "prove", "--id", "42", "--url", "https://x.com/", "--expect-title", "Home / X", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session prove) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "prove" || command.ID != 42 || command.URL != "https://x.com/" || command.ExpectedTitle != "Home / X" || !command.JSON {
		t.Fatalf("prove command = %+v, want parsed proof command", command)
	}

	if _, err := ParseBrowser([]string{"session", "prove", "--id", "42", "--url", "https://x.com/", "--json"}); err == nil || !strings.Contains(err.Error(), "--expect-title is required") {
		t.Fatalf("ParseBrowser(session prove missing title) error = %v, want title required", err)
	}
	if _, err := ParseBrowser([]string{"session", "prove", "--id", "42", "--url", "/relative", "--expect-title", "Home / X", "--json"}); err == nil || !strings.Contains(err.Error(), "--url must be an absolute URL") {
		t.Fatalf("ParseBrowser(session prove relative url) error = %v, want absolute URL rejection", err)
	}
	if _, err := ParseBrowser([]string{"session", "show", "--id", "42", "--url", "https://x.com/", "--json"}); err == nil || !strings.Contains(err.Error(), "does not accept --url") {
		t.Fatalf("ParseBrowser(session show with proof url) error = %v, want proof-only flag rejection", err)
	}
}

func TestParseBrowserSessionPrepareProfileCommand(t *testing.T) {
	command, err := ParseBrowser([]string{"session", "prepare-profile", "--id", "42", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session prepare-profile) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "prepare-profile" || command.ID != 42 || !command.JSON {
		t.Fatalf("prepare-profile command = %+v, want parsed prepare-profile request", command)
	}

	_, err = ParseBrowser([]string{"session", "prepare-profile", "--id", "42", "--profile-path", "browser-sessions/profiles/other", "--json"})
	if err == nil || !strings.Contains(err.Error(), "only accepts --id and --json") {
		t.Fatalf("ParseBrowser(session prepare-profile with profile path) error = %v, want rejected profile path", err)
	}
}

func TestParseBrowserSessionProfileRetentionCleanupCommand(t *testing.T) {
	command, err := ParseBrowser([]string{"session", "profile", "retention", "cleanup", "--session-id", "42", "--apply", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session profile retention cleanup) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "profile" || command.ProfileAction != "retention" || command.RetentionAction != "cleanup" || command.SessionID != 42 || !command.Apply || !command.JSON {
		t.Fatalf("command = %+v, want parsed profile retention cleanup", command)
	}

	dryRun, err := ParseBrowser([]string{"session", "profile", "retention", "cleanup", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session profile retention cleanup dry-run) error = %v", err)
	}
	if dryRun.Apply || dryRun.SessionID != 0 || dryRun.ProfileAction != "retention" || dryRun.RetentionAction != "cleanup" {
		t.Fatalf("dryRun command = %+v, want global dry-run cleanup", dryRun)
	}

	if _, err := ParseBrowser([]string{"session", "profile", "retention", "cleanup", "--id", "42", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile retention cleanup with --id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "retention", "cleanup", "--session-id", "0", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile retention cleanup with invalid session id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "rotate", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile rotate) error = nil, want rejection")
	}
}

func TestParseBrowserSessionProfileArtifactCreateFixtureCommand(t *testing.T) {
	command, err := ParseBrowser([]string{
		"session", "profile", "artifact", "create-fixture",
		"--session-id", "42",
		"--name", "fixture-one",
		"--plaintext-file", "/tmp/fixture-profile.txt",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(session profile artifact create-fixture) error = %v", err)
	}
	if command.Name != "session" || command.SessionAction != "profile" || command.ProfileAction != "artifact" || command.ArtifactAction != "create-fixture" || command.SessionID != 42 || command.ArtifactName != "fixture-one" || command.PlaintextFile != "/tmp/fixture-profile.txt" || !command.JSON {
		t.Fatalf("command = %+v, want parsed fixture artifact create", command)
	}

	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "create-fixture", "--name", "fixture-one", "--plaintext-file", "/tmp/fixture-profile.txt", "--json"}); err == nil {
		t.Fatal("ParseBrowser(create-fixture missing session id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "create-fixture", "--session-id", "42", "--plaintext-file", "/tmp/fixture-profile.txt", "--json"}); err == nil {
		t.Fatal("ParseBrowser(create-fixture missing name) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "create-fixture", "--session-id", "42", "--name", "fixture-one", "--json"}); err == nil {
		t.Fatal("ParseBrowser(create-fixture missing plaintext file) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "create-fixture", "--session-id", "42", "--name", "fixture-one", "--plaintext-file", "/tmp/fixture-profile.txt", "--apply", "--json"}); err == nil {
		t.Fatal("ParseBrowser(create-fixture with apply) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "rotate", "--session-id", "42", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact unsupported action) error = nil, want rejection")
	}
}

func TestParseBrowserSessionProfileArtifactStatusCommands(t *testing.T) {
	show, err := ParseBrowser([]string{
		"session", "profile", "artifact", "show",
		"--id", "9",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(profile artifact show) error = %v", err)
	}
	if show.Name != "session" || show.SessionAction != "profile" || show.ProfileAction != "artifact" || show.ArtifactAction != "show" || show.ID != 9 || !show.JSON {
		t.Fatalf("show command = %+v, want parsed artifact show", show)
	}

	list, err := ParseBrowser([]string{
		"session", "profile", "artifact", "list",
		"--session-id", "42",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(profile artifact list) error = %v", err)
	}
	if list.SessionAction != "profile" || list.ProfileAction != "artifact" || list.ArtifactAction != "list" || list.SessionID != 42 || !list.JSON {
		t.Fatalf("list command = %+v, want parsed artifact list", list)
	}

	revoke, err := ParseBrowser([]string{
		"session", "profile", "artifact", "revoke",
		"--id", "9",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(profile artifact revoke) error = %v", err)
	}
	if revoke.SessionAction != "profile" || revoke.ProfileAction != "artifact" || revoke.ArtifactAction != "revoke" || revoke.ID != 9 || !revoke.JSON {
		t.Fatalf("revoke command = %+v, want parsed artifact revoke", revoke)
	}

	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "show", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact show missing id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "list", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact list missing session id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "revoke", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact revoke missing id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "show", "--id", "9", "--plaintext-file", "/tmp/plain", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact show with plaintext file) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "list", "--session-id", "42", "--apply", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact list with apply) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "revoke", "--id", "9", "--session-id", "42", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact revoke with session id) error = nil, want rejection")
	}
}

func TestParseBrowserSessionProfileArtifactMaterializationCommands(t *testing.T) {
	materialize, err := ParseBrowser([]string{
		"session", "profile", "artifact", "materialize",
		"--id", "9",
		"--target-dir", "runtime/browser-profile-materializations/proof",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(profile artifact materialize) error = %v", err)
	}
	if materialize.SessionAction != "profile" || materialize.ProfileAction != "artifact" || materialize.ArtifactAction != "materialize" || materialize.ID != 9 || materialize.TargetDir != "runtime/browser-profile-materializations/proof" || !materialize.JSON {
		t.Fatalf("materialize command = %+v, want parsed artifact materialization", materialize)
	}

	cleanup, err := ParseBrowser([]string{
		"session", "profile", "artifact", "cleanup-materialization",
		"--id", "9",
		"--target-dir", "runtime/browser-profile-materializations/proof",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseBrowser(profile artifact cleanup-materialization) error = %v", err)
	}
	if cleanup.ArtifactAction != "cleanup-materialization" || cleanup.ID != 9 || cleanup.TargetDir != "runtime/browser-profile-materializations/proof" || !cleanup.JSON {
		t.Fatalf("cleanup command = %+v, want parsed artifact materialization cleanup", cleanup)
	}

	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "materialize", "--target-dir", "runtime/browser-profile-materializations/proof", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact materialize missing id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "materialize", "--id", "9", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact materialize missing target dir) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "cleanup-materialization", "--id", "9", "--target-dir", "runtime/browser-profile-materializations/proof", "--plaintext-file", "/tmp/plain", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact cleanup-materialization with plaintext file) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "profile", "artifact", "materialize", "--id", "9", "--target-dir", "runtime/browser-profile-materializations/proof", "--apply", "--json"}); err == nil {
		t.Fatal("ParseBrowser(profile artifact materialize with apply) error = nil, want rejection")
	}
}

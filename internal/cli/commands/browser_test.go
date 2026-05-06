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

	if _, err := ParseBrowser([]string{"session", "runner", "create", "--id", "3", "--login-request-id", "7", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner create with id) error = nil, want rejection")
	}
	if _, err := ParseBrowser([]string{"session", "runner", "status", "--id", "3", "--status", "requested", "--json"}); err == nil {
		t.Fatal("ParseBrowser(session runner status requested) error = nil, want rejection")
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

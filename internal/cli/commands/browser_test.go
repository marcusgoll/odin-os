package commands

import "testing"

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
	request, err := ParseBrowser([]string{"session", "login-request", "--id", "42", "--json"})
	if err != nil {
		t.Fatalf("ParseBrowser(session login-request) error = %v", err)
	}
	if request.Name != "session" || request.SessionAction != "login-request" || request.ID != 42 || !request.JSON {
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

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

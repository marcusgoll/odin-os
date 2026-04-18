package commands

import "testing"

func TestParseCompanionCreate(t *testing.T) {
	t.Parallel()

	command, err := ParseCompanion([]string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"})
	if err != nil {
		t.Fatalf("ParseCompanion() error = %v", err)
	}
	if command.Name != "create" {
		t.Fatalf("Name = %q, want create", command.Name)
	}
	if command.Kind != "advisor" {
		t.Fatalf("Kind = %q, want advisor", command.Kind)
	}
	if command.Key != "finance" {
		t.Fatalf("Key = %q, want finance", command.Key)
	}
	if command.Title != "Finance Advisor" {
		t.Fatalf("Title = %q, want Finance Advisor", command.Title)
	}
	if command.JSON {
		t.Fatalf("JSON = true, want false")
	}
}

func TestParseCompanionListJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseCompanion([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("ParseCompanion() error = %v", err)
	}
	if command.Name != "list" {
		t.Fatalf("Name = %q, want list", command.Name)
	}
	if !command.JSON {
		t.Fatalf("JSON = false, want true")
	}
}

func TestParseCompanionReadCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantName string
		wantKey  string
		wantJSON bool
	}{
		{
			name:     "get text",
			args:     []string{"get", "finance"},
			wantName: "get",
			wantKey:  "finance",
		},
		{
			name:     "state json",
			args:     []string{"state", "finance", "--json"},
			wantName: "state",
			wantKey:  "finance",
			wantJSON: true,
		},
		{
			name:     "capabilities json",
			args:     []string{"capabilities", "finance", "--json"},
			wantName: "capabilities",
			wantKey:  "finance",
			wantJSON: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			command, err := ParseCompanion(test.args)
			if err != nil {
				t.Fatalf("ParseCompanion() error = %v", err)
			}
			if command.Name != test.wantName {
				t.Fatalf("Name = %q, want %q", command.Name, test.wantName)
			}
			if command.Key != test.wantKey {
				t.Fatalf("Key = %q, want %q", command.Key, test.wantKey)
			}
			if command.JSON != test.wantJSON {
				t.Fatalf("JSON = %t, want %t", command.JSON, test.wantJSON)
			}
		})
	}
}

func TestParseCompanionRunJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseCompanion([]string{"run", "finance", "--objective", "review April budget", "--trigger", "build_plus_review", "--json"})
	if err != nil {
		t.Fatalf("ParseCompanion() error = %v", err)
	}
	if command.Name != "run" {
		t.Fatalf("Name = %q, want run", command.Name)
	}
	if command.Key != "finance" {
		t.Fatalf("Key = %q, want finance", command.Key)
	}
	if command.Objective != "review April budget" {
		t.Fatalf("Objective = %q, want review April budget", command.Objective)
	}
	if command.Trigger != "build_plus_review" {
		t.Fatalf("Trigger = %q, want build_plus_review", command.Trigger)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseCompanionRunRejectsMissingObjective(t *testing.T) {
	t.Parallel()

	if _, err := ParseCompanion([]string{"run", "finance"}); err == nil {
		t.Fatal("ParseCompanion() error = nil, want missing objective error")
	}
}

func TestParseCompanionRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	if _, err := ParseCompanion([]string{"create", "--kind", "banana", "--key", "finance", "--title", "Finance Advisor"}); err == nil {
		t.Fatal("ParseCompanion() error = nil, want unsupported kind error")
	}
}

func TestParseCompanionRejectsUnsupportedSubcommand(t *testing.T) {
	t.Parallel()

	if _, err := ParseCompanion([]string{"delete", "finance"}); err == nil {
		t.Fatal("ParseCompanion() error = nil, want unsupported subcommand error")
	}
}

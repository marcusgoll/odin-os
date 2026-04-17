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

func TestParseCompanionRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	if _, err := ParseCompanion([]string{"create", "--kind", "banana", "--key", "finance", "--title", "Finance Advisor"}); err == nil {
		t.Fatal("ParseCompanion() error = nil, want unsupported kind error")
	}
}

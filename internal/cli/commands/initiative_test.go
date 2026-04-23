package commands

import "testing"

func TestParseInitiativeCreate(t *testing.T) {
	t.Parallel()

	command, err := ParseInitiative([]string{"create", "--kind", "routine", "--key", "life-admin", "--title", "Life Admin"})
	if err != nil {
		t.Fatalf("ParseInitiative() error = %v", err)
	}
	if command.Name != "create" {
		t.Fatalf("Name = %q, want create", command.Name)
	}
	if command.Kind != "routine" {
		t.Fatalf("Kind = %q, want routine", command.Kind)
	}
	if command.Key != "life-admin" {
		t.Fatalf("Key = %q, want life-admin", command.Key)
	}
	if command.Title != "Life Admin" {
		t.Fatalf("Title = %q, want Life Admin", command.Title)
	}
	if command.JSON {
		t.Fatalf("JSON = true, want false")
	}
}

func TestParseInitiativeListJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseInitiative([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("ParseInitiative() error = %v", err)
	}
	if command.Name != "list" {
		t.Fatalf("Name = %q, want list", command.Name)
	}
	if !command.JSON {
		t.Fatalf("JSON = false, want true")
	}
}

func TestParseInitiativeRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()

	if _, err := ParseInitiative([]string{"create", "--kind", "banana", "--key", "life-admin", "--title", "Life Admin"}); err == nil {
		t.Fatal("ParseInitiative() error = nil, want unsupported kind error")
	}
}

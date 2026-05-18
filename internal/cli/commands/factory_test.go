package commands

import "testing"

func TestParseFactoryStart(t *testing.T) {
	t.Parallel()

	command, err := ParseFactory([]string{"start", "--project", "odin-core", "--title", "Ship factory lane", "--json"})
	if err != nil {
		t.Fatalf("ParseFactory() error = %v", err)
	}
	if command.Action != "start" {
		t.Fatalf("Action = %q, want start", command.Action)
	}
	if command.Project != "odin-core" {
		t.Fatalf("Project = %q, want odin-core", command.Project)
	}
	if command.Title != "Ship factory lane" {
		t.Fatalf("Title = %q, want Ship factory lane", command.Title)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseFactoryStatus(t *testing.T) {
	t.Parallel()

	command, err := ParseFactory([]string{"status", "--task", "task-123", "--json"})
	if err != nil {
		t.Fatalf("ParseFactory() error = %v", err)
	}
	if command.Action != "status" {
		t.Fatalf("Action = %q, want status", command.Action)
	}
	if command.Task != "task-123" {
		t.Fatalf("Task = %q, want task-123", command.Task)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseFactoryPromoteIntake(t *testing.T) {
	t.Parallel()

	command, err := ParseFactory([]string{"promote-intake", "intake-42", "--json"})
	if err != nil {
		t.Fatalf("ParseFactory() error = %v", err)
	}
	if command.Action != "promote-intake" {
		t.Fatalf("Action = %q, want promote-intake", command.Action)
	}
	if command.IntakeID != "intake-42" {
		t.Fatalf("IntakeID = %q, want intake-42", command.IntakeID)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseFactoryMergeGate(t *testing.T) {
	t.Parallel()

	command, err := ParseFactory([]string{"merge-gate", "--task", "77", "--json"})
	if err != nil {
		t.Fatalf("ParseFactory() error = %v", err)
	}
	if command.Action != "merge-gate" {
		t.Fatalf("Action = %q, want merge-gate", command.Action)
	}
	if command.Task != "77" {
		t.Fatalf("Task = %q, want 77", command.Task)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseFactoryRejectsUnknownArgument(t *testing.T) {
	t.Parallel()

	if _, err := ParseFactory([]string{"start", "--project", "odin-core", "--title", "Ship", "--surprise"}); err == nil {
		t.Fatal("ParseFactory() error = nil, want unknown argument error")
	}
}

package commands

import "testing"

func TestParseTaskCreate(t *testing.T) {
	t.Parallel()

	command, err := ParseTask([]string{"create", "--project", "alpha", "--title", "cutover smoke"})
	if err != nil {
		t.Fatalf("ParseTask() error = %v", err)
	}
	if command.Name != "create" {
		t.Fatalf("Name = %q, want create", command.Name)
	}
	if command.ProjectKey != "alpha" {
		t.Fatalf("ProjectKey = %q, want alpha", command.ProjectKey)
	}
	if command.Title != "cutover smoke" {
		t.Fatalf("Title = %q, want cutover smoke", command.Title)
	}
	if command.JSON {
		t.Fatalf("JSON = true, want false")
	}
}

func TestParseTaskRunJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseTask([]string{
		"run",
		"--project", "alpha",
		"--title", "run from cli",
		"--acceptance", "fixture driver completes",
		"--acceptance-criteria", "run is recorded",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseTask() error = %v", err)
	}
	if command.Name != "run" {
		t.Fatalf("Name = %q, want run", command.Name)
	}
	if !command.JSON {
		t.Fatalf("JSON = false, want true")
	}
	if len(command.AcceptanceCriteria) != 2 || command.AcceptanceCriteria[0] != "fixture driver completes" || command.AcceptanceCriteria[1] != "run is recorded" {
		t.Fatalf("AcceptanceCriteria = %#v", command.AcceptanceCriteria)
	}
}

func TestParseTaskRejectsMissingTitle(t *testing.T) {
	t.Parallel()

	if _, err := ParseTask([]string{"create", "--project", "alpha"}); err == nil {
		t.Fatal("ParseTask() error = nil, want missing title error")
	}
}

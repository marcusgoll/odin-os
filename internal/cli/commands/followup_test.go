package commands

import (
	"testing"
	"time"
)

func TestParseFollowUpAdd(t *testing.T) {
	t.Parallel()

	command, err := ParseFollowUp([]string{"add", "--initiative", "life-admin", "--title", "Review mail", "--cadence", "weekly"})
	if err != nil {
		t.Fatalf("ParseFollowUp() error = %v", err)
	}
	if command.Name != "add" {
		t.Fatalf("Name = %q, want add", command.Name)
	}
	if command.Initiative != "life-admin" {
		t.Fatalf("Initiative = %q, want life-admin", command.Initiative)
	}
	if command.Title != "Review mail" {
		t.Fatalf("Title = %q, want Review mail", command.Title)
	}
	if command.Cadence != "weekly" {
		t.Fatalf("Cadence = %q, want weekly", command.Cadence)
	}
}

func TestParseFollowUpListJSON(t *testing.T) {
	t.Parallel()

	command, err := ParseFollowUp([]string{"list", "--json"})
	if err != nil {
		t.Fatalf("ParseFollowUp() error = %v", err)
	}
	if command.Name != "list" {
		t.Fatalf("Name = %q, want list", command.Name)
	}
	if !command.JSON {
		t.Fatal("JSON = false, want true")
	}
}

func TestParseFollowUpComplete(t *testing.T) {
	t.Parallel()

	command, err := ParseFollowUp([]string{"complete", "42"})
	if err != nil {
		t.Fatalf("ParseFollowUp() error = %v", err)
	}
	if command.Name != "complete" {
		t.Fatalf("Name = %q, want complete", command.Name)
	}
	if command.ID != 42 {
		t.Fatalf("ID = %d, want 42", command.ID)
	}
}

func TestParseFollowUpSnooze(t *testing.T) {
	t.Parallel()

	until := "2026-04-20T09:00:00Z"
	command, err := ParseFollowUp([]string{"snooze", "42", "--until", until})
	if err != nil {
		t.Fatalf("ParseFollowUp() error = %v", err)
	}
	if command.Name != "snooze" {
		t.Fatalf("Name = %q, want snooze", command.Name)
	}
	if command.ID != 42 {
		t.Fatalf("ID = %d, want 42", command.ID)
	}
	if got := command.Until.UTC().Format(time.RFC3339); got != until {
		t.Fatalf("Until = %s, want %s", got, until)
	}
}

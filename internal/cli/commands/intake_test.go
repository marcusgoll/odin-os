package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIntakeEnqueue(t *testing.T) {
	t.Parallel()

	command, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
		"--dedup-key", "ci_failure:pbs:1234",
		"--payload-file", "-",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseIntake() error = %v", err)
	}
	if command.Name != "enqueue" {
		t.Fatalf("Name = %q, want enqueue", command.Name)
	}
	if command.Source != "n8n" {
		t.Fatalf("Source = %q, want n8n", command.Source)
	}
	if command.ProjectKey != "pbs" {
		t.Fatalf("ProjectKey = %q, want pbs", command.ProjectKey)
	}
	if command.Title != "Investigate PBS CI failure" {
		t.Fatalf("Title = %q, want Investigate PBS CI failure", command.Title)
	}
	if command.Type != "ci_failure" {
		t.Fatalf("Type = %q, want ci_failure", command.Type)
	}
	if command.DedupKey != "ci_failure:pbs:1234" {
		t.Fatalf("DedupKey = %q, want ci_failure:pbs:1234", command.DedupKey)
	}
	if command.RequestedBy != "n8n" {
		t.Fatalf("RequestedBy = %q, want n8n", command.RequestedBy)
	}
	if command.PayloadFile != "-" {
		t.Fatalf("PayloadFile = %q, want -", command.PayloadFile)
	}
	if !command.JSON {
		t.Fatalf("JSON = false, want true")
	}
}

func TestParseIntakeRejectsMissingSource(t *testing.T) {
	t.Parallel()

	if _, err := ParseIntake([]string{
		"enqueue",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want missing source error")
	}
}

func TestParseIntakeRejectsMissingProject(t *testing.T) {
	t.Parallel()

	if _, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want missing project error")
	}
}

func TestParseIntakeRejectsInvalidDedupKey(t *testing.T) {
	t.Parallel()

	if _, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
		"--dedup-key", " ci_failure:pbs:1234 ",
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want invalid dedup key error")
	}
}

func TestParseIntakeRejectsDuplicateSourceFlag(t *testing.T) {
	t.Parallel()

	if _, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--source", "telegram",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want duplicate source flag error")
	}
}

func TestParseIntakeRejectsMalformedJSONPayloadFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	payloadPath := filepath.Join(tmpDir, "payload.json")
	if err := os.WriteFile(payloadPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
		"--payload-file", payloadPath,
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want malformed JSON payload error")
	}
}

func TestParseIntakeRejectsNonObjectJSONPayloadFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	payloadPath := filepath.Join(tmpDir, "payload.json")
	if err := os.WriteFile(payloadPath, []byte("[]"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := ParseIntake([]string{
		"enqueue",
		"--source", "n8n",
		"--project", "pbs",
		"--title", "Investigate PBS CI failure",
		"--type", "ci_failure",
		"--payload-file", payloadPath,
	}); err == nil {
		t.Fatal("ParseIntake() error = nil, want non-object JSON payload error")
	}
}

package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
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

func TestParseIntakeRawCreate(t *testing.T) {
	t.Parallel()

	command, err := ParseIntake([]string{
		"raw",
		"create",
		"--source", "operator",
		"--project", "odin-core",
		"--title", "Capture governed intake",
		"--type", "request",
		"--dedup-key", "governed-intake:1",
		"--requested-by", "codex",
		"--payload-file", "-",
		"--json",
	})
	if err != nil {
		t.Fatalf("ParseIntake(raw create) error = %v", err)
	}
	if command.Name != "raw" || command.RawAction != "create" {
		t.Fatalf("command = %+v, want raw create", command)
	}
	if command.Source != "operator" || command.Type != "request" || command.DedupKey != "governed-intake:1" {
		t.Fatalf("command identity = %+v, want operator request governed-intake:1", command)
	}
	if command.ProjectKey != "odin-core" || command.RequestedBy != "codex" || command.PayloadFile != "-" || !command.JSON {
		t.Fatalf("command = %+v, want project/requested-by/payload/json parsed", command)
	}
}

func TestParseIntakeRawListAndShow(t *testing.T) {
	t.Parallel()

	listCommand, err := ParseIntake([]string{"raw", "list", "--json"})
	if err != nil {
		t.Fatalf("ParseIntake(raw list) error = %v", err)
	}
	if listCommand.Name != "raw" || listCommand.RawAction != "list" || !listCommand.JSON {
		t.Fatalf("list command = %+v, want raw list json", listCommand)
	}

	showCommand, err := ParseIntake([]string{"raw", "show", "intake-42", "--json"})
	if err != nil {
		t.Fatalf("ParseIntake(raw show) error = %v", err)
	}
	if showCommand.Name != "raw" || showCommand.RawAction != "show" || showCommand.ShowRef != "intake-42" || !showCommand.JSON {
		t.Fatalf("show command = %+v, want raw show intake-42 json", showCommand)
	}
}

func TestParseIntakeProcess(t *testing.T) {
	t.Parallel()

	command, err := ParseIntake([]string{"process", "--id", "intake-42", "--json"})
	if err != nil {
		t.Fatalf("ParseIntake(process) error = %v", err)
	}
	if command.Name != "process" || command.ShowRef != "intake-42" || !command.JSON {
		t.Fatalf("command = %+v, want process intake-42 json", command)
	}
}

func TestParseIntakeReviewCommands(t *testing.T) {
	t.Parallel()

	listCommand, err := ParseIntake([]string{"review", "list", "--json"})
	if err != nil {
		t.Fatalf("ParseIntake(review list) error = %v", err)
	}
	if listCommand.Name != "review" || listCommand.ReviewAction != "list" || !listCommand.JSON {
		t.Fatalf("list command = %+v, want review list json", listCommand)
	}

	for _, action := range []string{"show", "accept", "reject", "clarify", "archive"} {
		command, err := ParseIntake([]string{"review", action, "intake-42", "--json"})
		if err != nil {
			t.Fatalf("ParseIntake(review %s) error = %v", action, err)
		}
		if command.Name != "review" || command.ReviewAction != action || command.ShowRef != "intake-42" || !command.JSON {
			t.Fatalf("command = %+v, want review %s intake-42 json", command, action)
		}
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

func TestRawIntakeStoreCreatesReviewableItemWithoutTask(t *testing.T) {
	ctx := context.Background()
	store := openRawIntakeCommandStore(t)
	defer store.Close()

	item, err := store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         "default",
		SourceFamily:        "operator",
		EventKind:           "request",
		Subject:             "Capture governed intake",
		DedupeKey:           "governed-intake:1",
		DedupeRecipeVersion: "raw-cli-v1",
		SourceFactsJSON:     `{"source":"operator","intake_type":"request","requested_by":"codex","payload":{}}`,
		Status:              "received",
		Scope:               "project",
		ScopeKey:            "odin-core",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem() error = %v", err)
	}
	if item.ID == 0 || item.Status != "received" {
		t.Fatalf("item = %+v, want persisted received intake item", item)
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks error = %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("task count = %d, want raw intake to create no tasks", taskCount)
	}
}

func openRawIntakeCommandStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

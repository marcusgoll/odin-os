package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExternalIssueUpsertIsIdempotentByProviderRepoAndNumber(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "external-issues.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	first, err := store.UpsertExternalIssue(ctx, UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       "acme/alpha",
		Number:     42,
		Title:      "Initial title",
		BodyHash:   "sha256:initial",
		URL:        "https://github.example/acme/alpha/issues/42",
		State:      "open",
		LabelsJSON: `["odin:ready"]`,
		SyncStatus: "eligible",
		SyncCursor: "github:issue:acme/alpha:42:v1",
	})
	if err != nil {
		t.Fatalf("UpsertExternalIssue(first) error = %v", err)
	}

	second, err := store.UpsertExternalIssue(ctx, UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       "acme/alpha",
		Number:     42,
		Title:      "Updated title",
		BodyHash:   "sha256:updated",
		URL:        "https://github.example/acme/alpha/issues/42",
		State:      "open",
		LabelsJSON: `["odin:ready","backend"]`,
		SyncStatus: "eligible",
		SyncCursor: "github:issue:acme/alpha:42:v2",
	})
	if err != nil {
		t.Fatalf("UpsertExternalIssue(second) error = %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second.ID = %d, want idempotent ID %d", second.ID, first.ID)
	}
	if second.Title != "Updated title" || second.BodyHash != "sha256:updated" || second.LabelsJSON != `["odin:ready","backend"]` {
		t.Fatalf("updated issue = %+v, want latest fields", second)
	}
	if second.SyncCursor != "github:issue:acme/alpha:42:v2" {
		t.Fatalf("updated cursor = %q, want latest cursor", second.SyncCursor)
	}

	issues, err := store.ListExternalIssues(ctx, ListExternalIssuesParams{
		Repo:       "acme/alpha",
		SyncStatus: "eligible",
	})
	if err != nil {
		t.Fatalf("ListExternalIssues() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("ListExternalIssues() len = %d, want 1: %+v", len(issues), issues)
	}
	if issues[0].ID != first.ID || issues[0].Number != 42 || issues[0].Title != "Updated title" {
		t.Fatalf("listed issue = %+v, want updated issue", issues[0])
	}
	if issues[0].SyncCursor != "github:issue:acme/alpha:42:v2" {
		t.Fatalf("listed cursor = %q, want persisted cursor", issues[0].SyncCursor)
	}
}

func TestExternalIssueListSurvivesStoreRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "external-issues-restart.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.UpsertExternalIssue(ctx, UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       "acme/alpha",
		Number:     43,
		Title:      "Restart readable intake",
		BodyHash:   "sha256:restart",
		URL:        "https://github.example/acme/alpha/issues/43",
		State:      "open",
		LabelsJSON: `["odin:ready"]`,
		SyncStatus: "eligible",
		SyncCursor: "github:issue:acme/alpha:43",
	}); err != nil {
		_ = store.Close()
		t.Fatalf("UpsertExternalIssue() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(restart) error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(restart) error = %v", err)
	}
	issues, err := reopened.ListExternalIssues(ctx, ListExternalIssuesParams{
		Repo:       "acme/alpha",
		SyncStatus: "eligible",
	})
	if err != nil {
		t.Fatalf("ListExternalIssues(restart) error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("ListExternalIssues(restart) len = %d, want 1: %+v", len(issues), issues)
	}
	if issues[0].Number != 43 || issues[0].SyncCursor != "github:issue:acme/alpha:43" {
		t.Fatalf("restarted issue = %+v, want durable external intake cursor", issues[0])
	}
}

package sqlite

import (
	"context"
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
}

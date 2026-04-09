package projections_test

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestProjectTransitionProjectionIncludesCurrentStateAndLatestReport(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "compare",
		Controller:         "legacy_odin",
		LimitedActionsJSON: "",
		Notes:              "comparing decisions",
		ChangedBy:          "operator",
	}); err != nil {
		t.Fatalf("SetProjectTransition() error = %v", err)
	}

	if _, err := store.RecordProjectTransitionReport(ctx, sqlite.RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "compare_report",
		Summary:     "decision mismatch",
		DetailsJSON: `{"verdict":"mismatch"}`,
	}); err != nil {
		t.Fatalf("RecordProjectTransitionReport() error = %v", err)
	}

	views, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}

	if len(views) != 1 {
		t.Fatalf("ListProjectTransitionViews() len = %d, want 1", len(views))
	}
	if views[0].TransitionState != "compare" {
		t.Fatalf("TransitionState = %q, want %q", views[0].TransitionState, "compare")
	}
	if views[0].Controller != "legacy_odin" {
		t.Fatalf("Controller = %q, want %q", views[0].Controller, "legacy_odin")
	}
	if views[0].LastReportType != "compare_report" {
		t.Fatalf("LastReportType = %q, want %q", views[0].LastReportType, "compare_report")
	}
	if views[0].LastReportAt == nil {
		t.Fatalf("LastReportAt = nil, want timestamp")
	}
}

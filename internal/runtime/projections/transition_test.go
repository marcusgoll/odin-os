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

func TestProjectTransitionProjectionAggregatesTasksEventsAndReportsIndependently(t *testing.T) {
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

	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "done",
		Title:       "Done task",
		ActionKey:   "docs.cleanup",
		Status:      "completed",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask(done) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "open",
		Title:       "Open task",
		ActionKey:   "docs.cleanup",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask(open) error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO events (stream_type, stream_id, event_type, event_version, scope, project_id, payload_json, occurred_at)
		VALUES
			('project', ?, 'test.first', 1, 'project', ?, '{}', '2026-05-17T01:00:00Z'),
			('project', ?, 'test.second', 1, 'project', ?, '{}', '2099-05-17T02:00:00Z')
	`, project.ID, project.ID, project.ID, project.ID); err != nil {
		t.Fatalf("insert events error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO project_transition_reports (project_id, report_type, summary, details_json, recorded_at)
		VALUES
			(?, 'first_report', 'first', '{}', '2026-05-17T03:00:00Z'),
			(?, 'latest_report', 'latest', '{}', '2026-05-17T04:00:00Z')
	`, project.ID, project.ID); err != nil {
		t.Fatalf("insert transition reports error = %v", err)
	}

	views, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("ListProjectTransitionViews() len = %d, want 1", len(views))
	}
	if views[0].TaskCount != 2 {
		t.Fatalf("TaskCount = %d, want 2", views[0].TaskCount)
	}
	if views[0].OpenTaskCount != 1 {
		t.Fatalf("OpenTaskCount = %d, want 1", views[0].OpenTaskCount)
	}
	lastEventAt := ""
	if views[0].LastEventAt != nil {
		lastEventAt = *views[0].LastEventAt
	}
	if lastEventAt != "2099-05-17T02:00:00Z" {
		t.Fatalf("LastEventAt = %q, want 2099-05-17T02:00:00Z", lastEventAt)
	}
	if views[0].LastReportType != "latest_report" {
		t.Fatalf("LastReportType = %q, want latest_report", views[0].LastReportType)
	}
}

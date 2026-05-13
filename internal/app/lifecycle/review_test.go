package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/workspaces"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/store/sqlite"
)

func TestReviewQueueSourceCompositionUsesGovernedSources(t *testing.T) {
	sources := defaultReviewQueueSources()
	got := make([]string, 0, len(sources))
	for _, source := range sources {
		got = append(got, source.Name())
	}

	want := []string{
		"intake",
		"goal",
		"approval",
		"skill_artifact",
		"context_pack",
		"memory_proposal",
		"recovery",
		"failed_work",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultReviewQueueSources() = %#v, want %#v", got, want)
	}
}

func TestReviewQueueIncludesAllGovernedDecisionSources(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	seedReviewQueueSourceFixture(t, ctx, app)

	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		t.Fatalf("listReviewQueueEntries() error = %v", err)
	}

	bySource := map[string]bool{}
	for _, entry := range entries {
		bySource[entry.SourceType] = true
	}

	required := []string{
		"intake_review",
		"intake_approval",
		"intake_goal_conversion",
		"goal",
		"task_approval",
		"skill_artifact",
		"context_pack",
		"memory_proposal",
		"recovery",
		"failed_work",
	}
	for _, source := range required {
		if !bySource[source] {
			t.Fatalf("review queue missing source %q; got %#v", source, bySource)
		}
	}
}

func TestReviewQueueIncludesScheduledApprovalWorkAsTaskApproval(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	seedReviewQueueSourceFixture(t, ctx, app)

	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		t.Fatalf("listReviewQueueEntries() error = %v", err)
	}

	for _, entry := range entries {
		if entry.SourceType == "task_approval" && entry.TaskKey == "scheduled-approval-task" && entry.WorkKind == "automation_trigger" {
			if entry.Status != "pending" {
				t.Fatalf("scheduled approval status = %q, want pending", entry.Status)
			}
			return
		}
	}
	t.Fatalf("review queue missing scheduled approval task; entries = %#v", entries)
}

func TestReviewListJSONIncludesOperatorFieldsAndFilters(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	seedReviewQueueSourceFixture(t, ctx, app)

	var stdout bytes.Buffer
	if err := runReview(ctx, app, []string{"list", "--json", "--source", "failed_work", "--status", "failed", "--severity", "medium"}, &stdout); err != nil {
		t.Fatalf("runReview(list filtered) error = %v", err)
	}

	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal(review list JSON) error = %v\n%s", err, stdout.String())
	}
	if len(payload.Items) != 1 {
		t.Fatalf("filtered review items len = %d, want 1; payload = %#v", len(payload.Items), payload.Items)
	}

	item := payload.Items[0]
	if item["source_type"] != "failed_work" || item["status"] != "failed" || item["severity"] != "medium" {
		t.Fatalf("filtered item source/status/severity = %v/%v/%v, want failed_work/failed/medium", item["source_type"], item["status"], item["severity"])
	}
	for _, field := range []string{"source_type", "source_id", "status", "severity", "created_at", "updated_at", "recommended_action", "operator_next_step"} {
		value, ok := item[field]
		if !ok {
			t.Fatalf("filtered review item missing %q: %#v", field, item)
		}
		if text, ok := value.(string); ok && text == "" {
			t.Fatalf("filtered review item field %q is empty: %#v", field, item)
		}
	}
}

func TestReviewQueueBlockedFailedWorkDoesNotAdvertiseRetry(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "blocked-failed-work",
		Name:          "Blocked Failed Work",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "retry-exhausted-task",
		Title:       "Retry exhausted task",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := app.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:         task.ID,
		Status:         "failed",
		NextEligibleAt: time.Time{},
		LastError:      "retry budget exhausted",
		RetryCount:     2,
		MaxAttempts:    3,
	}); err != nil {
		t.Fatalf("UpdateTaskQueueState() error = %v", err)
	}

	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		t.Fatalf("listReviewQueueEntries() error = %v", err)
	}
	for _, entry := range entries {
		if entry.TaskKey != "retry-exhausted-task" {
			continue
		}
		if entry.RetryEligible == nil || *entry.RetryEligible {
			t.Fatalf("RetryEligible = %v, want false", entry.RetryEligible)
		}
		if containsString(entry.AllowedActions, "retry") {
			t.Fatalf("AllowedActions = %#v, want retry omitted when retry policy blocks it", entry.AllowedActions)
		}
		if !containsString(entry.AllowedActions, "follow-up") {
			t.Fatalf("AllowedActions = %#v, want follow-up action", entry.AllowedActions)
		}
		if entry.RecommendedAction != "follow-up" {
			t.Fatalf("RecommendedAction = %q, want follow-up", entry.RecommendedAction)
		}
		if entry.Severity != "high" {
			t.Fatalf("Severity = %q, want high for blocked failed work", entry.Severity)
		}
		return
	}
	t.Fatalf("review queue missing retry-exhausted-task; entries = %#v", entries)
}

func TestReviewShowFailedWorkIncludesNormalizedOperatorFields(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "show-failed-work",
		Name:          "Show Failed Work",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "show-failed-task",
		Title:       "Show failed task",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runReview(ctx, app, []string{"show", "failed-work:" + int64String(task.ID), "--json"}, &stdout); err != nil {
		t.Fatalf("runReview(show failed-work) error = %v", err)
	}
	var payload struct {
		Entry map[string]any `json:"entry"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal(review show JSON) error = %v\n%s", err, stdout.String())
	}
	for _, field := range []string{"source_type", "source_id", "status", "severity", "created_at", "updated_at", "recommended_action", "operator_next_step", "recovery_recommendation"} {
		value, ok := payload.Entry[field]
		if !ok {
			t.Fatalf("failed-work show entry missing %q: %#v", field, payload.Entry)
		}
		if text, ok := value.(string); ok && text == "" {
			t.Fatalf("failed-work show entry field %q is empty: %#v", field, payload.Entry)
		}
	}
}

func TestReviewQueueShowsBrowserEvidenceForApprovalAndFailedWork(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "browser-review",
		Name:          "Browser Review",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "browser-review-task",
		Title:       "Review browser evidence",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := app.Store.StartRun(ctx, sqlite.StartRunParams{TaskID: task.ID, Executor: "huginn_browser", Attempt: 1, Status: "running"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := app.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: "browser_evidence",
		Summary:      "Browser evidence summary",
		DetailsJSON:  `{"page_title":"Docs","url":"https://example.com/docs","selected_links":[{"text":"Docs","url":"https://example.com/docs"}],"confidence":"deterministic_test","limitations":["fixture"]}`,
	}); err != nil {
		t.Fatalf("RecordRunArtifact() error = %v", err)
	}
	if _, _, err := app.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:          run.ID,
		RunStatus:      "failed",
		TaskStatus:     "failed",
		Summary:        "Browser capture failed",
		TerminalReason: "browser_evidence_capture_failed",
		ArtifactsJSON:  `[{"type":"browser_evidence","summary":"Browser evidence summary"}]`,
	}); err != nil {
		t.Fatalf("FinishRunAndSetTaskStatus() error = %v", err)
	}
	approval, err := app.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	var listOut bytes.Buffer
	if err := runReview(ctx, app, []string{"list", "--json", "--source", "task_approval"}, &listOut); err != nil {
		t.Fatalf("runReview(list) error = %v\n%s", err, listOut.String())
	}
	if !strings.Contains(listOut.String(), `"browser_evidence_count": 1`) {
		t.Fatalf("review list output = %s, want browser evidence count", listOut.String())
	}

	var approvalOut bytes.Buffer
	if err := runReview(ctx, app, []string{"show", "approval:" + int64String(approval.ID), "--json"}, &approvalOut); err != nil {
		t.Fatalf("runReview(show approval) error = %v\n%s", err, approvalOut.String())
	}
	if !strings.Contains(approvalOut.String(), `"browser_evidence":`) || !strings.Contains(approvalOut.String(), `"selected_links"`) {
		t.Fatalf("approval show output = %s, want browser evidence details", approvalOut.String())
	}

	var failedOut bytes.Buffer
	if err := runReview(ctx, app, []string{"show", "failed-work:" + int64String(task.ID), "--json"}, &failedOut); err != nil {
		t.Fatalf("runReview(show failed-work) error = %v\n%s", err, failedOut.String())
	}
	if !strings.Contains(failedOut.String(), `"browser_evidence":`) || !strings.Contains(failedOut.String(), `"confidence"`) {
		t.Fatalf("failed-work show output = %s, want browser evidence details", failedOut.String())
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func newLifecycleReviewTestApp(t *testing.T, ctx context.Context) bootstrap.App {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := bootstrap.BootstrapWorkspaceRuntimeState(ctx, store); err != nil {
		t.Fatalf("BootstrapWorkspaceRuntimeState() error = %v", err)
	}
	return bootstrap.App{Store: store}
}

func seedReviewQueueSourceFixture(t *testing.T, ctx context.Context, app bootstrap.App) {
	t.Helper()

	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "review-fixture",
		Name:          "Review Fixture",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "review-intake",
		EventKind:           "request",
		Subject:             "Review an intake item",
		DedupeKey:           "review-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "review_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Review an intake item",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(review) error = %v", err)
	}
	if _, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "approval-intake",
		EventKind:           "request",
		Subject:             "Approve an intake item",
		DedupeKey:           "approval-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "approval_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Approve an intake item",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(approval) error = %v", err)
	}

	convertedGoal, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Converted intake goal"})
	if err != nil {
		t.Fatalf("CreateGoal(converted) error = %v", err)
	}
	convertedIntake, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "goal-intake",
		EventKind:           "request",
		Subject:             "Convert intake to goal",
		DedupeKey:           "goal-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "review_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Convert intake to goal",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem(goal) error = %v", err)
	}
	if _, err := app.Store.ProcessIntakeItem(ctx, sqlite.ProcessIntakeItemParams{
		ID:      convertedIntake.ID,
		Status:  "review_required",
		Summary: convertedIntake.Summary,
		GoalID:  &convertedGoal.ID,
	}); err != nil {
		t.Fatalf("ProcessIntakeItem(goal) error = %v", err)
	}

	if _, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Manual goal review"}); err != nil {
		t.Fatalf("CreateGoal(manual) error = %v", err)
	}

	approvalTask, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Approval task",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	if _, err := app.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      approvalTask.ID,
		Status:      "pending",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	scheduledApprovalTask, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   "scheduled-approval-task",
		Title:                 "Scheduled approval task",
		Status:                "blocked",
		Scope:                 "project",
		RequestedBy:           "automation_trigger:scheduled-review",
		WorkKind:              "automation_trigger",
		ExecutionIntent:       "governance",
		ExecutionIntentSource: "trigger",
	})
	if err != nil {
		t.Fatalf("CreateTask(scheduled approval) error = %v", err)
	}
	if _, err := app.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      scheduledApprovalTask.ID,
		Status:      "pending",
		RequestedBy: "automation_trigger:scheduled-review",
	}); err != nil {
		t.Fatalf("RequestApproval(scheduled) error = %v", err)
	}

	if _, err := app.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         "review-fixture-skill",
		Scope:            "project",
		ProjectID:        &project.ID,
		Status:           "review_required",
		ArtifactType:     "proposal",
		Summary:          "Review fixture skill artifact",
		OutputJSON:       `{"title":"Review fixture skill artifact"}`,
		RawOutput:        `{"title":"Review fixture skill artifact"}`,
		HandlerRef:       "fixture",
		ExecutionProfile: "test",
		PermissionsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("CreateSkillArtifact() error = %v", err)
	}

	contextTask, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "context-task",
		Title:       "Context task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(context) error = %v", err)
	}
	if _, err := (runtimeknowledge.Service{Store: app.Store}).ProposeContextPack(ctx, runtimeknowledge.ContextPackParams{
		TaskRef:    contextTask.Key,
		ProjectKey: project.Key,
	}); err != nil {
		t.Fatalf("ProposeContextPack() error = %v", err)
	}
	if _, err := app.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		TaskID:      &contextTask.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		MemoryType:  "memory_proposal",
		Summary:     "Review fixture memory proposal",
		DetailsJSON: `{"fields":{"approval":"pending","source":"review_fixture"}}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(memory proposal) error = %v", err)
	}

	if _, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "failed-task",
		Title:       "Failed task",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask(failed) error = %v", err)
	}

	if _, err := app.Store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		Severity:    "error",
		Status:      "open",
		Summary:     "Review recovery incident",
		DetailsJSON: `{"fault_key":"wake_packet_invalid","subject_key":"task:failed-task","decision_mode":"incident_only","next_action":"review wake packet evidence"}`,
	}); err != nil {
		t.Fatalf("OpenIncident(recovery) error = %v", err)
	}
}

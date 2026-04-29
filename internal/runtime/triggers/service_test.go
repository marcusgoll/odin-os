package triggers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestEvaluateDueMaterializesEnabledScheduleTriggerOnce(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute)
	store.Now = func() time.Time {
		return now
	}
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "due-nightly",
		RuleJSON:       `{"summary":"due proof"}`,
		RuleSummary:    "due proof",
		WorkItemTitle:  "Run due nightly",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue(first) error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateDue(first) = %+v, want one evaluated and one materialized", result)
	}

	trigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "due-nightly")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey() error = %v", err)
	}
	if trigger.LastWorkItemID == nil {
		t.Fatalf("LastWorkItemID = nil, want materialized Work Item")
	}
	if trigger.NextEligibleAt != nil {
		t.Fatalf("NextEligibleAt = %v, want cleared after one-shot due evaluation", trigger.NextEligibleAt)
	}
	task, err := store.GetTask(ctx, *trigger.LastWorkItemID)
	if err != nil {
		t.Fatalf("GetTask(last work item) error = %v", err)
	}
	if task.Status != "queued" || task.RequestedBy != "automation_trigger:due-nightly" {
		t.Fatalf("materialized task = %+v, want queued automation-trigger Work Item", task)
	}

	result, err = Service{Store: store}.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue(second) error = %v", err)
	}
	if result.Evaluated != 0 || result.Materialized != 0 {
		t.Fatalf("EvaluateDue(second) = %+v, want no duplicate due materialization", result)
	}

	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var materialized int
	for _, record := range records {
		if record.Type == events.EventAutomationTriggerMaterialized {
			materialized++
		}
	}
	if materialized != 1 {
		t.Fatalf("materialized events = %d, want 1", materialized)
	}
}

func TestEvaluateDueReschedulesRecurringCadenceFromDueWindow(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	currentNow := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	dueAt := currentNow.Add(-time.Minute)
	store.Now = func() time.Time {
		return currentNow
	}
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "recurring-nightly",
		RuleJSON:       `{"summary":"recurring proof","cadence":"15m"}`,
		RuleSummary:    "recurring proof",
		WorkItemTitle:  "Run recurring nightly",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, currentNow)
	if err != nil {
		t.Fatalf("EvaluateDue(first) error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateDue(first) = %+v, want one evaluated and one materialized", result)
	}

	trigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "recurring-nightly")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey(first) error = %v", err)
	}
	wantNext := dueAt.Add(15 * time.Minute)
	if trigger.NextEligibleAt == nil || !trigger.NextEligibleAt.Equal(wantNext) {
		t.Fatalf("NextEligibleAt = %v, want %s", trigger.NextEligibleAt, wantNext.Format(time.RFC3339))
	}

	result, err = Service{Store: store}.EvaluateDue(ctx, currentNow)
	if err != nil {
		t.Fatalf("EvaluateDue(before next window) error = %v", err)
	}
	if result.Evaluated != 0 || result.Materialized != 0 {
		t.Fatalf("EvaluateDue(before next window) = %+v, want no duplicate work before next window", result)
	}

	currentNow = wantNext
	result, err = Service{Store: store}.EvaluateDue(ctx, currentNow)
	if err != nil {
		t.Fatalf("EvaluateDue(second window) error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateDue(second window) = %+v, want next schedule window materialized", result)
	}

	trigger, err = store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "recurring-nightly")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey(second) error = %v", err)
	}
	wantFollowing := wantNext.Add(15 * time.Minute)
	if trigger.NextEligibleAt == nil || !trigger.NextEligibleAt.Equal(wantFollowing) {
		t.Fatalf("NextEligibleAt after second window = %v, want %s", trigger.NextEligibleAt, wantFollowing.Format(time.RFC3339))
	}
}

func TestEvaluateDueReschedulesCronRuleFromDueWindow(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	currentNow := time.Date(2026, 4, 25, 12, 1, 0, 0, time.UTC)
	dueAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return currentNow
	}
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "cron-quarter-hour",
		RuleJSON:       `{"summary":"cron proof","cron":"*/15 * * * *"}`,
		RuleSummary:    "cron proof",
		WorkItemTitle:  "Run cron proof",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, currentNow)
	if err != nil {
		t.Fatalf("EvaluateDue(first) error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 || result.Errored != 0 {
		t.Fatalf("EvaluateDue(first) = %+v, want one materialized cron window", result)
	}

	trigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "cron-quarter-hour")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey(first) error = %v", err)
	}
	wantNext := time.Date(2026, 4, 25, 12, 15, 0, 0, time.UTC)
	if trigger.NextEligibleAt == nil || !trigger.NextEligibleAt.Equal(wantNext) {
		t.Fatalf("NextEligibleAt = %v, want %s", trigger.NextEligibleAt, wantNext.Format(time.RFC3339))
	}
}

func TestEvaluateDueMarksInvalidTriggerRuleErroredAndContinues(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute)
	store.Now = func() time.Time {
		return now
	}
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "bad-cadence",
		RuleJSON:       `{"summary":"bad cadence","cadence":"bogus"}`,
		RuleSummary:    "bad cadence",
		WorkItemTitle:  "Bad cadence",
		NextEligibleAt: &dueAt,
	})
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "good-cadence",
		RuleJSON:       `{"summary":"good cadence"}`,
		RuleSummary:    "good cadence",
		WorkItemTitle:  "Good cadence",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 2 || result.Materialized != 1 || result.Errored != 1 {
		t.Fatalf("EvaluateDue() = %+v, want two evaluated, one materialized, one errored", result)
	}

	badTrigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "bad-cadence")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey(bad) error = %v", err)
	}
	if badTrigger.Status != "errored" {
		t.Fatalf("bad trigger status = %q, want errored", badTrigger.Status)
	}
	if badTrigger.LastWorkItemID != nil {
		t.Fatalf("bad trigger LastWorkItemID = %d, want no materialized Work Item", *badTrigger.LastWorkItemID)
	}
}

func openTriggerStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedTrigger(t *testing.T, ctx context.Context, store *sqlite.Store, params sqlite.UpsertAutomationTriggerParams) sqlite.AutomationTrigger {
	t.Helper()
	project, err := store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		project, err = store.CreateProject(ctx, sqlite.CreateProjectParams{
			Key:           "odin-core",
			Name:          "Odin Core",
			Scope:         "odin-core",
			GitRoot:       "/home/orchestrator/odin-os",
			DefaultBranch: "main",
			ManifestPath:  "config/projects.yaml",
		})
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}
	}
	params.WorkspaceID = "default"
	params.ProjectID = project.ID
	params.InitiativeKey = "odin-core"
	params.Kind = "schedule"
	params.Status = "enabled"
	trigger, err := store.UpsertAutomationTrigger(ctx, params)
	if err != nil {
		t.Fatalf("UpsertAutomationTrigger(%s) error = %v", params.Key, err)
	}
	return trigger
}

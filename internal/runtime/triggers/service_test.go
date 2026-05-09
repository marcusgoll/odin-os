package triggers

import (
	"context"
	"encoding/json"
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

	payload := lastAutomationTriggerMaterializedPayload(t, ctx, store)
	envelope := payload.Envelope
	if envelope.Source != "schedule" || envelope.TriggerType != "schedule" {
		t.Fatalf("envelope source/type = %q/%q, want schedule/schedule", envelope.Source, envelope.TriggerType)
	}
	if envelope.DedupeKey != "default:cron-quarter-hour:schedule:due-20260425t120000z" {
		t.Fatalf("envelope dedupe_key = %q, want deterministic schedule materialization key", envelope.DedupeKey)
	}
	if envelope.DueAt != dueAt.UTC().Format(time.RFC3339) {
		t.Fatalf("envelope due_at = %q, want %s", envelope.DueAt, dueAt.UTC().Format(time.RFC3339))
	}
	if envelope.OccurredAt != currentNow.UTC().Format(time.RFC3339) {
		t.Fatalf("envelope occurred_at = %q, want %s", envelope.OccurredAt, currentNow.UTC().Format(time.RFC3339))
	}
	if envelope.Schedule == nil || envelope.Schedule.Cron != "*/15 * * * *" || envelope.Schedule.Summary != "cron proof" {
		t.Fatalf("envelope schedule = %+v, want cron proof metadata", envelope.Schedule)
	}
	if envelope.RecoveryState != "not_started" {
		t.Fatalf("envelope recovery_state = %q, want not_started", envelope.RecoveryState)
	}
}

func TestEvaluateEventsMaterializesGovernanceTriggerWithEnvelope(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	service := Service{Store: store}
	store.Now = func() time.Time {
		return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	}
	trigger := seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "github-governance-event",
		Kind:           "event",
		RuleJSON:       `{"summary":"github governance proof","event_type":"external.github.issue","match_provider":"github","match_repo":"odin-os","execution_intent":"governance"}`,
		RuleSummary:    "github governance proof",
		WorkItemTitle:  "Review governance issue",
		NextEligibleAt: nil,
	})

	store.Now = func() time.Time {
		return trigger.CreatedAt.Add(time.Minute)
	}
	issue, err := store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
		ProjectID:  trigger.ProjectID,
		Provider:   "github",
		Repo:       "odin-os",
		Number:     42,
		Title:      "Governance issue",
		State:      "opened",
		SyncStatus: "event_received",
	})
	if err != nil {
		t.Fatalf("UpsertExternalIssue() error = %v", err)
	}
	if err := store.RecordExternalGitHubIssueEvent(ctx, sqlite.RecordExternalGitHubIssueEventParams{
		ProjectID:        trigger.ProjectID,
		ProjectKey:       trigger.InitiativeKey,
		ExternalIssueID:  issue.ID,
		Provider:         issue.Provider,
		Repo:             issue.Repo,
		Number:           issue.Number,
		Action:           "opened",
		Title:            issue.Title,
		ExternalEventKey: "github:issue:odin-os:42:opened",
	}); err != nil {
		t.Fatalf("RecordExternalGitHubIssueEvent() error = %v", err)
	}

	store.Now = func() time.Time {
		return trigger.CreatedAt.Add(2 * time.Minute)
	}
	result, err := service.EvaluateEvents(ctx)
	if err != nil {
		t.Fatalf("EvaluateEvents() error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateEvents() = %+v, want one event materialization", result)
	}

	payload := lastAutomationTriggerMaterializedPayload(t, ctx, store)
	envelope := payload.Envelope
	if envelope.Source != "event" || envelope.TriggerType != "event" {
		t.Fatalf("envelope source/type = %q/%q, want event/event", envelope.Source, envelope.TriggerType)
	}
	if envelope.DedupeKey != "default:github-governance-event:event:external-github-issue-odin-os-42-opened" {
		t.Fatalf("envelope dedupe_key = %q, want deterministic event materialization key", envelope.DedupeKey)
	}
	if envelope.Risk == nil || envelope.Risk.ExecutionIntent != "governance" || !envelope.Risk.ApprovalRequired {
		t.Fatalf("envelope risk = %+v, want governance approval-required metadata", envelope.Risk)
	}
	if envelope.SourceOccurredAt == "" {
		t.Fatalf("envelope source_occurred_at = empty, want source event timestamp")
	}
	if envelope.RecoveryState != "not_started" {
		t.Fatalf("envelope recovery_state = %q, want not_started", envelope.RecoveryState)
	}
	if payload.SourceEventID == nil || payload.SourceEventType != string(events.EventExternalGitHubIssue) {
		t.Fatalf("payload source event = %v/%q, want external GitHub issue provenance", payload.SourceEventID, payload.SourceEventType)
	}
}

func TestEvaluateDueUsesWorkspaceProfileQuietHoursByDefault(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	workspace := seedDefaultWorkspace(t, ctx, store)
	if _, err := store.UpsertWorkspaceProfile(ctx, sqlite.UpsertWorkspaceProfileParams{
		WorkspaceID:         workspace.ID,
		PreferencesJSON:     `{"quiet_hours":"02:00-06:00"}`,
		BoundariesJSON:      `{}`,
		CadenceDefaultsJSON: `{}`,
	}); err != nil {
		t.Fatalf("UpsertWorkspaceProfile() error = %v", err)
	}

	now := time.Date(2026, 5, 5, 3, 30, 0, 0, time.UTC)
	dueAt := time.Date(2026, 5, 5, 3, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "profile-quiet",
		RuleJSON:       `{"summary":"profile quiet proof","cadence":"1h"}`,
		RuleSummary:    "profile quiet proof",
		WorkItemTitle:  "Profile quiet proof",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 1 || result.Deferred != 1 || result.Materialized != 0 || len(result.Deferrals) != 1 {
		t.Fatalf("EvaluateDue() = %+v, want one profile quiet-hours deferral and no materialized work", result)
	}
	wantDeferredUntil := time.Date(2026, 5, 5, 6, 0, 0, 0, time.UTC)
	if !result.Deferrals[0].DeferredUntil.Equal(wantDeferredUntil) {
		t.Fatalf("DeferredUntil = %s, want %s", result.Deferrals[0].DeferredUntil, wantDeferredUntil)
	}
	trigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", "profile-quiet")
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey() error = %v", err)
	}
	if trigger.LastWorkItemID != nil {
		t.Fatalf("LastWorkItemID = %d, want no work item during quiet hours", *trigger.LastWorkItemID)
	}
	if trigger.NextEligibleAt == nil || !trigger.NextEligibleAt.Equal(wantDeferredUntil) {
		t.Fatalf("NextEligibleAt = %v, want deferred until %s", trigger.NextEligibleAt, wantDeferredUntil)
	}
}

func TestEvaluateDueBatchesCompatibleScheduleTriggersPreservingIntentAndAudit(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 5, 9, 30, 0, 0, time.UTC)
	firstDueAt := time.Date(2026, 5, 5, 9, 5, 0, 0, time.UTC)
	secondDueAt := time.Date(2026, 5, 5, 9, 20, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	batchRule := `{"summary":"batch proof","batch_key":"ops-review","batch_window":"1h","execution_intent":"governance"}`
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "batch-first",
		RuleJSON:       batchRule,
		RuleSummary:    "batch proof",
		WorkItemTitle:  "First batched proof",
		NextEligibleAt: &firstDueAt,
	})
	seedTrigger(t, ctx, store, sqlite.UpsertAutomationTriggerParams{
		Key:            "batch-second",
		RuleJSON:       batchRule,
		RuleSummary:    "batch proof",
		WorkItemTitle:  "Second batched proof",
		NextEligibleAt: &secondDueAt,
	})

	result, err := Service{Store: store}.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 2 || result.Materialized != 1 || result.Deferred != 0 || result.Errored != 0 || len(result.Results) != 2 {
		t.Fatalf("EvaluateDue() = %+v, want two evaluated triggers grouped into one work item", result)
	}
	firstTaskID := result.Results[0].WorkItem.ID
	if firstTaskID == 0 || result.Results[1].WorkItem.ID != firstTaskID {
		t.Fatalf("batched work item IDs = %d and %d, want same non-zero task", firstTaskID, result.Results[1].WorkItem.ID)
	}
	if result.Results[0].WorkItem.ExecutionIntent != "governance" || result.Results[0].WorkItem.ExecutionIntentSource != "trigger" {
		t.Fatalf("batched work intent = %q/%q, want governance/trigger", result.Results[0].WorkItem.ExecutionIntent, result.Results[0].WorkItem.ExecutionIntentSource)
	}
	if !result.Results[0].CreatedWorkItem || result.Results[1].CreatedWorkItem {
		t.Fatalf("CreatedWorkItem flags = %t/%t, want first create and second reuse", result.Results[0].CreatedWorkItem, result.Results[1].CreatedWorkItem)
	}
	for _, key := range []string{"batch-first", "batch-second"} {
		trigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", key)
		if err != nil {
			t.Fatalf("GetAutomationTriggerByWorkspaceKey(%s) error = %v", key, err)
		}
		if trigger.LastWorkItemID == nil || *trigger.LastWorkItemID != firstTaskID {
			t.Fatalf("%s LastWorkItemID = %v, want shared task %d", key, trigger.LastWorkItemID, firstTaskID)
		}
	}

	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[events.Type]int{}
	for _, record := range records {
		counts[record.Type]++
	}
	if counts[events.EventTaskCreated] != 1 {
		t.Fatalf("task.created count = %d, want one batched task", counts[events.EventTaskCreated])
	}
	if counts[events.EventAutomationTriggerMaterialized] != 2 {
		t.Fatalf("automation_trigger.materialized count = %d, want one event per source trigger", counts[events.EventAutomationTriggerMaterialized])
	}
	if counts[events.EventAutomationTriggerFireRequested] != 2 {
		t.Fatalf("automation_trigger.fire_requested count = %d, want one event per source trigger", counts[events.EventAutomationTriggerFireRequested])
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

func seedDefaultWorkspace(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Workspace {
	t.Helper()
	if workspace, err := store.GetWorkspaceByKey(ctx, "default"); err == nil {
		return workspace
	}
	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "default",
		Name:                "Default Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(default) error = %v", err)
	}
	return workspace
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
	if params.Kind == "" {
		params.Kind = "schedule"
	}
	params.Status = "enabled"
	trigger, err := store.UpsertAutomationTrigger(ctx, params)
	if err != nil {
		t.Fatalf("UpsertAutomationTrigger(%s) error = %v", params.Key, err)
	}
	return trigger
}

func lastAutomationTriggerMaterializedPayload(t *testing.T, ctx context.Context, store *sqlite.Store) events.AutomationTriggerMaterializedPayload {
	t.Helper()
	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Type != events.EventAutomationTriggerMaterialized {
			continue
		}
		var payload events.AutomationTriggerMaterializedPayload
		if err := json.Unmarshal(records[i].Payload, &payload); err != nil {
			t.Fatalf("decode materialized payload: %v", err)
		}
		return payload
	}
	t.Fatalf("missing %s event", events.EventAutomationTriggerMaterialized)
	return events.AutomationTriggerMaterializedPayload{}
}

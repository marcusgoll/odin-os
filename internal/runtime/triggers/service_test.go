package triggers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/core/projects"
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
		RuleJSON:       `{"summary":"due proof","execution_intent":"read_only"}`,
		RuleSummary:    "due proof",
		WorkItemTitle:  "Run due nightly",
		NextEligibleAt: &dueAt,
	})

	service := Service{Store: store, Registry: writeTriggerRegistry(t)}
	result, err := service.EvaluateDue(ctx, now)
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

	result, err = service.EvaluateDue(ctx, now)
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

func TestEvaluateDueMaterializesHighRiskIntentAsBlockedApprovalWork(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC)
	dueAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	service := Service{
		Store:    store,
		Registry: writeTriggerRegistry(t),
	}

	for _, intent := range []string{"governance", "destructive"} {
		if _, err := service.Upsert(ctx, UpsertParams{
			Key:             intent + "-approval",
			InitiativeKey:   "odin-core",
			Kind:            "schedule",
			Status:          "enabled",
			RuleSummary:     intent + " approval parity",
			WorkItemTitle:   "Neutral periodic check",
			NextEligibleAt:  &dueAt,
			ExecutionIntent: intent,
		}); err != nil {
			t.Fatalf("Upsert(%s) error = %v", intent, err)
		}
	}

	result, err := service.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 2 || result.Materialized != 2 {
		t.Fatalf("EvaluateDue() = %+v, want two high-risk materializations", result)
	}
	if len(result.Results) != 2 {
		t.Fatalf("EvaluateDue().Results = %+v, want materialized work item", result.Results)
	}

	for _, item := range result.Results {
		task, err := store.GetTask(ctx, item.WorkItem.ID)
		if err != nil {
			t.Fatalf("GetTask(materialized) error = %v", err)
		}
		if task.ExecutionIntentSource != "trigger" {
			t.Fatalf("task intent source = %q, want trigger", task.ExecutionIntentSource)
		}
		if task.ExecutionIntent != "governance" && task.ExecutionIntent != "destructive" {
			t.Fatalf("task intent = %q, want governance or destructive", task.ExecutionIntent)
		}
		if task.Status != "blocked" {
			t.Fatalf("task status = %q, want blocked", task.Status)
		}
		if task.BlockedReason != "approval_required" {
			t.Fatalf("task blocked reason = %q, want approval_required", task.BlockedReason)
		}
		approval, err := store.GetLatestTaskApproval(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetLatestTaskApproval() error = %v", err)
		}
		if approval.Status != "pending" {
			t.Fatalf("approval status = %q, want pending", approval.Status)
		}
	}
}

func TestEvaluateDueAppliesJobAdmissionForInferredHighRiskTriggerWork(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC)
	dueAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	service := Service{
		Store:    store,
		Registry: writeTriggerRegistry(t),
	}

	if _, err := service.Upsert(ctx, UpsertParams{
		Key:            "inferred-governance-trigger",
		InitiativeKey:  "odin-core",
		Kind:           "schedule",
		Status:         "enabled",
		RuleSummary:    "inferred high-risk trigger",
		WorkItemTitle:  "Governance transition review",
		NextEligibleAt: &dueAt,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	result, err := service.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateDue() = %+v, want one inferred high-risk materialization", result)
	}

	task, err := store.GetTask(ctx, result.Results[0].WorkItem.ID)
	if err != nil {
		t.Fatalf("GetTask(materialized) error = %v", err)
	}
	if task.ExecutionIntent != "governance" || task.ExecutionIntentSource != "safety_classifier" {
		t.Fatalf("task stored intent = %q/%q, want governance/safety_classifier", task.ExecutionIntent, task.ExecutionIntentSource)
	}
	if task.Status != "blocked" {
		t.Fatalf("task status = %q, want blocked", task.Status)
	}
	if task.BlockedReason != "approval_required" {
		t.Fatalf("task blocked reason = %q, want approval_required", task.BlockedReason)
	}
	approval, err := store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("approval status = %q, want pending", approval.Status)
	}
}

func TestEvaluateDueMaterializesReadOnlyIntentWithoutApprovalBlock(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC)
	dueAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	service := Service{
		Store:    store,
		Registry: writeTriggerRegistry(t),
	}

	if _, err := service.Upsert(ctx, UpsertParams{
		Key:             "read-only-trigger",
		InitiativeKey:   "odin-core",
		Kind:            "schedule",
		Status:          "enabled",
		RuleSummary:     "read-only parity",
		WorkItemTitle:   "Inspect status",
		NextEligibleAt:  &dueAt,
		ExecutionIntent: "read_only",
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	result, err := service.EvaluateDue(ctx, now)
	if err != nil {
		t.Fatalf("EvaluateDue() error = %v", err)
	}
	if result.Evaluated != 1 || result.Materialized != 1 {
		t.Fatalf("EvaluateDue() = %+v, want one read-only materialization", result)
	}
	task, err := store.GetTask(ctx, result.Results[0].WorkItem.ID)
	if err != nil {
		t.Fatalf("GetTask(materialized) error = %v", err)
	}
	if task.ExecutionIntent != "read_only" || task.ExecutionIntentSource != "trigger" {
		t.Fatalf("task intent = %q/%q, want read_only/trigger", task.ExecutionIntent, task.ExecutionIntentSource)
	}
	if task.Status != "queued" || task.BlockedReason != "" {
		t.Fatalf("task = %+v, want queued without approval block", task)
	}
	if _, err := store.GetLatestTaskApproval(ctx, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetLatestTaskApproval() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExternalAdapterEventsCreateIntakeOnly(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()
	service := Service{Store: store, Registry: writeTriggerRegistry(t)}

	result, err := service.IngestGitHubIssue(ctx, GitHubIssueIngestParams{
		ProjectKey: "odin-core",
		Repo:       "owner/repo",
		Number:     42,
		Action:     "opened",
		Title:      "Fix failing CI",
		URL:        "https://github.com/owner/repo/issues/42",
	})
	if err != nil {
		t.Fatalf("IngestGitHubIssue() error = %v", err)
	}
	if result.EventType != "external.github.issue" {
		t.Fatalf("event type = %q, want external.github.issue", result.EventType)
	}
	if countTasks(t, ctx, store) != 0 {
		t.Fatalf("external issue ingest created executable tasks")
	}

	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var externalEvents int
	for _, record := range records {
		if record.Type == events.EventExternalGitHubIssue {
			externalEvents++
		}
	}
	if externalEvents != 1 {
		t.Fatalf("external github issue events = %d, want 1", externalEvents)
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
		RuleJSON:       `{"summary":"recurring proof","cadence":"15m","execution_intent":"read_only"}`,
		RuleSummary:    "recurring proof",
		WorkItemTitle:  "Run recurring nightly",
		NextEligibleAt: &dueAt,
	})

	service := Service{Store: store, Registry: writeTriggerRegistry(t)}
	result, err := service.EvaluateDue(ctx, currentNow)
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

	result, err = service.EvaluateDue(ctx, currentNow)
	if err != nil {
		t.Fatalf("EvaluateDue(before next window) error = %v", err)
	}
	if result.Evaluated != 0 || result.Materialized != 0 {
		t.Fatalf("EvaluateDue(before next window) = %+v, want no duplicate work before next window", result)
	}

	currentNow = wantNext
	result, err = service.EvaluateDue(ctx, currentNow)
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
		RuleJSON:       `{"summary":"cron proof","cron":"*/15 * * * *","execution_intent":"read_only"}`,
		RuleSummary:    "cron proof",
		WorkItemTitle:  "Run cron proof",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store, Registry: writeTriggerRegistry(t)}.EvaluateDue(ctx, currentNow)
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

	service := Service{
		Store:    store,
		Registry: writeTriggerRegistry(t),
	}
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

func TestPreviewTriggerEventReportsCanonicalMatchWithoutMaterializing(t *testing.T) {
	ctx := context.Background()
	store := openTriggerStore(t)
	defer store.Close()

	service := Service{
		Store:    store,
		Registry: writeTriggerRegistry(t),
	}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	trigger, err := service.Upsert(ctx, UpsertParams{
		Key:             "gh-opened-proof",
		InitiativeKey:   "odin-core",
		Kind:            "event",
		Status:          "enabled",
		EventType:       CanonicalGitHubIssueEventType,
		MatchProvider:   "github",
		MatchRepo:       "marcusgoll/odin-os",
		RuleSummary:     "github opened proof",
		WorkItemTitle:   "Review opened issue",
		ExecutionIntent: "governance",
	})
	if err != nil {
		t.Fatalf("Upsert(event trigger) error = %v", err)
	}

	store.Now = func() time.Time {
		return now.Add(time.Minute)
	}
	if _, err := service.IngestGitHubIssue(ctx, GitHubIssueIngestParams{
		ProjectKey: "odin-core",
		Repo:       "marcusgoll/odin-os",
		Number:     123,
		Action:     "opened",
		Title:      "Issue opened",
		Labels:     "bug",
	}); err != nil {
		t.Fatalf("IngestGitHubIssue() error = %v", err)
	}

	beforeTasks := countTasks(t, ctx, store)
	result, err := service.PreviewTrigger(ctx, PreviewTriggerParams{
		WorkspaceID: "default",
		Key:         trigger.Key,
		Now:         now.Add(2 * time.Minute),
		Source:      "events",
	})
	if err != nil {
		t.Fatalf("PreviewTrigger(event) error = %v", err)
	}
	if result.Evaluated != 1 || result.WouldRun != 1 {
		t.Fatalf("PreviewTrigger(event) = %+v, want one would-run decision", result)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("PreviewTrigger(event).Decisions = %d, want 1", len(result.Decisions))
	}
	decision := result.Decisions[0]
	if decision.EventType != CanonicalGitHubIssueEventType || decision.CandidateEvents != 1 || len(decision.MatchedEvents) != 1 {
		t.Fatalf("event decision = %+v, want one canonical event match", decision)
	}
	if !decision.ApprovalRequired {
		t.Fatalf("decision approval required = false, want true for governance event trigger")
	}
	if tasks := countTasks(t, ctx, store); tasks != beforeTasks {
		t.Fatalf("task count after preview = %d, want unchanged %d", tasks, beforeTasks)
	}
	if materializations := countAutomationTriggerMaterializations(t, ctx, store); materializations != 0 {
		t.Fatalf("materialization count after preview = %d, want 0", materializations)
	}
	afterTrigger, err := store.GetAutomationTriggerByWorkspaceKey(ctx, "default", trigger.Key)
	if err != nil {
		t.Fatalf("GetAutomationTriggerByWorkspaceKey() error = %v", err)
	}
	if afterTrigger.LastWorkItemID != nil || afterTrigger.LastMaterializationKey != "" {
		t.Fatalf("trigger after preview = %+v, want no materialized work", afterTrigger)
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

	result, err := Service{Store: store, Registry: writeTriggerRegistry(t)}.EvaluateDue(ctx, now)
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

	result, err := Service{Store: store, Registry: writeTriggerRegistry(t)}.EvaluateDue(ctx, now)
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
		RuleJSON:       `{"summary":"good cadence","execution_intent":"read_only"}`,
		RuleSummary:    "good cadence",
		WorkItemTitle:  "Good cadence",
		NextEligibleAt: &dueAt,
	})

	result, err := Service{Store: store, Registry: writeTriggerRegistry(t)}.EvaluateDue(ctx, now)
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

func countTasks(t *testing.T, ctx context.Context, store *sqlite.Store) int {
	t.Helper()
	row := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return count
}

func countAutomationTriggerMaterializations(t *testing.T, ctx context.Context, store *sqlite.Store) int {
	t.Helper()
	row := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM automation_trigger_materializations`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count automation trigger materializations: %v", err)
	}
	return count
}

func writeTriggerRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	gitRoot := filepath.Join(root, "odin-core")
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	configYAML := fmt.Sprintf(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: %s
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`, gitRoot)

	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}
	return registry
}

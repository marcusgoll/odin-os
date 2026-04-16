package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/projections"
)

func TestStoreMigrateLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() first run error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() second run error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-03",
		Title:       "Implement runtime store",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, err = store.FinishRun(ctx, FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "store baseline complete",
	})
	if err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "completed",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(completed) error = %v", err)
	}

	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	approval, err = store.ResolveApproval(ctx, ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe to proceed",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "transient issue observed",
		DetailsJSON: `{"stage":"verification"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	recovery, err := store.StartRecovery(ctx, StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "retry-once",
		DetailsJSON: `{"attempt":1}`,
	})
	if err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	recovery, err = store.CompleteRecovery(ctx, CompleteRecoveryParams{
		RecoveryID:  recovery.ID,
		Status:      "completed",
		DetailsJSON: `{"result":"success"}`,
	})
	if err != nil {
		t.Fatalf("CompleteRecovery() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "phase 02 baseline",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:      &task.ID,
		RunID:       &run.ID,
		PacketKind:  "wake",
		Summary:     "handoff state",
		PayloadJSON: `{"task":"phase-03"}`,
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	allEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(all) error = %v", err)
	}

	if len(allEvents) != 14 {
		t.Fatalf("ListEvents(all) len = %d, want 14", len(allEvents))
	}

	if allEvents[0].Type != runtimeevents.EventProjectCreated {
		t.Fatalf("first event type = %q, want %q", allEvents[0].Type, runtimeevents.EventProjectCreated)
	}

	packetEventPayload, err := runtimeevents.DecodePayload[runtimeevents.ContextPacketCreatedPayload](allEvents[len(allEvents)-1].Payload)
	if err != nil {
		t.Fatalf("DecodePayload(ContextPacketCreatedPayload) error = %v", err)
	}
	if packetEventPayload.PacketScope != "task_wake_packet" {
		t.Fatalf("context packet event scope = %q, want %q", packetEventPayload.PacketScope, "task_wake_packet")
	}
	if packetEventPayload.Trigger != "handoff" {
		t.Fatalf("context packet event trigger = %q, want %q", packetEventPayload.Trigger, "handoff")
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}
	if len(views) != 1 || views[0].Status != "completed" {
		t.Fatalf("task views = %+v, want one completed task", views)
	}

	pendingApprovals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(pendingApprovals) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(pendingApprovals))
	}

	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRunSummaryViews() error = %v", err)
	}
	if len(runViews) != 1 || runViews[0].Status != "completed" {
		t.Fatalf("run views = %+v, want one completed run", runViews)
	}

	projectViews, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}
	if len(projectViews) != 1 || projectViews[0].TaskCount != 1 {
		t.Fatalf("project views = %+v, want one project with one task", projectViews)
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations count query error = %v", err)
	}
	if migrationCount != 7 {
		t.Fatalf("schema_migrations count = %d, want 7", migrationCount)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer reopened.Close()

	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopen) error = %v", err)
	}

	gotTask, err := reopened.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "completed")
	}

	gotRun, err := reopened.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "completed" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "completed")
	}

	gotApproval, err := reopened.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "approved" {
		t.Fatalf("GetApproval().Status = %q, want %q", gotApproval.Status, "approved")
	}
}

func TestProjectTransitionStateLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	transition, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "inventory",
		Controller:         "legacy_odin",
		LimitedActionsJSON: "",
		Notes:              "initial enrollment",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(inventory) error = %v", err)
	}

	if transition.State != "inventory" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "inventory")
	}
	if transition.Controller != "legacy_odin" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "legacy_odin")
	}

	transition, err = store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "limited_action",
		Controller:         "odin_os",
		LimitedActionsJSON: `["isolated_mutation"]`,
		Notes:              "allow proposal work only",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(limited_action) error = %v", err)
	}

	if transition.State != "limited_action" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "limited_action")
	}
	if transition.Controller != "odin_os" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "odin_os")
	}
	if transition.LimitedActionsJSON != `["isolated_mutation"]` {
		t.Fatalf("transition.LimitedActionsJSON = %q, want %q", transition.LimitedActionsJSON, `["isolated_mutation"]`)
	}

	got, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectTransition() error = %v", err)
	}

	if got.State != "limited_action" {
		t.Fatalf("GetProjectTransition().State = %q, want %q", got.State, "limited_action")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var transitionEvents int
	for _, event := range projectEvents {
		if event.Type == runtimeevents.EventProjectTransitionChanged {
			transitionEvents++
		}
	}
	if transitionEvents != 2 {
		t.Fatalf("transition event count = %d, want 2", transitionEvents)
	}
}

func TestProjectTransitionReportsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:  project.ID,
		State:      "compare",
		Controller: "legacy_odin",
		ChangedBy:  "operator",
		Notes:      "compare before cutover",
	}); err != nil {
		t.Fatalf("SetProjectTransition(compare) error = %v", err)
	}

	shadowReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "shadow_observation",
		Summary:     "legacy run observed",
		DetailsJSON: `{"task":"deploy","status":"completed"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(shadow) error = %v", err)
	}

	compareReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "compare_report",
		Summary:     "decision mismatch",
		DetailsJSON: `{"legacy_summary":"ship","odin_summary":"hold","verdict":"mismatch"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(compare) error = %v", err)
	}

	if shadowReport.ID == compareReport.ID {
		t.Fatalf("report ids should differ, both were %d", shadowReport.ID)
	}

	reports, err := store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("ListProjectTransitionReports() len = %d, want 2", len(reports))
	}
	if reports[0].ReportType != "shadow_observation" {
		t.Fatalf("reports[0].ReportType = %q, want %q", reports[0].ReportType, "shadow_observation")
	}
	if reports[1].ReportType != "compare_report" {
		t.Fatalf("reports[1].ReportType = %q, want %q", reports[1].ReportType, "compare_report")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var shadowEvents int
	var compareEvents int
	for _, event := range projectEvents {
		switch event.Type {
		case runtimeevents.EventProjectShadowObservationRecorded:
			shadowEvents++
		case runtimeevents.EventProjectCompareReportRecorded:
			compareEvents++
		}
	}

	if shadowEvents != 1 {
		t.Fatalf("shadow event count = %d, want 1", shadowEvents)
	}
	if compareEvents != 1 {
		t.Fatalf("compare event count = %d, want 1", compareEvents)
	}
}

func TestLearningProposalLifecycleSupportsEvaluationPromotionAndRollback(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	firstProposal, err := store.CreateLearningProposal(ctx, CreateLearningProposalParams{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer low-latency primary route",
		Hypothesis:        "Lower latency without more policy violations",
		ChangePayloadJSON: `{"executor":"codex","priority":10}`,
		CreatedBy:         "odin",
		Status:            "draft",
	})
	if err != nil {
		t.Fatalf("CreateLearningProposal(first) error = %v", err)
	}

	firstProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: firstProposal.ID,
		Status:     "submitted",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(submitted) error = %v", err)
	}

	firstEvaluation, err := store.RecordLearningEvaluation(ctx, RecordLearningEvaluationParams{
		ProposalID:           firstProposal.ID,
		FixtureKey:           "router-latency-fixture",
		Mode:                 "replay",
		Score:                0.82,
		BaselineSummaryJSON:  `{"success_rate":0.93,"latency_ms":220,"policy_violations":0}`,
		CandidateSummaryJSON: `{"success_rate":0.94,"latency_ms":180,"policy_violations":0}`,
		ResultSummary:        "candidate improved latency while preserving policy compliance",
		Outcome:              "approved",
	})
	if err != nil {
		t.Fatalf("RecordLearningEvaluation(first) error = %v", err)
	}

	if firstEvaluation.Outcome != "approved" {
		t.Fatalf("first evaluation outcome = %q, want %q", firstEvaluation.Outcome, "approved")
	}

	firstProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: firstProposal.ID,
		Status:     "approved",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(approved) error = %v", err)
	}

	firstPromotion, err := store.PromoteLearningProposal(ctx, PromoteLearningProposalParams{
		ProposalID: firstProposal.ID,
		PromotedBy: "operator",
	})
	if err != nil {
		t.Fatalf("PromoteLearningProposal(first) error = %v", err)
	}

	if firstPromotion.Status != "active" {
		t.Fatalf("first promotion status = %q, want %q", firstPromotion.Status, "active")
	}

	secondProposal, err := store.CreateLearningProposal(ctx, CreateLearningProposalParams{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer lower-cost route",
		Hypothesis:        "Lower cost while keeping success rate stable",
		ChangePayloadJSON: `{"executor":"openai_api","priority":20}`,
		CreatedBy:         "odin",
		Status:            "draft",
	})
	if err != nil {
		t.Fatalf("CreateLearningProposal(second) error = %v", err)
	}

	secondProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: secondProposal.ID,
		Status:     "submitted",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(second submitted) error = %v", err)
	}

	if _, err := store.RecordLearningEvaluation(ctx, RecordLearningEvaluationParams{
		ProposalID:           secondProposal.ID,
		FixtureKey:           "router-cost-fixture",
		Mode:                 "sandbox",
		Score:                0.87,
		BaselineSummaryJSON:  `{"success_rate":0.94,"cost":0.021,"violations":0}`,
		CandidateSummaryJSON: `{"success_rate":0.94,"cost":0.015,"violations":0}`,
		ResultSummary:        "candidate reduced cost without quality regression",
		Outcome:              "approved",
	}); err != nil {
		t.Fatalf("RecordLearningEvaluation(second) error = %v", err)
	}

	secondProposal, err = store.UpdateLearningProposalStatus(ctx, UpdateLearningProposalStatusParams{
		ProposalID: secondProposal.ID,
		Status:     "approved",
	})
	if err != nil {
		t.Fatalf("UpdateLearningProposalStatus(second approved) error = %v", err)
	}

	secondPromotion, err := store.PromoteLearningProposal(ctx, PromoteLearningProposalParams{
		ProposalID: secondProposal.ID,
		PromotedBy: "operator",
	})
	if err != nil {
		t.Fatalf("PromoteLearningProposal(second) error = %v", err)
	}

	if secondPromotion.Status != "active" {
		t.Fatalf("second promotion status = %q, want %q", secondPromotion.Status, "active")
	}
	if secondPromotion.SupersedesPromotionID == nil || *secondPromotion.SupersedesPromotionID != firstPromotion.ID {
		t.Fatalf("second promotion supersedes = %v, want %d", secondPromotion.SupersedesPromotionID, firstPromotion.ID)
	}

	activePromotions, err := store.ListActiveLearningPromotions(ctx)
	if err != nil {
		t.Fatalf("ListActiveLearningPromotions() error = %v", err)
	}
	if len(activePromotions) != 1 || activePromotions[0].ID != secondPromotion.ID {
		t.Fatalf("active promotions = %+v, want second promotion %d", activePromotions, secondPromotion.ID)
	}

	rolledBack, err := store.RollbackLearningPromotion(ctx, RollbackLearningPromotionParams{
		PromotionID:    secondPromotion.ID,
		RolledBackBy:   "operator",
		RollbackReason: "cost win was too narrow under review",
	})
	if err != nil {
		t.Fatalf("RollbackLearningPromotion() error = %v", err)
	}

	if rolledBack.Status != "rolled_back" {
		t.Fatalf("rolled back promotion status = %q, want %q", rolledBack.Status, "rolled_back")
	}

	activePromotions, err = store.ListActiveLearningPromotions(ctx)
	if err != nil {
		t.Fatalf("ListActiveLearningPromotions(after rollback) error = %v", err)
	}
	if len(activePromotions) != 1 || activePromotions[0].ID != firstPromotion.ID {
		t.Fatalf("active promotions after rollback = %+v, want first promotion %d", activePromotions, firstPromotion.ID)
	}

	firstPromotionAfterRollback, err := store.GetLearningPromotion(ctx, firstPromotion.ID)
	if err != nil {
		t.Fatalf("GetLearningPromotion(first) error = %v", err)
	}
	if firstPromotionAfterRollback.Status != "active" {
		t.Fatalf("first promotion after rollback status = %q, want %q", firstPromotionAfterRollback.Status, "active")
	}

	evaluations, err := store.ListLearningEvaluations(ctx, firstProposal.ID)
	if err != nil {
		t.Fatalf("ListLearningEvaluations(first proposal) error = %v", err)
	}
	if len(evaluations) != 1 {
		t.Fatalf("ListLearningEvaluations(first proposal) len = %d, want 1", len(evaluations))
	}

	allEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(all) error = %v", err)
	}

	counts := make(map[runtimeevents.Type]int)
	for _, event := range allEvents {
		counts[event.Type]++
	}

	if counts[runtimeevents.EventLearningProposalCreated] != 2 {
		t.Fatalf("learning.proposal_created count = %d, want 2", counts[runtimeevents.EventLearningProposalCreated])
	}
	if counts[runtimeevents.EventLearningProposalSubmitted] != 2 {
		t.Fatalf("learning.proposal_submitted count = %d, want 2", counts[runtimeevents.EventLearningProposalSubmitted])
	}
	if counts[runtimeevents.EventLearningEvaluationRecorded] != 2 {
		t.Fatalf("learning.evaluation_recorded count = %d, want 2", counts[runtimeevents.EventLearningEvaluationRecorded])
	}
	if counts[runtimeevents.EventLearningPromotionApplied] != 2 {
		t.Fatalf("learning.promotion_applied count = %d, want 2", counts[runtimeevents.EventLearningPromotionApplied])
	}
	if counts[runtimeevents.EventLearningPromotionRolledBack] != 1 {
		t.Fatalf("learning.promotion_rolled_back count = %d, want 1", counts[runtimeevents.EventLearningPromotionRolledBack])
	}
}

func TestRuntimeStateStoreLifecycleAndHeartbeat(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "runtime-state.db")
	defer store.Close()

	bootAt := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	readyAt := bootAt.Add(90 * time.Second)
	heartbeatAt := readyAt.Add(45 * time.Second)
	stoppedAt := heartbeatAt.Add(30 * time.Second)

	state, err := store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:          "boot-1",
		Status:          "booting",
		PID:             1234,
		StartedAt:       bootAt,
		LastHeartbeatAt: bootAt,
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(booting) error = %v", err)
	}
	if state.SingletonKey != "primary" {
		t.Fatalf("SingletonKey = %q, want %q", state.SingletonKey, "primary")
	}
	if state.Status != "booting" {
		t.Fatalf("Status = %q, want %q", state.Status, "booting")
	}

	state, err = store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:          "boot-1",
		Status:          "ready",
		PID:             1234,
		StartedAt:       bootAt,
		ReadyAt:         &readyAt,
		LastHeartbeatAt: readyAt,
		UpdatedAt:       readyAt,
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(ready) error = %v", err)
	}
	if state.ReadyAt == nil || !state.ReadyAt.Equal(readyAt) {
		t.Fatalf("ReadyAt = %v, want %v", state.ReadyAt, readyAt)
	}

	store.Now = func() time.Time { return heartbeatAt }
	state, err = store.UpdateRuntimeHeartbeat(ctx)
	if err != nil {
		t.Fatalf("UpdateRuntimeHeartbeat() error = %v", err)
	}
	if !state.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("LastHeartbeatAt = %v, want %v", state.LastHeartbeatAt, heartbeatAt)
	}

	state, err = store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:             "boot-1",
		Status:             "stopped",
		PID:                1234,
		StartedAt:          bootAt,
		ReadyAt:            &readyAt,
		LastHeartbeatAt:    heartbeatAt,
		LastShutdownReason: "operator requested shutdown",
		UpdatedAt:          stoppedAt,
		EventReason:        "operator requested shutdown",
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(stopped) error = %v", err)
	}
	if state.LastShutdownReason != "operator requested shutdown" {
		t.Fatalf("LastShutdownReason = %q, want %q", state.LastShutdownReason, "operator requested shutdown")
	}

	got, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("GetRuntimeState().Status = %q, want %q", got.Status, "stopped")
	}
	if !got.LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("GetRuntimeState().LastHeartbeatAt = %v, want %v", got.LastHeartbeatAt, heartbeatAt)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var lifecycleStatuses []string
	var sawHeartbeat bool
	for _, event := range events {
		switch event.Type {
		case runtimeevents.EventServiceLifecycleChanged:
			payload, err := runtimeevents.DecodePayload[runtimeevents.ServiceLifecyclePayload](event.Payload)
			if err != nil {
				t.Fatalf("DecodePayload(ServiceLifecyclePayload) error = %v", err)
			}
			lifecycleStatuses = append(lifecycleStatuses, payload.Status)
		case runtimeevents.EventServiceHeartbeatRecorded:
			sawHeartbeat = true
		}
	}

	if len(lifecycleStatuses) != 3 {
		t.Fatalf("lifecycle event count = %d, want %d", len(lifecycleStatuses), 3)
	}
	if lifecycleStatuses[0] != "booting" || lifecycleStatuses[1] != "ready" || lifecycleStatuses[2] != "stopped" {
		t.Fatalf("lifecycle statuses = %v, want [booting ready stopped]", lifecycleStatuses)
	}
	if !sawHeartbeat {
		t.Fatalf("expected service heartbeat event, got %+v", events)
	}
}

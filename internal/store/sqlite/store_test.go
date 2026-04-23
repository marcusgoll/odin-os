package sqlite

import (
	"context"
	"encoding/json"
	"errors"
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

	var workspaceTableCount int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name IN ('workspaces', 'workspace_policies')
	`).Scan(&workspaceTableCount); err != nil {
		t.Fatalf("workspace table count query error = %v", err)
	}
	if workspaceTableCount != 2 {
		t.Fatalf("workspace table count = %d, want 2", workspaceTableCount)
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

func TestUpdateTaskStatusUpdatesFieldsWhenStatusUnchanged(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "update-task-status-unchanged.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "update-task-status-unchanged",
		Name:          "Update Task Status Unchanged",
		Scope:         "project",
		GitRoot:       "/tmp/update-task-status-unchanged",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-unchanged-task",
		Title:       "Refresh task state",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "blocked",
		Summary:        "first rerun summary",
		TerminalReason: "swarm_results_pending",
		ArtifactsJSON:  `[{"type":"swarm_aggregation","summary":"first rerun summary","confidence":0.42}]`,
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(first rerun) error = %v", err)
	}
	if task.Summary != "first rerun summary" {
		t.Fatalf("first update summary = %q, want first rerun summary", task.Summary)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "blocked",
		Summary:        "second rerun summary",
		TerminalReason: "swarm_review_gate_pending_verifier",
		ArtifactsJSON:  `[{"type":"swarm_aggregation","summary":"second rerun summary","confidence":0.87}]`,
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(second rerun) error = %v", err)
	}
	if task.Summary != "second rerun summary" {
		t.Fatalf("second update summary = %q, want second rerun summary", task.Summary)
	}
	if task.TerminalReason != "swarm_review_gate_pending_verifier" {
		t.Fatalf("second update terminal reason = %q, want swarm_review_gate_pending_verifier", task.TerminalReason)
	}
	if task.ArtifactsJSON != `[{"type":"swarm_aggregation","summary":"second rerun summary","confidence":0.87}]` {
		t.Fatalf("second update artifacts JSON = %q, want updated envelope", task.ArtifactsJSON)
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
	}, RuntimeStateWriteOptions{})
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
	}, RuntimeStateWriteOptions{ExpectedBootID: "boot-1"})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(ready) error = %v", err)
	}
	if state.ReadyAt == nil || !state.ReadyAt.Equal(readyAt) {
		t.Fatalf("ReadyAt = %v, want %v", state.ReadyAt, readyAt)
	}

	store.Now = func() time.Time { return heartbeatAt }
	state, err = store.UpdateRuntimeHeartbeat(ctx, "boot-1")
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
	}, RuntimeStateWriteOptions{
		ExpectedBootID: "boot-1",
		EventReason:    "operator requested shutdown",
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

func TestRuntimeStateStoreRejectsStaleSameBootSnapshot(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "runtime-state-stale-snapshot.db")
	defer store.Close()

	bootAt := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	degradedAt := bootAt.Add(1 * time.Minute)
	staleWriteAt := degradedAt.Add(1 * time.Minute)

	booting, err := store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:          "boot-1",
		Status:          "booting",
		PID:             1234,
		StartedAt:       bootAt,
		LastHeartbeatAt: bootAt,
	}, RuntimeStateWriteOptions{})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(booting) error = %v", err)
	}

	snapshot, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState(snapshot) error = %v", err)
	}

	degraded, err := store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:          "boot-1",
		Status:          "degraded",
		PID:             booting.PID,
		StartedAt:       booting.StartedAt,
		LastHeartbeatAt: degradedAt,
		LastError:       "dependency stale",
		UpdatedAt:       degradedAt,
	}, RuntimeStateWriteOptions{
		ExpectedBootID:    "boot-1",
		ExpectedUpdatedAt: snapshot.UpdatedAt,
		EventReason:       "dependency stale",
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeState(degraded) error = %v", err)
	}

	_, err = store.UpsertRuntimeState(ctx, UpsertRuntimeStateParams{
		BootID:          "boot-1",
		Status:          "ready",
		PID:             snapshot.PID,
		StartedAt:       snapshot.StartedAt,
		ReadyAt:         &staleWriteAt,
		LastHeartbeatAt: staleWriteAt,
		LastError:       snapshot.LastError,
		UpdatedAt:       staleWriteAt,
	}, RuntimeStateWriteOptions{
		ExpectedBootID:    "boot-1",
		ExpectedUpdatedAt: snapshot.UpdatedAt,
	})
	if !errors.Is(err, ErrRuntimeStateConcurrentUpdate) {
		t.Fatalf("UpsertRuntimeState(stale snapshot) error = %v, want %v", err, ErrRuntimeStateConcurrentUpdate)
	}

	got, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "degraded" {
		t.Fatalf("GetRuntimeState().Status = %q, want %q", got.Status, "degraded")
	}
	if got.LastError != "dependency stale" {
		t.Fatalf("GetRuntimeState().LastError = %q, want %q", got.LastError, "dependency stale")
	}
	if !got.UpdatedAt.Equal(degraded.UpdatedAt) {
		t.Fatalf("GetRuntimeState().UpdatedAt = %v, want %v", got.UpdatedAt, degraded.UpdatedAt)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var lifecycleCount int
	for _, event := range events {
		if event.Type == runtimeevents.EventServiceLifecycleChanged {
			lifecycleCount++
		}
	}
	if lifecycleCount != 2 {
		t.Fatalf("lifecycle event count = %d, want %d", lifecycleCount, 2)
	}
}

func TestTaskQueueDefaults(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "task-queue-defaults.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "queue-defaults",
		Name:          "Queue Defaults",
		Scope:         "project",
		GitRoot:       "/tmp/queue-defaults",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "default-queue-task",
		Title:       "Check queue defaults",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if !task.NextEligibleAt.IsZero() {
		t.Fatalf("NextEligibleAt = %v, want zero time", task.NextEligibleAt)
	}
	if task.Priority != 100 {
		t.Fatalf("Priority = %d, want 100", task.Priority)
	}
	if task.LastError != "" {
		t.Fatalf("LastError = %q, want empty", task.LastError)
	}
	if task.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", task.RetryCount)
	}
	if task.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", task.MaxAttempts)
	}
	if task.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty", task.BlockedReason)
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("ListTaskStatusViews() len = %d, want 1", len(views))
	}
	if views[0].NextEligibleAt != "0001-01-01T00:00:00Z" {
		t.Fatalf("NextEligibleAt view = %q, want zero RFC3339 time", views[0].NextEligibleAt)
	}
}

func TestBlockedTaskRecordsReason(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "blocked-task.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "blocked-task",
		Name:          "Blocked Task",
		Scope:         "project",
		GitRoot:       "/tmp/blocked-task",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "blocked-queue-task",
		Title:       "Wait on approval",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	blocked, err := store.BlockTask(ctx, BlockTaskParams{
		TaskID: task.ID,
		Reason: "approval_required",
	})
	if err != nil {
		t.Fatalf("BlockTask() error = %v", err)
	}

	if blocked.Status != "blocked" {
		t.Fatalf("Status = %q, want blocked", blocked.Status)
	}
	if blocked.BlockedReason != "approval_required" {
		t.Fatalf("BlockedReason = %q, want %q", blocked.BlockedReason, "approval_required")
	}

	views, err := projections.ListBlockedItemViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListBlockedItemViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("ListBlockedItemViews() len = %d, want 1", len(views))
	}
	if views[0].Source != "task" {
		t.Fatalf("BlockedItemView.Source = %q, want %q", views[0].Source, "task")
	}
	if views[0].Reason != "approval_required" {
		t.Fatalf("BlockedItemView.Reason = %q, want %q", views[0].Reason, "approval_required")
	}
}

func TestTaskQueueStatusChangesEmitReplayableEvents(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "task-queue-events.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "queue-events",
		Name:          "Queue Events",
		Scope:         "project",
		GitRoot:       "/tmp/queue-events",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "queue-events-task",
		Title:       "Track queue transitions",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.BlockTask(ctx, BlockTaskParams{
		TaskID: task.ID,
		Reason: "approval_required",
	}); err != nil {
		t.Fatalf("BlockTask() error = %v", err)
	}
	if _, err := store.RequeueTaskAt(ctx, RequeueTaskAtParams{
		TaskID:         task.ID,
		NextEligibleAt: time.Date(2026, 4, 17, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RequeueTaskAt() error = %v", err)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents(task) error = %v", err)
	}

	var statusChanged int
	var queueStateChanged int
	for _, event := range events {
		if event.Type == runtimeevents.EventTaskStatusChanged {
			statusChanged++
		}
		if event.Type == runtimeevents.EventTaskQueueStateChanged {
			queueStateChanged++
		}
	}
	if statusChanged != 2 {
		t.Fatalf("task status changed events = %d, want 2", statusChanged)
	}
	if queueStateChanged != 2 {
		t.Fatalf("task queue state changed events = %d, want 2", queueStateChanged)
	}

	replay, err := projections.ReplayLifecycle(events)
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}
	if replay.Tasks[task.ID].Status != "queued" {
		t.Fatalf("ReplayLifecycle().Tasks[%d].Status = %q, want queued", task.ID, replay.Tasks[task.ID].Status)
	}
	if replay.Tasks[task.ID].BlockedReason != "" {
		t.Fatalf("ReplayLifecycle().Tasks[%d].BlockedReason = %q, want cleared", task.ID, replay.Tasks[task.ID].BlockedReason)
	}
	if replay.Tasks[task.ID].NextEligibleAt != "2026-04-17T11:00:00.000000000Z" {
		t.Fatalf("ReplayLifecycle().Tasks[%d].NextEligibleAt = %q, want retry time", task.ID, replay.Tasks[task.ID].NextEligibleAt)
	}
}

func TestRunLifecycleEventsReplayCurrentRunDuringPreparing(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "run-lifecycle-events.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "run-events",
		Name:          "Run Events",
		Scope:         "project",
		GitRoot:       "/tmp/run-events",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "run-events-task",
		Title:       "Track run lifecycle",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:     task.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "preparing",
		TaskStatus: "preparing",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, _, err := store.UpdateRunAndTaskStatus(ctx, UpdateRunAndTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "running",
		TaskStatus: "running",
	}); err != nil {
		t.Fatalf("UpdateRunAndTaskStatus() error = %v", err)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	replay, err := projections.ReplayLifecycle(events)
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}

	replayedTask := replay.Tasks[task.ID]
	if replayedTask.CurrentRunID == nil || *replayedTask.CurrentRunID != run.ID {
		t.Fatalf("ReplayLifecycle().Tasks[%d].CurrentRunID = %v, want %d", task.ID, replayedTask.CurrentRunID, run.ID)
	}
	if replay.Runs[run.ID].Status != "running" {
		t.Fatalf("ReplayLifecycle().Runs[%d].Status = %q, want running", run.ID, replay.Runs[run.ID].Status)
	}
}

func TestRetryBackoffUpdatesQueueState(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "retry-backoff.db")
	defer store.Close()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "retry-backoff",
		Name:          "Retry Backoff",
		Scope:         "project",
		GitRoot:       "/tmp/retry-backoff",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "retry-queue-task",
		Title:       "Retry later",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	retryAt := now.Add(500 * time.Millisecond)
	updated, err := store.IncrementTaskRetry(ctx, IncrementTaskRetryParams{
		TaskID:         task.ID,
		LastError:      "transient executor failure",
		NextEligibleAt: retryAt,
	})
	if err != nil {
		t.Fatalf("IncrementTaskRetry() error = %v", err)
	}

	if updated.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", updated.RetryCount)
	}
	if updated.NextEligibleAt != retryAt {
		t.Fatalf("NextEligibleAt = %v, want %v", updated.NextEligibleAt, retryAt)
	}
	if updated.LastError != "transient executor failure" {
		t.Fatalf("LastError = %q, want %q", updated.LastError, "transient executor failure")
	}

	requeued, err := store.RequeueTaskAt(ctx, RequeueTaskAtParams{
		TaskID:         task.ID,
		NextEligibleAt: retryAt,
	})
	if err != nil {
		t.Fatalf("RequeueTaskAt() error = %v", err)
	}
	if requeued.NextEligibleAt != retryAt {
		t.Fatalf("RequeueTaskAt().NextEligibleAt = %v, want %v", requeued.NextEligibleAt, retryAt)
	}

	eligible, err := store.ListEligibleQueuedTasks(ctx, now)
	if err != nil {
		t.Fatalf("ListEligibleQueuedTasks() error = %v", err)
	}
	if len(eligible) != 0 {
		t.Fatalf("ListEligibleQueuedTasks() len = %d, want 0 before retry window", len(eligible))
	}

	eligible, err = store.ListEligibleQueuedTasks(ctx, retryAt)
	if err != nil {
		t.Fatalf("ListEligibleQueuedTasks(retryAt) error = %v", err)
	}
	if len(eligible) != 1 || eligible[0].ID != task.ID {
		t.Fatalf("ListEligibleQueuedTasks(retryAt) = %+v, want task %d", eligible, task.ID)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents(task) error = %v", err)
	}

	replay, err := projections.ReplayLifecycle(events)
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}
	if replay.Tasks[task.ID].RetryCount != 1 {
		t.Fatalf("ReplayLifecycle().Tasks[%d].RetryCount = %d, want 1", task.ID, replay.Tasks[task.ID].RetryCount)
	}
	if replay.Tasks[task.ID].LastError != "transient executor failure" {
		t.Fatalf("ReplayLifecycle().Tasks[%d].LastError = %q, want transient executor failure", task.ID, replay.Tasks[task.ID].LastError)
	}
	if replay.Tasks[task.ID].NextEligibleAt != "2026-04-17T10:00:00.500000000Z" {
		t.Fatalf("ReplayLifecycle().Tasks[%d].NextEligibleAt = %q, want retry window", task.ID, replay.Tasks[task.ID].NextEligibleAt)
	}
}

func TestFailRunAndRetryTaskUpdatesRunAndTaskTogether(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "retry-run-later.db")
	defer store.Close()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "retry-run-later",
		Name:          "Retry Run Later",
		Scope:         "project",
		GitRoot:       "/tmp/retry-run-later",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "retry-run-later-task",
		Title:       "Retry this run later",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	lease, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/retry-run-later/task-1/run-1/try-1",
		WorktreePath: "/tmp/retry-run-later/.odin/task-1/run-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	retryAt := now.Add(2 * time.Second)
	retriedTask, retriedRun, err := store.FailRunAndRetryTask(ctx, FailRunAndRetryTaskParams{
		RunID:          run.ID,
		Summary:        "temporary executor outage",
		LastError:      "temporary executor outage",
		NextEligibleAt: retryAt,
	})
	if err != nil {
		t.Fatalf("FailRunAndRetryTask() error = %v", err)
	}

	if retriedRun.Status != "failed" {
		t.Fatalf("Run.Status = %q, want failed", retriedRun.Status)
	}
	if retriedRun.Summary != "temporary executor outage" {
		t.Fatalf("Run.Summary = %q, want temporary executor outage", retriedRun.Summary)
	}
	if retriedTask.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", retriedTask.Status)
	}
	if retriedTask.CurrentRunID != nil {
		t.Fatalf("Task.CurrentRunID = %v, want nil", retriedTask.CurrentRunID)
	}
	if retriedTask.RetryCount != 1 {
		t.Fatalf("Task.RetryCount = %d, want 1", retriedTask.RetryCount)
	}
	if retriedTask.LastError != "temporary executor outage" {
		t.Fatalf("Task.LastError = %q, want temporary executor outage", retriedTask.LastError)
	}
	if !retriedTask.NextEligibleAt.Equal(retryAt) {
		t.Fatalf("Task.NextEligibleAt = %v, want %v", retriedTask.NextEligibleAt, retryAt)
	}
	releasedLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if releasedLease.State != "released" {
		t.Fatalf("GetWorktreeLease().State = %q, want released", releasedLease.State)
	}
}

func TestFinishRunAndSetTaskStatusUpdatesRunAndTaskTogether(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "finish-run-status.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "finish-run-status",
		Name:          "Finish Run Status",
		Scope:         "project",
		GitRoot:       "/tmp/finish-run-status",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "finish-run-status-task",
		Title:       "Finish this run",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	lease, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/finish-run-status/task-1/run-1/try-1",
		WorktreePath: "/tmp/finish-run-status/.odin/task-1/run-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	finishedTask, finishedRun, err := store.FinishRunAndSetTaskStatus(ctx, FinishRunAndSetTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "failed",
		Summary:    "exhausted retries",
		TaskStatus: "failed",
	})
	if err != nil {
		t.Fatalf("FinishRunAndSetTaskStatus() error = %v", err)
	}

	if finishedRun.Status != "failed" {
		t.Fatalf("Run.Status = %q, want failed", finishedRun.Status)
	}
	if finishedRun.Summary != "exhausted retries" {
		t.Fatalf("Run.Summary = %q, want exhausted retries", finishedRun.Summary)
	}
	if finishedTask.Status != "failed" {
		t.Fatalf("Task.Status = %q, want failed", finishedTask.Status)
	}
	if finishedTask.CurrentRunID != nil {
		t.Fatalf("Task.CurrentRunID = %v, want nil", finishedTask.CurrentRunID)
	}
	releasedLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if releasedLease.State != "released" {
		t.Fatalf("GetWorktreeLease().State = %q, want released", releasedLease.State)
	}
}

func TestFormatTimeUsesFixedWidthUTC(t *testing.T) {
	got := formatTime(time.Date(2026, 4, 17, 10, 0, 0, 5*1000*1000, time.FixedZone("offset", 3*60*60)))
	want := "2026-04-17T07:00:00.005000000Z"
	if got != want {
		t.Fatalf("formatTime() = %q, want %q", got, want)
	}
}

func TestParseTimeAcceptsVariableWidthRFC3339Nano(t *testing.T) {
	got, err := parseTime("2026-04-17T07:00:00.5Z")
	if err != nil {
		t.Fatalf("parseTime() error = %v", err)
	}

	want := time.Date(2026, 4, 17, 7, 0, 0, 500*1000*1000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseTime() = %v, want %v", got, want)
	}
}

func TestUpdateMemorySummaryDetails(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
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

	summary, err := store.RecordMemorySummary(ctx, RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		MemoryType:  "social_draft",
		Summary:     "Draft awaiting approval",
		DetailsJSON: `{"source":"cli","approval":"pending"}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	updated, err := store.UpdateMemorySummaryDetails(ctx, UpdateMemorySummaryDetailsParams{
		MemoryID:    summary.ID,
		DetailsJSON: `{"source":"cli","approval":"approved"}`,
	})
	if err != nil {
		t.Fatalf("UpdateMemorySummaryDetails() error = %v", err)
	}
	if updated.ID != summary.ID {
		t.Fatalf("updated.ID = %d, want %d", updated.ID, summary.ID)
	}
	if updated.DetailsJSON != `{"source":"cli","approval":"approved"}` {
		t.Fatalf("updated.DetailsJSON = %q, want updated details", updated.DetailsJSON)
	}

	summaries, err := store.ListMemorySummaries(ctx, ListMemorySummariesParams{
		ProjectID: &project.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].DetailsJSON != updated.DetailsJSON {
		t.Fatalf("stored details = %q, want %q", summaries[0].DetailsJSON, updated.DetailsJSON)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var updatedEventFound bool
	for _, event := range events {
		if event.Type != runtimeevents.EventMemorySummaryUpdated {
			continue
		}
		if event.StreamType != runtimeevents.StreamMemorySummary {
			t.Fatalf("memory update event stream type = %q, want %q", event.StreamType, runtimeevents.StreamMemorySummary)
		}
		var payload runtimeevents.MemorySummaryUpdatedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("json.Unmarshal(memory update payload) error = %v", err)
		}
		if payload.Scope != "project" || payload.ScopeKey != project.Key || payload.MemoryType != "social_draft" {
			t.Fatalf("memory update payload = %+v, want project social_draft payload", payload)
		}
		updatedEventFound = true
	}
	if !updatedEventFound {
		t.Fatal("memory summary updated event not found")
	}
}

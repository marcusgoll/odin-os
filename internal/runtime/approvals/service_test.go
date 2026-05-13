package approvals

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/adapters/web"
	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/transfers"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

func TestDetailMarksUnsupportedApproval(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedPendingApproval(t, ctx, store)

	detail, err := Service{Store: store}.Detail(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if detail.Approval.ID != fixture.Approval.ID {
		t.Fatalf("detail.Approval.ID = %d, want %d", detail.Approval.ID, fixture.Approval.ID)
	}
	if detail.Task.ID != fixture.Task.ID {
		t.Fatalf("detail.Task.ID = %d, want %d", detail.Task.ID, fixture.Task.ID)
	}
	if detail.ResolverSupport != ResolverUnsupported {
		t.Fatalf("detail.ResolverSupport = %q, want %q", detail.ResolverSupport, ResolverUnsupported)
	}
}

func TestResolveApproveUnsupportedApprovalLeavesPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedPendingApproval(t, ctx, store)

	result, err := Service{Store: store}.Resolve(ctx, ResolveParams{
		ApprovalID: fixture.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if !errors.Is(err, ErrUnsupportedResolver) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedResolver", err)
	}

	if result.ResolverSupport != ResolverUnsupported {
		t.Fatalf("result.ResolverSupport = %q, want %q", result.ResolverSupport, ResolverUnsupported)
	}
	if result.SubmitRun != nil {
		t.Fatalf("SubmitRun = %+v, want nil for unsupported approval", result.SubmitRun)
	}
	persisted, err := store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if persisted.Status != "pending" {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, "pending")
	}
	if persisted.DecisionBy != "" {
		t.Fatalf("persisted.DecisionBy = %q, want empty", persisted.DecisionBy)
	}
	if persisted.Reason != "" {
		t.Fatalf("persisted.Reason = %q, want empty", persisted.Reason)
	}

	runIDs := listTaskRunIDs(t, ctx, store, fixture.Task.ID)
	if len(runIDs) != 1 {
		t.Fatalf("task run count = %d, want 1", len(runIDs))
	}
}

func TestResolveDenyUnsupportedApprovalLeavesPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedPendingApproval(t, ctx, store)

	result, err := Service{Store: store}.Resolve(ctx, ResolveParams{
		ApprovalID: fixture.Approval.ID,
		Action:     "deny",
		DecisionBy: "operator",
		Reason:     "amount is wrong",
	})
	if !errors.Is(err, ErrUnsupportedResolver) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedResolver", err)
	}

	if result.ResolverSupport != ResolverUnsupported {
		t.Fatalf("result.ResolverSupport = %q, want %q", result.ResolverSupport, ResolverUnsupported)
	}
	if result.SubmitRun != nil {
		t.Fatalf("SubmitRun = %+v, want nil for unsupported approval", result.SubmitRun)
	}
	persisted, err := store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if persisted.Status != "pending" {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, "pending")
	}
	if persisted.DecisionBy != "" {
		t.Fatalf("persisted.DecisionBy = %q, want empty", persisted.DecisionBy)
	}
	if persisted.Reason != "" {
		t.Fatalf("persisted.Reason = %q, want empty", persisted.Reason)
	}

	runIDs := listTaskRunIDs(t, ctx, store, fixture.Task.ID)
	if len(runIDs) != 1 {
		t.Fatalf("task run count = %d, want 1", len(runIDs))
	}

	task, err := store.GetTask(ctx, fixture.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "blocked")
	}
	if task.TerminalReason != "" {
		t.Fatalf("task.TerminalReason = %q, want empty", task.TerminalReason)
	}
}

func TestResolveNonPendingApprovalRefusesReresolution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedPendingApproval(t, ctx, store)

	approved, err := store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: fixture.Approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe to proceed",
	})
	if err != nil {
		t.Fatalf("ResolveApproval(setup) error = %v", err)
	}

	if _, err := (Service{Store: store}).Resolve(ctx, ResolveParams{
		ApprovalID: approved.ID,
		Action:     "deny",
		DecisionBy: "reviewer",
		Reason:     "changed mind",
	}); err == nil {
		t.Fatal("Resolve() error = nil, want re-resolution rejection")
	} else if errors.Is(err, ErrUnsupportedResolver) {
		t.Fatalf("Resolve() error = %v, want non-pending rejection before resolver support", err)
	}

	persisted, err := store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if persisted.Status != "approved" {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, "approved")
	}
	if persisted.DecisionBy != "operator" {
		t.Fatalf("persisted.DecisionBy = %q, want %q", persisted.DecisionBy, "operator")
	}
	if persisted.Reason != "safe to proceed" {
		t.Fatalf("persisted.Reason = %q, want %q", persisted.Reason, "safe to proceed")
	}

	runIDs := listTaskRunIDs(t, ctx, store, fixture.Task.ID)
	if len(runIDs) != 1 {
		t.Fatalf("task run count = %d, want 1", len(runIDs))
	}
}

func TestResolveRejectsApprovalWhenPolicySnapshotChanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedTaskBackedApproval(t, ctx, store)

	if _, err := store.UpsertProject(ctx, sqlite.UpsertProjectParams{
		Key:           "family-ops",
		Name:          "Family Ops",
		Scope:         "system",
		GitRoot:       "/tmp/family-ops-new",
		DefaultBranch: "main",
		ManifestPath:  "/tmp/family-ops-new/project.yaml",
	}); err != nil {
		t.Fatalf("UpsertProject(policy drift) error = %v", err)
	}

	_, err := Service{Store: store}.Resolve(ctx, ResolveParams{
		ApprovalID: fixture.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("Resolve() error = %v, want ErrStaleApproval", err)
	}

	persisted, err := store.GetApproval(ctx, fixture.Approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if persisted.Status != "pending" {
		t.Fatalf("persisted.Status = %q, want pending after stale conflict", persisted.Status)
	}
}

func TestResolveClarifyPreservesReasonAndRecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	fixture := seedTaskBackedApproval(t, ctx, store)

	result, err := Service{Store: store}.Resolve(ctx, ResolveParams{
		ApprovalID: fixture.Approval.ID,
		Action:     "clarify",
		DecisionBy: "operator",
		Reason:     "need current account balance evidence",
	})
	if err != nil {
		t.Fatalf("Resolve(clarify) error = %v", err)
	}
	if result.Approval.Status != "clarification_requested" {
		t.Fatalf("approval.Status = %q, want clarification_requested", result.Approval.Status)
	}
	if result.Approval.Reason != "need current account balance evidence" {
		t.Fatalf("approval.Reason = %q, want preserved clarification reason", result.Approval.Reason)
	}

	task, err := store.GetTask(ctx, fixture.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" || task.BlockedReason != "clarification_requested" {
		t.Fatalf("task status=%q blocked_reason=%q, want blocked clarification_requested", task.Status, task.BlockedReason)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &fixture.Task.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var resolved bool
	for _, event := range events {
		if event.Type == "approval.resolved" && strings.Contains(string(event.Payload), `"status":"clarification_requested"`) && strings.Contains(string(event.Payload), `"reason":"need current account balance evidence"`) {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("events = %+v, want clarification approval.resolved audit event", events)
	}
}

func TestResolveApprovePreparedTransferStartsSubmitContinuation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openApprovalTestStore(t)
	registry := writeApprovalRegistry(t)
	fixed := time.Date(2026, 4, 22, 3, 4, 5, 0, time.UTC)

	prepare, err := transfers.Service{
		Store:       store,
		Registry:    registry,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
		Now:         func() time.Time { return fixed },
	}.Prepare(ctx, transfers.PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	result, err := Service{
		Store:       store,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
	}.Resolve(ctx, ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.SubmitRun == nil {
		t.Fatalf("SubmitRun = nil, want continuation run")
	}
	if result.SubmitRun.Executor != "robinhood_transfer_submit" {
		t.Fatalf("SubmitRun.Executor = %q, want %q", result.SubmitRun.Executor, "robinhood_transfer_submit")
	}
	if result.SubmitRun.Status != "completed" {
		t.Fatalf("SubmitRun.Status = %q, want %q", result.SubmitRun.Status, "completed")
	}

	task, err := store.GetTask(ctx, prepare.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "completed")
	}
}

func TestResolveApproveSubmittedMarksTaskCompleted(t *testing.T) {
	ctx := context.Background()
	store := openApprovalTestStore(t)
	registry := writeApprovalRegistry(t)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", writeApprovalDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer submitted","artifacts":{"session_state":"submitted","current_url":"https://robinhood.com/transfers","next_action":"verify transfer status"}}'
`))

	prepare, err := transfers.Service{
		Store:       store,
		Registry:    registry,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
	}.Prepare(ctx, transfers.PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	result, err := Service{
		Store:       store,
		Checkpoints: checkpoints.Service{Store: store},
	}.Resolve(ctx, ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.SubmitRun == nil {
		t.Fatalf("SubmitRun = nil, want continuation run")
	}
	if result.SubmitRun.Status != "completed" {
		t.Fatalf("SubmitRun.Status = %q, want %q", result.SubmitRun.Status, "completed")
	}

	task, err := store.GetTask(ctx, prepare.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "completed")
	}

	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.SubmitRun.ID})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("ListRunArtifacts() len = %d, want 1", len(artifacts))
	}
	if !strings.Contains(artifacts[0].DetailsJSON, `"session_state":"submitted"`) {
		t.Fatalf("DetailsJSON = %q, want session_state submitted", artifacts[0].DetailsJSON)
	}
}

func TestResolveApproveSessionExpiredBlocksTaskWithStaleContextAndSealsOldWake(t *testing.T) {
	ctx := context.Background()
	store := openApprovalTestStore(t)
	registry := writeApprovalRegistry(t)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", writeApprovalDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood session expired during transfer","artifacts":{"session_state":"session_expired","current_url":"https://robinhood.com/login","next_action":"reestablish session"}}'
`))

	prepare, err := transfers.Service{
		Store:       store,
		Registry:    registry,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
	}.Prepare(ctx, transfers.PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}

	result, err := Service{
		Store:       store,
		Checkpoints: checkpoints.Service{Store: store},
	}.Resolve(ctx, ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.SubmitRun == nil {
		t.Fatalf("SubmitRun = nil, want continuation run")
	}
	if result.SubmitRun.Status != "failed" {
		t.Fatalf("SubmitRun.Status = %q, want %q", result.SubmitRun.Status, "failed")
	}

	task, err := store.GetTask(ctx, prepare.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "blocked")
	}

	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.SubmitRun.ID})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("ListRunArtifacts() len = %d, want 1", len(artifacts))
	}
	if !strings.Contains(artifacts[0].DetailsJSON, `"session_state":"session_expired"`) {
		t.Fatalf("DetailsJSON = %q, want session_state session_expired", artifacts[0].DetailsJSON)
	}

	activePackets, err := store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &prepare.Task.ID,
		PacketScope: "task_wake_packet",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("ListContextPackets(active) error = %v", err)
	}
	if len(activePackets) != 0 {
		t.Fatalf("active wake packets = %d, want 0", len(activePackets))
	}

	wakePacket, err := store.GetContextPacket(ctx, prepare.WakePacket.ID)
	if err != nil {
		t.Fatalf("GetContextPacket() error = %v", err)
	}
	if wakePacket.Status != "sealed" {
		t.Fatalf("wakePacket.Status = %q, want %q", wakePacket.Status, "sealed")
	}
	if !strings.Contains(wakePacket.PayloadJSON, `"blocking_reason":"stale_context"`) {
		t.Fatalf("wakePacket.PayloadJSON = %q, want blocking_reason stale_context", wakePacket.PayloadJSON)
	}

	if _, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, prepare.Task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("LoadResumeState() error = %v, want sql.ErrNoRows", err)
	}
}

func TestResolveApproveResumeVerificationFailedPersistsPriorSessionStateAndNoActiveReplacementWake(t *testing.T) {
	ctx := context.Background()
	store := openApprovalTestStore(t)
	registry := writeApprovalRegistry(t)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", writeApprovalDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood review continuity could not be verified","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","current_url":"https://robinhood.com/transfer","next_action":"fresh prepare required"}}'
`))

	prepare, err := transfers.Service{
		Store:       store,
		Registry:    registry,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
	}.Prepare(ctx, transfers.PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}

	result, err := Service{
		Store:       store,
		Checkpoints: checkpoints.Service{Store: store},
	}.Resolve(ctx, ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.SubmitRun == nil {
		t.Fatalf("SubmitRun = nil, want continuation run")
	}
	if result.SubmitRun.Status != "failed" {
		t.Fatalf("SubmitRun.Status = %q, want %q", result.SubmitRun.Status, "failed")
	}

	task, err := store.GetTask(ctx, prepare.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "blocked")
	}

	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.SubmitRun.ID})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("ListRunArtifacts() len = %d, want 1", len(artifacts))
	}
	if !strings.Contains(artifacts[0].DetailsJSON, `"session_state":"resume_verification_failed"`) {
		t.Fatalf("DetailsJSON = %q, want session_state resume_verification_failed", artifacts[0].DetailsJSON)
	}
	if !strings.Contains(artifacts[0].DetailsJSON, `"prior_session_state":"session_expired"`) {
		t.Fatalf("DetailsJSON = %q, want prior_session_state session_expired", artifacts[0].DetailsJSON)
	}

	activePackets, err := store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &prepare.Task.ID,
		PacketScope: "task_wake_packet",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("ListContextPackets(active) error = %v", err)
	}
	if len(activePackets) != 0 {
		t.Fatalf("active wake packets = %d, want 0", len(activePackets))
	}

	wakePacket, err := store.GetContextPacket(ctx, prepare.WakePacket.ID)
	if err != nil {
		t.Fatalf("GetContextPacket() error = %v", err)
	}
	if wakePacket.Status != "sealed" {
		t.Fatalf("wakePacket.Status = %q, want %q", wakePacket.Status, "sealed")
	}
	if !strings.Contains(wakePacket.PayloadJSON, `"blocking_reason":"stale_context"`) {
		t.Fatalf("wakePacket.PayloadJSON = %q, want blocking_reason stale_context", wakePacket.PayloadJSON)
	}

	if _, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, prepare.Task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("LoadResumeState() error = %v, want sql.ErrNoRows", err)
	}
}

func TestResolveDenyPreparedTransferSealsApprovalWakeAndClearsResumeState(t *testing.T) {
	ctx := context.Background()
	store := openApprovalTestStore(t)
	registry := writeApprovalRegistry(t)

	prepare, err := transfers.Service{
		Store:       store,
		Registry:    registry,
		Checkpoints: checkpoints.Service{Store: store},
		Invocation:  approvalTransferInvocation(),
	}.Prepare(ctx, transfers.PrepareParams{
		ProjectKey:         "family-ops",
		Direction:          "deposit",
		AmountUSD:          "25.00",
		SourceAccount:      "checking",
		DestinationAccount: "brokerage",
		Memo:               "household-test",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "family-ops")
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}

	result, err := Service{
		Store:       store,
		Checkpoints: checkpoints.Service{Store: store},
	}.Resolve(ctx, ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "deny",
		DecisionBy: "operator",
		Reason:     "amount is wrong",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.SubmitRun != nil {
		t.Fatalf("SubmitRun = %+v, want nil on deny", result.SubmitRun)
	}

	task, err := store.GetTask(ctx, prepare.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("task.Status = %q, want %q", task.Status, "blocked")
	}
	if task.TerminalReason != "operator_denied" {
		t.Fatalf("task.TerminalReason = %q, want %q", task.TerminalReason, "operator_denied")
	}

	activePackets, err := store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &prepare.Task.ID,
		PacketScope: "task_wake_packet",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("ListContextPackets(active) error = %v", err)
	}
	if len(activePackets) != 0 {
		t.Fatalf("active wake packets = %d, want 0", len(activePackets))
	}

	wakePacket, err := store.GetContextPacket(ctx, prepare.WakePacket.ID)
	if err != nil {
		t.Fatalf("GetContextPacket() error = %v", err)
	}
	if wakePacket.Status != "sealed" {
		t.Fatalf("wakePacket.Status = %q, want %q", wakePacket.Status, "sealed")
	}
	if !strings.Contains(wakePacket.PayloadJSON, `"blocking_reason":"operator_denied"`) {
		t.Fatalf("wakePacket.PayloadJSON = %q, want blocking_reason operator_denied", wakePacket.PayloadJSON)
	}

	if _, err := (checkpoints.Service{Store: store}).LoadResumeState(ctx, project.ID, prepare.Task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("LoadResumeState() error = %v, want sql.ErrNoRows", err)
	}
}

type approvalFixture struct {
	Task       sqlite.Task
	PrepareRun sqlite.Run
	Approval   sqlite.Approval
}

func openApprovalTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

func approvalTransferInvocation() invocation.Service {
	return invocation.Service{
		RobinhoodTransferDriver: web.RobinhoodTransferDriver{
			InvokeFunc: func(_ context.Context, request web.RobinhoodTransferRequest) (web.RobinhoodTransferResponse, error) {
				if request.Input.Mode == "submit" {
					return web.RobinhoodTransferResponse{
						ToolKey: web.RobinhoodTransferToolKey,
						Summary: "Robinhood transfer submitted",
						Artifacts: map[string]any{
							"session_state": "submitted",
							"current_url":   "https://robinhood.com/transfers",
							"next_action":   "verify transfer status",
						},
						RawOutput: `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer submitted","artifacts":{"session_state":"submitted","current_url":"https://robinhood.com/transfers","next_action":"verify transfer status"}}`,
					}, nil
				}
				return web.RobinhoodTransferResponse{
					ToolKey: web.RobinhoodTransferToolKey,
					Summary: "Robinhood transfer review ready",
					Artifacts: map[string]any{
						"session_state": "review_ready",
						"current_url":   "https://robinhood.com/transfer",
						"next_action":   "request approval",
					},
					RawOutput: `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer review ready","artifacts":{"session_state":"review_ready","current_url":"https://robinhood.com/transfer","next_action":"request approval"}}`,
				}, nil
			},
		},
	}
}

func seedPendingApproval(t *testing.T, ctx context.Context, store *sqlite.Store) approvalFixture {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "finance-transfer-review",
		Title:       "Prepare Robinhood transfer review",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "blocked",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	return approvalFixture{
		Task:       task,
		PrepareRun: run,
		Approval:   approval,
	}
}

func seedTaskBackedApproval(t *testing.T, ctx context.Context, store *sqlite.Store) approvalFixture {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "family-ops",
		Name:          "Family Ops",
		Scope:         "system",
		GitRoot:       "/tmp/family-ops",
		DefaultBranch: "main",
		ManifestPath:  "/tmp/family-ops/project.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   "task-backed-approval",
		Title:                 "Task backed approval",
		ActionKey:             "external_mutation",
		Status:                "queued",
		Scope:                 "project",
		RequestedBy:           "operator",
		WorkKind:              "external_mutation",
		ExecutionIntent:       "mutate",
		ExecutionIntentSource: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	blocked, err := store.BlockTask(ctx, sqlite.BlockTaskParams{TaskID: task.ID, Reason: "approval_required"})
	if err != nil {
		t.Fatalf("BlockTask() error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		Status:      "pending",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	return approvalFixture{Task: blocked, Approval: approval}
}

func writeApprovalRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")
	familyOpsRoot := filepath.Join(root, "family-ops")
	if err := os.MkdirAll(filepath.Join(familyOpsRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	content := `version: 1
projects:
  - key: family-ops
    name: Family-Ops
    project_class: local_git_project
    git_root: ` + familyOpsRoot + `
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
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
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

func listTaskRunIDs(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64) []int64 {
	t.Helper()

	rows, err := store.DB().QueryContext(ctx, `SELECT id FROM runs WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		t.Fatalf("QueryContext(runs) error = %v", err)
	}
	defer rows.Close()

	var runIDs []int64
	for rows.Next() {
		var runID int64
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	return runIDs
}

func writeApprovalDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "robinhood-submit-driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}

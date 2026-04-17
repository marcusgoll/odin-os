package sqlite

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDelegationLifecycleCRUD(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "delegations.db")
	defer store.Close()

	project, parentTask, parentRun := seedContextPacketTask(t, ctx, store)

	parentRunID := parentRun.ID
	delegation, err := store.CreateDelegation(ctx, CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRunID,
		ProjectID:       project.ID,
		Scope:           parentTask.Scope,
		DelegationKey:   "research-1",
		Role:            "specialist",
		ActionClass:     "analysis",
		ActionKey:       "inventory",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge_by_scope",
		ArtifactTarget:  "notes",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"inventory repo state"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}

	if delegation.ParentTaskID != parentTask.ID {
		t.Fatalf("CreateDelegation().ParentTaskID = %d, want %d", delegation.ParentTaskID, parentTask.ID)
	}
	if delegation.ParentRunID == nil || *delegation.ParentRunID != parentRun.ID {
		t.Fatalf("CreateDelegation().ParentRunID = %v, want %d", delegation.ParentRunID, parentRun.ID)
	}
	if delegation.Status != "queued" {
		t.Fatalf("CreateDelegation().Status = %q, want %q", delegation.Status, "queued")
	}

	if _, err := store.CreateDelegation(ctx, CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ProjectID:       project.ID,
		Scope:           parentTask.Scope,
		DelegationKey:   "delivery-1",
		Role:            "specialist",
		ActionClass:     "mutation",
		ActionKey:       "apply",
		MutationMode:    "isolated_worktree",
		Status:          "completed",
		ConvergenceMode: "rank_and_select",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"deliver change"}`,
	}); err != nil {
		t.Fatalf("CreateDelegation(second) error = %v", err)
	}

	got, err := store.GetDelegation(ctx, delegation.ID)
	if err != nil {
		t.Fatalf("GetDelegation() error = %v", err)
	}
	if got.DelegationKey != delegation.DelegationKey {
		t.Fatalf("GetDelegation().DelegationKey = %q, want %q", got.DelegationKey, delegation.DelegationKey)
	}

	queuedDelegations, err := store.ListDelegations(ctx, ListDelegationsParams{
		ParentTaskID: &parentTask.ID,
		Status:       "queued",
	})
	if err != nil {
		t.Fatalf("ListDelegations(parent/status) error = %v", err)
	}
	if len(queuedDelegations) != 1 || queuedDelegations[0].ID != delegation.ID {
		t.Fatalf("ListDelegations(parent/status) = %+v, want delegation %d only", queuedDelegations, delegation.ID)
	}

	childTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "delegation-child",
		Title:       "Delegated task",
		ActionKey:   "inventory",
		Status:      "running",
		Scope:       parentTask.Scope,
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}

	childRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   childTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(child) error = %v", err)
	}

	attached, err := store.AttachDelegationChildTask(ctx, AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
		ChildRunID:   &childRun.ID,
	})
	if err != nil {
		t.Fatalf("AttachDelegationChildTask() error = %v", err)
	}
	if attached.ChildTaskID == nil || *attached.ChildTaskID != childTask.ID {
		t.Fatalf("AttachDelegationChildTask().ChildTaskID = %v, want %d", attached.ChildTaskID, childTask.ID)
	}
	if attached.ChildRunID == nil || *attached.ChildRunID != childRun.ID {
		t.Fatalf("AttachDelegationChildTask().ChildRunID = %v, want %d", attached.ChildRunID, childRun.ID)
	}

	lease, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       childTask.ID,
		RunID:        childRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/delegation-child",
		WorktreePath: "/tmp/odin/cfipros/delegation-child",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	attached, err = store.AttachDelegationWorktree(ctx, AttachDelegationWorktreeParams{
		DelegationID:    delegation.ID,
		WorktreeLeaseID: &lease.ID,
		BranchName:      lease.BranchName,
	})
	if err != nil {
		t.Fatalf("AttachDelegationWorktree() error = %v", err)
	}
	if attached.WorktreeLeaseID == nil || *attached.WorktreeLeaseID != lease.ID {
		t.Fatalf("AttachDelegationWorktree().WorktreeLeaseID = %v, want %d", attached.WorktreeLeaseID, lease.ID)
	}
	if attached.BranchName != lease.BranchName {
		t.Fatalf("AttachDelegationWorktree().BranchName = %q, want %q", attached.BranchName, lease.BranchName)
	}

	updated, err := store.UpdateDelegationStatus(ctx, UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       "running",
	})
	if err != nil {
		t.Fatalf("UpdateDelegationStatus() error = %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("UpdateDelegationStatus().Status = %q, want %q", updated.Status, "running")
	}

	byChildTask, err := store.ListDelegations(ctx, ListDelegationsParams{
		ChildTaskID: &childTask.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegations(child task) error = %v", err)
	}
	if len(byChildTask) != 1 || byChildTask[0].ID != delegation.ID {
		t.Fatalf("ListDelegations(child task) = %+v, want delegation %d only", byChildTask, delegation.ID)
	}

	byWorktree, err := store.ListDelegations(ctx, ListDelegationsParams{
		WorktreeLeaseID: &lease.ID,
		Status:          "running",
	})
	if err != nil {
		t.Fatalf("ListDelegations(worktree/status) error = %v", err)
	}
	if len(byWorktree) != 1 || byWorktree[0].ID != delegation.ID {
		t.Fatalf("ListDelegations(worktree/status) = %+v, want delegation %d only", byWorktree, delegation.ID)
	}
}

func TestDelegationArtifactCRUD(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "delegation-artifacts.db")
	defer store.Close()

	project, parentTask, parentRun := seedContextPacketTask(t, ctx, store)
	parentRunID := parentRun.ID
	delegation, err := store.CreateDelegation(ctx, CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRunID,
		ProjectID:       project.ID,
		Scope:           parentTask.Scope,
		DelegationKey:   "artifact-1",
		Role:            "specialist",
		ActionClass:     "analysis",
		ActionKey:       "report",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "consensus_with_dissent",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"produce report"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}

	planArtifact, err := store.CreateDelegationArtifact(ctx, CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "plan",
		Summary:      "Delegation plan",
		DetailsJSON:  `{"status":"completed","confidence":"high","evidence_refs":["docs/plan.md"],"unresolved_risks":[],"proposed_next_actions":["publish plan"],"proposed_memory_candidates":["delegation-plan"]}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegationArtifact(plan) error = %v", err)
	}

	resultArtifact, err := store.CreateDelegationArtifact(ctx, CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "result",
		Summary:      "Delegation result",
		DetailsJSON:  `{"status":"completed","confidence":0.82,"evidence_refs":["docs/result.md"],"unresolved_risks":["awaiting approval"],"proposed_next_actions":["request approval"],"proposed_memory_candidates":["delegation-result"]}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegationArtifact(result) error = %v", err)
	}
	var envelope struct {
		Status                   string   `json:"status"`
		Confidence               string   `json:"confidence"`
		EvidenceRefs             []string `json:"evidence_refs"`
		UnresolvedRisks          []string `json:"unresolved_risks"`
		ProposedNextActions      []string `json:"proposed_next_actions"`
		ProposedMemoryCandidates []string `json:"proposed_memory_candidates"`
	}
	if err := json.Unmarshal([]byte(planArtifact.DetailsJSON), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(plan artifact details) error = %v", err)
	}
	if envelope.Status != "completed" || envelope.Confidence != "high" {
		t.Fatalf("plan artifact envelope = %+v, want completed/high", envelope)
	}
	if len(envelope.EvidenceRefs) != 1 || envelope.EvidenceRefs[0] != "docs/plan.md" {
		t.Fatalf("plan artifact evidence refs = %#v, want [docs/plan.md]", envelope.EvidenceRefs)
	}
	if len(envelope.ProposedNextActions) != 1 || envelope.ProposedNextActions[0] != "publish plan" {
		t.Fatalf("plan artifact next actions = %#v, want [publish plan]", envelope.ProposedNextActions)
	}

	artifacts, err := store.ListDelegationArtifacts(ctx, ListDelegationArtifactsParams{
		DelegationID: delegation.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegationArtifacts(all) error = %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("ListDelegationArtifacts(all) len = %d, want 2", len(artifacts))
	}
	if artifacts[0].ID != planArtifact.ID {
		t.Fatalf("ListDelegationArtifacts(all)[0].ID = %d, want %d", artifacts[0].ID, planArtifact.ID)
	}
	if artifacts[1].ID != resultArtifact.ID {
		t.Fatalf("ListDelegationArtifacts(all)[1].ID = %d, want %d", artifacts[1].ID, resultArtifact.ID)
	}

	resultArtifacts, err := store.ListDelegationArtifacts(ctx, ListDelegationArtifactsParams{
		DelegationID: delegation.ID,
		ArtifactType: "result",
	})
	if err != nil {
		t.Fatalf("ListDelegationArtifacts(filtered) error = %v", err)
	}
	if len(resultArtifacts) != 1 || resultArtifacts[0].ID != resultArtifact.ID {
		t.Fatalf("ListDelegationArtifacts(filtered) = %+v, want result artifact %d only", resultArtifacts, resultArtifact.ID)
	}
	var resultEnvelope struct {
		Status                   string   `json:"status"`
		Confidence               float64  `json:"confidence"`
		EvidenceRefs             []string `json:"evidence_refs"`
		UnresolvedRisks          []string `json:"unresolved_risks"`
		ProposedNextActions      []string `json:"proposed_next_actions"`
		ProposedMemoryCandidates []string `json:"proposed_memory_candidates"`
	}
	if err := json.Unmarshal([]byte(resultArtifacts[0].DetailsJSON), &resultEnvelope); err != nil {
		t.Fatalf("json.Unmarshal(result artifact details) error = %v", err)
	}
	if resultEnvelope.Status != "completed" || resultEnvelope.Confidence != 0.82 {
		t.Fatalf("result artifact envelope = %+v, want completed/0.82", resultEnvelope)
	}
	if len(resultEnvelope.UnresolvedRisks) != 1 || resultEnvelope.UnresolvedRisks[0] != "awaiting approval" {
		t.Fatalf("result artifact unresolved risks = %#v, want [awaiting approval]", resultEnvelope.UnresolvedRisks)
	}
	if len(resultEnvelope.ProposedMemoryCandidates) != 1 || resultEnvelope.ProposedMemoryCandidates[0] != "delegation-result" {
		t.Fatalf("result artifact memory candidates = %#v, want [delegation-result]", resultEnvelope.ProposedMemoryCandidates)
	}
}

func TestDelegationDefaultsBlankStatusToQueued(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "delegation-default-status.db")
	defer store.Close()

	project, parentTask, _ := seedContextPacketTask(t, ctx, store)

	delegation, err := store.CreateDelegation(ctx, CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ProjectID:       project.ID,
		Scope:           parentTask.Scope,
		DelegationKey:   "default-status",
		Role:            "specialist",
		ActionClass:     "analysis",
		ActionKey:       "inventory",
		MutationMode:    "read_only",
		Status:          "",
		ConvergenceMode: "merge_by_scope",
		ArtifactTarget:  "notes",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"default queued status"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}

	if delegation.Status != "queued" {
		t.Fatalf("CreateDelegation().Status = %q, want %q", delegation.Status, "queued")
	}
}

func TestCreateDelegationsIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "delegation-batch.db")
	defer store.Close()

	project, parentTask, parentRun := seedContextPacketTask(t, ctx, store)
	parentRunID := parentRun.ID

	_, err := store.CreateDelegations(ctx, []CreateDelegationParams{
		{
			ParentTaskID:    parentTask.ID,
			ParentRunID:     &parentRunID,
			ProjectID:       project.ID,
			Scope:           parentTask.Scope,
			DelegationKey:   "valid",
			Role:            "specialist",
			ActionClass:     "analysis",
			ActionKey:       "inventory",
			MutationMode:    "read_only",
			Status:          "queued",
			ConvergenceMode: "merge",
			ArtifactTarget:  "notes",
			Executor:        "codex",
			DetailsJSON:     `{"objective":"valid"}`,
		},
		{
			ParentTaskID:    parentTask.ID,
			ParentRunID:     &parentRunID,
			ProjectID:       project.ID,
			Scope:           parentTask.Scope,
			DelegationKey:   "invalid",
			Role:            "specialist",
			ActionClass:     "analysis",
			ActionKey:       "inventory",
			MutationMode:    "read_only",
			Status:          "queued",
			ConvergenceMode: "merge",
			ArtifactTarget:  "notes",
			Executor:        "codex",
			DetailsJSON:     `not-json`,
		},
	})
	if err == nil {
		t.Fatal("CreateDelegations() error = nil, want validation failure")
	}

	delegations, err := store.ListDelegations(ctx, ListDelegationsParams{
		ParentTaskID: &parentTask.ID,
	})
	if err != nil {
		t.Fatalf("ListDelegations() error = %v", err)
	}
	if len(delegations) != 0 {
		t.Fatalf("ListDelegations() len = %d, want 0 after failed batch", len(delegations))
	}
}

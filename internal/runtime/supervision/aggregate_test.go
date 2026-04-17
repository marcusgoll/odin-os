package supervision

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	runtimejobs "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

func TestAggregateMergeConvergenceMergesNonOverlappingChildArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "docs",
				Role:           "writer",
				ActionClass:    "mutation",
				ActionKey:      "document",
				MutationMode:   "read_only",
				ArtifactTarget: "docs",
				Objective:      "Document the change",
			},
			{
				DelegationKey:  "tests",
				Role:           "tester",
				ActionClass:    "mutation",
				ActionKey:      "test",
				MutationMode:   "read_only",
				ArtifactTarget: "tests",
				Objective:      "Add regression coverage",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Updated docs", resultEnvelopeJSON(t, "completed", 0.72, []string{"docs/companion.md"}, nil, []string{"merge docs"}, []string{"docs updated"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Added tests", resultEnvelopeJSON(t, "completed", 0.81, []string{"tests/companion_test.go"}, nil, []string{"run regression suite"}, []string{"test coverage updated"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("AggregateSwarm().Status = %q, want completed", result.Status)
	}
	if result.ParentTask.Status != "completed" {
		t.Fatalf("AggregateSwarm().ParentTask.Status = %q, want completed", result.ParentTask.Status)
	}
	if !strings.Contains(result.Summary, "Updated docs") || !strings.Contains(result.Summary, "Added tests") {
		t.Fatalf("AggregateSwarm().Summary = %q, want merged child summaries", result.Summary)
	}
	if len(result.EvidenceRefs) != 2 {
		t.Fatalf("AggregateSwarm().EvidenceRefs len = %d, want 2", len(result.EvidenceRefs))
	}
}

func TestConvergenceReviewGateRequiresVerifierArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "implement",
				Role:           "builder",
				ActionClass:    "mutation",
				ActionKey:      "implement",
				MutationMode:   "isolated_worktree",
				ArtifactTarget: "branch",
				Objective:      "Implement the change",
			},
			{
				DelegationKey:  "review",
				Role:           "reviewer",
				ActionClass:    "analysis",
				ActionKey:      "review",
				MutationMode:   "read_only",
				ArtifactTarget: "report",
				Objective:      "Verify the implementation",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Implementation ready", resultEnvelopeJSON(t, "completed", 0.78, []string{"branch:implement"}, nil, []string{"request review"}, []string{"implementation notes"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_review_gate_pending_verifier" {
		t.Fatalf("AggregateSwarm().TerminalReason = %q, want swarm_review_gate_pending_verifier", result.TerminalReason)
	}
	if result.ParentTask.Status != "blocked" {
		t.Fatalf("AggregateSwarm().ParentTask.Status = %q, want blocked", result.ParentTask.Status)
	}
}

func TestConvergenceRankSelectsWinningChildSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "rank",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "option-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal",
				Objective:      "Produce option A",
			},
			{
				DelegationKey:  "option-b",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal",
				Objective:      "Produce option B",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A", resultEnvelopeJSON(t, "completed", 0.44, []string{"notes:a"}, nil, []string{"consider A"}, []string{"option a"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Option B", resultEnvelopeJSON(t, "completed", 0.91, []string{"notes:b"}, nil, []string{"select B"}, []string{"option b"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("AggregateSwarm().Status = %q, want completed", result.Status)
	}
	if result.WinningDelegationID == nil || *result.WinningDelegationID != plan.Delegations[1].ID {
		t.Fatalf("AggregateSwarm().WinningDelegationID = %v, want %d", result.WinningDelegationID, plan.Delegations[1].ID)
	}
	if result.Summary != "Option B" {
		t.Fatalf("AggregateSwarm().Summary = %q, want Option B", result.Summary)
	}
}

func TestAggregatePropagatesUnresolvedRisksIntoParentState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "merge",
		RequestedBudget: 2,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "analysis-a",
				Role:           "analyst",
				ActionClass:    "analysis",
				ActionKey:      "analyze",
				MutationMode:   "read_only",
				ArtifactTarget: "notes-a",
				Objective:      "Analyze option A",
			},
			{
				DelegationKey:  "analysis-b",
				Role:           "analyst",
				ActionClass:    "analysis",
				ActionKey:      "analyze",
				MutationMode:   "read_only",
				ArtifactTarget: "notes-b",
				Objective:      "Analyze option B",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A analysis", resultEnvelopeJSON(t, "completed", 0.63, []string{"notes:a"}, []string{"human approval required"}, []string{"seek approval"}, []string{"approval note"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Option B analysis", resultEnvelopeJSON(t, "completed", 0.59, []string{"notes:b"}, nil, []string{"decide option"}, []string{"analysis note"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_unresolved_risks" {
		t.Fatalf("AggregateSwarm().TerminalReason = %q, want swarm_unresolved_risks", result.TerminalReason)
	}
	if len(result.UnresolvedRisks) != 1 || result.UnresolvedRisks[0] != "human approval required" {
		t.Fatalf("AggregateSwarm().UnresolvedRisks = %#v, want propagated risk", result.UnresolvedRisks)
	}

	persisted, err := store.GetTask(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("GetTask(parent) error = %v", err)
	}
	if persisted.Status != "blocked" {
		t.Fatalf("GetTask(parent).Status = %q, want blocked", persisted.Status)
	}

	var artifacts []map[string]any
	if err := json.Unmarshal([]byte(persisted.ArtifactsJSON), &artifacts); err != nil {
		t.Fatalf("json.Unmarshal(parent artifacts) error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("parent artifacts len = %d, want 1", len(artifacts))
	}
	gotRisks, ok := artifacts[0]["unresolved_risks"].([]any)
	if !ok || len(gotRisks) != 1 || gotRisks[0] != "human approval required" {
		t.Fatalf("parent artifacts unresolved_risks = %#v, want propagated risk", artifacts[0]["unresolved_risks"])
	}
}

func mustCreateDelegationResultArtifact(t *testing.T, ctx context.Context, store *sqlite.Store, delegation sqlite.Delegation, summary string, detailsJSON string) sqlite.DelegationArtifact {
	t.Helper()

	artifact, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "result",
		Summary:      summary,
		DetailsJSON:  detailsJSON,
	})
	if err != nil {
		t.Fatalf("CreateDelegationArtifact(%s) error = %v", delegation.DelegationKey, err)
	}
	return artifact
}

func resultEnvelopeJSON(t *testing.T, status string, confidence float64, evidenceRefs, unresolvedRisks, nextActions, memoryCandidates []string) string {
	t.Helper()

	encoded, err := json.Marshal(map[string]any{
		"status":                     status,
		"confidence":                 confidence,
		"evidence_refs":              evidenceRefs,
		"unresolved_risks":           unresolvedRisks,
		"proposed_next_actions":      nextActions,
		"proposed_memory_candidates": memoryCandidates,
	})
	if err != nil {
		t.Fatalf("json.Marshal(result envelope) error = %v", err)
	}
	return string(encoded)
}

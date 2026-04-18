package supervision

import (
	"context"
	"encoding/json"
	"errors"
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

	artifacts := []sqlite.DelegationArtifact{
		{
			ID:           1,
			DelegationID: 1,
			ArtifactType: "result",
			Summary:      "Producer summary",
			DetailsJSON:  resultEnvelopeJSON(t, "completed", 0.72, []string{"doc-a"}, nil, []string{"request review"}, []string{"memory-a"}),
		},
	}

	_, err := AggregateConvergence("review_gate", artifacts)
	if !errors.Is(err, ErrVerifierArtifactRequired) {
		t.Fatalf("AggregateConvergence(review_gate) error = %v, want %v", err, ErrVerifierArtifactRequired)
	}
}

func TestConvergenceReviewGateRequiresProducerOutput(t *testing.T) {
	t.Parallel()

	artifacts := []sqlite.DelegationArtifact{
		{
			ID:           1,
			DelegationID: 1,
			ArtifactType: "verifier_result",
			Summary:      "Verifier approved",
			DetailsJSON:  resultEnvelopeJSON(t, "completed", 0.91, []string{"doc-a"}, nil, []string{"publish"}, []string{"memory-a"}),
		},
	}

	result, err := AggregateConvergence("review_gate", artifacts)
	if err != nil {
		t.Fatalf("AggregateConvergence(review_gate) error = %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("AggregateConvergence(review_gate).Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_results_pending" {
		t.Fatalf("AggregateConvergence(review_gate).TerminalReason = %q, want swarm_results_pending", result.TerminalReason)
	}
}

func TestConvergenceReviewGateServiceRequiresVerifierArtifact(t *testing.T) {
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

func TestConvergenceReviewGateServiceRequiresProducerOutput(t *testing.T) {
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

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Review approved", resultEnvelopeJSON(t, "completed", 0.93, []string{"branch:review"}, nil, []string{"publish"}, []string{"review notes"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_results_pending" {
		t.Fatalf("AggregateSwarm().TerminalReason = %q, want swarm_results_pending", result.TerminalReason)
	}
	if result.ParentTask.Status != "blocked" {
		t.Fatalf("AggregateSwarm().ParentTask.Status = %q, want blocked", result.ParentTask.Status)
	}
}

func TestConvergenceReviewGateServiceRequiresEveryProducerDelegation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":3}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerBuildPlusReview,
		ConvergenceMode: "review_gate",
		RequestedBudget: 3,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "implement-a",
				Role:           "builder",
				ActionClass:    "mutation",
				ActionKey:      "implement",
				MutationMode:   "isolated_worktree",
				ArtifactTarget: "branch-a",
				Objective:      "Implement the change",
			},
			{
				DelegationKey:  "implement-b",
				Role:           "builder",
				ActionClass:    "mutation",
				ActionKey:      "implement",
				MutationMode:   "isolated_worktree",
				ArtifactTarget: "branch-b",
				Objective:      "Implement the other change",
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

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Implementation ready", resultEnvelopeJSON(t, "completed", 0.78, []string{"branch:implement-a"}, nil, []string{"request review"}, []string{"implementation notes"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[2], "Review approved", resultEnvelopeJSON(t, "completed", 0.93, []string{"branch:review"}, nil, []string{"publish"}, []string{"review notes"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_results_pending" {
		t.Fatalf("AggregateSwarm().TerminalReason = %q, want swarm_results_pending", result.TerminalReason)
	}
	if result.ParentTask.Status != "blocked" {
		t.Fatalf("AggregateSwarm().ParentTask.Status = %q, want blocked", result.ParentTask.Status)
	}
}

func TestAggregateSwarmDuplicateResultsFromOneDelegationDoNotCompleteMergeOrRank(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	modes := []string{"merge", "rank"}

	for _, mode := range modes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			store := openSupervisionStore(t)
			defer store.Close()

			_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
			service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

			plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
				ParentTaskID:    parentTask.ID,
				ParentRunID:     &parentRun.ID,
				Trigger:         TriggerParallelResearch,
				ConvergenceMode: mode,
				RequestedBudget: 2,
				DelegationPlans: []DelegationPlan{
					{
						DelegationKey:  "child-a",
						Role:           "researcher",
						ActionClass:    "analysis",
						ActionKey:      "option",
						MutationMode:   "read_only",
						ArtifactTarget: "proposal-a",
						Objective:      "Produce option A",
					},
					{
						DelegationKey:  "child-b",
						Role:           "researcher",
						ActionClass:    "analysis",
						ActionKey:      "option",
						MutationMode:   "read_only",
						ArtifactTarget: "proposal-b",
						Objective:      "Produce option B",
					},
				},
			})
			if err != nil {
				t.Fatalf("PlanSwarm() error = %v", err)
			}

			mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A v1", resultEnvelopeJSON(t, "completed", 0.41, []string{"proposal-a"}, nil, []string{"review A"}, []string{"memory-a"}))
			mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A v2", resultEnvelopeJSON(t, "completed", 0.72, []string{"proposal-a"}, nil, []string{"review A again"}, []string{"memory-a-2"}))

			result, err := service.AggregateSwarm(ctx, parentTask.ID)
			if err != nil {
				t.Fatalf("AggregateSwarm() error = %v", err)
			}
			if result.Status != "blocked" {
				t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
			}
			wantReason := "swarm_results_pending"
			if mode == "quorum" {
				wantReason = "swarm_quorum_not_reached"
			}
			if result.TerminalReason != wantReason {
				t.Fatalf("AggregateSwarm().TerminalReason = %q, want %s", result.TerminalReason, wantReason)
			}
		})
	}
}

func TestAggregateSwarmUsesLatestResultArtifactPerDelegation(t *testing.T) {
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
				DelegationKey:  "child-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-a",
				Objective:      "Produce option A",
			},
			{
				DelegationKey:  "child-b",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-b",
				Objective:      "Produce option B",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Old result", resultEnvelopeJSON(t, "completed", 0.21, []string{"proposal-a"}, nil, []string{"review A"}, []string{"memory-a"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "New result", resultEnvelopeJSON(t, "completed", 0.83, []string{"proposal-a"}, nil, []string{"review A again"}, []string{"memory-a-2"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Peer result", resultEnvelopeJSON(t, "completed", 0.74, []string{"proposal-b"}, nil, []string{"review B"}, []string{"memory-b"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("AggregateSwarm().Status = %q, want completed", result.Status)
	}
	if !strings.Contains(result.Summary, "New result") {
		t.Fatalf("AggregateSwarm().Summary = %q, want latest result summary", result.Summary)
	}
	if strings.Contains(result.Summary, "Old result") {
		t.Fatalf("AggregateSwarm().Summary = %q, want old result omitted", result.Summary)
	}
}

func TestAggregateSwarmQuorumUsesDelegationCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openSupervisionStore(t)
	defer store.Close()

	_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":4}}`)
	service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

	plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		Trigger:         TriggerParallelResearch,
		ConvergenceMode: "quorum",
		RequestedBudget: 4,
		DelegationPlans: []DelegationPlan{
			{
				DelegationKey:  "child-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-a",
				Objective:      "Produce option A",
			},
			{
				DelegationKey:  "child-b",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-b",
				Objective:      "Produce option B",
			},
			{
				DelegationKey:  "child-c",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-c",
				Objective:      "Produce option C",
			},
			{
				DelegationKey:  "child-d",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-d",
				Objective:      "Produce option D",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A v1", resultEnvelopeJSON(t, "completed", 0.33, []string{"proposal-a"}, nil, []string{"consider A"}, []string{"memory-a"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A v2", resultEnvelopeJSON(t, "completed", 0.44, []string{"proposal-a"}, nil, []string{"consider A again"}, []string{"memory-a-2"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A v3", resultEnvelopeJSON(t, "completed", 0.55, []string{"proposal-a"}, nil, []string{"consider A again"}, []string{"memory-a-3"}))
	mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[1], "Option A peer", resultEnvelopeJSON(t, "completed", 0.61, []string{"proposal-b"}, nil, []string{"consider A peer"}, []string{"memory-b"}))

	result, err := service.AggregateSwarm(ctx, parentTask.ID)
	if err != nil {
		t.Fatalf("AggregateSwarm() error = %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
	}
	if result.TerminalReason != "swarm_quorum_not_reached" {
		t.Fatalf("AggregateSwarm().TerminalReason = %q, want swarm_quorum_not_reached", result.TerminalReason)
	}
}

func TestAggregateSwarmBlocksPartialResultsForMergeRankAndQuorum(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	modes := []string{"merge", "rank", "quorum"}

	for _, mode := range modes {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			store := openSupervisionStore(t)
			defer store.Close()

			_, parentTask, parentRun, _ := mustCreateSwarmParentContext(t, ctx, store, `{"allow":["repo_read","branch_proposal"]}`, `{"mode":"initiative"}`, `{"swarm":{"max_children":2}}`)
			service := Service{Store: store, Jobs: runtimejobs.Service{Store: store}}

			plan, err := service.PlanSwarm(ctx, PlanSwarmParams{
				ParentTaskID:    parentTask.ID,
				ParentRunID:     &parentRun.ID,
				Trigger:         TriggerParallelResearch,
				ConvergenceMode: mode,
				RequestedBudget: 2,
				DelegationPlans: []DelegationPlan{
					{
						DelegationKey:  "child-a",
						Role:           "researcher",
						ActionClass:    "analysis",
						ActionKey:      "option",
						MutationMode:   "read_only",
						ArtifactTarget: "proposal-a",
						Objective:      "Produce option A",
					},
					{
						DelegationKey:  "child-b",
						Role:           "researcher",
						ActionClass:    "analysis",
						ActionKey:      "option",
						MutationMode:   "read_only",
						ArtifactTarget: "proposal-b",
						Objective:      "Produce option B",
					},
				},
			})
			if err != nil {
				t.Fatalf("PlanSwarm() error = %v", err)
			}

			mustCreateDelegationResultArtifact(t, ctx, store, plan.Delegations[0], "Option A", resultEnvelopeJSON(t, "completed", 0.51, []string{"proposal-a"}, nil, []string{"review A"}, []string{"memory-a"}))

			result, err := service.AggregateSwarm(ctx, parentTask.ID)
			if err != nil {
				t.Fatalf("AggregateSwarm() error = %v", err)
			}
			if result.Status != "blocked" {
				t.Fatalf("AggregateSwarm().Status = %q, want blocked", result.Status)
			}
			wantReason := "swarm_results_pending"
			if mode == "quorum" {
				wantReason = "swarm_quorum_not_reached"
			}
			if result.TerminalReason != wantReason {
				t.Fatalf("AggregateSwarm().TerminalReason = %q, want %s", result.TerminalReason, wantReason)
			}
		})
	}
}

func TestAggregateDelegationArtifactsIgnoresPlanArtifacts(t *testing.T) {
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
				DelegationKey:  "child-a",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-a",
				Objective:      "Produce option A",
			},
			{
				DelegationKey:  "child-b",
				Role:           "researcher",
				ActionClass:    "analysis",
				ActionKey:      "option",
				MutationMode:   "read_only",
				ArtifactTarget: "proposal-b",
				Objective:      "Produce option B",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanSwarm() error = %v", err)
	}

	delegation := plan.Delegations[0]
	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "plan",
		Summary:      "Draft plan",
		DetailsJSON:  resultEnvelopeJSON(t, "completed", 0.12, []string{"proposal-a"}, nil, []string{"draft"}, []string{"memory-plan"}),
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(plan) error = %v", err)
	}
	mustCreateDelegationResultArtifact(t, ctx, store, delegation, "Final result", resultEnvelopeJSON(t, "completed", 0.88, []string{"proposal-a"}, nil, []string{"publish"}, []string{"memory-result"}))

	result, err := service.AggregateDelegationArtifacts(ctx, delegation.ID)
	if err != nil {
		t.Fatalf("AggregateDelegationArtifacts() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("AggregateDelegationArtifacts().Status = %q, want completed", result.Status)
	}
	if result.Summary != "Final result" {
		t.Fatalf("AggregateDelegationArtifacts().Summary = %q, want Final result", result.Summary)
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
	gotConfidence, ok := artifacts[0]["confidence"].(float64)
	if !ok {
		t.Fatalf("parent artifacts confidence = %#v, want float64", artifacts[0]["confidence"])
	}
	if gotConfidence == 0 {
		t.Fatalf("parent artifacts confidence = %v, want non-zero confidence", gotConfidence)
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

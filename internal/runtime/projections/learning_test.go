package projections_test

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/learning/evaluator"
	"odin-os/internal/learning/promotion"
	"odin-os/internal/learning/proposals"
	"odin-os/internal/learning/replay"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestLearningProjectionsExposeProposalStatusLatestEvaluationAndActivePromotion(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	proposalService := proposals.Service{Store: store}
	promotionService := promotion.Service{
		Store:     store,
		Evaluator: evaluator.Service{ApprovalThreshold: 0},
	}

	proposal, err := proposalService.Create(ctx, proposals.CreateInput{
		ProposalType:      "retry_policy_refinement",
		Scope:             "global",
		TargetKey:         "recovery/retry-once",
		Summary:           "Relax retry backoff",
		Hypothesis:        "Reduce operator intervention without more failures",
		ChangePayloadJSON: `{"max_retries":2}`,
		CreatedBy:         "odin",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := proposalService.Submit(ctx, proposal.ID); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if _, _, err := promotionService.Evaluate(ctx, proposal.ID, replay.Fixture{
		Key:  "retry-fixture",
		Mode: replay.ModeReplay,
		Baseline: replay.Metrics{
			SuccessRate:           0.92,
			Cost:                  0.010,
			LatencyMS:             90,
			PolicyViolations:      0,
			OperatorInterventions: 2,
		},
		Candidate: replay.Metrics{
			SuccessRate:           0.94,
			Cost:                  0.011,
			LatencyMS:             88,
			PolicyViolations:      0,
			OperatorInterventions: 1,
		},
	}); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if _, err := proposalService.ApprovePromotion(ctx, proposal.ID); err != nil {
		t.Fatalf("ApprovePromotion() error = %v", err)
	}

	if _, err := promotionService.Promote(ctx, proposal.ID, "operator"); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}

	proposalViews, err := projections.ListLearningProposalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListLearningProposalViews() error = %v", err)
	}
	if len(proposalViews) != 1 {
		t.Fatalf("ListLearningProposalViews() len = %d, want 1", len(proposalViews))
	}
	if proposalViews[0].Status != "promoted" {
		t.Fatalf("proposal view status = %q, want %q", proposalViews[0].Status, "promoted")
	}
	if proposalViews[0].LatestOutcome != "approved" {
		t.Fatalf("proposal view latest outcome = %q, want %q", proposalViews[0].LatestOutcome, "approved")
	}
	if proposalViews[0].LatestScore == nil || *proposalViews[0].LatestScore <= 0 {
		t.Fatalf("proposal view latest score = %v, want positive score", proposalViews[0].LatestScore)
	}

	promotionViews, err := projections.ListActiveLearningPromotionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListActiveLearningPromotionViews() error = %v", err)
	}
	if len(promotionViews) != 1 {
		t.Fatalf("ListActiveLearningPromotionViews() len = %d, want 1", len(promotionViews))
	}
	if promotionViews[0].TargetKey != "recovery/retry-once" {
		t.Fatalf("promotion view target = %q, want %q", promotionViews[0].TargetKey, "recovery/retry-once")
	}
}

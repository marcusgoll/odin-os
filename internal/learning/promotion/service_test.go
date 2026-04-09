package promotion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/learning/evaluator"
	"odin-os/internal/learning/proposals"
	"odin-os/internal/learning/replay"
	"odin-os/internal/store/sqlite"
)

func TestPromotionServiceEvaluatesPromotesAndRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openPromotionStore(t)
	defer store.Close()

	proposalService := proposals.Service{Store: store}
	service := Service{
		Store:     store,
		Evaluator: evaluator.Service{ApprovalThreshold: 0},
	}

	firstProposal, err := proposalService.Create(ctx, proposals.CreateInput{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer latency winner",
		Hypothesis:        "Improve latency without policy regressions",
		ChangePayloadJSON: `{"executor":"codex","priority":10}`,
		CreatedBy:         "odin",
	})
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	firstProposal, err = proposalService.Submit(ctx, firstProposal.ID)
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}

	firstEvaluation, updatedFirstProposal, err := service.Evaluate(ctx, firstProposal.ID, replay.Fixture{
		Key:  "router-latency",
		Mode: replay.ModeReplay,
		Baseline: replay.Metrics{
			SuccessRate:           0.93,
			Cost:                  0.021,
			LatencyMS:             220,
			PolicyViolations:      0,
			OperatorInterventions: 1,
		},
		Candidate: replay.Metrics{
			SuccessRate:           0.95,
			Cost:                  0.018,
			LatencyMS:             180,
			PolicyViolations:      0,
			OperatorInterventions: 0,
		},
	})
	if err != nil {
		t.Fatalf("Evaluate(first) error = %v", err)
	}
	if firstEvaluation.Outcome != "approved" {
		t.Fatalf("firstEvaluation.Outcome = %q, want %q", firstEvaluation.Outcome, "approved")
	}
	if updatedFirstProposal.Status != "approved" {
		t.Fatalf("updatedFirstProposal.Status = %q, want %q", updatedFirstProposal.Status, "approved")
	}

	firstPromotion, err := service.Promote(ctx, firstProposal.ID, "operator")
	if err != nil {
		t.Fatalf("Promote(first) error = %v", err)
	}
	if firstPromotion.Status != "active" {
		t.Fatalf("firstPromotion.Status = %q, want %q", firstPromotion.Status, "active")
	}

	secondProposal, err := proposalService.Create(ctx, proposals.CreateInput{
		ProposalType:      "routing_rule_refinement",
		Scope:             "global",
		TargetKey:         "router/default",
		Summary:           "Prefer lower cost route",
		Hypothesis:        "Reduce cost while keeping success rate stable",
		ChangePayloadJSON: `{"executor":"openai_api","priority":20}`,
		CreatedBy:         "odin",
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	secondProposal, err = proposalService.Submit(ctx, secondProposal.ID)
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	_, updatedSecondProposal, err := service.Evaluate(ctx, secondProposal.ID, replay.Fixture{
		Key:  "router-cost",
		Mode: replay.ModeSandbox,
		Baseline: replay.Metrics{
			SuccessRate:           0.94,
			Cost:                  0.021,
			LatencyMS:             180,
			PolicyViolations:      0,
			OperatorInterventions: 0,
		},
		Candidate: replay.Metrics{
			SuccessRate:           0.94,
			Cost:                  0.015,
			LatencyMS:             181,
			PolicyViolations:      0,
			OperatorInterventions: 0,
		},
	})
	if err != nil {
		t.Fatalf("Evaluate(second) error = %v", err)
	}
	if updatedSecondProposal.Status != "approved" {
		t.Fatalf("updatedSecondProposal.Status = %q, want %q", updatedSecondProposal.Status, "approved")
	}

	configPath := filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml"))
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(before config) error = %v", err)
	}

	secondPromotion, err := service.Promote(ctx, secondProposal.ID, "operator")
	if err != nil {
		t.Fatalf("Promote(second) error = %v", err)
	}
	if secondPromotion.SupersedesPromotionID == nil || *secondPromotion.SupersedesPromotionID != firstPromotion.ID {
		t.Fatalf("secondPromotion.SupersedesPromotionID = %v, want %d", secondPromotion.SupersedesPromotionID, firstPromotion.ID)
	}

	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(after config) error = %v", err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatalf("promotion changed canonical file %s", configPath)
	}

	rolledBack, err := service.Rollback(ctx, secondPromotion.ID, "operator", "cost win was too narrow")
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if rolledBack.Status != "rolled_back" {
		t.Fatalf("rolledBack.Status = %q, want %q", rolledBack.Status, "rolled_back")
	}

	active, err := service.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(active) != 1 || active[0].ID != firstPromotion.ID {
		t.Fatalf("active promotions = %+v, want first promotion %d", active, firstPromotion.ID)
	}
}

func openPromotionStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

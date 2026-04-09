package promotion

import (
	"context"
	"encoding/json"
	"fmt"

	"odin-os/internal/learning/evaluator"
	"odin-os/internal/learning/replay"
	"odin-os/internal/store/sqlite"
)

type Evaluator interface {
	Evaluate(replay.Fixture) (evaluator.Result, error)
}

type Service struct {
	Store     *sqlite.Store
	Evaluator Evaluator
}

func (service Service) Evaluate(ctx context.Context, proposalID int64, fixture replay.Fixture) (sqlite.LearningEvaluation, sqlite.LearningProposal, error) {
	if service.Store == nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, fmt.Errorf("promotion store is required")
	}
	if service.Evaluator == nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, fmt.Errorf("evaluator is required")
	}

	proposal, err := service.Store.GetLearningProposal(ctx, proposalID)
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}
	if proposal.Status != "submitted" && proposal.Status != "approved" {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, fmt.Errorf("proposal %d must be submitted before evaluation", proposal.ID)
	}

	result, err := service.Evaluator.Evaluate(fixture)
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}

	baselineJSON, err := json.Marshal(fixture.Baseline)
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}
	candidateJSON, err := json.Marshal(fixture.Candidate)
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}

	evaluationRecord, err := service.Store.RecordLearningEvaluation(ctx, sqlite.RecordLearningEvaluationParams{
		ProposalID:           proposal.ID,
		FixtureKey:           fixture.Key,
		Mode:                 string(fixture.Mode),
		Score:                result.Score,
		BaselineSummaryJSON:  string(baselineJSON),
		CandidateSummaryJSON: string(candidateJSON),
		ResultSummary:        fmt.Sprintf("%s evaluation via %s fixture %s", result.Outcome, fixture.Mode, fixture.Key),
		Outcome:              result.Outcome,
	})
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}

	status := "rejected"
	if result.Outcome == "approved" {
		status = "approved"
	}
	updatedProposal, err := service.Store.UpdateLearningProposalStatus(ctx, sqlite.UpdateLearningProposalStatusParams{
		ProposalID: proposal.ID,
		Status:     status,
	})
	if err != nil {
		return sqlite.LearningEvaluation{}, sqlite.LearningProposal{}, err
	}

	return evaluationRecord, updatedProposal, nil
}

func (service Service) Promote(ctx context.Context, proposalID int64, promotedBy string) (sqlite.LearningPromotion, error) {
	if service.Store == nil {
		return sqlite.LearningPromotion{}, fmt.Errorf("promotion store is required")
	}

	proposal, err := service.Store.GetLearningProposal(ctx, proposalID)
	if err != nil {
		return sqlite.LearningPromotion{}, err
	}
	if proposal.Status != "approved" {
		return sqlite.LearningPromotion{}, fmt.Errorf("proposal %d must be approved before promotion", proposal.ID)
	}

	return service.Store.PromoteLearningProposal(ctx, sqlite.PromoteLearningProposalParams{
		ProposalID: proposalID,
		PromotedBy: promotedBy,
	})
}

func (service Service) Rollback(ctx context.Context, promotionID int64, rolledBackBy string, rollbackReason string) (sqlite.LearningPromotion, error) {
	if service.Store == nil {
		return sqlite.LearningPromotion{}, fmt.Errorf("promotion store is required")
	}
	return service.Store.RollbackLearningPromotion(ctx, sqlite.RollbackLearningPromotionParams{
		PromotionID:    promotionID,
		RolledBackBy:   rolledBackBy,
		RollbackReason: rollbackReason,
	})
}

func (service Service) ListActive(ctx context.Context) ([]sqlite.LearningPromotion, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("promotion store is required")
	}
	return service.Store.ListActiveLearningPromotions(ctx)
}

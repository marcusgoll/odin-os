package proposals

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

type CreateInput struct {
	ProjectID         *int64
	ProposalType      string
	Scope             string
	TargetKey         string
	Summary           string
	Hypothesis        string
	ChangePayloadJSON string
	CreatedBy         string
}

func (service Service) Create(ctx context.Context, input CreateInput) (sqlite.LearningProposal, error) {
	if service.Store == nil {
		return sqlite.LearningProposal{}, fmt.Errorf("proposal store is required")
	}

	return service.Store.CreateLearningProposal(ctx, sqlite.CreateLearningProposalParams{
		ProjectID:         input.ProjectID,
		ProposalType:      input.ProposalType,
		Scope:             input.Scope,
		TargetKey:         input.TargetKey,
		Summary:           input.Summary,
		Hypothesis:        input.Hypothesis,
		ChangePayloadJSON: input.ChangePayloadJSON,
		Status:            "draft",
		CreatedBy:         input.CreatedBy,
	})
}

func (service Service) Submit(ctx context.Context, proposalID int64) (sqlite.LearningProposal, error) {
	if service.Store == nil {
		return sqlite.LearningProposal{}, fmt.Errorf("proposal store is required")
	}
	return service.Store.UpdateLearningProposalStatus(ctx, sqlite.UpdateLearningProposalStatusParams{
		ProposalID: proposalID,
		Status:     "submitted",
	})
}

func (service Service) Reject(ctx context.Context, proposalID int64) (sqlite.LearningProposal, error) {
	if service.Store == nil {
		return sqlite.LearningProposal{}, fmt.Errorf("proposal store is required")
	}
	return service.Store.UpdateLearningProposalStatus(ctx, sqlite.UpdateLearningProposalStatusParams{
		ProposalID: proposalID,
		Status:     "rejected",
	})
}

func (service Service) ApprovePromotion(ctx context.Context, proposalID int64) (sqlite.LearningProposal, error) {
	if service.Store == nil {
		return sqlite.LearningProposal{}, fmt.Errorf("proposal store is required")
	}

	proposal, err := service.Store.GetLearningProposal(ctx, proposalID)
	if err != nil {
		return sqlite.LearningProposal{}, err
	}
	if proposal.Status != "approved" {
		return sqlite.LearningProposal{}, fmt.Errorf("proposal %d must be approved by evaluation before promotion approval", proposal.ID)
	}

	return service.Store.UpdateLearningProposalStatus(ctx, sqlite.UpdateLearningProposalStatusParams{
		ProposalID: proposalID,
		Status:     "promotion_ready",
	})
}

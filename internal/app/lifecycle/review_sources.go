package lifecycle

import (
	"context"
	"strings"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/workspaces"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

type reviewQueueSource interface {
	Name() string
	ListReviewQueueEntries(context.Context, bootstrap.App, *reviewQueueSourceState) ([]reviewQueueEntry, error)
}

type reviewQueueSourceState struct {
	convertedGoalIDs map[int64]bool
}

func newReviewQueueSourceState() *reviewQueueSourceState {
	return &reviewQueueSourceState{convertedGoalIDs: map[int64]bool{}}
}

func defaultReviewQueueSources() []reviewQueueSource {
	return []reviewQueueSource{
		intakeReviewQueueSource{},
		goalReviewQueueSource{},
		approvalReviewQueueSource{},
		skillArtifactReviewQueueSource{},
		contextPackReviewQueueSource{},
		memoryProposalReviewQueueSource{},
		recoveryReviewQueueSource{},
		failedWorkReviewQueueSource{},
	}
}

func listReviewQueueEntries(ctx context.Context, app bootstrap.App) ([]reviewQueueEntry, error) {
	state := newReviewQueueSourceState()
	entries := make([]reviewQueueEntry, 0)
	for _, source := range defaultReviewQueueSources() {
		sourceEntries, err := source.ListReviewQueueEntries(ctx, app, state)
		if err != nil {
			return nil, err
		}
		entries = append(entries, sourceEntries...)
	}
	return entries, nil
}

type intakeReviewQueueSource struct{}

func (intakeReviewQueueSource) Name() string {
	return "intake"
}

func (intakeReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, state *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	intakeItems, err := app.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, err
	}

	entries := make([]reviewQueueEntry, 0)
	for _, item := range intakeItems {
		if isIntakeGoalReviewItem(item) {
			entry, err := reviewEntryFromIntakeGoalItem(item)
			if err != nil {
				return nil, err
			}
			if item.GoalID != nil {
				goal, err := app.Store.GetGoal(ctx, *item.GoalID)
				if err != nil {
					return nil, err
				}
				state.convertedGoalIDs[*item.GoalID] = true
				if goal.Status == sqlite.GoalStatusBlocked {
					continue
				}
			}
			entries = append(entries, entry)
			continue
		}
		if item.Status == "approval_required" {
			entry, err := reviewEntryFromIntakeItem(item, "intake-approval")
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
			continue
		}
		if isReviewableIntakeStatus(item.Status) {
			entry, err := reviewEntryFromIntakeItem(item, "intake-review")
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func isIntakeGoalReviewItem(item sqlite.IntakeItem) bool {
	return item.GoalID != nil || isDraftGoalIntakeItem(item)
}

func isDraftGoalIntakeItem(item sqlite.IntakeItem) bool {
	notes, err := intakeNotesFromItem(item)
	if err != nil || notes.DraftArtifact == nil {
		return false
	}
	return item.Status == "review_required" && strings.TrimSpace(notes.DraftArtifact.Kind) == "draft_goal"
}

type goalReviewQueueSource struct{}

func (goalReviewQueueSource) Name() string {
	return "goal"
}

func (goalReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, state *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	return listGoalReviewEntries(ctx, app.Store, state.convertedGoalIDs)
}

type approvalReviewQueueSource struct{}

func (approvalReviewQueueSource) Name() string {
	return "approval"
}

func (approvalReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	pendingApprovals, err := projections.ListPendingApprovalViews(ctx, app.Store.DB())
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0, len(pendingApprovals))
	for _, view := range pendingApprovals {
		entries = append(entries, reviewEntryFromPendingApproval(view))
	}
	return entries, nil
}

type skillArtifactReviewQueueSource struct{}

func (skillArtifactReviewQueueSource) Name() string {
	return "skill_artifact"
}

func (skillArtifactReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	artifacts, err := app.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !isReviewQueueSkillArtifactStatus(artifact.Status) {
			continue
		}
		entry, err := reviewEntryFromSkillArtifact(ctx, app.Store, artifact)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type contextPackReviewQueueSource struct{}

func (contextPackReviewQueueSource) Name() string {
	return "context_pack"
}

func (contextPackReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	contextPacks, err := runtimeknowledge.Service{Store: app.Store}.ListContextPackProposals(ctx, "")
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0, len(contextPacks))
	for _, proposal := range contextPacks {
		entries = append(entries, reviewEntryFromContextPackProposal(proposal))
	}
	return entries, nil
}

type failedWorkReviewQueueSource struct{}

func (failedWorkReviewQueueSource) Name() string {
	return "failed_work"
}

func (failedWorkReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	taskViews, err := projections.ListTaskStatusViews(ctx, app.Store.DB())
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0)
	for _, task := range taskViews {
		if !strings.EqualFold(strings.TrimSpace(task.Status), "failed") {
			continue
		}
		entries = append(entries, reviewEntryFromFailedTask(task))
	}
	return entries, nil
}

type recoveryReviewQueueSource struct{}

func (recoveryReviewQueueSource) Name() string {
	return "recovery"
}

func (recoveryReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	incidents, err := projections.ListIncidentViews(ctx, app.Store.DB())
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0)
	for _, incident := range incidents {
		if strings.EqualFold(strings.TrimSpace(incident.Status), "resolved") {
			continue
		}
		entry := reviewEntryFromRecoveryIncidentView(incident)
		if entry.Decision == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type memoryProposalReviewQueueSource struct{}

func (memoryProposalReviewQueueSource) Name() string {
	return "memory_proposal"
}

func (memoryProposalReviewQueueSource) ListReviewQueueEntries(ctx context.Context, app bootstrap.App, _ *reviewQueueSourceState) ([]reviewQueueEntry, error) {
	memorySummaries, err := app.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{})
	if err != nil {
		return nil, err
	}
	entries := make([]reviewQueueEntry, 0)
	for _, summary := range memorySummaries {
		if !isReviewQueueMemoryProposal(summary) {
			continue
		}
		entry, err := reviewEntryFromMemoryProposal(ctx, app.Store, summary)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

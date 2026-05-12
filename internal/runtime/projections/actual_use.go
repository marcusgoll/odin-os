package projections

import (
	"context"
	"strings"
)

type ActualUseSummaryView struct {
	WorkItems                 int
	OpenWorkItems             int
	ActiveRunAttempts         int
	PendingApprovals          int
	BlockedWorkItems          int
	FailedWorkItems           int
	RecoveryRecommendations   int
	IntakeReviewItems         int
	KnowledgeReviewItems      int
	SkillArtifactReviewItems  int
	ReviewQueueItems          int
	AutomationTriggers        int
	EnabledAutomationTriggers int
	ExplicitIntentWorkItems   int
	FallbackIntentWorkItems   int
	ActionRequiredItems       int
}

func GetActualUseSummaryView(ctx context.Context, queryer Queryer, workspaceKey string) (ActualUseSummaryView, error) {
	tasks, err := ListTaskStatusViews(ctx, queryer)
	if err != nil {
		return ActualUseSummaryView{}, err
	}
	activeRuns, err := ListActiveRunViews(ctx, queryer)
	if err != nil {
		return ActualUseSummaryView{}, err
	}
	approvals, err := ListPendingApprovalViews(ctx, queryer)
	if err != nil {
		return ActualUseSummaryView{}, err
	}
	blocked, err := ListBlockedItemViews(ctx, queryer)
	if err != nil {
		return ActualUseSummaryView{}, err
	}

	view := ActualUseSummaryView{
		WorkItems:         len(tasks),
		OpenWorkItems:     0,
		ActiveRunAttempts: len(activeRuns),
		PendingApprovals:  len(approvals),
		BlockedWorkItems:  len(blocked),
	}
	for _, task := range tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if !isClosedActualUseStatus(status) {
			view.OpenWorkItems++
		}
		if status == "failed" {
			view.FailedWorkItems++
			view.RecoveryRecommendations++
		}
		if strings.TrimSpace(task.ExecutionIntent) == "" {
			view.FallbackIntentWorkItems++
		} else {
			view.ExplicitIntentWorkItems++
		}
	}

	var errCount error
	view.IntakeReviewItems, errCount = countActualUseRows(ctx, queryer, `
		SELECT COUNT(*)
		FROM intake_items
		WHERE (? = '' OR workspace_id = ?)
		  AND status IN ('review_required', 'approval_required')
	`, workspaceKey, workspaceKey)
	if errCount != nil {
		return ActualUseSummaryView{}, errCount
	}
	view.KnowledgeReviewItems, errCount = countActualUseRows(ctx, queryer, `
		SELECT COUNT(*)
		FROM context_packets
		WHERE packet_kind = 'context_pack'
		  AND packet_scope = 'operator_context_pack'
		  AND status = 'review_required'
	`)
	if errCount != nil {
		return ActualUseSummaryView{}, errCount
	}
	view.SkillArtifactReviewItems, errCount = countActualUseRows(ctx, queryer, `
		SELECT COUNT(*)
		FROM skill_artifacts
		WHERE status = 'review_required'
	`)
	if errCount != nil {
		return ActualUseSummaryView{}, errCount
	}
	view.AutomationTriggers, errCount = countActualUseRows(ctx, queryer, `
		SELECT COUNT(*)
		FROM automation_triggers
		WHERE (? = '' OR workspace_id = ?)
	`, workspaceKey, workspaceKey)
	if errCount != nil {
		return ActualUseSummaryView{}, errCount
	}
	view.EnabledAutomationTriggers, errCount = countActualUseRows(ctx, queryer, `
		SELECT COUNT(*)
		FROM automation_triggers
		WHERE (? = '' OR workspace_id = ?)
		  AND status = 'active'
	`, workspaceKey, workspaceKey)
	if errCount != nil {
		return ActualUseSummaryView{}, errCount
	}

	view.ReviewQueueItems = view.IntakeReviewItems + view.PendingApprovals + view.KnowledgeReviewItems + view.SkillArtifactReviewItems + view.FailedWorkItems
	view.ActionRequiredItems = view.ReviewQueueItems + view.BlockedWorkItems
	return view, nil
}

func countActualUseRows(ctx context.Context, queryer Queryer, query string, args ...any) (int, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, err
	}
	return count, rows.Err()
}

func isClosedActualUseStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed", "cancelled", "canceled", "archived", "failed":
		return true
	default:
		return false
	}
}

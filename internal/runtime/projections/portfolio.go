package projections

import (
	"context"
)

type WorkspaceHomeView struct {
	WorkspaceKey         string
	WorkspaceName        string
	InitiativeCount      int
	CompanionCount       int
	PendingApprovalCount int
	BlockedItemCount     int
}

type InitiativePortfolioView struct {
	WorkspaceKey      string
	InitiativeKey     string
	Title             string
	Kind              string
	Status            string
	ProjectKey        string
	OwnerCompanionKey string
	OpenWorkItemCount int
}

type InitiativeWorkItemView struct {
	WorkspaceKey  string
	InitiativeKey string
	ProjectKey    string
	TaskKey       string
	Title         string
	Status        string
}

func ListWorkspaceHomeViews(ctx context.Context, queryer Queryer) ([]WorkspaceHomeView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			w.key,
			w.name,
			(SELECT COUNT(*) FROM initiatives i WHERE i.workspace_id = w.id),
			(SELECT COUNT(*) FROM companions c WHERE c.workspace_id = w.id)
		FROM workspaces w
		ORDER BY w.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []WorkspaceHomeView
	for rows.Next() {
		var view WorkspaceHomeView
		if err := rows.Scan(&view.WorkspaceKey, &view.WorkspaceName, &view.InitiativeCount, &view.CompanionCount); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for index := range views {
		approvals, err := ListWorkspacePendingApprovalViews(ctx, queryer, views[index].WorkspaceKey)
		if err != nil {
			return nil, err
		}
		blocked, err := ListWorkspaceBlockedItemViews(ctx, queryer, views[index].WorkspaceKey)
		if err != nil {
			return nil, err
		}
		views[index].PendingApprovalCount = len(approvals)
		views[index].BlockedItemCount = len(blocked)
	}

	return views, nil
}

func ListInitiativePortfolioViews(ctx context.Context, queryer Queryer, workspaceKey string) ([]InitiativePortfolioView, error) {
	query := `
		SELECT
			w.key,
			i.key,
			i.title,
			i.kind,
			i.status,
			COALESCE(p.key, ''),
			COALESCE(c.key, ''),
			COALESCE((
				SELECT COUNT(*)
				FROM tasks t
				WHERE t.initiative_id = i.id AND t.status NOT IN ('completed', 'cancelled')
			), 0)
		FROM initiatives i
		JOIN workspaces w ON w.id = i.workspace_id
		LEFT JOIN projects p ON p.id = i.linked_project_id
		LEFT JOIN companions c ON c.id = i.owner_companion_id
	`
	var args []any
	if workspaceKey != "" {
		query += ` WHERE w.key = ?`
		args = append(args, workspaceKey)
	}
	query += ` ORDER BY i.id ASC`

	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []InitiativePortfolioView
	for rows.Next() {
		var view InitiativePortfolioView
		if err := rows.Scan(
			&view.WorkspaceKey,
			&view.InitiativeKey,
			&view.Title,
			&view.Kind,
			&view.Status,
			&view.ProjectKey,
			&view.OwnerCompanionKey,
			&view.OpenWorkItemCount,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListInitiativeWorkItemViews(ctx context.Context, queryer Queryer, workspaceKey, initiativeKey string) ([]InitiativeWorkItemView, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT
			w.key,
			i.key,
			p.key,
			t.key,
			t.title,
			t.status
		FROM tasks t
		JOIN workspaces w ON w.id = t.workspace_id
		JOIN initiatives i ON i.id = t.initiative_id
		JOIN projects p ON p.id = t.project_id
		WHERE w.key = ? AND i.key = ?
		ORDER BY t.id ASC
	`, workspaceKey, initiativeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []InitiativeWorkItemView
	for rows.Next() {
		var view InitiativeWorkItemView
		if err := rows.Scan(
			&view.WorkspaceKey,
			&view.InitiativeKey,
			&view.ProjectKey,
			&view.TaskKey,
			&view.Title,
			&view.Status,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListWorkspacePendingApprovalViews(ctx context.Context, queryer Queryer, workspaceKey string) ([]PendingApprovalView, error) {
	query := `
		SELECT
			a.id,
			a.task_id,
			p.key,
			t.key,
			a.status,
			a.requested_at
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		JOIN projects p ON p.id = t.project_id
		JOIN workspaces w ON w.id = t.workspace_id
		WHERE a.status = 'pending'
	`
	var args []any
	if workspaceKey != "" {
		query += ` AND w.key = ?`
		args = append(args, workspaceKey)
	}
	query += ` ORDER BY a.id ASC`

	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []PendingApprovalView
	for rows.Next() {
		var view PendingApprovalView
		if err := rows.Scan(
			&view.ApprovalID,
			&view.TaskID,
			&view.ProjectKey,
			&view.TaskKey,
			&view.Status,
			&view.RequestedAt,
		); err != nil {
			return nil, err
		}
		views = append(views, view)
	}

	return views, rows.Err()
}

func ListWorkspaceBlockedItemViews(ctx context.Context, queryer Queryer, workspaceKey string) ([]BlockedItemView, error) {
	views, err := ListBlockedItemViews(ctx, queryer)
	if err != nil {
		return nil, err
	}

	orderedTaskIDs := make([]int64, 0, len(views))
	indexed := make(map[int64]BlockedItemView, len(views))
	for _, view := range views {
		if workspaceKey != "" && view.WorkspaceKey != workspaceKey {
			continue
		}

		taskID := view.TaskID
		existing, ok := indexed[taskID]
		if !ok {
			orderedTaskIDs = append(orderedTaskIDs, taskID)
			indexed[taskID] = view
			continue
		}

		if blockedItemPriority(view) > blockedItemPriority(existing) || (blockedItemPriority(view) == blockedItemPriority(existing) && existing.NextStep == "" && view.NextStep != "") {
			indexed[taskID] = view
		}
	}

	filtered := make([]BlockedItemView, 0, len(orderedTaskIDs))
	for _, taskID := range orderedTaskIDs {
		filtered = append(filtered, indexed[taskID])
	}
	return filtered, nil
}

func blockedItemPriority(view BlockedItemView) int {
	switch view.Source {
	case "wake_packet":
		return 3
	case "incident":
		return 2
	case "approval":
		return 1
	default:
		return 0
	}
}

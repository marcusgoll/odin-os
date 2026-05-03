package lifecycle

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	"odin-os/internal/core/workspaces"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const reviewUsage = "usage: odin review list [--json] | odin review show <queue-id> [--json] | odin review act <queue-id> <accept|reject|archive|approve|deny|clarify> [--json]"

type reviewQueueListView struct {
	Items []reviewQueueEntry `json:"items"`
}

type reviewQueueShowView struct {
	Entry  reviewQueueEntry `json:"entry"`
	Detail any              `json:"detail"`
}

type reviewQueueEntry struct {
	QueueID        string   `json:"queue_id"`
	SourceType     string   `json:"source_type"`
	ObjectID       int64    `json:"object_id"`
	ObjectKey      string   `json:"object_key"`
	Status         string   `json:"status"`
	ProjectScope   string   `json:"project_scope,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	AllowedActions []string `json:"allowed_actions"`
}

type reviewQueueRef struct {
	Kind string
	ID   int64
}

func runReview(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 || strings.EqualFold(remaining[0], "help") || strings.EqualFold(remaining[0], "--help") {
		_, err := fmt.Fprintln(stdout, reviewUsage)
		return err
	}

	switch strings.ToLower(strings.TrimSpace(remaining[0])) {
	case "list":
		if len(remaining) != 1 {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewList(ctx, app, jsonOutput, stdout)
	case "show":
		if len(remaining) != 2 {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewShow(ctx, app, remaining[1], jsonOutput, stdout)
	case "act":
		if len(remaining) != 3 {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewAct(ctx, app, remaining[1], remaining[2], jsonOutput, stdout)
	default:
		return fmt.Errorf(reviewUsage)
	}
}

func runReviewList(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, reviewQueueListView{Items: entries})
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(stdout, "no review items")
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(stdout, "review=%s source=%s status=%s object=%s actions=%s\n", entry.QueueID, entry.SourceType, entry.Status, entry.ObjectKey, strings.Join(entry.AllowedActions, ",")); err != nil {
			return err
		}
	}
	return nil
}

func runReviewShow(ctx context.Context, app bootstrap.App, queueID string, jsonOutput bool, stdout io.Writer) error {
	ref, err := parseReviewQueueRef(queueID)
	if err != nil {
		return err
	}
	entry, detail, err := reviewQueueDetail(ctx, app, ref, true)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, reviewQueueShowView{Entry: entry, Detail: detail})
	}
	_, err = fmt.Fprintf(stdout, "review=%s source=%s status=%s object=%s actions=%s\n", entry.QueueID, entry.SourceType, entry.Status, entry.ObjectKey, strings.Join(entry.AllowedActions, ","))
	return err
}

func runReviewAct(ctx context.Context, app bootstrap.App, queueID string, action string, jsonOutput bool, stdout io.Writer) error {
	ref, err := parseReviewQueueRef(queueID)
	if err != nil {
		return err
	}
	action = strings.ToLower(strings.TrimSpace(action))
	idRef := strconv.FormatInt(ref.ID, 10)

	switch ref.Kind {
	case "intake-review":
		if !oneOf(action, "accept", "reject", "archive", "clarify") {
			return fmt.Errorf("intake review action must be one of accept, reject, archive, clarify")
		}
		return runIntakeReviewDecision(ctx, app, commands.IntakeCommand{
			Name:         "review",
			ReviewAction: action,
			ShowRef:      rawIntakeKey(ref.ID),
		}, jsonOutput, stdout)
	case "intake-approval":
		if !oneOf(action, "approve", "deny") {
			return fmt.Errorf("intake approval action must be one of approve, deny")
		}
		return runIntakeApprovalDecision(ctx, app, commands.IntakeCommand{
			Name:           "approval",
			ApprovalAction: action,
			ShowRef:        rawIntakeKey(ref.ID),
		}, jsonOutput, stdout)
	case "approval":
		if !oneOf(action, "approve", "deny") {
			return fmt.Errorf("task approval action must be one of approve, deny")
		}
		args := []string{"resolve", idRef, action, "unified", "review", "decision"}
		if jsonOutput {
			args = append(args, "--json")
		}
		return runApprovals(ctx, app, args, stdout)
	case "skill-artifact":
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("skill artifact action must be one of accept, reject, archive")
		}
		return runSkillArtifactReview(ctx, app, action, idRef, jsonOutput, stdout)
	default:
		return fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

func listReviewQueueEntries(ctx context.Context, app bootstrap.App) ([]reviewQueueEntry, error) {
	intakeItems, err := app.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, err
	}

	entries := make([]reviewQueueEntry, 0)
	for _, item := range intakeItems {
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

	pendingApprovals, err := projections.ListPendingApprovalViews(ctx, app.Store.DB())
	if err != nil {
		return nil, err
	}
	for _, view := range pendingApprovals {
		entries = append(entries, reviewEntryFromPendingApproval(view))
	}

	artifacts, err := app.Store.ListSkillArtifacts(ctx, sqlite.ListSkillArtifactsParams{})
	if err != nil {
		return nil, err
	}
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

func reviewQueueDetail(ctx context.Context, app bootstrap.App, ref reviewQueueRef, includePayload bool) (reviewQueueEntry, any, error) {
	switch ref.Kind {
	case "intake-review", "intake-approval":
		item, err := findRawIntakeItem(ctx, app.Store, rawIntakeKey(ref.ID))
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, err := reviewEntryFromIntakeItem(item, ref.Kind)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		view, err := rawIntakeView(item, includePayload)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, rawIntakeItemEnvelope{IntakeItem: view}, nil
	case "approval":
		detail, err := approvalsvc.Service{Store: app.Store}.Detail(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry := reviewEntryFromApprovalDetail(ctx, app.Store, detail)
		return entry, struct {
			ID              int64  `json:"id"`
			Status          string `json:"status"`
			TaskID          int64  `json:"task_id"`
			TaskKey         string `json:"task_key"`
			TaskStatus      string `json:"task_status"`
			RunID           *int64 `json:"run_id,omitempty"`
			ResolverSupport string `json:"resolver_support"`
		}{
			ID:              detail.Approval.ID,
			Status:          detail.Approval.Status,
			TaskID:          detail.Task.ID,
			TaskKey:         detail.Task.Key,
			TaskStatus:      detail.Task.Status,
			RunID:           detail.Approval.RunID,
			ResolverSupport: string(detail.ResolverSupport),
		}, nil
	case "skill-artifact":
		artifact, err := app.Store.GetSkillArtifact(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, err := reviewEntryFromSkillArtifact(ctx, app.Store, artifact)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, renderSkillReviewArtifact(artifact), nil
	default:
		return reviewQueueEntry{}, nil, fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

func reviewEntryFromIntakeItem(item sqlite.IntakeItem, kind string) (reviewQueueEntry, error) {
	view, err := rawIntakeView(item, false)
	if err != nil {
		return reviewQueueEntry{}, err
	}
	sourceType := "intake_review"
	actions := intakeReviewAllowedActions(item.Status)
	if kind == "intake-approval" {
		sourceType = "intake_approval"
		actions = []string{"approve", "deny"}
	}
	return reviewQueueEntry{
		QueueID:        fmt.Sprintf("%s:%d", kind, item.ID),
		SourceType:     sourceType,
		ObjectID:       item.ID,
		ObjectKey:      rawIntakeKey(item.ID),
		Status:         item.Status,
		ProjectScope:   view.ProjectKey,
		Summary:        firstNonBlank(item.Summary, item.Subject),
		AllowedActions: actions,
	}, nil
}

func reviewEntryFromPendingApproval(view projections.PendingApprovalView) reviewQueueEntry {
	return reviewQueueEntry{
		QueueID:        fmt.Sprintf("approval:%d", view.ApprovalID),
		SourceType:     "task_approval",
		ObjectID:       view.ApprovalID,
		ObjectKey:      fmt.Sprintf("approval-%d", view.ApprovalID),
		Status:         view.Status,
		ProjectScope:   view.ProjectKey,
		Summary:        view.TaskKey,
		AllowedActions: []string{"approve", "deny"},
	}
}

func reviewEntryFromApprovalDetail(ctx context.Context, store *sqlite.Store, detail approvalsvc.Detail) reviewQueueEntry {
	projectScope := ""
	if detail.Task.ProjectID > 0 {
		if project, err := store.GetProject(ctx, detail.Task.ProjectID); err == nil {
			projectScope = project.Key
		}
	}
	return reviewQueueEntry{
		QueueID:        fmt.Sprintf("approval:%d", detail.Approval.ID),
		SourceType:     "task_approval",
		ObjectID:       detail.Approval.ID,
		ObjectKey:      fmt.Sprintf("approval-%d", detail.Approval.ID),
		Status:         detail.Approval.Status,
		ProjectScope:   projectScope,
		Summary:        detail.Task.Key,
		AllowedActions: taskApprovalAllowedActions(detail.Approval.Status),
	}
}

func reviewEntryFromSkillArtifact(ctx context.Context, store *sqlite.Store, artifact sqlite.SkillArtifact) (reviewQueueEntry, error) {
	projectScope := ""
	if artifact.ProjectID != nil {
		project, err := store.GetProject(ctx, *artifact.ProjectID)
		if err != nil {
			return reviewQueueEntry{}, err
		}
		projectScope = project.Key
	}
	return reviewQueueEntry{
		QueueID:        fmt.Sprintf("skill-artifact:%d", artifact.ID),
		SourceType:     "skill_artifact",
		ObjectID:       artifact.ID,
		ObjectKey:      fmt.Sprintf("skill-artifact-%d", artifact.ID),
		Status:         artifact.Status,
		ProjectScope:   projectScope,
		Summary:        artifact.Summary,
		AllowedActions: skillArtifactAllowedActions(artifact.Status),
	}, nil
}

func parseReviewQueueRef(queueID string) (reviewQueueRef, error) {
	queueID = strings.TrimSpace(queueID)
	parts := strings.SplitN(queueID, ":", 2)
	if len(parts) != 2 {
		return reviewQueueRef{}, fmt.Errorf("review queue id must look like intake-review:<id>, intake-approval:<id>, approval:<id>, or skill-artifact:<id>")
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	idRef := strings.TrimSpace(parts[1])
	switch kind {
	case "intake-review", "intake-approval":
		idRef = strings.TrimPrefix(idRef, "intake-")
	case "approval":
		idRef = strings.TrimPrefix(idRef, "approval-")
	case "skill-artifact":
		idRef = strings.TrimPrefix(idRef, "skill-artifact-")
	default:
		return reviewQueueRef{}, fmt.Errorf("unsupported review queue source %q", kind)
	}
	id, err := strconv.ParseInt(idRef, 10, 64)
	if err != nil || id <= 0 {
		return reviewQueueRef{}, fmt.Errorf("review queue id %q must include a positive object id", queueID)
	}
	return reviewQueueRef{Kind: kind, ID: id}, nil
}

func intakeReviewAllowedActions(status string) []string {
	switch status {
	case "review_required":
		return []string{"accept", "reject", "clarify", "archive"}
	case "needs_clarification":
		return []string{"reject", "clarify", "archive"}
	case "duplicate_linked_or_suppressed":
		return []string{"accept", "archive"}
	default:
		return nil
	}
}

func taskApprovalAllowedActions(status string) []string {
	switch status {
	case "pending":
		return []string{"approve", "deny"}
	case "approved":
		return []string{"approve"}
	case "denied":
		return []string{"deny"}
	default:
		return nil
	}
}

func skillArtifactAllowedActions(status string) []string {
	switch status {
	case "review_required":
		return []string{"accept", "reject", "archive"}
	case "accepted":
		return []string{"accept"}
	case "rejected":
		return []string{"reject"}
	case "archived":
		return []string{"archive"}
	default:
		return nil
	}
}

func isReviewQueueSkillArtifactStatus(status string) bool {
	return oneOf(status, "review_required", "accepted", "rejected", "archived")
}

func oneOf(value string, candidates ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range candidates {
		if value == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/adapters/web"
	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	"odin-os/internal/core/workspaces"
	browserexecutor "odin-os/internal/executors/browser"
	approvalsvc "odin-os/internal/runtime/approvals"
	jobsvc "odin-os/internal/runtime/jobs"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

const reviewUsage = "usage: odin review list [--json] | odin review show <queue-id>|--id <queue-id> [--json] | odin review approve --id <queue-id> [--json] | odin review reject --id <queue-id> --reason <reason> [--json] | odin review act <queue-id> <accept|reject|archive|approve|deny|clarify|retry> [--json]"

type reviewQueueListView struct {
	Items []reviewQueueEntry `json:"items"`
}

type reviewQueueShowView struct {
	Entry  reviewQueueEntry `json:"entry"`
	Detail any              `json:"detail"`
}

type reviewApproveView struct {
	ReviewID    string            `json:"review_id"`
	SourceType  string            `json:"source_type"`
	SourceID    int64             `json:"source_id"`
	GoalID      int64             `json:"goal_id"`
	Decision    string            `json:"decision"`
	Status      string            `json:"status"`
	Transitions []string          `json:"transitions"`
	Goal        commands.GoalView `json:"goal"`
}

type reviewRejectView struct {
	ReviewID   string            `json:"review_id"`
	SourceType string            `json:"source_type"`
	SourceID   int64             `json:"source_id"`
	GoalID     int64             `json:"goal_id"`
	Decision   string            `json:"decision"`
	Status     string            `json:"status"`
	Reason     string            `json:"reason"`
	Goal       commands.GoalView `json:"goal"`
	Blocker    goalBlockerEntry  `json:"blocker"`
}

type reviewUnsupportedActionView struct {
	ReviewID   string            `json:"review_id"`
	SourceType string            `json:"source_type"`
	SourceID   int64             `json:"source_id"`
	GoalID     int64             `json:"goal_id"`
	BlockerID  int64             `json:"blocker_id"`
	Action     string            `json:"action"`
	Status     string            `json:"status"`
	Result     string            `json:"result"`
	Error      string            `json:"error"`
	Summary    string            `json:"summary"`
	Goal       commands.GoalView `json:"goal"`
	Blocker    goalBlockerEntry  `json:"blocker"`
}

type reviewQueueEntry struct {
	ReviewID               string   `json:"review_id,omitempty"`
	QueueID                string   `json:"queue_id"`
	SourceType             string   `json:"source_type"`
	SourceID               int64    `json:"source_id,omitempty"`
	ObjectID               int64    `json:"object_id"`
	ObjectKey              string   `json:"object_key"`
	GoalID                 int64    `json:"goal_id,omitempty"`
	Status                 string   `json:"status"`
	Reason                 string   `json:"reason,omitempty"`
	Title                  string   `json:"title,omitempty"`
	CreatedAt              string   `json:"created_at,omitempty"`
	ProjectScope           string   `json:"project_scope,omitempty"`
	Summary                string   `json:"summary,omitempty"`
	TaskID                 int64    `json:"task_id,omitempty"`
	TaskKey                string   `json:"task_key,omitempty"`
	TaskStatus             string   `json:"task_status,omitempty"`
	WorkKind               string   `json:"work_kind,omitempty"`
	Source                 string   `json:"source,omitempty"`
	Decision               string   `json:"decision,omitempty"`
	RetryEligible          *bool    `json:"retry_eligible,omitempty"`
	RetryBlockReason       string   `json:"retry_block_reason,omitempty"`
	RecoveryRecommendation string   `json:"recovery_recommendation,omitempty"`
	AllowedActions         []string `json:"allowed_actions"`
}

type reviewQueueRef struct {
	Kind string
	ID   int64
}

type goalBlockerReviewDetail struct {
	Goal    commands.GoalView `json:"goal"`
	Blocker goalBlockerEntry  `json:"blocker"`
}

type goalBlockerEntry struct {
	ID          int64  `json:"id"`
	GoalID      int64  `json:"goal_id"`
	Status      string `json:"status"`
	BlockerType string `json:"blocker_type,omitempty"`
	Summary     string `json:"summary"`
	CreatedBy   string `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at"`
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
		queueID, err := reviewShowID(remaining[1:])
		if err != nil {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewShow(ctx, app, queueID, jsonOutput, stdout)
	case "approve":
		queueID, err := reviewApproveID(remaining[1:])
		if err != nil {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewApprove(ctx, app, queueID, jsonOutput, stdout)
	case "reject":
		queueID, reason, err := reviewRejectOptions(remaining[1:])
		if err != nil {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewReject(ctx, app, queueID, reason, jsonOutput, stdout)
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

func reviewShowID(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	if len(args) == 2 && args[0] == "--id" {
		return args[1], nil
	}
	return "", fmt.Errorf(reviewUsage)
}

func reviewApproveID(args []string) (string, error) {
	if len(args) == 2 && args[0] == "--id" {
		return args[1], nil
	}
	return "", fmt.Errorf(reviewUsage)
}

func reviewRejectOptions(args []string) (string, string, error) {
	var queueID string
	var reason string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--id":
			if queueID != "" || index+1 >= len(args) {
				return "", "", fmt.Errorf(reviewUsage)
			}
			queueID = args[index+1]
			index++
		case "--reason":
			if reason != "" || index+1 >= len(args) {
				return "", "", fmt.Errorf(reviewUsage)
			}
			reason = args[index+1]
			index++
		default:
			return "", "", fmt.Errorf(reviewUsage)
		}
	}
	if strings.TrimSpace(queueID) == "" || strings.TrimSpace(reason) == "" {
		return "", "", fmt.Errorf(reviewUsage)
	}
	return queueID, reason, nil
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
	case "intake-goal", "goal", "goal-approval", "goal-blocker":
		if action != "approve" {
			return fmt.Errorf("goal review action must use review approve --id or review reject --id --reason")
		}
		return runReviewApprove(ctx, app, queueID, jsonOutput, stdout)
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
		if !oneOf(action, "approve", "deny", "clarify") {
			return fmt.Errorf("task approval action must be one of approve, deny, clarify")
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
	case "design-artifact":
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("design artifact action must be one of accept, reject, archive")
		}
		return runDesignArtifactReview(ctx, app, action, idRef, jsonOutput, stdout)
	case "context-pack":
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("context pack action must be one of accept, reject, archive")
		}
		return runContextPackReview(ctx, app, ref.ID, action, jsonOutput, stdout)
	case "failed-work":
		if action != "retry" {
			return fmt.Errorf("failed work action must be retry")
		}
		return runFailedWorkReviewRetry(ctx, app, ref.ID, jsonOutput, stdout)
	default:
		return fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

func runReviewReject(ctx context.Context, app bootstrap.App, queueID string, reason string, jsonOutput bool, stdout io.Writer) error {
	ref, err := parseReviewQueueRef(queueID)
	if err != nil {
		return err
	}
	if ref.Kind == "goal-blocker" && jsonOutput {
		return writeUnsupportedGoalBlockerReviewAction(ctx, app.Store, ref, "reject", stdout)
	}
	view, err := rejectGoalReviewItem(ctx, app.Store, ref, reason)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "review=%s decision=%s goal=%d status=%s blocker=%d reason=%q\n", view.ReviewID, view.Decision, view.GoalID, view.Status, view.Blocker.ID, view.Reason)
	return err
}

func runReviewApprove(ctx context.Context, app bootstrap.App, queueID string, jsonOutput bool, stdout io.Writer) error {
	ref, err := parseReviewQueueRef(queueID)
	if err != nil {
		return err
	}
	if ref.Kind == "goal-blocker" && jsonOutput {
		return writeUnsupportedGoalBlockerReviewAction(ctx, app.Store, ref, "approve", stdout)
	}
	view, err := approveGoalReviewItem(ctx, app.Store, ref)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "review=%s decision=%s goal=%d status=%s transitions=%s\n", view.ReviewID, view.Decision, view.GoalID, view.Status, strings.Join(view.Transitions, ","))
	return err
}

func writeUnsupportedGoalBlockerReviewAction(ctx context.Context, store *sqlite.Store, ref reviewQueueRef, action string, stdout io.Writer) error {
	blocker, goal, err := findGoalBlockerReviewDetail(ctx, store, ref.ID)
	if err != nil {
		return err
	}
	if err := commands.WriteJSON(stdout, reviewUnsupportedActionView{
		ReviewID:   fmt.Sprintf("goal-blocker:%d", blocker.ID),
		SourceType: "goal_blocker",
		SourceID:   blocker.ID,
		GoalID:     goal.ID,
		BlockerID:  blocker.ID,
		Action:     action,
		Status:     "unsupported",
		Result:     "not_resolved",
		Error:      "blocker_resolution_not_supported",
		Summary:    "goal blocker resolution is not implemented; inspect only",
		Goal:       newGoalView(goal),
		Blocker:    goalBlockerView(blocker),
	}); err != nil {
		return err
	}
	return fmt.Errorf("review %s does not support goal-blocker:%d; blocker resolution is not implemented", action, ref.ID)
}

func rejectGoalReviewItem(ctx context.Context, store *sqlite.Store, ref reviewQueueRef, reason string) (reviewRejectView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reviewRejectView{}, fmt.Errorf("review rejection reason is required")
	}
	reviewID := fmt.Sprintf("%s:%d", ref.Kind, ref.ID)
	sourceType := ""
	sourceID := ref.ID
	var goal sqlite.Goal
	var err error

	switch ref.Kind {
	case "intake-goal":
		item, err := findRawIntakeItem(ctx, store, rawIntakeKey(ref.ID))
		if err != nil {
			return reviewRejectView{}, err
		}
		if item.GoalID == nil {
			return reviewRejectView{}, fmt.Errorf("intake goal review %s has no linked goal", reviewID)
		}
		goal, err = store.GetGoal(ctx, *item.GoalID)
		if err != nil {
			return reviewRejectView{}, err
		}
		sourceType = "intake_goal_conversion"
		sourceID = item.ID
	case "goal":
		goal, err = store.GetGoal(ctx, ref.ID)
		if err != nil {
			return reviewRejectView{}, err
		}
		sourceType = "goal"
	case "goal-approval":
		goal, err = store.GetGoal(ctx, ref.ID)
		if err != nil {
			return reviewRejectView{}, err
		}
		sourceType = "goal"
	case "goal-blocker":
		return reviewRejectView{}, fmt.Errorf("review reject does not support goal-blocker:%d; blocker resolution is not implemented", ref.ID)
	default:
		return reviewRejectView{}, fmt.Errorf("review reject only supports intake-goal, goal, and goal-approval review items")
	}
	if goal.Status != sqlite.GoalStatusCreated && goal.Status != sqlite.GoalStatusPlanned {
		return reviewRejectView{}, fmt.Errorf("review reject requires goal status created or planned; goal %d is %s", goal.ID, goal.Status)
	}

	blocker, err := store.AddGoalBlocker(ctx, sqlite.AddGoalBlockerParams{
		GoalID:      goal.ID,
		Status:      "open",
		BlockerType: "review_rejected",
		Summary:     reason,
		DetailsJSON: `{"reason":"review_rejected"}`,
		CreatedBy:   "review",
	})
	if err != nil {
		return reviewRejectView{}, err
	}
	blocked, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
		GoalID: goal.ID,
		Status: sqlite.GoalStatusBlocked,
		Actor:  "review",
		Reason: "rejected via " + reviewID + ": " + reason,
	})
	if err != nil {
		return reviewRejectView{}, err
	}
	if err := store.RecordReviewRejected(ctx, sqlite.RecordReviewRejectedParams{
		ReviewID:   reviewID,
		SourceType: sourceType,
		SourceID:   sourceID,
		GoalID:     blocked.ID,
		BlockerID:  blocker.ID,
		Status:     blocked.Status,
		Actor:      "review",
		Reason:     reason,
	}); err != nil {
		return reviewRejectView{}, err
	}
	return reviewRejectView{
		ReviewID:   reviewID,
		SourceType: sourceType,
		SourceID:   sourceID,
		GoalID:     blocked.ID,
		Decision:   "rejected",
		Status:     string(blocked.Status),
		Reason:     reason,
		Goal:       newGoalView(blocked),
		Blocker:    goalBlockerView(blocker),
	}, nil
}

func approveGoalReviewItem(ctx context.Context, store *sqlite.Store, ref reviewQueueRef) (reviewApproveView, error) {
	reviewID := fmt.Sprintf("%s:%d", ref.Kind, ref.ID)
	sourceType := ""
	sourceID := ref.ID
	var goal sqlite.Goal
	var err error

	switch ref.Kind {
	case "intake-goal":
		item, err := findRawIntakeItem(ctx, store, rawIntakeKey(ref.ID))
		if err != nil {
			return reviewApproveView{}, err
		}
		if item.GoalID == nil {
			return reviewApproveView{}, fmt.Errorf("intake goal review %s has no linked goal", reviewID)
		}
		goal, err = store.GetGoal(ctx, *item.GoalID)
		if err != nil {
			return reviewApproveView{}, err
		}
		sourceType = "intake_goal_conversion"
		sourceID = item.ID
	case "goal":
		goal, err = store.GetGoal(ctx, ref.ID)
		if err != nil {
			return reviewApproveView{}, err
		}
		sourceType = "goal"
	case "goal-approval":
		goal, err = store.GetGoal(ctx, ref.ID)
		if err != nil {
			return reviewApproveView{}, err
		}
		sourceType = "goal"
	case "goal-blocker":
		return reviewApproveView{}, fmt.Errorf("review approve does not support goal-blocker:%d; blocker resolution is not implemented", ref.ID)
	default:
		return reviewApproveView{}, fmt.Errorf("review approve only supports intake-goal, goal, and goal-approval review items")
	}

	approved, transitions, err := approveGoalThroughReview(ctx, store, goal, reviewID)
	if err != nil {
		return reviewApproveView{}, err
	}
	if err := store.RecordReviewApproved(ctx, sqlite.RecordReviewApprovedParams{
		ReviewID:   reviewID,
		SourceType: sourceType,
		SourceID:   sourceID,
		GoalID:     approved.ID,
		Status:     approved.Status,
		Actor:      "review",
		Reason:     "operator approved goal-derived review item",
	}); err != nil {
		return reviewApproveView{}, err
	}

	return reviewApproveView{
		ReviewID:    reviewID,
		SourceType:  sourceType,
		SourceID:    sourceID,
		GoalID:      approved.ID,
		Decision:    "approved",
		Status:      string(approved.Status),
		Transitions: transitions,
		Goal:        newGoalView(approved),
	}, nil
}

func approveGoalThroughReview(ctx context.Context, store *sqlite.Store, goal sqlite.Goal, reviewID string) (sqlite.Goal, []string, error) {
	transitions := make([]string, 0, 2)
	switch goal.Status {
	case sqlite.GoalStatusCreated:
		planned, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: goal.ID,
			Status: sqlite.GoalStatusPlanned,
			Actor:  "review",
			Reason: "approved via " + reviewID,
		})
		if err != nil {
			return sqlite.Goal{}, nil, err
		}
		transitions = append(transitions, string(planned.Status))
		approved, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: planned.ID,
			Status: sqlite.GoalStatusApprovedForExecution,
			Actor:  "review",
			Reason: "approved via " + reviewID,
		})
		if err != nil {
			return sqlite.Goal{}, nil, err
		}
		transitions = append(transitions, string(approved.Status))
		return approved, transitions, nil
	case sqlite.GoalStatusPlanned:
		approved, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: goal.ID,
			Status: sqlite.GoalStatusApprovedForExecution,
			Actor:  "review",
			Reason: "approved via " + reviewID,
		})
		if err != nil {
			return sqlite.Goal{}, nil, err
		}
		transitions = append(transitions, string(approved.Status))
		return approved, transitions, nil
	default:
		return sqlite.Goal{}, nil, fmt.Errorf("review approve requires goal status created or planned; goal %d is %s", goal.ID, goal.Status)
	}
}

func listReviewQueueEntries(ctx context.Context, app bootstrap.App) ([]reviewQueueEntry, error) {
	intakeItems, err := app.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, err
	}

	entries := make([]reviewQueueEntry, 0)
	convertedGoalIDs := map[int64]bool{}
	for _, item := range intakeItems {
		if item.GoalID != nil {
			goal, err := app.Store.GetGoal(ctx, *item.GoalID)
			if err != nil {
				return nil, err
			}
			entry, err := reviewEntryFromIntakeGoalItem(item)
			if err != nil {
				return nil, err
			}
			convertedGoalIDs[*item.GoalID] = true
			if goal.Status == sqlite.GoalStatusBlocked {
				continue
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

	goalEntries, err := listGoalReviewEntries(ctx, app.Store, convertedGoalIDs)
	if err != nil {
		return nil, err
	}
	entries = append(entries, goalEntries...)

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
		if isDesignArtifactType(artifact.ArtifactType) {
			if !isReviewQueueDesignArtifactStatus(artifact.Status) {
				continue
			}
			entry, err := reviewEntryFromDesignArtifact(ctx, app.Store, artifact)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
			continue
		}
		if !isReviewQueueSkillArtifactStatus(artifact.Status) {
			continue
		}
		entry, err := reviewEntryFromSkillArtifact(ctx, app.Store, artifact)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	contextPacks, err := runtimeknowledge.Service{Store: app.Store}.ListContextPackProposals(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, proposal := range contextPacks {
		entries = append(entries, reviewEntryFromContextPackProposal(proposal))
	}

	taskViews, err := projections.ListTaskStatusViews(ctx, app.Store.DB())
	if err != nil {
		return nil, err
	}
	for _, task := range taskViews {
		storedTask, err := app.Store.GetTask(ctx, task.TaskID)
		if err != nil {
			return nil, err
		}
		if artifacts := browserexecutor.ParseTaskEvidenceArtifacts(storedTask.ArtifactsJSON); len(artifacts) > 0 && strings.EqualFold(strings.TrimSpace(task.BlockedReason), "browser_evidence_review") {
			entries = append(entries, reviewEntryFromBrowserEvidence(task, artifacts[len(artifacts)-1]))
			continue
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") && strings.EqualFold(strings.TrimSpace(task.BlockedReason), "clarification_requested") {
			entries = append(entries, reviewEntryFromWorkClarification(task))
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(task.Status), "failed") {
			continue
		}
		entries = append(entries, reviewEntryFromFailedTask(task))
	}
	return entries, nil
}

func reviewQueueDetail(ctx context.Context, app bootstrap.App, ref reviewQueueRef, includePayload bool) (reviewQueueEntry, any, error) {
	switch ref.Kind {
	case "intake-goal":
		item, err := findRawIntakeItem(ctx, app.Store, rawIntakeKey(ref.ID))
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, err := reviewEntryFromIntakeGoalItem(item)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		view, err := rawIntakeView(item, includePayload)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, rawIntakeItemEnvelope{IntakeItem: view}, nil
	case "goal", "goal-approval":
		goal, err := app.Store.GetGoal(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry := reviewEntryFromGoal(goal)
		if ref.Kind == "goal-approval" {
			entry = reviewEntryFromPlannedGoal(goal)
		}
		return entry, commands.GoalEnvelope{Goal: newGoalView(goal)}, nil
	case "goal-blocker":
		blocker, goal, err := findGoalBlockerReviewDetail(ctx, app.Store, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry := reviewEntryFromGoalBlocker(goal, blocker)
		return entry, goalBlockerReviewDetail{
			Goal:    newGoalView(goal),
			Blocker: goalBlockerView(blocker),
		}, nil
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
	case "browser-evidence":
		task, err := app.Store.GetTask(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		project, err := app.Store.GetProject(ctx, task.ProjectID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		artifacts := browserexecutor.ParseTaskEvidenceArtifacts(task.ArtifactsJSON)
		if len(artifacts) == 0 {
			return reviewQueueEntry{}, nil, fmt.Errorf("browser evidence review %d has no browser evidence artifacts", ref.ID)
		}
		taskView := projections.TaskStatusView{
			TaskID:        task.ID,
			ProjectID:     task.ProjectID,
			ProjectKey:    project.Key,
			TaskKey:       task.Key,
			Title:         task.Title,
			RequestedBy:   task.RequestedBy,
			WorkKind:      task.WorkKind,
			Status:        task.Status,
			Scope:         task.Scope,
			BlockedReason: task.BlockedReason,
		}
		entry := reviewEntryFromBrowserEvidence(taskView, artifacts[len(artifacts)-1])
		return entry, struct {
			TaskID   int64                              `json:"task_id"`
			TaskKey  string                             `json:"task_key"`
			Status   string                             `json:"status"`
			Evidence []browserexecutor.EvidenceArtifact `json:"evidence"`
		}{
			TaskID:   task.ID,
			TaskKey:  task.Key,
			Status:   task.Status,
			Evidence: artifacts,
		}, nil
	case "design-artifact":
		artifact, err := app.Store.GetSkillArtifact(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, err := reviewEntryFromDesignArtifact(ctx, app.Store, artifact)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, renderSkillReviewArtifact(artifact), nil
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
	case "context-pack":
		proposal, err := runtimeknowledge.Service{Store: app.Store}.GetContextPackProposal(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry := reviewEntryFromContextPackProposal(proposal)
		return entry, commands.NewKnowledgeContextPackProposalView(proposal), nil
	case "failed-work":
		task, err := app.Store.GetTask(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, detail, err := reviewFailedTaskDetail(ctx, app.Store, task)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, detail, nil
	default:
		return reviewQueueEntry{}, nil, fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

type failedWorkReviewDetail struct {
	TaskID                 int64                  `json:"task_id"`
	TaskKey                string                 `json:"task_key"`
	TaskStatus             string                 `json:"task_status"`
	ProjectKey             string                 `json:"project_key"`
	Decision               string                 `json:"decision"`
	RetryEligible          bool                   `json:"retry_eligible"`
	RetryBlockReason       string                 `json:"retry_block_reason,omitempty"`
	RecoveryRecommendation string                 `json:"recovery_recommendation"`
	RetryCount             int                    `json:"retry_count"`
	MaxAttempts            int                    `json:"max_attempts"`
	LastError              string                 `json:"last_error,omitempty"`
	RunAttempts            []failedWorkRunAttempt `json:"run_attempts"`
}

type failedWorkRunAttempt struct {
	RunID    int64  `json:"run_id"`
	Status   string `json:"status"`
	Attempt  int    `json:"attempt"`
	Executor string `json:"executor"`
}

type failedWorkRetryView struct {
	Retried                bool                    `json:"retried"`
	Reason                 string                  `json:"reason"`
	Decision               string                  `json:"decision"`
	RetryEligible          bool                    `json:"retry_eligible"`
	RecoveryRecommendation string                  `json:"recovery_recommendation,omitempty"`
	Task                   failedWorkRetryTaskView `json:"task,omitempty"`
}

type failedWorkRetryTaskView struct {
	ID             int64  `json:"id"`
	ProjectID      int64  `json:"project_id"`
	Key            string `json:"key"`
	Status         string `json:"status"`
	RetryCount     int    `json:"retry_count"`
	MaxAttempts    int    `json:"max_attempts"`
	LastError      string `json:"last_error,omitempty"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
	NextEligibleAt string `json:"next_eligible_at,omitempty"`
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
		ReviewID:       fmt.Sprintf("%s:%d", kind, item.ID),
		QueueID:        fmt.Sprintf("%s:%d", kind, item.ID),
		SourceType:     sourceType,
		SourceID:       item.ID,
		ObjectID:       item.ID,
		ObjectKey:      rawIntakeKey(item.ID),
		Status:         item.Status,
		Reason:         item.Status,
		Title:          item.Subject,
		CreatedAt:      formatReviewTime(item.CreatedAt),
		ProjectScope:   view.ProjectKey,
		Summary:        firstNonBlank(item.Summary, item.Subject),
		AllowedActions: actions,
	}, nil
}

func reviewEntryFromIntakeGoalItem(item sqlite.IntakeItem) (reviewQueueEntry, error) {
	view, err := rawIntakeView(item, false)
	if err != nil {
		return reviewQueueEntry{}, err
	}
	goalID := int64(0)
	if item.GoalID != nil {
		goalID = *item.GoalID
	}
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("intake-goal:%d", item.ID),
		QueueID:        fmt.Sprintf("intake-goal:%d", item.ID),
		SourceType:     "intake_goal_conversion",
		SourceID:       item.ID,
		ObjectID:       item.ID,
		ObjectKey:      rawIntakeKey(item.ID),
		GoalID:         goalID,
		Status:         item.Status,
		Reason:         "intake_routed_to_goal_review_required",
		Title:          item.Subject,
		CreatedAt:      formatReviewTime(item.UpdatedAt),
		ProjectScope:   view.ProjectKey,
		Summary:        firstNonBlank(item.Summary, item.Subject),
		AllowedActions: []string{},
	}, nil
}

func listGoalReviewEntries(ctx context.Context, store *sqlite.Store, convertedGoalIDs map[int64]bool) ([]reviewQueueEntry, error) {
	goals, err := store.ListGoals(ctx, sqlite.ListGoalsParams{})
	if err != nil {
		return nil, err
	}
	openBlockers, err := store.ListGoalBlockers(ctx, sqlite.ListGoalBlockersParams{Status: "open"})
	if err != nil {
		return nil, err
	}
	blockersByGoal := make(map[int64][]sqlite.GoalBlocker)
	for _, blocker := range openBlockers {
		blockersByGoal[blocker.GoalID] = append(blockersByGoal[blocker.GoalID], blocker)
	}

	entries := make([]reviewQueueEntry, 0)
	for _, goal := range goals {
		switch goal.Status {
		case sqlite.GoalStatusCreated:
			if convertedGoalIDs[goal.ID] {
				continue
			}
			entries = append(entries, reviewEntryFromGoal(goal))
		case sqlite.GoalStatusPlanned:
			entries = append(entries, reviewEntryFromPlannedGoal(goal))
		case sqlite.GoalStatusBlocked:
			for _, blocker := range blockersByGoal[goal.ID] {
				entries = append(entries, reviewEntryFromGoalBlocker(goal, blocker))
			}
		}
	}
	return entries, nil
}

func reviewEntryFromGoal(goal sqlite.Goal) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("goal:%d", goal.ID),
		QueueID:        fmt.Sprintf("goal:%d", goal.ID),
		SourceType:     "goal",
		SourceID:       goal.ID,
		ObjectID:       goal.ID,
		ObjectKey:      fmt.Sprintf("goal-%d", goal.ID),
		GoalID:         goal.ID,
		Status:         string(goal.Status),
		Reason:         "goal_created_needs_planning",
		Title:          goal.Title,
		CreatedAt:      formatReviewTime(goal.CreatedAt),
		Summary:        firstNonBlank(goal.Description, goal.Title),
		AllowedActions: []string{},
	}
}

func reviewEntryFromPlannedGoal(goal sqlite.Goal) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("goal-approval:%d", goal.ID),
		QueueID:        fmt.Sprintf("goal-approval:%d", goal.ID),
		SourceType:     "goal",
		SourceID:       goal.ID,
		ObjectID:       goal.ID,
		ObjectKey:      fmt.Sprintf("goal-%d", goal.ID),
		GoalID:         goal.ID,
		Status:         string(goal.Status),
		Reason:         "goal_planned_awaiting_approval",
		Title:          goal.Title,
		CreatedAt:      formatReviewTime(goal.UpdatedAt),
		Summary:        firstNonBlank(goal.Description, goal.Title),
		AllowedActions: []string{},
	}
}

func reviewEntryFromGoalBlocker(goal sqlite.Goal, blocker sqlite.GoalBlocker) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("goal-blocker:%d", blocker.ID),
		QueueID:        fmt.Sprintf("goal-blocker:%d", blocker.ID),
		SourceType:     "goal_blocker",
		SourceID:       blocker.ID,
		ObjectID:       blocker.ID,
		ObjectKey:      fmt.Sprintf("goal-blocker-%d", blocker.ID),
		GoalID:         goal.ID,
		Status:         blocker.Status,
		Reason:         firstNonBlank(blocker.Summary, blocker.BlockerType),
		Title:          goal.Title,
		CreatedAt:      formatReviewTime(blocker.CreatedAt),
		Summary:        firstNonBlank(blocker.Summary, goal.Title),
		AllowedActions: []string{},
	}
}

func findGoalBlockerReviewDetail(ctx context.Context, store *sqlite.Store, blockerID int64) (sqlite.GoalBlocker, sqlite.Goal, error) {
	blockers, err := store.ListGoalBlockers(ctx, sqlite.ListGoalBlockersParams{})
	if err != nil {
		return sqlite.GoalBlocker{}, sqlite.Goal{}, err
	}
	for _, blocker := range blockers {
		if blocker.ID != blockerID {
			continue
		}
		goal, err := store.GetGoal(ctx, blocker.GoalID)
		if err != nil {
			return sqlite.GoalBlocker{}, sqlite.Goal{}, err
		}
		return blocker, goal, nil
	}
	return sqlite.GoalBlocker{}, sqlite.Goal{}, fmt.Errorf("goal blocker %d not found", blockerID)
}

func goalBlockerView(blocker sqlite.GoalBlocker) goalBlockerEntry {
	return goalBlockerEntry{
		ID:          blocker.ID,
		GoalID:      blocker.GoalID,
		Status:      blocker.Status,
		BlockerType: blocker.BlockerType,
		Summary:     blocker.Summary,
		CreatedBy:   blocker.CreatedBy,
		CreatedAt:   formatReviewTime(blocker.CreatedAt),
	}
}

func reviewEntryFromPendingApproval(view projections.PendingApprovalView) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("approval:%d", view.ApprovalID),
		QueueID:        fmt.Sprintf("approval:%d", view.ApprovalID),
		SourceType:     "task_approval",
		SourceID:       view.ApprovalID,
		ObjectID:       view.ApprovalID,
		ObjectKey:      fmt.Sprintf("approval-%d", view.ApprovalID),
		Status:         view.Status,
		Reason:         "task_approval_pending",
		Title:          view.TaskKey,
		CreatedAt:      view.RequestedAt,
		ProjectScope:   view.ProjectKey,
		Summary:        view.TaskKey,
		AllowedActions: []string{"approve", "deny", "clarify"},
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
		ReviewID:       fmt.Sprintf("approval:%d", detail.Approval.ID),
		QueueID:        fmt.Sprintf("approval:%d", detail.Approval.ID),
		SourceType:     "task_approval",
		SourceID:       detail.Approval.ID,
		ObjectID:       detail.Approval.ID,
		ObjectKey:      fmt.Sprintf("approval-%d", detail.Approval.ID),
		Status:         detail.Approval.Status,
		Reason:         "task_approval_" + detail.Approval.Status,
		Title:          detail.Task.Key,
		CreatedAt:      formatReviewTime(detail.Approval.RequestedAt),
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
		ReviewID:       fmt.Sprintf("skill-artifact:%d", artifact.ID),
		QueueID:        fmt.Sprintf("skill-artifact:%d", artifact.ID),
		SourceType:     "skill_artifact",
		SourceID:       artifact.ID,
		ObjectID:       artifact.ID,
		ObjectKey:      fmt.Sprintf("skill-artifact-%d", artifact.ID),
		Status:         artifact.Status,
		Reason:         artifact.Status,
		Title:          artifact.Summary,
		CreatedAt:      formatReviewTime(artifact.CreatedAt),
		ProjectScope:   projectScope,
		Summary:        artifact.Summary,
		AllowedActions: skillArtifactAllowedActions(artifact.Status),
	}, nil
}

func reviewEntryFromDesignArtifact(ctx context.Context, store *sqlite.Store, artifact sqlite.SkillArtifact) (reviewQueueEntry, error) {
	projectScope := ""
	if artifact.ProjectID != nil {
		project, err := store.GetProject(ctx, *artifact.ProjectID)
		if err != nil {
			return reviewQueueEntry{}, err
		}
		projectScope = project.Key
	}

	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("design-artifact:%d", artifact.ID),
		QueueID:        fmt.Sprintf("design-artifact:%d", artifact.ID),
		SourceType:     "design_artifact",
		SourceID:       artifact.ID,
		ObjectID:       artifact.ID,
		ObjectKey:      fmt.Sprintf("design-artifact-%d", artifact.ID),
		Status:         artifact.Status,
		Reason:         artifact.Status,
		Title:          artifact.Summary,
		CreatedAt:      formatReviewTime(artifact.CreatedAt),
		ProjectScope:   projectScope,
		Summary:        artifact.Summary,
		AllowedActions: designArtifactReviewAllowedActions(artifact.Status),
	}, nil
}

func reviewEntryFromContextPackProposal(proposal runtimeknowledge.ContextPackProposal) reviewQueueEntry {
	projectScope := proposal.ContextPack.ProjectKey
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("context-pack:%d", proposal.Packet.ID),
		QueueID:        fmt.Sprintf("context-pack:%d", proposal.Packet.ID),
		SourceType:     "context_pack",
		SourceID:       proposal.Packet.ID,
		ObjectID:       proposal.Packet.ID,
		ObjectKey:      fmt.Sprintf("context-pack-%d", proposal.Packet.ID),
		Status:         proposal.Packet.Status,
		Reason:         proposal.Packet.Status,
		Title:          proposal.Packet.Summary,
		CreatedAt:      formatReviewTime(proposal.Packet.CreatedAt),
		ProjectScope:   projectScope,
		Summary:        proposal.Packet.Summary,
		TaskID:         proposal.ContextPack.Task.ID,
		TaskKey:        proposal.ContextPack.Task.Key,
		TaskStatus:     proposal.ContextPack.Task.Status,
		WorkKind:       proposal.ContextPack.Task.WorkKind,
		AllowedActions: runtimeknowledge.ContextPackAllowedActions(proposal.Packet.Status),
	}
}

func reviewEntryFromBrowserEvidence(task projections.TaskStatusView, artifact browserexecutor.EvidenceArtifact) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("browser-evidence:%d", task.TaskID),
		QueueID:        fmt.Sprintf("browser-evidence:%d", task.TaskID),
		SourceType:     "browser_evidence",
		SourceID:       task.TaskID,
		ObjectID:       task.TaskID,
		ObjectKey:      task.TaskKey,
		Status:         firstNonBlank(artifact.Status, "review_required"),
		Reason:         "browser_evidence_review",
		Title:          task.Title,
		ProjectScope:   task.ProjectKey,
		Summary:        firstNonBlank(artifact.Summary, task.Title),
		TaskID:         task.TaskID,
		TaskKey:        task.TaskKey,
		TaskStatus:     task.Status,
		WorkKind:       task.WorkKind,
		AllowedActions: []string{"inspect"},
	}
}

func reviewEntryFromWorkClarification(task projections.TaskStatusView) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("work-clarification:%d", task.TaskID),
		QueueID:        fmt.Sprintf("work-clarification:%d", task.TaskID),
		SourceType:     "work_clarification",
		SourceID:       task.TaskID,
		ObjectID:       task.TaskID,
		ObjectKey:      task.TaskKey,
		Status:         task.Status,
		Reason:         "clarification_requested",
		Title:          task.Title,
		ProjectScope:   task.ProjectKey,
		Summary:        firstNonBlank(task.LastError, task.Title),
		TaskID:         task.TaskID,
		TaskKey:        task.TaskKey,
		TaskStatus:     task.Status,
		WorkKind:       task.WorkKind,
		AllowedActions: []string{"inspect"},
	}
}

func recoveryWorkKindForTask(workKind string, lastError string) string {
	if strings.Contains(strings.ToLower(lastError), "browser") || strings.Contains(strings.ToLower(lastError), "huginn") {
		return "browser_evidence"
	}
	return workKind
}

func reviewEntryFromFailedTask(task projections.TaskStatusView) reviewQueueEntry {
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    recoveryWorkKindForTask(task.WorkKind, task.LastError),
		RequestedBy: task.RequestedBy,
	})
	retryEligible := guidance.RetryEligible
	return reviewQueueEntry{
		ReviewID:               fmt.Sprintf("failed-work:%d", task.TaskID),
		QueueID:                fmt.Sprintf("failed-work:%d", task.TaskID),
		SourceType:             "failed_work",
		SourceID:               task.TaskID,
		ObjectID:               task.TaskID,
		ObjectKey:              task.TaskKey,
		Status:                 task.Status,
		Reason:                 guidance.Decision,
		Title:                  task.Title,
		ProjectScope:           task.ProjectKey,
		Summary:                firstNonBlank(task.LastError, task.Title),
		TaskID:                 task.TaskID,
		TaskKey:                task.TaskKey,
		TaskStatus:             task.Status,
		WorkKind:               task.WorkKind,
		Source:                 guidance.Source,
		Decision:               guidance.Decision,
		RetryEligible:          &retryEligible,
		RetryBlockReason:       retryBlockReason(guidance.Decision, guidance.RetryEligible),
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		AllowedActions:         []string{"retry"},
	}
}

func reviewFailedTaskDetail(ctx context.Context, store *sqlite.Store, task sqlite.Task) (reviewQueueEntry, failedWorkReviewDetail, error) {
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return reviewQueueEntry{}, failedWorkReviewDetail{}, err
	}
	taskView := projections.TaskStatusView{
		TaskID:      task.ID,
		ProjectID:   task.ProjectID,
		ProjectKey:  project.Key,
		TaskKey:     task.Key,
		Title:       task.Title,
		RequestedBy: task.RequestedBy,
		WorkKind:    task.WorkKind,
		Status:      task.Status,
		Scope:       task.Scope,
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		LastError:   task.LastError,
	}
	entry := reviewEntryFromFailedTask(taskView)
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    recoveryWorkKindForTask(task.WorkKind, task.LastError),
		RequestedBy: task.RequestedBy,
	})
	runs, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		return reviewQueueEntry{}, failedWorkReviewDetail{}, err
	}
	attempts := make([]failedWorkRunAttempt, 0)
	for _, run := range runs {
		if run.TaskID != task.ID {
			continue
		}
		attempts = append(attempts, failedWorkRunAttempt{
			RunID:    run.RunID,
			Status:   run.Status,
			Attempt:  run.Attempt,
			Executor: run.Executor,
		})
	}
	return entry, failedWorkReviewDetail{
		TaskID:                 task.ID,
		TaskKey:                task.Key,
		TaskStatus:             task.Status,
		ProjectKey:             project.Key,
		Decision:               guidance.Decision,
		RetryEligible:          guidance.RetryEligible,
		RetryBlockReason:       retryBlockReason(guidance.Decision, guidance.RetryEligible),
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		RetryCount:             task.RetryCount,
		MaxAttempts:            task.MaxAttempts,
		LastError:              task.LastError,
		RunAttempts:            attempts,
	}, nil
}

func parseReviewQueueRef(queueID string) (reviewQueueRef, error) {
	queueID = strings.TrimSpace(queueID)
	parts := strings.SplitN(queueID, ":", 2)
	if len(parts) != 2 {
		return reviewQueueRef{}, fmt.Errorf("review queue id must look like intake-goal:<id>, goal:<id>, goal-approval:<id>, goal-blocker:<id>, intake-review:<id>, intake-approval:<id>, approval:<id>, browser-evidence:<id>, design-artifact:<id>, skill-artifact:<id>, context-pack:<id>, or failed-work:<id>")
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	idRef := strings.TrimSpace(parts[1])
	switch kind {
	case "intake-goal", "intake-review", "intake-approval":
		idRef = strings.TrimPrefix(idRef, "intake-")
	case "goal", "goal-approval":
		idRef = strings.TrimPrefix(idRef, "goal-")
	case "goal-blocker":
		idRef = strings.TrimPrefix(idRef, "goal-blocker-")
	case "approval":
		idRef = strings.TrimPrefix(idRef, "approval-")
	case "browser-evidence":
		idRef = strings.TrimPrefix(idRef, "browser-evidence-")
	case "design-artifact":
		idRef = strings.TrimPrefix(idRef, "design-artifact-")
	case "skill-artifact":
		idRef = strings.TrimPrefix(idRef, "skill-artifact-")
	case "context-pack":
		idRef = strings.TrimPrefix(idRef, "context-pack-")
	case "failed-work":
		idRef = strings.TrimPrefix(idRef, "task-")
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
		return []string{"approve", "deny", "clarify"}
	case "approved":
		return []string{"approve"}
	case "denied":
		return []string{"deny"}
	case "clarification_requested":
		return []string{"clarify"}
	default:
		return nil
	}
}

func skillArtifactAllowedActions(status string) []string {
	switch status {
	case "review_required":
		return []string{"accept", "reject", "archive"}
	case "needs_review":
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

func designArtifactReviewAllowedActions(status string) []string {
	return skillArtifactAllowedActions(status)
}

func runDesignArtifactReview(ctx context.Context, app bootstrap.App, action string, artifactRef string, jsonOutput bool, stdout io.Writer) error {
	if app.Store == nil {
		return fmt.Errorf("design artifact review requires runtime store")
	}

	artifactID, err := strconv.ParseInt(artifactRef, 10, 64)
	if err != nil || artifactID <= 0 {
		return fmt.Errorf("design artifact id must be a positive integer")
	}

	artifact, err := app.Store.GetSkillArtifact(ctx, artifactID)
	if err != nil {
		return err
	}
	if !isDesignArtifactType(artifact.ArtifactType) {
		return fmt.Errorf("design artifact %d is not a design artifact", artifactID)
	}

	if isDesignRequestArtifactType(artifact.ArtifactType) {
		return runDesignRequestArtifactReview(ctx, app, artifact, action, jsonOutput, stdout)
	}

	return runDesignOutputArtifactReview(ctx, app, artifact, action, jsonOutput, stdout)
}

func runDesignRequestArtifactReview(ctx context.Context, app bootstrap.App, artifact sqlite.SkillArtifact, action string, jsonOutput bool, stdout io.Writer) error {
	decision := ""
	status := ""
	reason := ""
	repeated := false
	outputArtifactID := int64(0)

	switch action {
	case "accept":
		decision = "accepted"
		status = "accepted"
		reason = "design request accepted by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designRequestQueue {
			return fmt.Errorf("design artifact %d cannot be accepted from status %s", artifact.ID, artifact.Status)
		}
		outputArtifact, executeErr := executeDesignRequest(ctx, app, artifact)
		if executeErr != nil {
			decision = "rejected"
			status = "rejected"
			reason = fmt.Sprintf("design execution failed: %v", executeErr)
			break
		}
		outputArtifactID = outputArtifact.ID
	case "reject":
		decision = "rejected"
		status = "rejected"
		reason = "design request rejected by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designRequestQueue {
			return fmt.Errorf("design artifact %d cannot be rejected from status %s", artifact.ID, artifact.Status)
		}
	case "archive":
		decision = "archived"
		status = "archived"
		reason = "design request archived by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designRequestQueue {
			return fmt.Errorf("design artifact %d cannot be archived from status %s", artifact.ID, artifact.Status)
		}
	default:
		return fmt.Errorf("design artifact action must be one of accept, reject, archive")
	}

	updated, err := app.Store.ReviewSkillArtifact(ctx, sqlite.ReviewSkillArtifactParams{
		ArtifactID:        artifact.ID,
		Decision:          decision,
		Status:            status,
		ReviewedBy:        "operator",
		Reason:            reason,
		Repeated:          repeated,
		WorkCreated:       false,
		FollowOnTaskID:    nil,
		FollowOnTaskKey:   "",
		FollowOnTaskState: "",
	})
	if err != nil {
		return err
	}

	result := struct {
		Artifact         skills.ReviewArtifact `json:"artifact"`
		Decision         string                `json:"decision"`
		Repeated         bool                  `json:"repeated"`
		OutputArtifactID int64                 `json:"output_artifact_id,omitempty"`
	}{
		Artifact:         renderSkillReviewArtifact(updated),
		Decision:         decision,
		Repeated:         repeated,
		OutputArtifactID: outputArtifactID,
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, result)
	}
	if outputArtifactID > 0 {
		_, err = fmt.Fprintf(stdout, "design_artifact=%d decision=%s status=%s repeated=%t output_artifact=%d\n", artifact.ID, decision, updated.Status, repeated, outputArtifactID)
		return err
	}
	_, err = fmt.Fprintf(stdout, "design_artifact=%d decision=%s status=%s repeated=%t\n", artifact.ID, decision, updated.Status, repeated)
	return err
}

func runDesignOutputArtifactReview(ctx context.Context, app bootstrap.App, artifact sqlite.SkillArtifact, action string, jsonOutput bool, stdout io.Writer) error {
	decision := ""
	status := ""
	reason := ""
	repeated := false

	switch action {
	case "accept":
		decision = "accepted"
		status = "accepted"
		reason = "design artifact accepted by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designArtifactQueue {
			return fmt.Errorf("design artifact %d cannot be accepted from status %s", artifact.ID, artifact.Status)
		}
	case "reject":
		decision = "rejected"
		status = "rejected"
		reason = "design artifact rejected by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designArtifactQueue {
			return fmt.Errorf("design artifact %d cannot be rejected from status %s", artifact.ID, artifact.Status)
		}
	case "archive":
		decision = "archived"
		status = "archived"
		reason = "design artifact archived by operator"
		if artifact.Status == status {
			repeated = true
			break
		}
		if artifact.Status != designArtifactQueue {
			return fmt.Errorf("design artifact %d cannot be archived from status %s", artifact.ID, artifact.Status)
		}
	default:
		return fmt.Errorf("design artifact action must be one of accept, reject, archive")
	}

	updated, err := app.Store.ReviewSkillArtifact(ctx, sqlite.ReviewSkillArtifactParams{
		ArtifactID:        artifact.ID,
		Decision:          decision,
		Status:            status,
		ReviewedBy:        "operator",
		Reason:            reason,
		Repeated:          repeated,
		WorkCreated:       false,
		FollowOnTaskID:    nil,
		FollowOnTaskKey:   "",
		FollowOnTaskState: "",
	})
	if err != nil {
		return err
	}

	result := struct {
		Artifact skills.ReviewArtifact `json:"artifact"`
		Decision string                `json:"decision"`
		Repeated bool                  `json:"repeated"`
	}{
		Artifact: renderSkillReviewArtifact(updated),
		Decision: decision,
		Repeated: repeated,
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, result)
	}
	_, err = fmt.Fprintf(stdout, "design_artifact=%d decision=%s status=%s repeated=%t\n", artifact.ID, decision, updated.Status, repeated)
	return err
}

func executeDesignRequest(ctx context.Context, app bootstrap.App, requestArtifact sqlite.SkillArtifact) (sqlite.SkillArtifact, error) {
	var requestPayload map[string]any
	if err := json.Unmarshal([]byte(requestArtifact.OutputJSON), &requestPayload); err != nil {
		requestPayload = map[string]any{}
	}

	requestSummary := strings.TrimSpace(requestArtifact.Summary)
	if requestSummary == "" {
		requestSummary = strings.TrimSpace(requestArtifact.RawOutput)
	}

	if err := app.Store.RecordDesignExecutionStartedEvent(ctx, sqlite.RecordDesignExecutionStartedEventParams{
		RequestArtifactID: requestArtifact.ID,
		SkillKey:          requestArtifact.SkillKey,
		Scope:             requestArtifact.Scope,
		ToolKey:           strings.TrimSpace(requestArtifact.SkillKey),
		Summary:           requestSummary,
		ExecutionProfile:  "design_execution",
	}); err != nil {
		return sqlite.SkillArtifact{}, err
	}

	response, err := invocation.Service{RuntimeRoot: app.RuntimeRoot}.OpenDesign(ctx, web.OpenDesignRequest{
		ToolKey: strings.TrimSpace(requestArtifact.SkillKey),
		Input: web.OpenDesignInput{
			ArtifactID: fmt.Sprintf("%d", requestArtifact.ID),
			Artifact: map[string]any{
				"id":       requestArtifact.ID,
				"key":      requestArtifact.SkillKey,
				"scope":    requestArtifact.Scope,
				"status":   requestArtifact.Status,
				"summary":  requestSummary,
				"payload":  requestPayload,
				"raw_body": requestArtifact.RawOutput,
			},
		},
	})
	if err != nil {
		return sqlite.SkillArtifact{}, err
	}

	outputSummary := strings.TrimSpace(response.Summary)
	if outputSummary == "" {
		outputSummary = firstNonBlank(requestSummary, "design output")
	}

	outputPayload := map[string]any{
		"request_id": requestArtifact.ID,
		"summary":    outputSummary,
		"tool_key":   response.ToolKey,
	}
	if len(response.Artifacts) != 0 {
		outputPayload["artifacts"] = response.Artifacts
	}

	outputArtifact, err := app.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         requestArtifact.SkillKey,
		Scope:            requestArtifact.Scope,
		ProjectID:        requestArtifact.ProjectID,
		Status:           designArtifactQueue,
		ArtifactType:     designArtifactType,
		Summary:          outputSummary,
		OutputJSON:       artifactOutputJSON(outputPayload),
		RawOutput:        response.RawOutput,
		ExecutionProfile: "design_execution",
		PermissionsJSON:  requestArtifact.PermissionsJSON,
	})
	if err != nil {
		return sqlite.SkillArtifact{}, err
	}

	if err := app.Store.RecordDesignArtifactCreatedEvent(ctx, sqlite.RecordDesignArtifactCreatedEventParams{
		RequestArtifactID: requestArtifact.ID,
		OutputArtifactID:  outputArtifact.ID,
		SkillKey:          outputArtifact.SkillKey,
		ProjectID:         requestArtifact.ProjectID,
		Scope:             outputArtifact.Scope,
		ArtifactType:      outputArtifact.ArtifactType,
		Status:            outputArtifact.Status,
		Summary:           outputArtifact.Summary,
	}); err != nil {
		return outputArtifact, err
	}
	return outputArtifact, nil
}

func isReviewQueueDesignArtifactStatus(status string) bool {
	return oneOf(status, designArtifactQueue, designRequestQueue, "accepted", "rejected", "archived")
}

func runFailedWorkReviewRetry(ctx context.Context, app bootstrap.App, taskID int64, jsonOutput bool, stdout io.Writer) error {
	queueID := fmt.Sprintf("failed-work:%d", taskID)
	outcome, err := (jobsvc.Service{Store: app.Store, Registry: app.Registry}).RetryFailedTaskFromReview(ctx, taskID, queueID)
	if err != nil {
		return err
	}
	view := failedWorkRetryOutcomeView(outcome)
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "retried=%t reason=%s decision=%s retry_eligible=%t task=%s status=%s retry_count=%d recommendation=%q\n", view.Retried, view.Reason, view.Decision, view.RetryEligible, view.Task.Key, view.Task.Status, view.Task.RetryCount, view.RecoveryRecommendation)
	return err
}

func runContextPackReview(ctx context.Context, app bootstrap.App, packetID int64, action string, jsonOutput bool, stdout io.Writer) error {
	outcome, err := runtimeknowledge.Service{Store: app.Store}.ReviewContextPackProposal(ctx, packetID, action)
	if err != nil {
		return err
	}
	view := struct {
		Decision string `json:"decision"`
		Status   string `json:"status"`
		Repeated bool   `json:"repeated"`
		Proposal any    `json:"proposal"`
	}{
		Decision: outcome.Decision,
		Status:   outcome.Status,
		Repeated: outcome.Repeated,
		Proposal: commands.NewKnowledgeContextPackProposalView(outcome.Proposal),
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "context_pack id=%d decision=%s status=%s repeated=%t\n", packetID, outcome.Decision, outcome.Status, outcome.Repeated)
	return err
}

func failedWorkRetryOutcomeView(outcome jobsvc.RetryOutcome) failedWorkRetryView {
	view := failedWorkRetryView{
		Retried:                outcome.Retried,
		Reason:                 outcome.Reason,
		Decision:               outcome.Decision,
		RetryEligible:          outcome.RetryEligible,
		RecoveryRecommendation: outcome.RecoveryRecommendation,
	}
	if view.Reason == "" {
		view.Reason = "unknown"
	}
	if view.Decision == "" {
		view.Decision = view.Reason
	}
	if outcome.Task.ID != 0 {
		view.Task = failedWorkRetryTaskView{
			ID:            outcome.Task.ID,
			ProjectID:     outcome.Task.ProjectID,
			Key:           outcome.Task.Key,
			Status:        outcome.Task.Status,
			RetryCount:    outcome.Task.RetryCount,
			MaxAttempts:   outcome.Task.MaxAttempts,
			LastError:     outcome.Task.LastError,
			BlockedReason: outcome.Task.BlockedReason,
		}
		if !outcome.Task.NextEligibleAt.IsZero() {
			view.Task.NextEligibleAt = outcome.Task.NextEligibleAt.UTC().Format(time.RFC3339Nano)
		}
	}
	return view
}

func retryBlockReason(decision string, eligible bool) string {
	if eligible {
		return ""
	}
	return decision
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

func formatReviewTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

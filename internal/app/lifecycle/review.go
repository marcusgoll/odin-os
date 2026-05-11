package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	"odin-os/internal/core/followups"
	"odin-os/internal/core/workspaces"
	approvalsvc "odin-os/internal/runtime/approvals"
	jobsvc "odin-os/internal/runtime/jobs"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
)

const reviewUsage = "usage: odin review list [--json] | odin review show <queue-id>|--id <queue-id> [--json] | odin review approve --id <queue-id> [--json] | odin review reject --id <queue-id> --reason <reason> [--json] | odin review act <queue-id> <accept|reject|archive|approve|deny|clarify|retry|follow-up> [--dry-run] [--json]"

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

type reviewActionReceipt struct {
	ReviewID         string          `json:"review_id"`
	QueueID          string          `json:"queue_id"`
	SourceType       string          `json:"source_type"`
	SourceID         int64           `json:"source_id,omitempty"`
	Action           string          `json:"action"`
	Status           string          `json:"status"`
	Result           string          `json:"result"`
	Supported        bool            `json:"supported"`
	MutationScope    string          `json:"mutation_scope"`
	ApprovalRequired bool            `json:"approval_required"`
	ApprovalStatus   string          `json:"approval_status,omitempty"`
	ResolverSupport  string          `json:"resolver_support,omitempty"`
	Mutated          bool            `json:"mutated"`
	AuditEvent       string          `json:"audit_event,omitempty"`
	Error            string          `json:"error,omitempty"`
	NextStep         string          `json:"next_step,omitempty"`
	SourceResult     json.RawMessage `json:"source_result,omitempty"`
}

type failedWorkGitHubIssueProposal struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type failedWorkFollowUpProposal struct {
	ReviewID               string                        `json:"review_id"`
	TaskID                 int64                         `json:"task_id"`
	TaskKey                string                        `json:"task_key"`
	ProjectKey             string                        `json:"project_key"`
	Title                  string                        `json:"title"`
	Destination            string                        `json:"destination"`
	ApprovalRequired       bool                          `json:"approval_required"`
	RecoveryRecommendation string                        `json:"recovery_recommendation"`
	GitHubIssue            failedWorkGitHubIssueProposal `json:"github_issue"`
}

type failedWorkFollowUpOutcomeView struct {
	Action             string                        `json:"action"`
	ReviewID           string                        `json:"review_id"`
	DryRun             bool                          `json:"dry_run"`
	Created            bool                          `json:"created"`
	ApprovalRequired   bool                          `json:"approval_required"`
	GitHubIssueCreated bool                          `json:"github_issue_created"`
	GitHubIssue        failedWorkGitHubIssueProposal `json:"github_issue"`
	Proposal           failedWorkFollowUpProposal    `json:"proposal"`
	FollowUp           *commands.FollowUpView        `json:"follow_up,omitempty"`
}

type reviewQueueEntry struct {
	ReviewID               string   `json:"review_id,omitempty"`
	QueueID                string   `json:"queue_id"`
	Type                   string   `json:"type,omitempty"`
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
	Risk                   string   `json:"risk,omitempty"`
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
		queueID, action, dryRun, err := reviewActOptions(remaining[1:])
		if err != nil {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewAct(ctx, app, queueID, action, dryRun, jsonOutput, stdout)
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
		if err := writeReviewQueueEntryHuman(stdout, entry); err != nil {
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

func reviewActOptions(args []string) (string, string, bool, error) {
	if len(args) < 2 {
		return "", "", false, fmt.Errorf(reviewUsage)
	}
	queueID := strings.TrimSpace(args[0])
	action := strings.TrimSpace(args[1])
	if queueID == "" || action == "" {
		return "", "", false, fmt.Errorf(reviewUsage)
	}
	dryRun := false
	for _, arg := range args[2:] {
		switch arg {
		case "--dry-run":
			if dryRun {
				return "", "", false, fmt.Errorf("duplicate --dry-run flag")
			}
			dryRun = true
		default:
			return "", "", false, fmt.Errorf(reviewUsage)
		}
	}
	return queueID, action, dryRun, nil
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
	return writeReviewQueueEntryHuman(stdout, entry)
}

func writeReviewQueueEntryHuman(stdout io.Writer, entry reviewQueueEntry) error {
	if _, err := fmt.Fprintf(
		stdout,
		"review=%s type=%s source=%s source_type=%s risk=%s reason=%s status=%s object=%s actions=%s\n",
		entry.QueueID,
		reviewHumanValue(entry.Type),
		reviewHumanValue(entry.Source),
		reviewHumanValue(entry.SourceType),
		reviewHumanValue(entry.Risk),
		reviewHumanValue(entry.Reason),
		reviewHumanValue(entry.Status),
		reviewHumanValue(entry.ObjectKey),
		reviewActionsHuman(entry.AllowedActions),
	); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "next_steps=%s\n", reviewNextStepsHuman(entry))
	return err
}

func reviewActionsHuman(actions []string) string {
	if len(actions) == 0 {
		return "none"
	}
	return strings.Join(actions, ",")
}

func reviewHumanValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}

func reviewNextStepsHuman(entry reviewQueueEntry) string {
	if len(entry.AllowedActions) == 0 {
		return fmt.Sprintf("inspect with odin review show %s; no direct review action available", entry.QueueID)
	}
	return fmt.Sprintf("inspect with odin review show %s; act with odin review act %s <%s>", entry.QueueID, entry.QueueID, strings.Join(entry.AllowedActions, "|"))
}

func runReviewAct(ctx context.Context, app bootstrap.App, queueID string, action string, dryRun bool, jsonOutput bool, stdout io.Writer) error {
	ref, err := parseReviewQueueRef(queueID)
	if err != nil {
		return err
	}
	action = strings.ToLower(strings.TrimSpace(action))

	if !jsonOutput {
		return runReviewActSource(ctx, app, ref, action, dryRun, false, stdout)
	}

	receipt, err := reviewActionPreflight(ctx, app, ref, action, dryRun)
	if err != nil {
		if receipt.QueueID != "" {
			if writeErr := commands.WriteJSON(stdout, receipt); writeErr != nil {
				return writeErr
			}
		}
		return err
	}

	var sourceOutput bytes.Buffer
	if err := runReviewActSource(ctx, app, ref, action, dryRun, true, &sourceOutput); err != nil {
		return err
	}
	receipt.SourceResult = compactReviewActionSourceResult(sourceOutput.Bytes())
	applyReviewActionSourceResult(&receipt)
	return commands.WriteJSON(stdout, receipt)
}

func runReviewActSource(ctx context.Context, app bootstrap.App, ref reviewQueueRef, action string, dryRun bool, jsonOutput bool, stdout io.Writer) error {
	idRef := strconv.FormatInt(ref.ID, 10)

	switch ref.Kind {
	case "intake-goal", "goal", "goal-approval", "goal-blocker":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if action != "approve" {
			return fmt.Errorf("goal review action must use review approve --id or review reject --id --reason")
		}
		return runReviewApprove(ctx, app, fmt.Sprintf("%s:%d", ref.Kind, ref.ID), jsonOutput, stdout)
	case "intake-review":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if !oneOf(action, "accept", "reject", "archive", "clarify") {
			return fmt.Errorf("intake review action must be one of accept, reject, archive, clarify")
		}
		return runIntakeReviewDecision(ctx, app, commands.IntakeCommand{
			Name:         "review",
			ReviewAction: action,
			ShowRef:      rawIntakeKey(ref.ID),
		}, jsonOutput, stdout)
	case "intake-approval":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if !oneOf(action, "approve", "deny") {
			return fmt.Errorf("intake approval action must be one of approve, deny")
		}
		return runIntakeApprovalDecision(ctx, app, commands.IntakeCommand{
			Name:           "approval",
			ApprovalAction: action,
			ShowRef:        rawIntakeKey(ref.ID),
		}, jsonOutput, stdout)
	case "approval":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if !oneOf(action, "approve", "deny") {
			return fmt.Errorf("task approval action must be one of approve, deny")
		}
		args := []string{"resolve", idRef, action, "unified", "review", "decision"}
		if jsonOutput {
			args = append(args, "--json")
		}
		return runApprovals(ctx, app, args, stdout)
	case "skill-artifact":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("skill artifact action must be one of accept, reject, archive")
		}
		return runSkillArtifactReview(ctx, app, action, idRef, jsonOutput, stdout)
	case "context-pack":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("context pack action must be one of accept, reject, archive")
		}
		return runContextPackReview(ctx, app, ref.ID, action, jsonOutput, stdout)
	case "failed-work":
		switch action {
		case "retry":
			if dryRun {
				return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
			}
			return runFailedWorkReviewRetry(ctx, app, ref.ID, jsonOutput, stdout)
		case "follow-up":
			return runFailedWorkReviewFollowUp(ctx, app, ref.ID, dryRun, jsonOutput, stdout)
		default:
			return fmt.Errorf("failed work action must be retry or follow-up")
		}
	case "memory-proposal":
		if dryRun {
			return fmt.Errorf("--dry-run is only supported for failed-work follow-up review actions")
		}
		return fmt.Errorf("memory proposal review actions are not implemented; use the existing memory workflow")
	default:
		return fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

func reviewActionPreflight(ctx context.Context, app bootstrap.App, ref reviewQueueRef, action string, dryRun bool) (reviewActionReceipt, error) {
	receipt := reviewActionReceipt{
		ReviewID:         fmt.Sprintf("%s:%d", ref.Kind, ref.ID),
		QueueID:          fmt.Sprintf("%s:%d", ref.Kind, ref.ID),
		SourceType:       reviewSourceTypeForKind(ref.Kind),
		SourceID:         ref.ID,
		Action:           action,
		Status:           "resolved",
		Result:           reviewActionResult(action),
		Supported:        true,
		MutationScope:    reviewActionMutationScope(ref.Kind, action),
		ApprovalRequired: reviewActionRequiresApproval(ref.Kind),
		ApprovalStatus:   reviewActionApprovalStatus(ref.Kind, action),
		Mutated:          !dryRun,
		AuditEvent:       reviewActionAuditEvent(ref.Kind, action),
		NextStep:         reviewActionNextStep(ref.Kind, action),
	}
	if dryRun {
		receipt.Status = "dry_run"
		receipt.Result = "not_applied"
		receipt.Mutated = false
	}

	switch ref.Kind {
	case "intake-goal", "goal", "goal-approval":
		if action != "approve" {
			return reviewActionReceipt{}, fmt.Errorf("goal review action must use review approve --id or review reject --id --reason")
		}
	case "goal-blocker":
		return unsupportedReviewActionReceipt(receipt, "blocker_resolution_not_supported", "goal blocker resolution is not implemented; inspect only"), fmt.Errorf("review %s does not support goal-blocker:%d; blocker resolution is not implemented", action, ref.ID)
	case "intake-review":
		if !oneOf(action, "accept", "reject", "archive", "clarify") {
			return reviewActionReceipt{}, fmt.Errorf("intake review action must be one of accept, reject, archive, clarify")
		}
	case "intake-approval":
		if !oneOf(action, "approve", "deny") {
			return reviewActionReceipt{}, fmt.Errorf("intake approval action must be one of approve, deny")
		}
	case "approval":
		if !oneOf(action, "approve", "deny") {
			return reviewActionReceipt{}, fmt.Errorf("task approval action must be one of approve, deny")
		}
		detail, err := approvalsvc.Service{Store: app.Store}.Detail(ctx, ref.ID)
		if err != nil {
			return reviewActionReceipt{}, err
		}
		receipt.ResolverSupport = string(detail.ResolverSupport)
		if detail.ResolverSupport != approvalsvc.ResolverSupported {
			return unsupportedReviewActionReceipt(receipt, "approval_resolver_not_supported", "approval has no supported resolver/continuation contract"), approvalsvc.UnsupportedResolverError{ApprovalID: ref.ID}
		}
	case "skill-artifact":
		if !oneOf(action, "accept", "reject", "archive") {
			return reviewActionReceipt{}, fmt.Errorf("skill artifact action must be one of accept, reject, archive")
		}
	case "context-pack":
		if !oneOf(action, "accept", "reject", "archive") {
			return reviewActionReceipt{}, fmt.Errorf("context pack action must be one of accept, reject, archive")
		}
	case "failed-work":
		if !oneOf(action, "retry", "follow-up") {
			return reviewActionReceipt{}, fmt.Errorf("failed work action must be retry or follow-up")
		}
	case "memory-proposal":
		return unsupportedReviewActionReceipt(receipt, "memory_proposal_review_not_implemented", "memory proposal review actions are not implemented; use the existing memory workflow"), fmt.Errorf("memory proposal review actions are not implemented; use the existing memory workflow")
	default:
		return reviewActionReceipt{}, fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
	return receipt, nil
}

func unsupportedReviewActionReceipt(receipt reviewActionReceipt, errorKey string, nextStep string) reviewActionReceipt {
	receipt.Status = "unsupported"
	receipt.Result = "not_resolved"
	receipt.Supported = false
	receipt.MutationScope = "unsupported"
	receipt.Mutated = false
	receipt.AuditEvent = ""
	receipt.Error = errorKey
	receipt.NextStep = nextStep
	return receipt
}

func reviewSourceTypeForKind(kind string) string {
	switch kind {
	case "intake-goal":
		return "intake_goal_conversion"
	case "goal", "goal-approval":
		return "goal"
	case "goal-blocker":
		return "goal_blocker"
	case "intake-review":
		return "intake_review"
	case "intake-approval":
		return "intake_approval"
	case "approval":
		return "task_approval"
	case "skill-artifact":
		return "skill_artifact"
	case "context-pack":
		return "context_pack"
	case "failed-work":
		return "failed_work"
	case "memory-proposal":
		return "memory_proposal"
	default:
		return kind
	}
}

func reviewActionMutationScope(kind string, action string) string {
	switch kind {
	case "intake-goal", "goal", "goal-approval":
		return "execution_resuming"
	case "approval":
		if action == "approve" {
			return "execution_resuming"
		}
		return "review_state"
	case "failed-work":
		if action == "retry" {
			return "execution_resuming"
		}
		return "review_state"
	case "goal-blocker", "memory-proposal":
		return "unsupported"
	default:
		return "review_state"
	}
}

func reviewActionRequiresApproval(kind string) bool {
	switch kind {
	case "approval", "intake-approval", "goal-approval", "memory-proposal":
		return true
	default:
		return false
	}
}

func reviewActionApprovalStatus(kind string, action string) string {
	if !reviewActionRequiresApproval(kind) {
		return ""
	}
	switch action {
	case "approve":
		return "approved"
	case "deny":
		return "denied"
	default:
		return ""
	}
}

func reviewActionResult(action string) string {
	switch action {
	case "accept":
		return "accepted"
	case "approve":
		return "approved"
	case "deny":
		return "denied"
	case "reject":
		return "rejected"
	case "archive":
		return "archived"
	case "clarify":
		return "clarification_requested"
	case "retry":
		return "retried"
	case "follow-up":
		return "follow_up"
	default:
		return action
	}
}

func reviewActionAuditEvent(kind string, action string) string {
	switch kind {
	case "intake-review":
		return "intake.review_" + reviewActionResult(action)
	case "intake-approval":
		return "intake.approval_" + reviewActionResult(action)
	case "approval":
		return "approval.resolved"
	case "skill-artifact":
		return "skill.artifact_reviewed"
	case "context-pack":
		return "context_pack.reviewed"
	case "intake-goal", "goal", "goal-approval":
		return "review.approved"
	case "failed-work":
		return "work.review_" + action
	default:
		return ""
	}
}

func reviewActionNextStep(kind string, action string) string {
	switch kind {
	case "approval":
		if action == "approve" {
			return "inspect linked work item and latest run attempt"
		}
		return "inspect linked work item for denied approval state"
	case "failed-work":
		return "inspect failed work item and retry/follow-up evidence"
	case "memory-proposal":
		return "use the existing memory workflow"
	default:
		return "inspect source result and review queue state"
	}
}

func compactReviewActionSourceResult(output []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil
	}
	return append(json.RawMessage(nil), trimmed...)
}

func applyReviewActionSourceResult(receipt *reviewActionReceipt) {
	if receipt.SourceResult == nil {
		return
	}
	var result struct {
		Retried *bool  `json:"retried"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(receipt.SourceResult, &result); err != nil {
		return
	}
	if result.Retried != nil {
		receipt.Mutated = *result.Retried
		if !*result.Retried {
			receipt.Status = "not_resolved"
			receipt.Result = firstNonBlank(result.Reason, receipt.Result)
		}
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
		goal, err = ensureGoalForIntakeGoalReview(ctx, store, item)
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
		goal, err = ensureGoalForIntakeGoalReview(ctx, store, item)
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

func ensureGoalForIntakeGoalReview(ctx context.Context, store *sqlite.Store, item sqlite.IntakeItem) (sqlite.Goal, error) {
	if item.GoalID != nil {
		return store.GetGoal(ctx, *item.GoalID)
	}
	if !isDraftGoalIntakeItem(item) {
		return sqlite.Goal{}, fmt.Errorf("intake %s is not a draft goal review item", rawIntakeKey(item.ID))
	}
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{
		Title:       item.Subject,
		Description: "Created from raw intake " + rawIntakeKey(item.ID) + ". " + item.Summary,
		CreatedBy:   "intake_review:" + rawIntakeKey(item.ID),
		Source:      "intake",
	})
	if err != nil {
		return sqlite.Goal{}, err
	}
	if _, err := store.LinkIntakeItemGoal(ctx, item.ID, goal.ID); err != nil {
		return sqlite.Goal{}, err
	}
	return goal, nil
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
	case "memory-proposal":
		summary, err := findMemoryProposalSummary(ctx, app.Store, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry, err := reviewEntryFromMemoryProposal(ctx, app.Store, summary)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		detail, err := memoryProposalReviewDetailFromSummary(summary)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		return entry, detail, nil
	default:
		return reviewQueueEntry{}, nil, fmt.Errorf("unsupported review queue source %q", ref.Kind)
	}
}

type failedWorkReviewDetail struct {
	TaskID                 int64                      `json:"task_id"`
	TaskKey                string                     `json:"task_key"`
	TaskStatus             string                     `json:"task_status"`
	ProjectKey             string                     `json:"project_key"`
	Decision               string                     `json:"decision"`
	RetryEligible          bool                       `json:"retry_eligible"`
	RetryBlockReason       string                     `json:"retry_block_reason,omitempty"`
	RecoveryRecommendation string                     `json:"recovery_recommendation"`
	RetryCount             int                        `json:"retry_count"`
	MaxAttempts            int                        `json:"max_attempts"`
	LastError              string                     `json:"last_error,omitempty"`
	RunAttempts            []failedWorkRunAttempt     `json:"run_attempts"`
	FollowUp               failedWorkFollowUpProposal `json:"follow_up"`
}

type failedWorkRunAttempt struct {
	RunID    int64  `json:"run_id"`
	Status   string `json:"status"`
	Attempt  int    `json:"attempt"`
	Executor string `json:"executor"`
}

type memoryProposalReviewDetail struct {
	ID           int64             `json:"id"`
	Scope        string            `json:"scope"`
	ScopeKey     string            `json:"scope_key"`
	MemoryType   string            `json:"memory_type"`
	Summary      string            `json:"summary"`
	Fields       map[string]string `json:"fields"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	AllowedNotes string            `json:"allowed_notes,omitempty"`
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
		Type:           sourceType,
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
		Source:         "intake_items",
		Risk:           intakeReviewRisk(item.Status, kind),
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
		Type:           "intake_goal_conversion",
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
		Source:         "intake_items",
		Risk:           "medium",
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
		Type:           "goal",
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
		Source:         "goals",
		Risk:           "medium",
		AllowedActions: []string{},
	}
}

func reviewEntryFromPlannedGoal(goal sqlite.Goal) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("goal-approval:%d", goal.ID),
		QueueID:        fmt.Sprintf("goal-approval:%d", goal.ID),
		Type:           "goal",
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
		Source:         "goals",
		Risk:           "governance",
		AllowedActions: []string{},
	}
}

func reviewEntryFromGoalBlocker(goal sqlite.Goal, blocker sqlite.GoalBlocker) reviewQueueEntry {
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("goal-blocker:%d", blocker.ID),
		QueueID:        fmt.Sprintf("goal-blocker:%d", blocker.ID),
		Type:           "goal_blocker",
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
		Source:         "goal_blockers",
		Risk:           "medium",
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
		Type:           "task_approval",
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
		TaskID:         view.TaskID,
		TaskKey:        view.TaskKey,
		WorkKind:       view.WorkKind,
		Source:         "approval_requests",
		Risk:           "governance",
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
		ReviewID:       fmt.Sprintf("approval:%d", detail.Approval.ID),
		QueueID:        fmt.Sprintf("approval:%d", detail.Approval.ID),
		Type:           "task_approval",
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
		Source:         "approval_requests",
		Risk:           "governance",
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
		Type:           "skill_artifact",
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
		Source:         "skill_artifacts",
		Risk:           "medium",
		AllowedActions: skillArtifactAllowedActions(artifact.Status),
	}, nil
}

func reviewEntryFromContextPackProposal(proposal runtimeknowledge.ContextPackProposal) reviewQueueEntry {
	projectScope := proposal.ContextPack.ProjectKey
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("context-pack:%d", proposal.Packet.ID),
		QueueID:        fmt.Sprintf("context-pack:%d", proposal.Packet.ID),
		Type:           "context_pack",
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
		Source:         "context_packets",
		Risk:           "medium",
		AllowedActions: runtimeknowledge.ContextPackAllowedActions(proposal.Packet.Status),
	}
}

func reviewEntryFromFailedTask(task projections.TaskStatusView) reviewQueueEntry {
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    task.WorkKind,
		RequestedBy: task.RequestedBy,
	})
	retryEligible := guidance.RetryEligible
	return reviewQueueEntry{
		ReviewID:               fmt.Sprintf("failed-work:%d", task.TaskID),
		QueueID:                fmt.Sprintf("failed-work:%d", task.TaskID),
		Type:                   "failed_work",
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
		Risk:                   "medium",
		Decision:               guidance.Decision,
		RetryEligible:          &retryEligible,
		RetryBlockReason:       retryBlockReason(guidance.Decision, guidance.RetryEligible),
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		AllowedActions:         []string{"retry", "follow-up"},
	}
}

func isReviewQueueMemoryProposal(summary sqlite.MemorySummary) bool {
	fields, err := memorySummaryFields(summary.DetailsJSON)
	if err != nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(fields["approval"]), "pending") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(fields["status"]), "pending") {
		return true
	}
	return false
}

func reviewEntryFromMemoryProposal(ctx context.Context, store *sqlite.Store, summary sqlite.MemorySummary) (reviewQueueEntry, error) {
	fields, err := memorySummaryFields(summary.DetailsJSON)
	if err != nil {
		return reviewQueueEntry{}, err
	}
	projectScope := summary.ScopeKey
	if summary.ProjectID != nil {
		if project, err := store.GetProject(ctx, *summary.ProjectID); err == nil {
			projectScope = project.Key
		}
	}
	status := "pending"
	reason := "memory_proposal_pending"
	if value := strings.TrimSpace(fields["approval"]); value != "" {
		status = value
		reason = "memory_approval_" + value
	} else if value := strings.TrimSpace(fields["status"]); value != "" {
		status = value
		reason = "memory_status_" + value
	}
	return reviewQueueEntry{
		ReviewID:       fmt.Sprintf("memory-proposal:%d", summary.ID),
		QueueID:        fmt.Sprintf("memory-proposal:%d", summary.ID),
		Type:           "memory_proposal",
		SourceType:     "memory_proposal",
		SourceID:       summary.ID,
		ObjectID:       summary.ID,
		ObjectKey:      fmt.Sprintf("memory-proposal-%d", summary.ID),
		Status:         status,
		Reason:         reason,
		Title:          summary.Summary,
		CreatedAt:      formatReviewTime(summary.CreatedAt),
		ProjectScope:   projectScope,
		Summary:        summary.Summary,
		TaskID:         nullableInt64Value(summary.TaskID),
		Source:         "memory_summaries",
		Risk:           "governance",
		AllowedActions: []string{},
	}, nil
}

func findMemoryProposalSummary(ctx context.Context, store *sqlite.Store, memoryID int64) (sqlite.MemorySummary, error) {
	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{})
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	for _, summary := range summaries {
		if summary.ID == memoryID {
			if !isReviewQueueMemoryProposal(summary) {
				return sqlite.MemorySummary{}, fmt.Errorf("memory proposal %d is not pending review", memoryID)
			}
			return summary, nil
		}
	}
	return sqlite.MemorySummary{}, fmt.Errorf("memory proposal %d not found", memoryID)
}

func memoryProposalReviewDetailFromSummary(summary sqlite.MemorySummary) (memoryProposalReviewDetail, error) {
	fields, err := memorySummaryFields(summary.DetailsJSON)
	if err != nil {
		return memoryProposalReviewDetail{}, err
	}
	return memoryProposalReviewDetail{
		ID:           summary.ID,
		Scope:        summary.Scope,
		ScopeKey:     summary.ScopeKey,
		MemoryType:   summary.MemoryType,
		Summary:      summary.Summary,
		Fields:       fields,
		CreatedAt:    formatReviewTime(summary.CreatedAt),
		UpdatedAt:    formatReviewTime(summary.UpdatedAt),
		AllowedNotes: "read-only in odin review until a memory resolve command is available on the operator surface",
	}, nil
}

func memorySummaryFields(detailsJSON string) (map[string]string, error) {
	fields := map[string]string{}
	if strings.TrimSpace(detailsJSON) == "" {
		return fields, nil
	}
	var payload struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(detailsJSON), &payload); err != nil {
		return nil, fmt.Errorf("memory summary details are invalid: %w", err)
	}
	for key, value := range payload.Fields {
		fields[key] = value
	}
	return fields, nil
}

func nullableInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
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
		WorkKind:    task.WorkKind,
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
	proposal := failedWorkFollowUpProposalForTask(task, project.Key, guidance)
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
		FollowUp:               proposal,
	}, nil
}

func parseReviewQueueRef(queueID string) (reviewQueueRef, error) {
	queueID = strings.TrimSpace(queueID)
	parts := strings.SplitN(queueID, ":", 2)
	if len(parts) != 2 {
		return reviewQueueRef{}, fmt.Errorf("review queue id must look like intake-goal:<id>, goal:<id>, goal-approval:<id>, goal-blocker:<id>, intake-review:<id>, intake-approval:<id>, approval:<id>, skill-artifact:<id>, context-pack:<id>, failed-work:<id>, or memory-proposal:<id>")
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
	case "skill-artifact":
		idRef = strings.TrimPrefix(idRef, "skill-artifact-")
	case "context-pack":
		idRef = strings.TrimPrefix(idRef, "context-pack-")
	case "failed-work":
		idRef = strings.TrimPrefix(idRef, "task-")
	case "memory-proposal":
		idRef = strings.TrimPrefix(idRef, "memory-proposal-")
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

func intakeReviewRisk(status string, kind string) string {
	if kind == "intake-approval" {
		return "governance"
	}
	switch status {
	case "approval_required":
		return "governance"
	case "needs_clarification", "duplicate_linked_or_suppressed":
		return "low"
	default:
		return "medium"
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

func runFailedWorkReviewFollowUp(ctx context.Context, app bootstrap.App, taskID int64, dryRun bool, jsonOutput bool, stdout io.Writer) error {
	task, err := app.Store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	project, err := app.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    task.WorkKind,
		RequestedBy: task.RequestedBy,
	})
	proposal := failedWorkFollowUpProposalForTask(task, project.Key, guidance)
	view := failedWorkFollowUpOutcomeView{
		Action:             "follow-up",
		ReviewID:           proposal.ReviewID,
		DryRun:             dryRun,
		Created:            false,
		ApprovalRequired:   proposal.ApprovalRequired,
		GitHubIssueCreated: false,
		GitHubIssue:        proposal.GitHubIssue,
		Proposal:           proposal,
	}

	if !dryRun {
		workspace, err := (workspaces.Service{Store: app.Store}).BootstrapDefaultWorkspace(ctx)
		if err != nil {
			return err
		}
		policyJSON, err := failedWorkFollowUpPolicyJSON(task, project.Key, guidance, proposal)
		if err != nil {
			return err
		}
		obligation, err := (followups.Service{Store: app.Store}).Create(ctx, followups.CreateParams{
			WorkspaceID:     workspace.ID,
			TargetProjectID: &task.ProjectID,
			Title:           proposal.Title,
			Cadence:         followups.Cadence{Mode: followups.CadenceModeOnce},
			NextDueAt:       time.Now().UTC(),
			PolicyJSON:      policyJSON,
		})
		if err != nil {
			return err
		}
		followUpView, err := renderFollowUpView(ctx, app.Store, obligation)
		if err != nil {
			return err
		}
		view.Created = true
		view.FollowUp = &followUpView
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "action=follow-up review=%s dry_run=%t created=%t destination=%s github_issue=%s title=%q\n", view.ReviewID, view.DryRun, view.Created, proposal.Destination, proposal.GitHubIssue.Status, proposal.Title)
	return err
}

func failedWorkFollowUpProposalForTask(task sqlite.Task, projectKey string, guidance recovery.RetryGuidance) failedWorkFollowUpProposal {
	return failedWorkFollowUpProposal{
		ReviewID:               fmt.Sprintf("failed-work:%d", task.ID),
		TaskID:                 task.ID,
		TaskKey:                task.Key,
		ProjectKey:             projectKey,
		Title:                  fmt.Sprintf("Follow up on failed work: %s", task.Key),
		Destination:            "odin_follow_up_obligation",
		ApprovalRequired:       true,
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		GitHubIssue: failedWorkGitHubIssueProposal{
			Status: "not_created",
			Reason: "github_issue_creation_requires_approved_tracker_mutation_contract",
		},
	}
}

func failedWorkFollowUpPolicyJSON(task sqlite.Task, projectKey string, guidance recovery.RetryGuidance, proposal failedWorkFollowUpProposal) (string, error) {
	payload := struct {
		Source                 string `json:"source"`
		ReviewID               string `json:"review_id"`
		TaskID                 int64  `json:"task_id"`
		TaskKey                string `json:"task_key"`
		ProjectKey             string `json:"project_key"`
		Decision               string `json:"decision"`
		RecoveryRecommendation string `json:"recovery_recommendation"`
		GitHubIssueStatus      string `json:"github_issue_status"`
		GitHubIssueReason      string `json:"github_issue_reason"`
	}{
		Source:                 "failure_analysis_review",
		ReviewID:               proposal.ReviewID,
		TaskID:                 task.ID,
		TaskKey:                task.Key,
		ProjectKey:             projectKey,
		Decision:               guidance.Decision,
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		GitHubIssueStatus:      proposal.GitHubIssue.Status,
		GitHubIssueReason:      proposal.GitHubIssue.Reason,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func runContextPackReview(ctx context.Context, app bootstrap.App, packetID int64, action string, jsonOutput bool, stdout io.Writer) error {
	outcome, err := runtimeknowledge.Service{Store: app.Store}.ReviewContextPackProposal(ctx, packetID, action)
	if err != nil {
		return err
	}
	view := struct {
		Decision      string                              `json:"decision"`
		Status        string                              `json:"status"`
		Repeated      bool                                `json:"repeated"`
		MemorySummary *contextPackMemorySummaryReviewView `json:"memory_summary,omitempty"`
		Proposal      any                                 `json:"proposal"`
	}{
		Decision:      outcome.Decision,
		Status:        outcome.Status,
		Repeated:      outcome.Repeated,
		MemorySummary: contextPackMemorySummaryReview(outcome.MemorySummary),
		Proposal:      commands.NewKnowledgeContextPackProposalView(outcome.Proposal),
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "context_pack id=%d decision=%s status=%s repeated=%t\n", packetID, outcome.Decision, outcome.Status, outcome.Repeated)
	return err
}

type contextPackMemorySummaryReviewView struct {
	ID         int64  `json:"id"`
	Scope      string `json:"scope"`
	ScopeKey   string `json:"scope_key"`
	MemoryType string `json:"memory_type"`
	TaskID     *int64 `json:"task_id,omitempty"`
	Details    any    `json:"details,omitempty"`
}

func contextPackMemorySummaryReview(summary *sqlite.MemorySummary) *contextPackMemorySummaryReviewView {
	if summary == nil {
		return nil
	}
	details := map[string]any{}
	if strings.TrimSpace(summary.DetailsJSON) != "" {
		_ = json.Unmarshal([]byte(summary.DetailsJSON), &details)
	}
	return &contextPackMemorySummaryReviewView{
		ID:         summary.ID,
		Scope:      summary.Scope,
		ScopeKey:   summary.ScopeKey,
		MemoryType: summary.MemoryType,
		TaskID:     summary.TaskID,
		Details:    details,
	}
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

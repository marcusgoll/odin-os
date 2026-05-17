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

	"odin-os/internal/adapters/web"
	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	cliscope "odin-os/internal/cli/scope"
	"odin-os/internal/core/followups"
	"odin-os/internal/core/workspaces"
	browserexecutor "odin-os/internal/executors/browser"
	approvalsvc "odin-os/internal/runtime/approvals"
	jobsvc "odin-os/internal/runtime/jobs"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/runtime/memoryproposal"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/runtime/reviewqueue"
	"odin-os/internal/skills"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/invocation"
)

const reviewUsage = "usage: odin review list [--json] [--source <source_type>] [--status <status>] [--severity <severity>] | odin review show <queue-id>|--id <queue-id> [--json] | odin review approve --id <queue-id> [--json] | odin review reject --id <queue-id> --reason <reason> [--json] | odin review act <queue-id> <accept|reject|archive|approve|deny|clarify|retry|follow-up> [--dry-run] [--json]"

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
	WorkItem    *goalWorkItemView `json:"work_item,omitempty"`
	Handoff     string            `json:"handoff,omitempty"`
}

type goalWorkItemView struct {
	ID                    int64  `json:"id"`
	Key                   string `json:"key"`
	Status                string `json:"status"`
	ProjectKey            string `json:"project_key"`
	WorkKind              string `json:"work_kind"`
	ExecutionIntent       string `json:"execution_intent"`
	ExecutionIntentSource string `json:"execution_intent_source"`
	Created               bool   `json:"created"`
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

type reviewQueueEntry = reviewqueue.Entry

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
		filters, err := reviewListOptions(remaining[1:])
		if err != nil {
			return fmt.Errorf(reviewUsage)
		}
		return runReviewList(ctx, app, filters, jsonOutput, stdout)
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

type reviewListFilters struct {
	Source   string
	Status   string
	Severity string
}

func reviewListOptions(args []string) (reviewListFilters, error) {
	var filters reviewListFilters
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch {
		case arg == "":
			return reviewListFilters{}, fmt.Errorf(reviewUsage)
		case arg == "--source":
			if filters.Source != "" || index+1 >= len(args) {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Source = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--source="):
			if filters.Source != "" {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Source = strings.TrimSpace(strings.TrimPrefix(arg, "--source="))
		case arg == "--status":
			if filters.Status != "" || index+1 >= len(args) {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Status = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--status="):
			if filters.Status != "" {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Status = strings.TrimSpace(strings.TrimPrefix(arg, "--status="))
		case arg == "--severity":
			if filters.Severity != "" || index+1 >= len(args) {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Severity = strings.TrimSpace(args[index+1])
			index++
		case strings.HasPrefix(arg, "--severity="):
			if filters.Severity != "" {
				return reviewListFilters{}, fmt.Errorf(reviewUsage)
			}
			filters.Severity = strings.TrimSpace(strings.TrimPrefix(arg, "--severity="))
		default:
			return reviewListFilters{}, fmt.Errorf(reviewUsage)
		}
	}
	if filters.Source == "" && filters.Status == "" && filters.Severity == "" {
		return filters, nil
	}
	if strings.TrimSpace(filters.Source) == "" && strings.TrimSpace(filters.Status) == "" && strings.TrimSpace(filters.Severity) == "" {
		return filters, nil
	}
	return filters, nil
}

func runReviewList(ctx context.Context, app bootstrap.App, filters reviewListFilters, jsonOutput bool, stdout io.Writer) error {
	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		return err
	}
	entries = filterReviewQueueEntries(entries, filters)
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

func filterReviewQueueEntries(entries []reviewQueueEntry, filters reviewListFilters) []reviewQueueEntry {
	if filters.Source == "" && filters.Status == "" && filters.Severity == "" {
		return entries
	}
	filtered := make([]reviewQueueEntry, 0, len(entries))
	for _, entry := range entries {
		if filters.Source != "" && !reviewFilterEqual(filters.Source, entry.SourceType) && !reviewFilterEqual(filters.Source, entry.Type) {
			continue
		}
		if filters.Status != "" && !reviewFilterEqual(filters.Status, entry.Status) {
			continue
		}
		if filters.Severity != "" && !reviewFilterEqual(filters.Severity, entry.Severity) && !reviewFilterEqual(filters.Severity, entry.Risk) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func reviewFilterEqual(want string, got string) bool {
	return strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got))
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
	normalizeReviewQueueEntry(&entry)
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
	if nextStep := strings.TrimSpace(entry.OperatorNextStep); nextStep != "" {
		return nextStep
	}
	return reviewOperatorNextStep(entry)
}

func reviewOperatorNextStep(entry reviewQueueEntry) string {
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
		if !oneOf(action, "approve", "deny", "clarify") {
			return fmt.Errorf("task approval action must be one of approve, deny, clarify")
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
	case "design-artifact":
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("design artifact action must be one of accept, reject, archive")
		}
		return runDesignArtifactReview(ctx, app, action, idRef, jsonOutput, stdout)
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
		if !oneOf(action, "accept", "reject", "archive") {
			return fmt.Errorf("memory proposal action must be accept, reject, or archive")
		}
		return runMemoryProposalReview(ctx, app, ref.ID, action, "unified review decision", jsonOutput, stdout)
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
		if !oneOf(action, "accept", "reject", "archive") {
			return reviewActionReceipt{}, fmt.Errorf("memory proposal action must be accept, reject, or archive")
		}
	case "recovery":
		return unsupportedReviewActionReceipt(receipt, "recovery_review_actions_not_implemented", "recovery review is read-only until recovery proposal approval persistence is available"), fmt.Errorf("recovery review actions are not implemented")
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
	case "recovery":
		return "recovery"
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
	case "goal-blocker", "recovery":
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
	case "memory-proposal":
		return "memory.proposal_resolved"
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
		return "inspect memory proposal and active memory list"
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
	view, err := approveGoalReviewItem(ctx, app, ref)
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

func approveGoalReviewItem(ctx context.Context, app bootstrap.App, ref reviewQueueRef) (reviewApproveView, error) {
	store := app.Store
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
	workItem, handoff, err := createApprovedGoalWorkItem(ctx, app, ref, approved, reviewID)
	if err != nil {
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
		WorkItem:    workItem,
		Handoff:     handoff,
	}, nil
}

func createApprovedGoalWorkItem(ctx context.Context, app bootstrap.App, ref reviewQueueRef, goal sqlite.Goal, reviewID string) (*goalWorkItemView, string, error) {
	if ref.Kind != "goal-approval" {
		return nil, "", nil
	}
	title := strings.TrimSpace(goal.Title)
	if title == "" {
		title = fmt.Sprintf("goal %d", goal.ID)
	}
	artifactsJSON := fmt.Sprintf(
		`{"handoff":"approved_planned_goal","goal_id":%d,"review_id":%q,"source_type":"goal_approval"}`,
		goal.ID,
		reviewID,
	)
	result, err := jobsvc.Service{
		Store:    app.Store,
		Registry: app.Registry,
	}.CreateTaskOnce(ctx, jobsvc.CreateTaskParams{
		Resolved: cliscope.Resolve(cliscope.ResolveInput{
			ExplicitTarget: &cliscope.Target{
				ProjectKey:    "odin-core",
				SystemProject: true,
			},
		}),
		Title:                 "Execute approved goal: " + title,
		Key:                   fmt.Sprintf("goal-%d-approved-delivery", goal.ID),
		AcceptanceCriteria:    []string{"Review evidence " + reviewID + " exists before execution.", "Work remains governed by project delivery and run evidence."},
		RequestedBy:           "review",
		WorkKind:              "delivery",
		ArtifactsJSON:         artifactsJSON,
		ExecutionIntent:       goalExecutionIntent(goal),
		ExecutionIntentSource: "review:goal_approval",
	})
	if err != nil {
		return nil, "", err
	}
	return &goalWorkItemView{
		ID:                    result.Task.ID,
		Key:                   result.Task.Key,
		Status:                result.Task.Status,
		ProjectKey:            "odin-core",
		WorkKind:              result.Task.WorkKind,
		ExecutionIntent:       result.Task.ExecutionIntent,
		ExecutionIntentSource: result.Task.ExecutionIntentSource,
		Created:               result.Created,
	}, "created_delivery_work_item", nil
}

func goalExecutionIntent(goal sqlite.Goal) string {
	if strings.EqualFold(strings.TrimSpace(goal.Source), "read_only") {
		return "read_only"
	}
	return "mutation"
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
		browserEvidence, err := listBrowserEvidenceForTask(ctx, app.Store, detail.Task.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry.BrowserEvidenceCount = len(browserEvidence)
		return entry, struct {
			ID              int64                          `json:"id"`
			Status          string                         `json:"status"`
			TaskID          int64                          `json:"task_id"`
			TaskKey         string                         `json:"task_key"`
			TaskStatus      string                         `json:"task_status"`
			RunID           *int64                         `json:"run_id,omitempty"`
			ResolverSupport string                         `json:"resolver_support"`
			BrowserEvidence []browserEvidenceReviewSummary `json:"browser_evidence,omitempty"`
		}{
			ID:              detail.Approval.ID,
			Status:          detail.Approval.Status,
			TaskID:          detail.Task.ID,
			TaskKey:         detail.Task.Key,
			TaskStatus:      detail.Task.Status,
			RunID:           detail.Approval.RunID,
			ResolverSupport: string(detail.ResolverSupport),
			BrowserEvidence: browserEvidence,
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
	case "recovery":
		incident, err := app.Store.GetIncident(ctx, ref.ID)
		if err != nil {
			return reviewQueueEntry{}, nil, err
		}
		entry := reviewEntryFromRecoveryIncident(incident, "")
		return entry, recoveryReviewDetailFromIncident(incident), nil
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
	TaskID                 int64                          `json:"task_id"`
	TaskKey                string                         `json:"task_key"`
	TaskStatus             string                         `json:"task_status"`
	ProjectKey             string                         `json:"project_key"`
	Decision               string                         `json:"decision"`
	RetryEligible          bool                           `json:"retry_eligible"`
	RetryBlockReason       string                         `json:"retry_block_reason,omitempty"`
	RecoveryRecommendation string                         `json:"recovery_recommendation"`
	RetryCount             int                            `json:"retry_count"`
	MaxAttempts            int                            `json:"max_attempts"`
	LastError              string                         `json:"last_error,omitempty"`
	RunAttempts            []failedWorkRunAttempt         `json:"run_attempts"`
	BrowserEvidence        []browserEvidenceReviewSummary `json:"browser_evidence,omitempty"`
	FollowUp               failedWorkFollowUpProposal     `json:"follow_up"`
}

type browserEvidenceReviewSummary struct {
	ArtifactID                int64            `json:"artifact_id"`
	RunID                     int64            `json:"run_id"`
	RunStatus                 string           `json:"run_status,omitempty"`
	RunAttempt                int              `json:"run_attempt,omitempty"`
	Executor                  string           `json:"executor,omitempty"`
	Summary                   string           `json:"summary"`
	PageTitle                 string           `json:"page_title,omitempty"`
	URL                       string           `json:"url,omitempty"`
	ExtractedTextSummary      string           `json:"extracted_text_summary,omitempty"`
	ScreenshotMetadata        []map[string]any `json:"screenshot_metadata,omitempty"`
	SelectedLinks             []map[string]any `json:"selected_links,omitempty"`
	DownloadedFiles           []map[string]any `json:"downloaded_files,omitempty"`
	FormStateSummary          string           `json:"form_state_summary,omitempty"`
	BrowserErrorRecoveryNotes []string         `json:"browser_error_recovery_notes,omitempty"`
	Confidence                string           `json:"confidence,omitempty"`
	Limitations               []string         `json:"limitations,omitempty"`
	CreatedAt                 string           `json:"created_at"`
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

type recoveryReviewDetail struct {
	IncidentID   int64  `json:"incident_id"`
	RunID        int64  `json:"run_id,omitempty"`
	Severity     string `json:"severity"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	FaultKey     string `json:"fault_key,omitempty"`
	SubjectKey   string `json:"subject_key,omitempty"`
	DecisionMode string `json:"decision_mode,omitempty"`
	NextAction   string `json:"next_action,omitempty"`
	ReviewNotes  string `json:"review_notes"`
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
		TaskID:         detail.Task.ID,
		TaskKey:        detail.Task.Key,
		TaskStatus:     detail.Task.Status,
		WorkKind:       detail.Task.WorkKind,
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
	text := strings.ToLower(workKind + " " + lastError)
	if strings.Contains(text, "browser") || strings.Contains(text, "huginn") {
		return "browser_evidence"
	}
	return workKind
}

func reviewEntryFromFailedTask(task projections.TaskStatusView) reviewQueueEntry {
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    recoveryWorkKindForTask(task.WorkKind+" "+task.Title, task.LastError),
		RequestedBy: task.RequestedBy,
	})
	retryEligible := guidance.RetryEligible
	allowedActions := []string{"follow-up"}
	severity := "high"
	recommendedAction := "follow-up"
	if retryEligible {
		allowedActions = []string{"retry", "follow-up"}
		severity = "medium"
		recommendedAction = "retry"
	}
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
		CreatedAt:              task.CreatedAt,
		UpdatedAt:              task.UpdatedAt,
		ProjectScope:           task.ProjectKey,
		Summary:                firstNonBlank(task.LastError, task.Title),
		TaskID:                 task.TaskID,
		TaskKey:                task.TaskKey,
		TaskStatus:             task.Status,
		WorkKind:               task.WorkKind,
		Source:                 guidance.Source,
		Risk:                   severity,
		Severity:               severity,
		Decision:               guidance.Decision,
		RecommendedAction:      recommendedAction,
		RetryEligible:          &retryEligible,
		RetryBlockReason:       retryBlockReason(guidance.Decision, guidance.RetryEligible),
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		AllowedActions:         allowedActions,
	}
}

func reviewEntryFromRecoveryIncidentView(incident projections.IncidentView) reviewQueueEntry {
	return reviewEntryFromRecoveryEvidence(
		incident.IncidentID,
		incident.TaskID,
		incident.TaskKey,
		incident.ProjectKey,
		incident.Severity,
		incident.Status,
		incident.Summary,
		incident.OpenedAt,
		incident.DetailsJSON,
	)
}

func reviewEntryFromRecoveryIncident(incident sqlite.Incident, projectScope string) reviewQueueEntry {
	return reviewEntryFromRecoveryEvidence(
		incident.ID,
		0,
		"",
		projectScope,
		incident.Severity,
		incident.Status,
		incident.Summary,
		formatReviewTime(incident.OpenedAt),
		incident.DetailsJSON,
	)
}

func reviewEntryFromRecoveryEvidence(incidentID int64, taskID int64, taskKey string, projectScope string, severity string, status string, summary string, createdAt string, detailsJSON string) reviewQueueEntry {
	evidence := decodeRecoveryReviewEvidence(detailsJSON)
	objectKey := firstNonBlank(evidence.ObjectKey(), fmt.Sprintf("incident-%d", incidentID))
	return reviewQueueEntry{
		ReviewID:               fmt.Sprintf("recovery:%d", incidentID),
		QueueID:                fmt.Sprintf("recovery:%d", incidentID),
		Type:                   "recovery_incident",
		SourceType:             "recovery",
		SourceID:               incidentID,
		ObjectID:               incidentID,
		ObjectKey:              objectKey,
		Status:                 status,
		Reason:                 firstNonBlank(evidence.DecisionMode, status),
		Title:                  firstNonBlank(summary, objectKey),
		CreatedAt:              createdAt,
		ProjectScope:           projectScope,
		Summary:                summary,
		TaskID:                 taskID,
		TaskKey:                taskKey,
		Source:                 "incidents",
		Risk:                   recoveryReviewRisk(severity, evidence.DecisionMode),
		Decision:               evidence.DecisionMode,
		RecoveryRecommendation: evidence.NextAction,
		AllowedActions:         []string{},
	}
}

type recoveryReviewEvidence struct {
	FaultKey     string `json:"fault_key"`
	SubjectKey   string `json:"subject_key"`
	DecisionMode string `json:"decision_mode"`
	NextAction   string `json:"next_action"`
}

func (e recoveryReviewEvidence) ObjectKey() string {
	if strings.TrimSpace(e.FaultKey) == "" || strings.TrimSpace(e.SubjectKey) == "" {
		return ""
	}
	return e.FaultKey + ":" + e.SubjectKey
}

func decodeRecoveryReviewEvidence(detailsJSON string) recoveryReviewEvidence {
	if strings.TrimSpace(detailsJSON) == "" {
		return recoveryReviewEvidence{}
	}
	var evidence recoveryReviewEvidence
	if err := json.Unmarshal([]byte(detailsJSON), &evidence); err != nil {
		return recoveryReviewEvidence{}
	}
	return evidence
}

func recoveryReviewRisk(severity string, decisionMode string) string {
	if strings.EqualFold(strings.TrimSpace(decisionMode), "approval_required") {
		return "high"
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "error", "high":
		return "high"
	default:
		return "medium"
	}
}

func recoveryReviewDetailFromIncident(incident sqlite.Incident) recoveryReviewDetail {
	evidence := decodeRecoveryReviewEvidence(incident.DetailsJSON)
	return recoveryReviewDetail{
		IncidentID:   incident.ID,
		RunID:        nullableInt64Value(incident.RunID),
		Severity:     incident.Severity,
		Status:       incident.Status,
		Summary:      incident.Summary,
		FaultKey:     evidence.FaultKey,
		SubjectKey:   evidence.SubjectKey,
		DecisionMode: evidence.DecisionMode,
		NextAction:   evidence.NextAction,
		ReviewNotes:  "read-only in odin review until recovery proposal approval persistence is available",
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
	allowedActions := []string{}
	if strings.EqualFold(status, "pending") && strings.EqualFold(fields["schema"], memoryproposal.SchemaV1) {
		allowedActions = []string{"accept", "reject", "archive"}
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
		AllowedActions: allowedActions,
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
	allowedNotes := ""
	if !strings.EqualFold(fields["schema"], memoryproposal.SchemaV1) {
		allowedNotes = "read-only in odin review until migrated to memory_proposal.v1"
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
		AllowedNotes: allowedNotes,
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
	if details, ok, err := memoryproposal.DecodeDetails(detailsJSON); err != nil {
		return nil, fmt.Errorf("memory summary details are invalid: %w", err)
	} else if ok {
		fields["schema"] = details.Schema
		fields["status"] = details.Status
		fields["approval"] = details.Approval
		fields["source_type"] = details.Source.Type
		fields["source_id"] = details.Source.ID
		fields["source_key"] = details.Source.Key
		fields["source_url"] = details.Source.URL
		fields["sensitivity"] = details.Safety.Sensitivity
		fields["reviewed_by"] = details.Provenance.ReviewedBy
		fields["review_reason"] = details.Provenance.ReviewReason
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
		CreatedAt:   formatReviewTime(task.CreatedAt),
		UpdatedAt:   formatReviewTime(task.UpdatedAt),
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		LastError:   task.LastError,
	}
	entry := reviewEntryFromFailedTask(taskView)
	browserEvidence, err := listBrowserEvidenceForTask(ctx, store, task.ID)
	if err != nil {
		return reviewQueueEntry{}, failedWorkReviewDetail{}, err
	}
	entry.BrowserEvidenceCount = len(browserEvidence)
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    recoveryWorkKindForTask(task.WorkKind+" "+task.Title, task.LastError),
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
		BrowserEvidence:        browserEvidence,
		FollowUp:               proposal,
	}, nil
}

func countBrowserEvidenceForTask(ctx context.Context, store *sqlite.Store, taskID int64) (int, error) {
	evidence, err := listBrowserEvidenceForTask(ctx, store, taskID)
	if err != nil {
		return 0, err
	}
	return len(evidence), nil
}

func listBrowserEvidenceForTask(ctx context.Context, store *sqlite.Store, taskID int64) ([]browserEvidenceReviewSummary, error) {
	rows, err := store.DB().QueryContext(ctx, `
		SELECT ra.id, ra.run_id, ra.summary, ra.details_json, ra.created_at, r.status, r.attempt, r.executor
		FROM run_artifacts ra
		JOIN runs r ON r.id = ra.run_id
		WHERE r.task_id = ?
		  AND ra.artifact_type = 'browser_evidence'
		ORDER BY ra.id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := make([]browserEvidenceReviewSummary, 0)
	for rows.Next() {
		var item browserEvidenceReviewSummary
		var details string
		if err := rows.Scan(&item.ArtifactID, &item.RunID, &item.Summary, &details, &item.CreatedAt, &item.RunStatus, &item.RunAttempt, &item.Executor); err != nil {
			return nil, err
		}
		applyBrowserEvidenceDetails(&item, details)
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func applyBrowserEvidenceDetails(item *browserEvidenceReviewSummary, details string) {
	if item == nil || strings.TrimSpace(details) == "" {
		return
	}
	var payload struct {
		PageTitle                 string           `json:"page_title"`
		URL                       string           `json:"url"`
		ExtractedTextSummary      string           `json:"extracted_text_summary"`
		ScreenshotMetadata        []map[string]any `json:"screenshot_metadata"`
		SelectedLinks             []map[string]any `json:"selected_links"`
		DownloadedFiles           []map[string]any `json:"downloaded_files"`
		FormStateSummary          string           `json:"form_state_summary"`
		BrowserErrorRecoveryNotes []string         `json:"browser_error_recovery_notes"`
		Confidence                string           `json:"confidence"`
		Limitations               []string         `json:"limitations"`
	}
	if err := json.Unmarshal([]byte(details), &payload); err != nil {
		return
	}
	item.PageTitle = payload.PageTitle
	item.URL = payload.URL
	item.ExtractedTextSummary = payload.ExtractedTextSummary
	item.ScreenshotMetadata = payload.ScreenshotMetadata
	item.SelectedLinks = payload.SelectedLinks
	item.DownloadedFiles = payload.DownloadedFiles
	item.FormStateSummary = payload.FormStateSummary
	item.BrowserErrorRecoveryNotes = payload.BrowserErrorRecoveryNotes
	item.Confidence = payload.Confidence
	item.Limitations = payload.Limitations
}

func parseReviewQueueRef(queueID string) (reviewQueueRef, error) {
	queueID = strings.TrimSpace(queueID)
	parts := strings.SplitN(queueID, ":", 2)
	if len(parts) != 2 {
		return reviewQueueRef{}, fmt.Errorf("review queue id must look like intake-goal:<id>, goal:<id>, goal-approval:<id>, goal-blocker:<id>, intake-review:<id>, intake-approval:<id>, approval:<id>, browser-evidence:<id>, design-artifact:<id>, skill-artifact:<id>, context-pack:<id>, failed-work:<id>, recovery:<id>, or memory-proposal:<id>")
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
	case "memory-proposal":
		idRef = strings.TrimPrefix(idRef, "memory-proposal-")
	case "recovery":
		idRef = strings.TrimPrefix(idRef, "incident-")
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
		if _, err := app.Store.BlockTask(ctx, sqlite.BlockTaskParams{
			TaskID: task.ID,
			Reason: "failed_work_follow_up_created",
		}); err != nil {
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

func runMemoryProposalReview(ctx context.Context, app bootstrap.App, memoryID int64, action string, reason string, jsonOutput bool, stdout io.Writer) error {
	proposal, repeated, err := memoryproposal.Service{Store: app.Store}.Resolve(ctx, memoryproposal.ResolveParams{
		ID:         memoryID,
		Decision:   action,
		ReviewedBy: "operator",
		Reason:     reason,
	})
	if err != nil {
		return err
	}
	view := struct {
		Decision string                  `json:"decision"`
		Status   string                  `json:"status"`
		Repeated bool                    `json:"repeated"`
		Memory   memoryproposal.Proposal `json:"memory"`
	}{
		Decision: action,
		Status:   proposal.Status,
		Repeated: repeated,
		Memory:   proposal,
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "memory=%d decision=%s status=%s repeated=%t\n", memoryID, action, proposal.Status, repeated)
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

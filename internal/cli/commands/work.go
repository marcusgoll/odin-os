package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/registry"
	"odin-os/internal/review"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackerintake "odin-os/internal/tracker/intake"
)

const workUsage = "usage: odin work status|profiles|start --project <key> --title <text> [--intent <read_only|mutation|governance|destructive>]|intake --project <key> [--dry-run]|reconcile --project <key>|proof (--task <id|key>|--intake <id|key>) [--json]|pr prepare --task <id|key> --summary <text> --test <text> --risk <text> --command <text> [--dry-run|--live --approval <id>] [--json]|dispatch [--task <id|key>] [--json]|execute --task <id|key> [--json]|retry --task <id|key> [--json]"

const workProofUsage = "usage: odin work proof (--task <id|key>|--intake <id|key>) [--json]"

var newIntakeTracker = trackerintake.NewGitHubTracker

type WorkOptions struct {
	JobService jobs.Service
}

type workDispatchView struct {
	Dispatched bool                 `json:"dispatched"`
	Reason     string               `json:"reason"`
	Task       workDispatchTaskView `json:"task,omitempty"`
	Run        *workDispatchRunView `json:"run,omitempty"`
}

type workDispatchTaskView struct {
	ID                    int64  `json:"id"`
	ProjectID             int64  `json:"project_id"`
	Key                   string `json:"key"`
	Status                string `json:"status"`
	CurrentRunID          *int64 `json:"current_run_id,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
}

type workDispatchRunView struct {
	ID       int64  `json:"id"`
	TaskID   int64  `json:"task_id"`
	Executor string `json:"executor"`
	Status   string `json:"status"`
	Attempt  int    `json:"attempt"`
	Summary  string `json:"summary,omitempty"`
}

func RunWork(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, snapshot registry.Snapshot, args []string, stdout io.Writer, options ...WorkOptions) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, workUsage)
		return err
	}

	switch args[0] {
	case "status":
		return runWorkStatus(ctx, store, snapshot, stdout)
	case "profiles":
		return runWorkProfiles(snapshot, stdout)
	case "start":
		return runWorkStart(ctx, store, projectRegistry, args[1:], stdout)
	case "intake":
		return runWorkIntake(ctx, store, projectRegistry, args[1:], stdout)
	case "reconcile":
		return runWorkReconcile(ctx, store, projectRegistry, args[1:], stdout)
	case "proof":
		return runWorkProof(ctx, store, args[1:], stdout)
	case "pr":
		return runWorkPR(ctx, store, args[1:], stdout)
	case "dispatch":
		return runWorkDispatch(ctx, store, projectRegistry, args[1:], stdout, options...)
	case "execute":
		return runWorkExecute(ctx, store, projectRegistry, args[1:], stdout, options...)
	case "retry":
		return runWorkRetry(ctx, store, projectRegistry, args[1:], stdout, options...)
	default:
		_, err := fmt.Fprintf(stdout, "unknown work command: %s\n%s\n", args[0], workUsage)
		return err
	}
}

func runWorkStatus(ctx context.Context, store *sqlite.Store, snapshot registry.Snapshot, stdout io.Writer) error {
	taskViews, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return err
	}
	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		return err
	}
	approvalViews, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		return err
	}
	rawIntakeItems, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return err
	}
	intakeReviewItems := 0
	intakeApprovalRequiredItems := 0
	for _, item := range rawIntakeItems {
		if isReviewableIntakeStatus(item.Status) {
			intakeReviewItems++
		}
		if strings.EqualFold(strings.TrimSpace(item.Status), "approval_required") {
			intakeApprovalRequiredItems++
		}
	}

	openWorkItems := 0
	failedRetryableWorkItems := 0
	retryBlockedWorkItems := 0
	explicitIntentWorkItems := 0
	fallbackIntentWorkItems := 0
	for _, view := range taskViews {
		if isOpenWorkItemStatus(view.Status) {
			openWorkItems++
		}
		if strings.TrimSpace(view.ExecutionIntent) != "" {
			explicitIntentWorkItems++
		} else {
			fallbackIntentWorkItems++
		}
		if strings.EqualFold(strings.TrimSpace(view.Status), "failed") {
			if isTaskRetryEligible(view.RetryCount, view.MaxAttempts) {
				failedRetryableWorkItems++
			} else {
				retryBlockedWorkItems++
			}
		}
	}

	activeRunAttempts := 0
	for _, view := range runViews {
		if isActiveRunAttemptStatus(view.Status) {
			activeRunAttempts++
		}
	}

	_, err = fmt.Fprintf(
		stdout,
		"work_items=%d open_work_items=%d active_run_attempts=%d pending_approvals=%d delivery_profiles=%d raw_intake_items=%d intake_review_items=%d intake_approval_required_items=%d failed_retryable_work_items=%d retry_blocked_work_items=%d explicit_intent_work_items=%d fallback_intent_work_items=%d dispatch=work_dispatch intake=raw_cli\n",
		len(taskViews),
		openWorkItems,
		activeRunAttempts,
		len(approvalViews),
		len(deliveryProfiles(snapshot)),
		len(rawIntakeItems),
		intakeReviewItems,
		intakeApprovalRequiredItems,
		failedRetryableWorkItems,
		retryBlockedWorkItems,
		explicitIntentWorkItems,
		fallbackIntentWorkItems,
	)
	return err
}

func runWorkDispatch(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer, options ...WorkOptions) error {
	params := parseWorkStartArgs(args)
	jsonOutput := parseBoolFlag(params, "json")
	if _, ok := params["help"]; ok {
		_, err := fmt.Fprintln(stdout, "usage: odin work dispatch [--task <id|key>] [--json]")
		return err
	}
	if err := rejectUnknownWorkArgs(params, "task", "json"); err != nil {
		return err
	}

	jobService := jobs.Service{Store: store, Registry: projectRegistry}
	if len(options) > 0 && options[0].JobService.Store != nil {
		jobService = options[0].JobService
	}

	var (
		outcome jobs.DispatchOutcome
		err     error
	)
	taskRef := strings.TrimSpace(params["task"])
	if _, ok := params["task"]; ok && taskRef == "" {
		return fmt.Errorf("usage: odin work dispatch --task <id|key> [--json]")
	}
	if taskRef == "" {
		outcome, err = jobService.DispatchNextRunAttempt(ctx)
	} else {
		task, findErr := findWorkTask(ctx, store, taskRef)
		if findErr != nil {
			return findErr
		}
		outcome, err = jobService.DispatchTaskRunAttempt(ctx, task.ID)
	}
	if err != nil {
		return err
	}

	view := workDispatchOutcomeView(outcome)
	if jsonOutput {
		return WriteJSON(stdout, view)
	}
	if view.Task.ID == 0 {
		_, err := fmt.Fprintf(stdout, "dispatched=%t reason=%s\n", view.Dispatched, view.Reason)
		return err
	}
	runID := int64(0)
	if view.Run != nil {
		runID = view.Run.ID
	}
	_, err = fmt.Fprintf(stdout, "dispatched=%t reason=%s task=%s status=%s run_id=%d\n", view.Dispatched, view.Reason, view.Task.Key, view.Task.Status, runID)
	return err
}

func rejectUnknownWorkArgs(params map[string]string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range params {
		if _, ok := allowedSet[key]; ok {
			continue
		}
		return fmt.Errorf("unknown work dispatch argument: %s", key)
	}
	return nil
}

func runWorkExecute(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer, options ...WorkOptions) error {
	params := parseWorkStartArgs(args)
	jsonOutput := parseBoolFlag(params, "json")
	if _, ok := params["help"]; ok {
		_, err := fmt.Fprintln(stdout, "usage: odin work execute --task <id|key> [--json]")
		return err
	}
	taskRef := strings.TrimSpace(params["task"])
	if taskRef == "" {
		return fmt.Errorf("usage: odin work execute --task <id|key> [--json]")
	}

	jobService := jobs.Service{Store: store, Registry: projectRegistry}
	if len(options) > 0 && options[0].JobService.Store != nil {
		jobService = options[0].JobService
	}
	task, err := findWorkTask(ctx, store, taskRef)
	if err != nil {
		return err
	}
	outcome, err := jobService.ExecuteDispatchedRun(ctx, task.ID)
	if err != nil && outcome.Task.ID == 0 {
		return err
	}

	view := workExecutionOutcomeView(outcome)
	if jsonOutput {
		return WriteJSON(stdout, view)
	}
	runStatus := "none"
	if view.Run != nil {
		runStatus = view.Run.Status
	}
	_, writeErr := fmt.Fprintf(stdout, "executed=%t reason=%s task=%s status=%s run_status=%s\n", view.Executed, view.Reason, view.Task.Key, view.Task.Status, runStatus)
	if err != nil {
		return err
	}
	return writeErr
}

func runWorkRetry(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer, options ...WorkOptions) error {
	params := parseWorkStartArgs(args)
	jsonOutput := parseBoolFlag(params, "json")
	if _, ok := params["help"]; ok {
		_, err := fmt.Fprintln(stdout, "usage: odin work retry --task <id|key> [--json]")
		return err
	}
	taskRef := strings.TrimSpace(params["task"])
	if taskRef == "" {
		return fmt.Errorf("usage: odin work retry --task <id|key> [--json]")
	}

	jobService := jobs.Service{Store: store, Registry: projectRegistry}
	if len(options) > 0 && options[0].JobService.Store != nil {
		jobService = options[0].JobService
	}
	task, err := findWorkTask(ctx, store, taskRef)
	if err != nil {
		return err
	}
	outcome, err := jobService.RetryFailedTask(ctx, task.ID)
	if err != nil {
		return err
	}
	view := workRetryOutcomeView(outcome)
	if jsonOutput {
		return WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "retried=%t reason=%s decision=%s retry_eligible=%t task=%s status=%s retry_count=%d recommendation=%q\n", view.Retried, view.Reason, view.Decision, view.RetryEligible, view.Task.Key, view.Task.Status, view.Task.RetryCount, view.RecoveryRecommendation)
	return err
}

const workPRPrepareUsage = "usage: odin work pr prepare --task <id|key> --summary <text> --test <text> --risk <text> --command <text> [--dry-run|--live --approval <id>] [--json]"

type workPRPrepareView struct {
	Prepared         bool                      `json:"prepared"`
	ApprovalRequired bool                      `json:"approval_required"`
	ExternalMutated  bool                      `json:"external_mutated"`
	DryRun           bool                      `json:"dry_run"`
	ApprovalID       *int64                    `json:"approval_id,omitempty"`
	Task             workProofTaskView         `json:"task"`
	PullRequest      workPRPreparePRView       `json:"pull_request"`
	Handoff          workPRHandoffView         `json:"handoff"`
	ReviewResults    []workProofPRReviewResult `json:"review_results"`
	NextSteps        []string                  `json:"next_steps"`
}

type workPRPreparePRView struct {
	Provider string `json:"provider,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Number   int    `json:"number,omitempty"`
	URL      string `json:"url,omitempty"`
	State    string `json:"state,omitempty"`
}

type workPRHandoffView struct {
	ID            int64    `json:"id"`
	URL           string   `json:"url"`
	State         string   `json:"state"`
	ReviewState   string   `json:"review_state"`
	SelectedRoles []string `json:"selected_roles"`
}

func runWorkPR(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, workPRPrepareUsage)
		return err
	}
	if args[0] != "prepare" {
		return fmt.Errorf("unsupported work pr subcommand: %s", args[0])
	}
	return runWorkPRPrepare(ctx, store, args[1:], stdout)
}

func runWorkPRPrepare(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	jsonOutput := parseBoolFlag(params, "json")
	if _, ok := params["help"]; ok {
		_, err := fmt.Fprintln(stdout, workPRPrepareUsage)
		return err
	}
	if err := rejectUnknownWorkProofArgs(params, "task", "summary", "test", "risk", "blocker", "command", "branch", "title", "approval", "dry-run", "live", "json"); err != nil {
		return err
	}
	live := parseBoolFlag(params, "live")
	if live && parseBoolFlag(params, "dry-run") {
		return fmt.Errorf("choose either --dry-run or --live, not both")
	}
	summary := strings.TrimSpace(params["summary"])
	testEvidence := strings.TrimSpace(params["test"])
	risk := strings.TrimSpace(params["risk"])
	command := strings.TrimSpace(params["command"])
	if summary == "" {
		return fmt.Errorf("summary evidence is required")
	}
	if testEvidence == "" {
		return fmt.Errorf("test evidence is required")
	}
	if risk == "" {
		return fmt.Errorf("risk evidence is required")
	}
	if command == "" {
		return fmt.Errorf("command evidence is required")
	}

	taskRef := strings.TrimSpace(params["task"])
	if taskRef == "" {
		return fmt.Errorf(workPRPrepareUsage)
	}
	task, err := findWorkTask(ctx, store, taskRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("work item not found: %s", taskRef)
		}
		return err
	}
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}

	issueURL := ""
	if intakeID, ok := intakeIDFromTask(task); ok {
		item, err := store.GetIntakeItem(ctx, intakeID)
		if err != nil {
			return err
		}
		issueURL = urlFromJSON(item.SourceFactsJSON)
	}
	branch := strings.TrimSpace(params["branch"])
	if branch == "" {
		branch = task.Key
	}
	title := strings.TrimSpace(params["title"])
	if title == "" {
		title = task.Title
	}
	repoID := project.GitHubRepo
	if repoID == "" {
		if live {
			return fmt.Errorf("GitHub repository is required before live pull request mutation")
		}
		repoID = "local/" + project.Key
	}
	owner, repoName := splitRepoID(repoID)

	var approvalID *int64
	if live {
		approvedApprovalID, ready, err := livePullRequestApproval(ctx, store, task, strings.TrimSpace(params["approval"]))
		if err != nil {
			return err
		}
		approvalID = &approvedApprovalID
		if !ready {
			currentTask, err := store.GetTask(ctx, task.ID)
			if err != nil {
				return err
			}
			view := workPRPrepareView{
				Prepared:         false,
				ApprovalRequired: true,
				ExternalMutated:  false,
				DryRun:           false,
				ApprovalID:       approvalID,
				Task:             workPRPrepareTaskView(currentTask),
				NextSteps:        []string{fmt.Sprintf("inspect approval %d, then resolve with odin approvals resolve %d <approve|deny> <reason...>", approvedApprovalID, approvedApprovalID)},
			}
			if jsonOutput {
				return WriteJSON(stdout, view)
			}
			_, err = fmt.Fprintf(stdout, "prepared=false approval_required=true external_mutated=false task=%s approval=%d next_steps=%s\n",
				task.Key,
				approvedApprovalID,
				strings.Join(view.NextSteps, "; "),
			)
			return err
		}
		if strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) == "" {
			return fmt.Errorf("GITHUB_TOKEN is required before live pull request mutation")
		}
	}

	manager := review.NewGitHubPullRequestManager(review.GitHubPullRequestConfig{
		Owner:      owner,
		Repo:       repoName,
		BaseBranch: defaultWorkString(project.DefaultBranch, "main"),
		BaseURL:    os.Getenv("ODIN_GITHUB_API_BASE_URL"),
		TokenEnv:   "GITHUB_TOKEN",
		DryRun:     !live,
	})
	result, err := review.HandoffOrchestrator{
		Store:        store,
		PullRequests: manager,
	}.Upsert(ctx, review.PullRequestHandoffRequest{
		ProjectID:              task.ProjectID,
		IssueURL:               issueURL,
		Title:                  title,
		Branch:                 branch,
		Summary:                summary,
		Tests:                  []string{testEvidence},
		Risks:                  []string{risk},
		Blockers:               optionalStringList(params["blocker"]),
		CommandsRun:            []string{command},
		ChangedFiles:           []string{},
		RuntimeBehaviorChanged: false,
		RealOdinProofIncluded:  strings.Contains(command, "odin"),
		PostComment:            false,
	})
	if err != nil {
		return err
	}
	if err := store.RecordPullRequestHandoffPrepared(ctx, sqlite.RecordPullRequestHandoffPreparedParams{
		Handoff:          result.Handoff,
		TaskID:           task.ID,
		DryRun:           !live,
		ExternalMutated:  live,
		ApprovalRequired: false,
	}); err != nil {
		return err
	}
	view := workPRPrepareView{
		Prepared:         true,
		ApprovalRequired: false,
		ExternalMutated:  live,
		DryRun:           !live,
		ApprovalID:       approvalID,
		Task:             workPRPrepareTaskView(task),
		PullRequest:      workPRPreparePullRequestView(result.PullRequest),
		Handoff: workPRHandoffView{
			ID:            result.Handoff.ID,
			URL:           result.Handoff.URL,
			State:         result.Handoff.State,
			ReviewState:   result.Handoff.ReviewState,
			SelectedRoles: result.Handoff.SelectedRoles,
		},
		ReviewResults: make([]workProofPRReviewResult, 0, len(result.ReviewResults)),
		NextSteps:     []string{"inspect work proof and obtain explicit human approval before merge or deployment"},
	}
	for _, reviewResult := range result.ReviewResults {
		view.ReviewResults = append(view.ReviewResults, workProofPRReviewResult{
			Role:     reviewResult.Role,
			State:    reviewResult.State,
			Summary:  reviewResult.Summary,
			Comments: reviewResult.Comments,
			Blockers: reviewResult.Blockers,
			Outcome:  reviewResult.Outcome,
		})
	}
	if jsonOutput {
		return WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "prepared=%t dry_run=%t external_mutated=%t task=%s pull_request=%s review=%s next_steps=%s\n",
		view.Prepared,
		view.DryRun,
		view.ExternalMutated,
		view.Task.Key,
		defaultWorkString(view.PullRequest.State, "missing"),
		view.Handoff.ReviewState,
		strings.Join(view.NextSteps, "; "),
	)
	return err
}

func livePullRequestApproval(ctx context.Context, store *sqlite.Store, task sqlite.Task, approvalRef string) (int64, bool, error) {
	if approvalRef == "" {
		approval, err := pendingPullRequestApproval(ctx, store, task)
		if err != nil {
			return 0, false, err
		}
		return approval.ID, false, nil
	}
	approvalID, err := strconv.ParseInt(approvalRef, 10, 64)
	if err != nil || approvalID <= 0 {
		return 0, false, fmt.Errorf("approval id must be a positive integer")
	}
	approval, err := store.GetApproval(ctx, approvalID)
	if err != nil {
		return 0, false, err
	}
	if approval.TaskID != task.ID {
		return 0, false, fmt.Errorf("approval %d belongs to task %d, not task %d", approval.ID, approval.TaskID, task.ID)
	}
	if approval.Status != "approved" {
		return 0, false, fmt.Errorf("approval %d is %s, want approved before live pull request mutation", approval.ID, approval.Status)
	}
	return approval.ID, true, nil
}

func pendingPullRequestApproval(ctx context.Context, store *sqlite.Store, task sqlite.Task) (sqlite.Approval, error) {
	pending, err := store.ListApprovals(ctx, sqlite.ListApprovalsParams{TaskID: &task.ID, Status: "pending"})
	if err != nil {
		return sqlite.Approval{}, err
	}
	if len(pending) > 0 {
		return pending[0], nil
	}
	if task.Status == "blocked" {
		return sqlite.Approval{}, fmt.Errorf("task %s is blocked without a pending live pull request approval", task.Key)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		Status:      "pending",
		RequestedBy: "work_pr_prepare",
	})
	if err != nil {
		return sqlite.Approval{}, err
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: task.ID,
		Reason: "approval_required",
	}); err != nil {
		return sqlite.Approval{}, err
	}
	return approval, nil
}

func workPRPreparePullRequestView(pullRequest review.PullRequest) workPRPreparePRView {
	return workPRPreparePRView{
		Provider: pullRequest.Provider,
		Repo:     pullRequest.Repo,
		Number:   pullRequest.Number,
		URL:      pullRequest.URL,
		State:    pullRequest.State,
	}
}

func workPRPrepareTaskView(task sqlite.Task) workProofTaskView {
	return workProofTaskView{
		ID:                    task.ID,
		ProjectID:             task.ProjectID,
		Key:                   task.Key,
		Title:                 task.Title,
		Status:                task.Status,
		ExecutionIntent:       task.ExecutionIntent,
		ExecutionIntentSource: task.ExecutionIntentSource,
		BlockedReason:         task.BlockedReason,
	}
}

func runWorkProof(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	jsonOutput := parseBoolFlag(params, "json")
	if _, ok := params["help"]; ok {
		_, err := fmt.Fprintln(stdout, workProofUsage)
		return err
	}
	if err := rejectUnknownWorkProofArgs(params, "task", "intake", "json"); err != nil {
		return err
	}
	taskRef := strings.TrimSpace(params["task"])
	intakeRef := strings.TrimSpace(params["intake"])
	if (taskRef == "") == (intakeRef == "") {
		return fmt.Errorf("%s; requires exactly one of --task or --intake", workProofUsage)
	}

	var view workProofView
	if taskRef != "" {
		task, err := findWorkTask(ctx, store, taskRef)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("work item not found: %s", taskRef)
			}
			return err
		}
		view, err = buildWorkProofView(ctx, store, task)
		if err != nil {
			return err
		}
	} else {
		item, err := findWorkIntake(ctx, store, intakeRef)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("intake item not found: %s", intakeRef)
			}
			return err
		}
		view, err = buildWorkProofForIntake(ctx, store, item)
		if err != nil {
			return err
		}
	}
	if jsonOutput {
		return WriteJSON(stdout, view)
	}
	taskKey := "none"
	taskStatus := "none"
	if view.Task != nil {
		taskKey = view.Task.Key
		taskStatus = view.Task.Status
	}
	_, err := fmt.Fprintf(stdout, "schema=%s task=%s status=%s proof_state=%s source=%s review=%s runs=%d approvals_pending=%d pull_request=%s mutated=%t next_steps=%s\n",
		view.Schema,
		taskKey,
		taskStatus,
		view.ProofState,
		noneIfEmpty(view.Source.Type),
		noneIfEmpty(view.Review.Status),
		len(view.Execution.Runs),
		len(view.Approvals.Pending),
		view.PullRequest.Status,
		view.Mutated,
		strings.Join(view.NextSteps, "; "),
	)
	return err
}

func rejectUnknownWorkProofArgs(params map[string]string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range params {
		if _, ok := allowedSet[key]; ok {
			continue
		}
		return fmt.Errorf("unknown work proof argument: %s", key)
	}
	return nil
}

type workProofView struct {
	Schema         string                 `json:"schema"`
	Task           *workProofTaskView     `json:"task,omitempty"`
	Source         workProofSourceView    `json:"source"`
	ProofState     string                 `json:"proof_state"`
	DraftArtifact  *workProofDraftView    `json:"draft_artifact,omitempty"`
	Clarification  workProofStatusView    `json:"clarification"`
	Review         workProofReviewView    `json:"review"`
	Execution      workProofExecutionView `json:"execution"`
	Delivery       workProofDeliveryView  `json:"delivery"`
	PullRequest    workProofPullRequest   `json:"pull_request"`
	Approvals      workProofApprovalsView `json:"approvals"`
	MergeGate      workProofGateView      `json:"merge_gate"`
	DeploymentGate workProofGateView      `json:"deployment_gate"`
	Events         workProofEventsView    `json:"events"`
	NextSteps      []string               `json:"next_steps"`
	Mutated        bool                   `json:"mutated"`
}

type workProofTaskView struct {
	ID                    int64  `json:"id"`
	ProjectID             int64  `json:"project_id"`
	Key                   string `json:"key"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
}

type workProofDraftView struct {
	Kind                  string `json:"kind"`
	Title                 string `json:"title"`
	ReviewState           string `json:"review_state"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

type workProofSourceView struct {
	Type        string `json:"type"`
	ID          int64  `json:"id,omitempty"`
	DedupeKey   string `json:"dedupe_key,omitempty"`
	URL         string `json:"url,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	RequestedBy string `json:"requested_by,omitempty"`
	Status      string `json:"status,omitempty"`
}

type workProofStatusView struct {
	Status    string   `json:"status"`
	Questions []string `json:"questions"`
}

type workProofReviewView struct {
	Status  string `json:"status"`
	QueueID string `json:"queue_id,omitempty"`
}

type workProofExecutionView struct {
	Runs        []workProofRunView `json:"runs"`
	ActiveRunID *int64             `json:"active_run_id"`
}

type workProofRunView struct {
	ID             int64  `json:"id"`
	TaskID         int64  `json:"task_id"`
	Executor       string `json:"executor"`
	Status         string `json:"status"`
	Attempt        int    `json:"attempt"`
	Summary        string `json:"summary,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
}

type workProofDeliveryView struct {
	EvidenceStatus string `json:"evidence_status"`
	GateStatus     string `json:"gate_status"`
}

type workProofPullRequest struct {
	Status        string                    `json:"status"`
	HandoffID     *int64                    `json:"handoff_id"`
	URL           string                    `json:"url"`
	Provider      string                    `json:"provider,omitempty"`
	Repo          string                    `json:"repo,omitempty"`
	Number        int                       `json:"number,omitempty"`
	State         string                    `json:"state,omitempty"`
	Branch        string                    `json:"branch,omitempty"`
	Title         string                    `json:"title,omitempty"`
	Summary       string                    `json:"summary,omitempty"`
	Tests         []string                  `json:"tests"`
	Risks         []string                  `json:"risks"`
	Blockers      []string                  `json:"blockers"`
	SelectedRoles []string                  `json:"selected_roles"`
	ReviewResults []workProofPRReviewResult `json:"review_results"`
}

type workProofPRReviewResult struct {
	Role     string   `json:"role"`
	State    string   `json:"state"`
	Summary  string   `json:"summary,omitempty"`
	Comments []string `json:"comments"`
	Blockers []string `json:"blockers"`
	Outcome  string   `json:"outcome,omitempty"`
}

type workProofApprovalsView struct {
	Pending  []workProofApprovalView `json:"pending"`
	Resolved []workProofApprovalView `json:"resolved"`
}

type workProofApprovalView struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	RunID      *int64 `json:"run_id,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	DecisionBy string `json:"decision_by,omitempty"`
}

type workProofGateView struct {
	Status                string `json:"status"`
	HumanApprovalRequired bool   `json:"human_approval_required"`
	Approved              bool   `json:"approved"`
}

type workProofEventsView struct {
	Count  int                  `json:"count"`
	Latest []workProofEventView `json:"latest"`
}

type workProofEventView struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

func buildWorkProofView(ctx context.Context, store *sqlite.Store, task sqlite.Task) (workProofView, error) {
	view := newWorkProofView(workProofState(task))
	view.Task = &workProofTaskView{
		ID:                    task.ID,
		ProjectID:             task.ProjectID,
		Key:                   task.Key,
		Title:                 task.Title,
		Status:                task.Status,
		ExecutionIntent:       task.ExecutionIntent,
		ExecutionIntentSource: task.ExecutionIntentSource,
		BlockedReason:         task.BlockedReason,
	}

	if intakeID, ok := intakeIDFromTask(task); ok {
		item, err := store.GetIntakeItem(ctx, intakeID)
		if err != nil {
			return workProofView{}, err
		}
		view.Source = workProofSourceView{
			Type:        "intake_item",
			ID:          item.ID,
			DedupeKey:   item.DedupeKey,
			URL:         urlFromJSON(item.SourceFactsJSON),
			SourceType:  item.EventKind,
			RequestedBy: sourceFactFromJSON(item.SourceFactsJSON, "requested_by"),
			Status:      item.Status,
		}
		view.Review = workProofReviewView{Status: item.Status, QueueID: "intake-review:" + rawIntakeProofKey(item.ID)}
		if item.Status == "needs_clarification" {
			view.Clarification = workProofStatusView{
				Status: "needs_clarification",
				Questions: []string{
					"What exact outcome should Odin prepare?",
					"Which acceptance criteria make this ready for work?",
				},
			}
		}
	}

	runs, err := store.ListRuns(ctx, sqlite.ListRunsParams{TaskID: &task.ID})
	if err != nil {
		return workProofView{}, err
	}
	view.Execution.Runs = make([]workProofRunView, 0, len(runs))
	for _, run := range runs {
		view.Execution.Runs = append(view.Execution.Runs, workProofRunView{
			ID:             run.ID,
			TaskID:         run.TaskID,
			Executor:       run.Executor,
			Status:         run.Status,
			Attempt:        run.Attempt,
			Summary:        run.Summary,
			TerminalReason: run.TerminalReason,
		})
	}
	view.Execution.ActiveRunID = task.CurrentRunID

	approvals, err := store.ListApprovals(ctx, sqlite.ListApprovalsParams{TaskID: &task.ID})
	if err != nil {
		return workProofView{}, err
	}
	for _, approval := range approvals {
		approvalView := workProofApprovalView{
			ID:         approval.ID,
			TaskID:     approval.TaskID,
			RunID:      approval.RunID,
			Status:     approval.Status,
			Reason:     approval.Reason,
			DecisionBy: approval.DecisionBy,
		}
		if strings.EqualFold(approval.Status, "pending") {
			view.Approvals.Pending = append(view.Approvals.Pending, approvalView)
		} else {
			view.Approvals.Resolved = append(view.Approvals.Resolved, approvalView)
		}
	}
	if len(view.Approvals.Pending) > 0 {
		view.ProofState = "approval_required"
	}

	handoff, found, err := findWorkProofPullRequestHandoff(ctx, store, task.ProjectID, view.Source.URL)
	if err != nil {
		return workProofView{}, err
	}
	if found {
		view.PullRequest.Status = handoff.ReviewState
		id := handoff.ID
		view.PullRequest.HandoffID = &id
		view.PullRequest.URL = handoff.URL
		view.PullRequest.Provider = handoff.Provider
		view.PullRequest.Repo = handoff.Repo
		view.PullRequest.Number = handoff.Number
		view.PullRequest.State = handoff.State
		view.PullRequest.Branch = handoff.Branch
		view.PullRequest.Title = handoff.Title
		view.PullRequest.Summary = handoff.Summary
		view.PullRequest.Tests = handoff.Tests
		view.PullRequest.Risks = handoff.Risks
		view.PullRequest.Blockers = handoff.Blockers
		view.PullRequest.SelectedRoles = handoff.SelectedRoles
		view.MergeGate.Status = "approval_required"
		results, err := store.ListPullRequestReviewResults(ctx, handoff.ID)
		if err != nil {
			return workProofView{}, err
		}
		view.PullRequest.ReviewResults = make([]workProofPRReviewResult, 0, len(results))
		for _, result := range results {
			view.PullRequest.ReviewResults = append(view.PullRequest.ReviewResults, workProofPRReviewResult{
				Role:     result.Role,
				State:    result.State,
				Summary:  result.Summary,
				Comments: result.Comments,
				Blockers: result.Blockers,
				Outcome:  result.Outcome,
			})
		}
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &task.ID})
	if err != nil {
		return workProofView{}, err
	}
	view.Events = workProofEvents(events)

	view.NextSteps = workProofNextSteps(view)
	return view, nil
}

func newWorkProofView(proofState string) workProofView {
	return workProofView{
		Schema:         "prompt_to_production_proof.v1",
		ProofState:     proofState,
		Source:         workProofSourceView{Type: "none"},
		Clarification:  workProofStatusView{Status: "not_required", Questions: []string{}},
		Review:         workProofReviewView{Status: "not_started"},
		Delivery:       workProofDeliveryView{EvidenceStatus: "missing", GateStatus: "not_started"},
		PullRequest:    workProofPullRequest{Status: "missing", URL: "", Tests: []string{}, Risks: []string{}, Blockers: []string{}, SelectedRoles: []string{}, ReviewResults: []workProofPRReviewResult{}},
		Approvals:      workProofApprovalsView{Pending: []workProofApprovalView{}, Resolved: []workProofApprovalView{}},
		MergeGate:      workProofGateView{Status: "not_ready", HumanApprovalRequired: true, Approved: false},
		DeploymentGate: workProofGateView{Status: "not_in_scope", HumanApprovalRequired: true, Approved: false},
		NextSteps:      []string{},
		Mutated:        false,
	}
}

func buildWorkProofForIntake(ctx context.Context, store *sqlite.Store, item sqlite.IntakeItem) (workProofView, error) {
	view := newWorkProofView(workProofStateForIntake(item))
	view.Source = workProofSourceView{
		Type:        "intake_item",
		ID:          item.ID,
		DedupeKey:   item.DedupeKey,
		URL:         urlFromJSON(item.SourceFactsJSON),
		SourceType:  item.EventKind,
		RequestedBy: sourceFactFromJSON(item.SourceFactsJSON, "requested_by"),
		Status:      item.Status,
	}
	if view.Source.RequestedBy == "" {
		view.Source.RequestedBy = item.SourceFamily
	}
	view.Review = workProofReviewView{Status: item.Status, QueueID: "intake-review:" + rawIntakeProofKey(item.ID)}

	notes, err := workProofIntakeNotesFromItem(item)
	if err != nil {
		return workProofView{}, err
	}
	if notes.DraftArtifact != nil {
		view.DraftArtifact = notes.DraftArtifact
	}
	if notes.Clarification != nil {
		view.Clarification = workProofStatusView{
			Status:    notes.Clarification.State,
			Questions: append([]string{}, notes.Clarification.Prompts...),
		}
	}
	if item.Status == "needs_clarification" && view.Clarification.Status != "needs_clarification" {
		view.Clarification = workProofStatusView{
			Status: "needs_clarification",
			Questions: []string{
				"What exact outcome should Odin prepare?",
				"Which acceptance criteria make this ready for work?",
			},
		}
	}

	streamType := runtimeevents.StreamIntakeItem
	streamID := item.ID
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{StreamType: &streamType, StreamID: &streamID})
	if err != nil {
		return workProofView{}, err
	}
	view.Events = workProofEvents(events)
	view.NextSteps = workProofNextSteps(view)
	return view, nil
}

type workProofIntakeNotes struct {
	DraftArtifact *workProofDraftView `json:"draft_artifact,omitempty"`
	Clarification *struct {
		State   string   `json:"state"`
		Prompts []string `json:"prompts"`
	} `json:"clarification,omitempty"`
}

func workProofIntakeNotesFromItem(item sqlite.IntakeItem) (workProofIntakeNotes, error) {
	var notes workProofIntakeNotes
	if strings.TrimSpace(item.RoutingNotes) == "" {
		return notes, nil
	}
	if err := json.Unmarshal([]byte(item.RoutingNotes), &notes); err != nil {
		return workProofIntakeNotes{}, fmt.Errorf("intake routing notes: %w", err)
	}
	return notes, nil
}

func workProofStateForIntake(item sqlite.IntakeItem) string {
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "needs_clarification":
		return "needs_clarification"
	case "review_required":
		return "review_required"
	case "approval_required":
		return "approval_required"
	case "accepted":
		return "accepted"
	case "duplicate_linked_or_suppressed":
		return "blocked"
	case "rejected", "archived":
		return "completed"
	default:
		if strings.TrimSpace(item.Status) == "" {
			return "blocked"
		}
		return item.Status
	}
}

func workProofState(task sqlite.Task) string {
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "blocked":
		if strings.EqualFold(strings.TrimSpace(task.BlockedReason), "approval_required") {
			return "approval_required"
		}
		return "blocked"
	case "running", "preparing":
		return "running"
	case "failed":
		return "failed"
	case "completed", "canceled", "cancelled":
		return "completed"
	case "queued":
		return "queued"
	default:
		if strings.TrimSpace(task.Status) == "" {
			return "blocked"
		}
		return task.Status
	}
}

func intakeIDFromTask(task sqlite.Task) (int64, bool) {
	ref := strings.TrimPrefix(strings.TrimSpace(task.RequestedBy), "intake_review:")
	if ref == task.RequestedBy || !strings.HasPrefix(ref, "intake-") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(ref, "intake-"), 10, 64)
	return id, err == nil && id > 0
}

func rawIntakeProofKey(id int64) string {
	return fmt.Sprintf("intake-%d", id)
}

func urlFromJSON(raw string) string {
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return ""
	}
	return findURLValue(value)
}

func sourceFactFromJSON(raw string, key string) string {
	var facts map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &facts); err != nil {
		return ""
	}
	if value, ok := facts[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func splitRepoID(repoID string) (string, string) {
	repoID = strings.TrimSpace(repoID)
	owner, repo, ok := strings.Cut(repoID, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return "local", defaultWorkString(repoID, "odin")
	}
	return strings.TrimSpace(owner), strings.TrimSpace(repo)
}

func optionalStringList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func defaultWorkString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func findURLValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"url", "html_url", "issue_url"} {
			if found, ok := typed[key].(string); ok && strings.TrimSpace(found) != "" {
				return strings.TrimSpace(found)
			}
		}
		for _, child := range typed {
			if found := findURLValue(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findURLValue(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func findWorkProofPullRequestHandoff(ctx context.Context, store *sqlite.Store, projectID int64, issueURL string) (sqlite.PullRequestHandoff, bool, error) {
	handoffs, err := store.ListPullRequestHandoffs(ctx, sqlite.ListPullRequestHandoffsParams{ProjectID: &projectID})
	if err != nil {
		return sqlite.PullRequestHandoff{}, false, err
	}
	if len(handoffs) == 0 {
		return sqlite.PullRequestHandoff{}, false, nil
	}
	for _, handoff := range handoffs {
		if issueURL != "" && handoff.IssueURL == issueURL {
			return handoff, true, nil
		}
	}
	return handoffs[0], true, nil
}

func workProofEvents(events []runtimeevents.Record) workProofEventsView {
	view := workProofEventsView{Count: len(events), Latest: []workProofEventView{}}
	start := len(events) - 5
	if start < 0 {
		start = 0
	}
	for _, event := range events[start:] {
		view.Latest = append(view.Latest, workProofEventView{ID: event.ID, Type: string(event.Type)})
	}
	return view
}

func workProofNextSteps(view workProofView) []string {
	if view.Task == nil && view.Source.Type == "intake_item" {
		switch view.ProofState {
		case "needs_clarification":
			return []string{"answer clarification prompts before drafting or accepting a Work Item"}
		case "review_required":
			return []string{"accept, reject, or clarify the intake draft before any Work Item exists"}
		case "approval_required":
			return []string{"resolve intake approval before promoting work"}
		default:
			return []string{"review intake state before creating or dispatching work"}
		}
	}
	if len(view.Approvals.Pending) > 0 {
		return []string{"resolve pending approval before execution or external mutation"}
	}
	if len(view.Execution.Runs) == 0 {
		return []string{"dispatch and execute the Work Item before delivery evidence or PR handoff"}
	}
	if view.Delivery.EvidenceStatus == "missing" {
		return []string{"record delivery evidence before PR handoff"}
	}
	if view.PullRequest.Status == "missing" {
		return []string{"prepare a pull request handoff for human review"}
	}
	if !view.MergeGate.Approved {
		return []string{"obtain explicit human merge approval before merge"}
	}
	return []string{"inspect proof evidence before marking external work complete"}
}

func findWorkIntake(ctx context.Context, store *sqlite.Store, ref string) (sqlite.IntakeItem, error) {
	ref = strings.TrimSpace(ref)
	idRef := strings.TrimPrefix(ref, "intake-")
	if id, err := strconv.ParseInt(idRef, 10, 64); err == nil && id > 0 {
		return store.GetIntakeItem(ctx, id)
	}
	return sqlite.IntakeItem{}, sql.ErrNoRows
}

func findWorkTask(ctx context.Context, store *sqlite.Store, ref string) (sqlite.Task, error) {
	ref = strings.TrimSpace(ref)
	idRef := strings.TrimPrefix(ref, "task-")
	if id, err := strconv.ParseInt(idRef, 10, 64); err == nil && id > 0 {
		return store.GetTask(ctx, id)
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return sqlite.Task{}, err
	}
	for _, view := range views {
		if view.TaskKey == ref {
			return store.GetTask(ctx, view.TaskID)
		}
	}
	return sqlite.Task{}, sql.ErrNoRows
}

type workRetryView struct {
	Retried                bool              `json:"retried"`
	Reason                 string            `json:"reason"`
	Decision               string            `json:"decision"`
	RetryEligible          bool              `json:"retry_eligible"`
	RecoveryRecommendation string            `json:"recovery_recommendation,omitempty"`
	Task                   workRetryTaskView `json:"task,omitempty"`
}

type workRetryTaskView struct {
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

func workRetryOutcomeView(outcome jobs.RetryOutcome) workRetryView {
	view := workRetryView{
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
		view.Task = workRetryTaskView{
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

func workDispatchOutcomeView(outcome jobs.DispatchOutcome) workDispatchView {
	view := workDispatchView{
		Dispatched: outcome.Dispatched,
		Reason:     outcome.Reason,
	}
	if view.Reason == "" {
		view.Reason = "unknown"
	}
	if outcome.Task.ID != 0 {
		view.Task = workDispatchTaskView{
			ID:                    outcome.Task.ID,
			ProjectID:             outcome.Task.ProjectID,
			Key:                   outcome.Task.Key,
			Status:                outcome.Task.Status,
			CurrentRunID:          outcome.Task.CurrentRunID,
			ExecutionIntent:       outcome.Task.ExecutionIntent,
			ExecutionIntentSource: outcome.Task.ExecutionIntentSource,
			BlockedReason:         outcome.Task.BlockedReason,
		}
	}
	if outcome.Run != nil {
		view.Run = &workDispatchRunView{
			ID:       outcome.Run.ID,
			TaskID:   outcome.Run.TaskID,
			Executor: outcome.Run.Executor,
			Status:   outcome.Run.Status,
			Attempt:  outcome.Run.Attempt,
			Summary:  outcome.Run.Summary,
		}
	}
	return view
}

type workExecutionView struct {
	Executed bool                 `json:"executed"`
	Reason   string               `json:"reason"`
	Task     workDispatchTaskView `json:"task,omitempty"`
	Run      *workDispatchRunView `json:"run,omitempty"`
}

func workExecutionOutcomeView(outcome jobs.RunExecutionOutcome) workExecutionView {
	view := workExecutionView{
		Executed: outcome.Executed,
		Reason:   outcome.Reason,
	}
	if view.Reason == "" {
		view.Reason = "unknown"
	}
	if outcome.Task.ID != 0 {
		view.Task = workDispatchTaskView{
			ID:                    outcome.Task.ID,
			ProjectID:             outcome.Task.ProjectID,
			Key:                   outcome.Task.Key,
			Status:                outcome.Task.Status,
			CurrentRunID:          outcome.Task.CurrentRunID,
			ExecutionIntent:       outcome.Task.ExecutionIntent,
			ExecutionIntentSource: outcome.Task.ExecutionIntentSource,
			BlockedReason:         outcome.Task.BlockedReason,
		}
	}
	if outcome.Run != nil {
		view.Run = &workDispatchRunView{
			ID:       outcome.Run.ID,
			TaskID:   outcome.Run.TaskID,
			Executor: outcome.Run.Executor,
			Status:   outcome.Run.Status,
			Attempt:  outcome.Run.Attempt,
			Summary:  outcome.Run.Summary,
		}
	}
	return view
}

func runWorkProfiles(snapshot registry.Snapshot, stdout io.Writer) error {
	profiles := deliveryProfiles(snapshot)
	if len(profiles) == 0 {
		_, err := fmt.Fprintln(stdout, "no delivery profiles")
		return err
	}

	for _, profile := range profiles {
		status := profile.Status
		if status == "" {
			status = "unknown"
		}
		if _, err := fmt.Fprintf(stdout, "%s status=%s entrypoint=%s summary=%s\n", profile.Key, status, profile.Entrypoint, profile.Summary); err != nil {
			return err
		}
	}
	return nil
}

func runWorkStart(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	projectKey := strings.TrimSpace(params["project"])
	title := strings.TrimSpace(params["title"])
	intent := strings.TrimSpace(params["intent"])
	if projectKey == "" || title == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work start --project <key> --title <text> [--intent <read_only|mutation|governance|destructive>]")
		return err
	}
	if intent != "" && !isValidWorkIntent(intent) {
		return fmt.Errorf("intent must be one of read_only, mutation, governance, destructive")
	}

	resolved := scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: projectKey,
	}
	if projectKey == "odin-core" {
		resolved.Kind = scope.ScopeOdinCore
	}

	task, err := jobs.Service{
		Store:    store,
		Registry: projectRegistry,
	}.CreateTaskOnce(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 title,
		RequestedBy:           "operator",
		ExecutionIntent:       intent,
		ExecutionIntentSource: intentSourceForWorkStart(intent),
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "work_item_id=%d project=%s key=%s status=%s intent=%s intent_source=%s\n", task.Task.ID, projectKey, task.Task.Key, task.Task.Status, noneIfEmpty(task.Task.ExecutionIntent), noneIfEmpty(task.Task.ExecutionIntentSource))
	return err
}

func isValidWorkIntent(intent string) bool {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "read_only", "mutation", "governance", "destructive":
		return true
	default:
		return false
	}
}

func intentSourceForWorkStart(intent string) string {
	if strings.TrimSpace(intent) == "" {
		return ""
	}
	return "operator"
}

func runWorkIntake(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	projectKey := strings.TrimSpace(params["project"])
	if projectKey == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work intake --project <key> [--dry-run]")
		return err
	}

	summary, err := trackerintake.Service{
		Store:    store,
		Registry: projectRegistry,
		NewTracker: func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
			return newIntakeTracker(project, options)
		},
	}.SyncProject(ctx, trackerintake.SyncOptions{
		ProjectKey: projectKey,
		DryRun:     parseBoolFlag(params, "dry-run"),
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"project=%s repo=%s fetched=%d persisted=%d dry_run=%t dispatch=not_started prs=not_created\n",
		summary.ProjectKey,
		summary.Repo,
		summary.Fetched,
		summary.Persisted,
		summary.DryRun,
	)
	return err
}

func runWorkReconcile(ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	projectKey := strings.TrimSpace(params["project"])
	if projectKey == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work reconcile --project <key>")
		return err
	}

	summary, err := trackerintake.Service{
		Store:    store,
		Registry: projectRegistry,
	}.ReconcileProject(ctx, trackerintake.ReconcileOptions{
		ProjectKey: projectKey,
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"project=%s repo=%s intake=not_started reconciliation=completed eligible=%d created=%d existing=%d linked=%d dispatch=not_started prs=not_created\n",
		summary.ProjectKey,
		summary.Repo,
		summary.Eligible,
		summary.Created,
		summary.Existing,
		summary.Linked,
	)
	return err
}

func parseWorkStartArgs(args []string) map[string]string {
	values := make(map[string]string)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if next := index + 1; next >= len(args) || strings.HasPrefix(args[next], "--") {
				values[key] = "true"
				continue
			}
			if next := index + 1; next < len(args) {
				values[key] = args[next]
				index = next
			}
			continue
		}
		if key, value, ok := strings.Cut(arg, "="); ok {
			values[strings.TrimLeft(key, "-")] = value
		}
	}
	return values
}

func parseBoolFlag(values map[string]string, key string) bool {
	value := strings.ToLower(strings.TrimSpace(values[key]))
	return value == "true" || value == "1" || value == "yes"
}

func deliveryProfiles(snapshot registry.Snapshot) []registry.Item {
	var profiles []registry.Item
	for _, workflow := range snapshot.ByKind[registry.KindWorkflow] {
		for _, tag := range workflow.Tags {
			if strings.EqualFold(tag, "delivery_profile") {
				profiles = append(profiles, workflow)
				break
			}
		}
	}
	sort.Slice(profiles, func(i int, j int) bool {
		return profiles[i].Key < profiles[j].Key
	})
	return profiles
}

func isOpenWorkItemStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "completed", "failed", "cancelled", "canceled":
		return false
	default:
		return true
	}
}

func isActiveRunAttemptStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "executing", "started":
		return true
	default:
		return false
	}
}

func isReviewableIntakeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "review_required", "needs_clarification", "duplicate_linked_or_suppressed", "approval_required":
		return true
	default:
		return false
	}
}

func isTaskRetryEligible(retryCount int, maxAttempts int) bool {
	return maxAttempts > 1 && retryCount+1 < maxAttempts
}

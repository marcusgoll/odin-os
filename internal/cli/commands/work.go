package commands

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackerintake "odin-os/internal/tracker/intake"
)

const workUsage = "usage: odin work status|profiles|start --project <key> --title <text>|intake --project <key> [--dry-run]|dispatch [--task <id|key>] [--json]|execute --task <id|key> [--json]|retry --task <id|key> [--json]"

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
	ID           int64  `json:"id"`
	ProjectID    int64  `json:"project_id"`
	Key          string `json:"key"`
	Status       string `json:"status"`
	CurrentRunID *int64 `json:"current_run_id,omitempty"`
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
	for _, view := range taskViews {
		if isOpenWorkItemStatus(view.Status) {
			openWorkItems++
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
		"work_items=%d open_work_items=%d active_run_attempts=%d pending_approvals=%d delivery_profiles=%d raw_intake_items=%d intake_review_items=%d intake_approval_required_items=%d failed_retryable_work_items=%d retry_blocked_work_items=%d dispatch=work_dispatch intake=raw_cli\n",
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

	jobService := jobs.Service{Store: store, Registry: projectRegistry}
	if len(options) > 0 && options[0].JobService.Store != nil {
		jobService = options[0].JobService
	}

	var (
		outcome jobs.DispatchOutcome
		err     error
	)
	taskRef := strings.TrimSpace(params["task"])
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
			ID:           outcome.Task.ID,
			ProjectID:    outcome.Task.ProjectID,
			Key:          outcome.Task.Key,
			Status:       outcome.Task.Status,
			CurrentRunID: outcome.Task.CurrentRunID,
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
			ID:           outcome.Task.ID,
			ProjectID:    outcome.Task.ProjectID,
			Key:          outcome.Task.Key,
			Status:       outcome.Task.Status,
			CurrentRunID: outcome.Task.CurrentRunID,
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
	if projectKey == "" || title == "" {
		_, err := fmt.Fprintln(stdout, "usage: odin work start --project <key> --title <text>")
		return err
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
	}.CreateTaskFromAct(ctx, resolved, title)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "work_item_id=%d project=%s key=%s status=%s\n", task.ID, projectKey, task.Key, task.Status)
	return err
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

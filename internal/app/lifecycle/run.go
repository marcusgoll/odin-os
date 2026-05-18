package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	clicommands "odin-os/internal/cli/commands"
	commands "odin-os/internal/cli/commands"
	clioverview "odin-os/internal/cli/overview"
	clirender "odin-os/internal/cli/render"
	"odin-os/internal/cli/repl"
	cliscope "odin-os/internal/cli/scope"
	scope "odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/cli/tui"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/companions"
	"odin-os/internal/core/followups"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/skillbinding"
	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/e2e"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/checkpoints"
	conversationsvc "odin-os/internal/runtime/conversation"
	delegationsvc "odin-os/internal/runtime/delegations"
	runtimeevents "odin-os/internal/runtime/events"
	goalruntime "odin-os/internal/runtime/goals"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	mediasvc "odin-os/internal/runtime/media"
	runtimenotifications "odin-os/internal/runtime/notifications"
	runtimeoverview "odin-os/internal/runtime/overview"
	"odin-os/internal/runtime/projections"
	openroutersmoke "odin-os/internal/runtime/providers/openrouter_smoke"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/runtime/reviewqueue"
	"odin-os/internal/runtime/runs"
	"odin-os/internal/runtime/socialcopilot"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	"odin-os/internal/tools/catalog"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

const rootUsageBanner = "Usage: odin <command> [args]\n\nCommands: help repl overview capabilities tui doctor healthcheck serve backup restore verify-backup status legacy project workspace work scope jobs runs leases approvals review intake agenda logs knowledge memory goal mobile browser x task initiative companion profile followup trigger scheduler transition skills design provider e2e\n\nRun detail: odin runs show <id>"
const runsUsage = "usage: odin runs [--json] | odin runs show <run-id> | odin runs routing (--run <run-id>|--task <id|key>) [--json]"

const schedulerUsage = "usage: odin scheduler tick [now=<RFC3339>] [recovery=<true|false>] [--dry-run|dry_run=<true|false>] [--json]"
const capabilitiesUsage = "usage: odin capabilities list [--kind agent|skill|workflow|command|tool] [--scope <scope>] [--json]\n       odin capabilities show <id> [--version <version>] [--json]"
const capabilityCommandSource = "capability_gateway"
const capabilityPluginModel = "plugins_are_packages_not_runtime_kind"
const serveUsage = "usage: odin serve"
const mobileUsage = "usage: odin mobile token"
const backupUsage = "usage: odin backup <archive-path>"
const restoreUsage = "usage: odin restore <archive-path> <destination-root>"
const verifyBackupUsage = "usage: odin verify-backup <archive-path>"

var (
	serveTaskLoopInterval     = 1 * time.Second
	serveFollowUpLoopInterval = 1 * time.Second
	serveMediaLoopInterval    = 30 * time.Second
	serveSelfHealLoopInterval = 30 * time.Second
	serveMetricsLoopInterval  = 1 * time.Minute
	serveOperationTimeout     = 30 * time.Second
	serveInitialHealthRetry   = 1 * time.Second
	serveHealthConfig         = healthsvc.DefaultConfig()
	serveListen               = net.Listen
	runTUI                    = runTUICommand
)

type serveLoopConfig struct {
	taskInterval      time.Duration
	schedulerInterval time.Duration
	goalInterval      time.Duration
	selfHealInterval  time.Duration
	leaseInterval     time.Duration
	leaseStaleAfter   time.Duration
	healthInterval    time.Duration
}

type serveLoopConfigKey struct{}

var defaultServeLoopConfig = serveLoopConfig{
	taskInterval:      1 * time.Second,
	schedulerInterval: 5 * time.Second,
	goalInterval:      30 * time.Second,
	selfHealInterval:  30 * time.Second,
	leaseInterval:     30 * time.Second,
	leaseStaleAfter:   5 * time.Minute,
	healthInterval:    30 * time.Second,
}

func withServeLoopConfig(ctx context.Context, cfg serveLoopConfig) context.Context {
	return context.WithValue(ctx, serveLoopConfigKey{}, cfg)
}

func serveLoopConfigFromContext(ctx context.Context) serveLoopConfig {
	cfg, _ := ctx.Value(serveLoopConfigKey{}).(serveLoopConfig)
	if cfg.taskInterval <= 0 {
		cfg.taskInterval = defaultServeLoopConfig.taskInterval
	}
	if cfg.schedulerInterval <= 0 {
		cfg.schedulerInterval = defaultServeLoopConfig.schedulerInterval
	}
	if cfg.goalInterval <= 0 {
		cfg.goalInterval = defaultServeLoopConfig.goalInterval
	}
	if cfg.selfHealInterval <= 0 {
		cfg.selfHealInterval = defaultServeLoopConfig.selfHealInterval
	}
	if cfg.leaseInterval <= 0 {
		cfg.leaseInterval = defaultServeLoopConfig.leaseInterval
	}
	if cfg.leaseStaleAfter <= 0 {
		cfg.leaseStaleAfter = defaultServeLoopConfig.leaseStaleAfter
	}
	if cfg.healthInterval <= 0 {
		cfg.healthInterval = defaultServeLoopConfig.healthInterval
	}
	return cfg
}

type healthLoopDeps struct {
	Store              *sqlite.Store
	RuntimeState       runtimestate.Service
	Health             healthsvc.Service
	Notifications      notificationRouter
	Executors          map[string]contract.Executor
	ExecutorConfig     executorrouter.Config
	RegistryHealthy    bool
	ProjectionSurfaces []string
	ShutdownRequested  *atomic.Bool
	BootID             string
	RuntimeRoot        string
}

type notificationRouter interface {
	RoutePendingEvents(context.Context) (runtimenotifications.RoutePendingResult, error)
}

type serveDashboardAdmin struct {
	ImmediateNotReady *atomic.Bool
	RuntimeState      runtimestate.Service
	Jobs              jobs.Service
	BootID            string
	RuntimeRoot       string
	Logger            *logs.Logger
}

func (admin serveDashboardAdmin) KillSwitchOn(ctx context.Context) error {
	if admin.ImmediateNotReady != nil {
		admin.ImmediateNotReady.Store(true)
	}
	if err := writeReadinessFlag(admin.RuntimeRoot, "dashboard kill switch enabled"); err != nil {
		logBackgroundError(admin.Logger, "dashboard_admin", err)
		return err
	}
	logBackgroundEvent(admin.Logger, logs.LevelWarn, "dashboard_admin", "kill switch enabled", map[string]any{
		"source": "dashboard",
	})
	if admin.RuntimeState.Store == nil || admin.BootID == "" {
		return nil
	}
	_, err := admin.RuntimeState.MarkDegraded(ctx, runtimestate.TransitionInput{
		BootID: admin.BootID,
		Reason: "dashboard kill switch enabled",
	})
	if errors.Is(err, runtimestate.ErrRuntimeStateDrainLatched) {
		return nil
	}
	return err
}

func (admin serveDashboardAdmin) KillSwitchOff(context.Context) error {
	if admin.ImmediateNotReady != nil {
		admin.ImmediateNotReady.Store(false)
	}
	if err := clearReadinessFlag(admin.RuntimeRoot); err != nil {
		logBackgroundError(admin.Logger, "dashboard_admin", err)
		return err
	}
	logBackgroundEvent(admin.Logger, logs.LevelInfo, "dashboard_admin", "kill switch disabled", map[string]any{
		"source": "dashboard",
	})
	return nil
}

func (admin serveDashboardAdmin) PauseIssue(ctx context.Context, issueID int64) error {
	task, err := admin.Jobs.PauseIssue(ctx, issueID)
	if err != nil {
		logBackgroundError(admin.Logger, "dashboard_admin", err)
		return dashboardAdminIssueActionError(err)
	}
	logBackgroundEvent(admin.Logger, logs.LevelWarn, "dashboard_admin", "issue paused", map[string]any{
		"external_issue_id": issueID,
		"task_id":           task.ID,
		"blocked_reason":    task.BlockedReason,
	})
	return nil
}

func (admin serveDashboardAdmin) ResumeIssue(ctx context.Context, issueID int64) error {
	task, err := admin.Jobs.ResumeIssue(ctx, issueID)
	if err != nil {
		logBackgroundError(admin.Logger, "dashboard_admin", err)
		return dashboardAdminIssueActionError(err)
	}
	logBackgroundEvent(admin.Logger, logs.LevelInfo, "dashboard_admin", "issue resumed", map[string]any{
		"external_issue_id": issueID,
		"task_id":           task.ID,
		"status":            task.Status,
	})
	return nil
}

func dashboardAdminIssueActionError(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: issue or work item not found", apihttp.ErrAdminTargetNotFound)
	case errors.Is(err, jobs.ErrOperatorPauseUnsupported), errors.Is(err, jobs.ErrOperatorResumeUnsupported):
		return fmt.Errorf("%w: %v", apihttp.ErrAdminActionConflict, err)
	default:
		return err
	}
}

// Run dispatches between the interactive shell and machine-oriented operational commands.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "e2e" {
		return e2e.Run(ctx, root, args[1:], stdout)
	}

	cfg, err := appconfig.Load(filepath.Join(root, "config", "odin.yaml"), root, runtimeEnv())
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "serve" && isHelpArgs(args[1:]) {
		_, err := fmt.Fprintln(stdout, serveUsage)
		return err
	}
	if len(args) > 0 && args[0] == "mobile" {
		return runMobilePreflight(cfg, args[1:], stdout)
	}
	if len(args) > 0 && isOperationalHelpCommand(args[0]) && isHelpArgs(args[1:]) {
		_, err := fmt.Fprintln(stdout, operationalHelpUsage(args[0]))
		return err
	}

	loadCtx := ctx
	if len(args) > 0 && args[0] == "serve" {
		serveLock, err := bootstrap.AcquireServiceLock(cfg.RuntimeRoot)
		if err != nil {
			return err
		}
		defer serveLock.Release()
		loadCtx = bootstrap.WithBootID(context.WithoutCancel(ctx), "boot-"+uuid.NewString())
	}

	app, err := bootstrap.Load(loadCtx, root, cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	defer app.Store.Close()

	if len(args) == 0 {
		_, err := fmt.Fprintln(stdout, rootUsageBanner)
		return err
	}

	switch args[0] {
	case "help":
		_, err := fmt.Fprintln(stdout, rootUsageBanner)
		return err
	case "repl":
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runRepl(ctx, app, stdin, stdout, now)
	case "overview":
		return runOverview(ctx, app, args[1:], stdout)
	case "capabilities":
		return runCapabilities(ctx, app, args[1:], stdout)
	case "tui":
		return runTUI(ctx, app, args[1:], stdout)
	case "status":
		return runStatus(ctx, app, cfg, args[1:], stdout)
	case "legacy":
		return commands.RunLegacy(ctx, args[1:], stdout)
	case "project":
		return runProject(ctx, app, args[1:], stdout)
	case "workspace":
		return commands.RunWorkspace(ctx, app.Store, app.Registry, args[1:], stdout)
	case "work":
		return commands.RunWork(ctx, app.Store, app.Registry, app.RegistrySnapshot, args[1:], stdout, commands.WorkOptions{
			JobService: jobs.Service{
				Store:              app.Store,
				RuntimeRoot:        app.RuntimeRoot,
				Registry:           app.Registry,
				Executors:          app.Executors,
				ExecutorConfig:     app.ExecutorConfig,
				ModelRegistry:      app.ModelRegistry,
				PromptRenderer:     app.PromptRenderer,
				PromptTemplateName: app.PromptTemplateName,
				Transitions:        projects.Service{Store: app.Store},
				Leases: leases.Manager{
					Store:        app.Store,
					Git:          gitadapter.Adapter{},
					WorktreeRoot: worktrees.DefaultRoot(),
				},
				Now: time.Now,
			},
		})
	case "scope":
		return runScope(app, args[1:], stdout)
	case "jobs":
		return runJobs(ctx, app, args[1:], stdout)
	case "runs":
		return runRuns(ctx, app, args[1:], stdout)
	case "leases":
		return runLeases(ctx, app, args[1:], stdout)
	case "approvals":
		return runApprovals(ctx, app, args[1:], stdout)
	case "review":
		return runReview(ctx, app, args[1:], stdout)
	case "intake":
		return runIntake(ctx, app, stdin, args[1:], stdout)
	case "agenda":
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runAgenda(ctx, app, args[1:], stdout, now)
	case "logs":
		return runLogs(ctx, app, args[1:], stdout)
	case "knowledge":
		return commands.RunKnowledge(ctx, app.Store, args[1:], stdout)
	case "memory":
		return commands.RunMemory(ctx, app.Store, args[1:], stdout)
	case "goal":
		return runGoal(ctx, app, args[1:], stdout)
	case "mobile":
		return runMobilePreflight(cfg, args[1:], stdout)
	case "browser":
		return runBrowser(ctx, app, args[1:], stdout)
	case "x":
		return runX(ctx, app, args[1:], stdout)
	case "transition":
		return runTransition(ctx, app, args[1:], stdout)
	case "task":
		return runTask(ctx, app, args[1:], stdout)
	case "initiative":
		return runInitiative(ctx, app, args[1:], stdout)
	case "companion":
		return runCompanion(ctx, app, args[1:], stdout)
	case "profile":
		return commands.RunProfile(ctx, app.Store, args[1:], stdout)
	case "followup":
		return runFollowup(ctx, app, args[1:], stdout)
	case "trigger":
		return commands.RunTrigger(ctx, triggers.Service{Store: app.Store, Registry: app.Registry}, args[1:], stdout)
	case "scheduler":
		return runScheduler(ctx, app, args[1:], stdout)
	case "skills":
		return runSkills(ctx, app, args[1:], stdout)
	case "design":
		return runDesign(ctx, app, args[1:], stdout)
	case "provider":
		return runProvider(ctx, app, args[1:], stdout)
	case "doctor":
		return runDoctor(ctx, app, cfg, args[1:], stdout)
	case "healthcheck":
		return runHealthcheck(ctx, app, cfg, stdout)
	case "serve":
		if isHelpArgs(args[1:]) {
			_, err := fmt.Fprintln(stdout, serveUsage)
			return err
		}
		if len(args) != 1 {
			return fmt.Errorf(serveUsage)
		}
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runServe(ctx, app, cfg, stdout, now)
	case "backup":
		return runBackup(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, args[1:], stdout)
	case "restore":
		return runRestore(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, args[1:], stdout)
	case "verify-backup":
		return runVerifyBackup(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, args[1:], stdout)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func isOperationalHelpCommand(command string) bool {
	switch command {
	case "backup", "restore", "verify-backup":
		return true
	default:
		return false
	}
}

func operationalHelpUsage(command string) string {
	switch command {
	case "backup":
		return backupUsage
	case "restore":
		return restoreUsage
	case "verify-backup":
		return verifyBackupUsage
	default:
		return rootUsageBanner
	}
}

func runMobilePreflight(cfg appconfig.Config, args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArgs(args) {
		_, err := fmt.Fprintln(stdout, mobileUsage)
		return err
	}
	if len(args) != 1 || args[0] != "token" {
		return fmt.Errorf(mobileUsage)
	}
	tokenEnv := strings.TrimSpace(cfg.Service.AdminTokenEnv)
	if tokenEnv == "" {
		tokenEnv = "ODIN_ADMIN_TOKEN"
	}
	token := strings.TrimSpace(cfg.AdminToken)
	if token == "" {
		return fmt.Errorf("%s is not configured for mobile device registration", tokenEnv)
	}
	_, err := fmt.Fprintf(stdout, "%s=%s\n", tokenEnv, token)
	return err
}

func runRepl(ctx context.Context, app bootstrap.App, stdin io.Reader, stdout io.Writer, now func() time.Time) error {
	shell, err := newShell(app, now)
	if err != nil {
		return err
	}
	if err := shell.Run(ctx, stdin, stdout); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func runTUICommand(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	return tui.RunWithProvider(ctx, args, stdout, tuiModelProvider{app: app})
}

type tuiModelProvider struct {
	app bootstrap.App
}

func (provider tuiModelProvider) EnrichModel(ctx context.Context, model *tui.Model) error {
	workspaceView, err := projections.GetWorkspaceOverviewView(ctx, provider.app.Store.DB(), workspaces.DefaultWorkspaceKey)
	if err == nil {
		model.Name = firstNonEmpty(workspaceView.Name, workspaceView.WorkspaceKey, "odin")
		if model.ActiveRuns == 0 {
			model.ActiveRuns = workspaceView.ActiveRunCount
		}
		if model.ApprovalsWaiting == 0 {
			model.ApprovalsWaiting = workspaceView.PendingApprovalCount
		}
		if model.BlockedItems == 0 {
			model.BlockedItems = workspaceView.BlockedWorkItemCount
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		model.Name = "odin"
	} else {
		return err
	}

	activeRuns, err := projections.ListActiveRunViews(ctx, provider.app.Store.DB())
	if err != nil {
		return err
	}
	if len(activeRuns) > 0 {
		model.ActiveRuns = len(activeRuns)
	}
	for _, run := range activeRuns {
		model.Agents = append(model.Agents, tui.AgentRow{
			Name:    firstNonEmpty(run.Executor, fmt.Sprintf("run-%d", run.RunID)),
			Task:    firstNonEmpty(run.TaskKey, fmt.Sprintf("task-%d", run.TaskID)),
			Project: run.ProjectKey,
			Status:  run.Status,
		})
		if len(model.Agents) >= 6 {
			break
		}
	}

	flows, err := listTUIFlowRows(ctx, provider.app.Store.DB())
	if err != nil {
		return err
	}
	model.Flows = append(model.Flows, flows...)

	approvals, err := projections.ListPendingApprovalViews(ctx, provider.app.Store.DB())
	if err != nil {
		return err
	}
	if len(approvals) > 0 {
		model.ApprovalsWaiting = len(approvals)
	}
	for _, approval := range approvals {
		model.Approvals = append(model.Approvals, tui.ApprovalRow{
			ID:       approval.ApprovalID,
			Task:     firstNonEmpty(approval.TaskKey, fmt.Sprintf("task-%d", approval.TaskID)),
			Project:  approval.ProjectKey,
			Status:   approval.Status,
			Resolver: "unknown",
		})
		if len(model.Approvals) >= 6 {
			break
		}
	}

	goals, err := provider.app.Store.ListGoals(ctx, sqlite.ListGoalsParams{})
	if err != nil {
		return err
	}
	for _, goal := range goals {
		if goal.Status == sqlite.GoalStatusCompleted {
			continue
		}
		currentRun := ""
		if goal.CurrentRunID != nil {
			currentRun = fmt.Sprintf("%d", *goal.CurrentRunID)
		}
		model.Goals = append(model.Goals, tui.GoalRow{
			ID:         goal.ID,
			Title:      goal.Title,
			Status:     string(goal.Status),
			CurrentRun: currentRun,
		})
		if len(model.Goals) >= 6 {
			break
		}
	}

	now := time.Now()
	triggers, err := provider.app.Store.ListAutomationTriggers(ctx, sqlite.ListAutomationTriggersParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return err
	}
	projectKeys := make(map[int64]string)
	for _, trigger := range triggers {
		projectKey, err := provider.projectKey(ctx, trigger.ProjectID, projectKeys)
		if err != nil {
			return err
		}
		lastWorkStatus := ""
		lastWorkDetail := ""
		lastWorkReview := ""
		if trigger.LastWorkItemID != nil {
			task, err := provider.app.Store.GetTask(ctx, *trigger.LastWorkItemID)
			if err == nil {
				lastWorkStatus = task.Status
				lastWorkDetail = firstNonEmpty(task.LastError, task.BlockedReason, task.TerminalReason)
				if task.Status == "failed" {
					lastWorkReview = fmt.Sprintf("failed-work:%d", task.ID)
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		model.Schedules = append(model.Schedules, tui.ScheduleRoutineRow{
			Source:         "schedule",
			Key:            trigger.Key,
			Project:        firstNonEmpty(projectKey, trigger.InitiativeKey),
			Status:         trigger.Status,
			DueStatus:      tuiAutomationTriggerDueStatus(trigger, now),
			NextDueAt:      formatTUITimePtr(trigger.NextEligibleAt),
			LastRanAt:      formatTUITimePtr(trigger.LastMaterializedAt),
			LastWorkItem:   trigger.LastWorkItemKey,
			LastWorkStatus: lastWorkStatus,
			LastWorkDetail: lastWorkDetail,
			LastWorkReview: lastWorkReview,
		})
	}
	followUps, err := projections.ListFollowUpSummaryViews(ctx, provider.app.Store.DB(), workspaces.DefaultWorkspaceKey, now)
	if err != nil {
		return err
	}
	for _, followUp := range followUps {
		model.Schedules = append(model.Schedules, tui.ScheduleRoutineRow{
			Source:    "routine",
			Key:       fmt.Sprintf("%d", followUp.ObligationID),
			Project:   followUp.TargetProjectKey,
			Status:    followUp.Status,
			DueStatus: followUp.DueStatus,
			NextDueAt: followUp.NextDueAt.UTC().Format(time.RFC3339),
			LastRanAt: formatTUITimePtr(followUp.LastCompletedAt),
		})
	}

	handoffs, err := provider.app.Store.ListPullRequestHandoffs(ctx, sqlite.ListPullRequestHandoffsParams{})
	if err != nil {
		return err
	}
	projectNames := make(map[int64]string)
	for _, handoff := range handoffs {
		if handoff.State == "closed" && handoff.ReviewState == "merged" {
			continue
		}
		if _, ok := projectNames[handoff.ProjectID]; !ok {
			project, err := provider.app.Store.GetProject(ctx, handoff.ProjectID)
			if err == nil {
				projectNames[handoff.ProjectID] = firstNonEmpty(project.Key, project.Name)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		model.PullRequests = append(model.PullRequests, tui.PullRequestRow{
			Project: firstNonEmpty(projectNames[handoff.ProjectID], fmt.Sprintf("project-%d", handoff.ProjectID)),
			Repo:    handoff.Repo,
			Number:  handoff.Number,
			Title:   handoff.Title,
			State:   firstNonEmpty(handoff.State, handoff.ReviewState),
			CI:      "not_wired",
			URL:     handoff.URL,
		})
		if len(model.PullRequests) >= 6 {
			break
		}
	}
	return nil
}

func (provider tuiModelProvider) projectKey(ctx context.Context, projectID int64, cache map[int64]string) (string, error) {
	if projectID == 0 {
		return "", nil
	}
	if key, ok := cache[projectID]; ok {
		return key, nil
	}
	project, err := provider.app.Store.GetProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		cache[projectID] = ""
		return "", nil
	}
	if err != nil {
		return "", err
	}
	cache[projectID] = firstNonEmpty(project.Key, project.Name)
	return cache[projectID], nil
}

func formatTUITimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func tuiAutomationTriggerDueStatus(trigger sqlite.AutomationTrigger, now time.Time) string {
	if trigger.Status != "enabled" {
		return trigger.Status
	}
	if trigger.NextEligibleAt == nil {
		return "manual"
	}
	if trigger.NextEligibleAt.After(now.UTC()) {
		if trigger.LastEvaluatedAt != nil && (trigger.LastMaterializedAt == nil || trigger.LastEvaluatedAt.After(*trigger.LastMaterializedAt)) {
			return "deferred"
		}
		return "waiting"
	}
	return "due"
}

func listTUIFlowRows(ctx context.Context, db *sql.DB) ([]tui.FlowRow, error) {
	inbox, err := listTUIInboxRows(ctx, db, 3)
	if err != nil {
		return nil, err
	}
	outbox, err := listTUIOutboxRows(ctx, db, 3)
	if err != nil {
		return nil, err
	}
	rows := make([]tui.FlowRow, 0, len(inbox)+len(outbox))
	rows = append(rows, inbox...)
	rows = append(rows, outbox...)
	if len(rows) > 6 {
		return rows[:6], nil
	}
	return rows, nil
}

func listTUIInboxRows(ctx context.Context, db *sql.DB, limit int) ([]tui.FlowRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT 'intake#' || id, source_family || '/' || event_kind, status, COALESCE(NULLIF(summary, ''), subject)
		FROM intake_items
		ORDER BY received_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flows := make([]tui.FlowRow, 0, limit)
	for rows.Next() {
		var flow tui.FlowRow
		flow.Direction = "IN"
		if err := rows.Scan(&flow.Ref, &flow.Source, &flow.Status, &flow.Subject); err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(flows) >= limit {
		return flows, nil
	}

	taskRows, err := db.QueryContext(ctx, `
		SELECT 'task-intake#' || ti.id, ti.source || '/' || ti.intake_type, t.status, COALESCE(NULLIF(t.summary, ''), t.title)
		FROM task_intakes ti
		JOIN tasks t ON t.id = ti.task_id
		ORDER BY ti.created_at DESC, ti.id DESC
		LIMIT ?
	`, limit-len(flows))
	if err != nil {
		return nil, err
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var flow tui.FlowRow
		flow.Direction = "IN"
		if err := taskRows.Scan(&flow.Ref, &flow.Source, &flow.Status, &flow.Subject); err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, taskRows.Err()
}

func listTUIOutboxRows(ctx context.Context, db *sql.DB, limit int) ([]tui.FlowRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ref, source, status, subject
		FROM (
			SELECT 'artifact#' || ra.id AS ref,
				ra.artifact_type AS source,
				r.status AS status,
				COALESCE(NULLIF(ra.summary, ''), NULLIF(r.summary, ''), t.title) AS subject,
				ra.created_at AS observed_at
			FROM run_artifacts ra
			JOIN runs r ON r.id = ra.run_id
			JOIN tasks t ON t.id = r.task_id
			UNION ALL
			SELECT 'run#' || r.id AS ref,
				r.executor AS source,
				r.status AS status,
				COALESCE(NULLIF(r.summary, ''), NULLIF(t.summary, ''), t.title) AS subject,
				COALESCE(r.finished_at, r.started_at) AS observed_at
			FROM runs r
			JOIN tasks t ON t.id = r.task_id
		)
		ORDER BY observed_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flows := make([]tui.FlowRow, 0, limit)
	for rows.Next() {
		var flow tui.FlowRow
		flow.Direction = "OUT"
		if err := rows.Scan(&flow.Ref, &flow.Source, &flow.Status, &flow.Subject); err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, rows.Err()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func runOverview(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin overview [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	readinessStatus, healthStatus := overviewRuntimeStatus(ctx, app)
	binaryPath, _ := os.Executable()
	view, err := clioverview.Service{
		Store:            app.Store,
		Registry:         app.Registry,
		RegistrySnapshot: app.RegistrySnapshot,
		ReadinessStatus:  readinessStatus,
		HealthStatus:     healthStatus,
		BinaryPath:       binaryPath,
		SourceRoot:       app.RepoRoot,
		ReviewQueueProjection: func(ctx context.Context) (reviewqueue.Projection, error) {
			return readReviewQueueProjection(ctx, app)
		},
	}.Build(ctx, state.Scope)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintln(stdout, clirender.RenderOverview(view))
	return err
}

func overviewRuntimeStatus(ctx context.Context, app bootstrap.App) (string, string) {
	health := healthsvc.Service{DB: app.Store.DB()}
	registryHealthy := len(app.RegistryDiagnostics) == 0
	report, ready, err := health.Readiness(ctx, registryHealthy)
	if err != nil {
		return "unknown", "unknown"
	}
	if ready {
		return "ready", string(report.Status)
	}
	return "not_ready", string(report.Status)
}

type capabilityListView struct {
	Source       string               `json:"source"`
	PluginModel  string               `json:"plugin_model"`
	Count        int                  `json:"count"`
	Capabilities []capabilityCardView `json:"capabilities"`
}

type capabilityShowView struct {
	Source      string                   `json:"source"`
	PluginModel string                   `json:"plugin_model"`
	Capability  capabilityDescriptorView `json:"capability"`
}

type capabilityCardView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
	Scope   string `json:"scope,omitempty"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

type capabilityDescriptorView struct {
	ID             string                         `json:"id"`
	APIVersion     string                         `json:"api_version,omitempty"`
	Kind           string                         `json:"kind"`
	Name           string                         `json:"name,omitempty"`
	Title          string                         `json:"title,omitempty"`
	Version        string                         `json:"version"`
	Summary        string                         `json:"summary,omitempty"`
	Status         string                         `json:"status,omitempty"`
	Availability   capabilityAvailabilityView     `json:"availability,omitempty"`
	Permissions    []string                       `json:"permissions,omitempty"`
	InputSchema    capabilitySchemaView           `json:"input_schema,omitempty"`
	OutputSchema   capabilitySchemaView           `json:"output_schema,omitempty"`
	Dependencies   []capabilityDependencyView     `json:"dependencies,omitempty"`
	Execution      capabilityExecutionView        `json:"execution,omitempty"`
	Implementation capabilityImplementationView   `json:"implementation,omitempty"`
	Scopes         []string                       `json:"scopes,omitempty"`
	Tags           []string                       `json:"tags,omitempty"`
	Owners         []string                       `json:"owners,omitempty"`
	Source         capabilityDescriptorSourceView `json:"source,omitempty"`
}

type capabilityAvailabilityView struct {
	Scope string `json:"scope,omitempty"`
	Mode  string `json:"mode,omitempty"`
}

type capabilitySchemaView struct {
	Ref  string `json:"ref,omitempty"`
	Type string `json:"type,omitempty"`
}

type capabilityDependencyView struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type capabilityExecutionView struct {
	Mode    string `json:"mode,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

type capabilityImplementationView struct {
	Kind string `json:"kind,omitempty"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

type capabilityDescriptorSourceView struct {
	Path         string `json:"path,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
}

func runCapabilities(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	_ = ctx
	if len(args) == 0 || args[0] == "help" {
		_, err := fmt.Fprintln(stdout, capabilitiesUsage)
		return err
	}

	gateway := newReadOnlyCapabilityGateway(app)
	if gateway == nil {
		return fmt.Errorf("capability gateway unavailable")
	}

	switch args[0] {
	case "list":
		return runCapabilitiesList(gateway, args[1:], stdout)
	case "show":
		return runCapabilitiesShow(gateway, args[1:], stdout)
	default:
		return fmt.Errorf("%s", capabilitiesUsage)
	}
}

func runCapabilitiesList(gateway *capabilities.Gateway, args []string, stdout io.Writer) error {
	jsonOutput, kind, scope, err := parseCapabilitiesListArgs(args)
	if err != nil {
		return err
	}

	cards := gateway.ListCapabilities(kind, scope)
	view := capabilityListView{
		Source:       capabilityCommandSource,
		PluginModel:  capabilityPluginModel,
		Count:        len(cards),
		Capabilities: make([]capabilityCardView, 0, len(cards)),
	}
	for _, card := range cards {
		view.Capabilities = append(view.Capabilities, capabilityCardToView(card))
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}

	for _, card := range view.Capabilities {
		if _, err := fmt.Fprintf(stdout, "%s version=%s kind=%s scope=%s status=%s\n", card.ID, card.Version, card.Kind, card.Scope, card.Status); err != nil {
			return err
		}
	}
	return nil
}

func runCapabilitiesShow(gateway *capabilities.Gateway, args []string, stdout io.Writer) error {
	id, version, jsonOutput, err := parseCapabilitiesShowArgs(args)
	if err != nil {
		return err
	}

	descriptor, err := resolveCLICapabilityDescriptor(gateway, id, version)
	if err != nil {
		return err
	}
	view := capabilityShowView{
		Source:      capabilityCommandSource,
		PluginModel: capabilityPluginModel,
		Capability:  capabilityDescriptorToView(descriptor),
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}

	_, err = fmt.Fprintf(stdout, "%s version=%s kind=%s scope=%s status=%s implementation=%s:%s\n",
		view.Capability.ID,
		view.Capability.Version,
		view.Capability.Kind,
		view.Capability.Availability.Scope,
		view.Capability.Status,
		view.Capability.Implementation.Kind,
		view.Capability.Implementation.Path,
	)
	return err
}

func parseCapabilitiesListArgs(args []string) (bool, registry.Kind, string, error) {
	jsonOutput := false
	kind := registry.KindUnknown
	scope := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			if jsonOutput {
				return false, registry.KindUnknown, "", fmt.Errorf("duplicate --json flag")
			}
			jsonOutput = true
		case "--kind":
			if kind != registry.KindUnknown {
				return false, registry.KindUnknown, "", fmt.Errorf("duplicate --kind flag")
			}
			if index+1 >= len(args) {
				return false, registry.KindUnknown, "", fmt.Errorf("--kind requires a value")
			}
			parsed, err := parseCapabilityKind(args[index+1])
			if err != nil {
				return false, registry.KindUnknown, "", err
			}
			kind = parsed
			index++
		case "--scope":
			if scope != "" {
				return false, registry.KindUnknown, "", fmt.Errorf("duplicate --scope flag")
			}
			if index+1 >= len(args) {
				return false, registry.KindUnknown, "", fmt.Errorf("--scope requires a value")
			}
			scope = strings.TrimSpace(args[index+1])
			if scope == "" {
				return false, registry.KindUnknown, "", fmt.Errorf("--scope requires a value")
			}
			index++
		default:
			return false, registry.KindUnknown, "", fmt.Errorf("%s", capabilitiesUsage)
		}
	}
	return jsonOutput, kind, scope, nil
}

func parseCapabilitiesShowArgs(args []string) (string, string, bool, error) {
	id := ""
	version := ""
	jsonOutput := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			if jsonOutput {
				return "", "", false, fmt.Errorf("duplicate --json flag")
			}
			jsonOutput = true
		case "--version":
			if version != "" {
				return "", "", false, fmt.Errorf("duplicate --version flag")
			}
			if index+1 >= len(args) {
				return "", "", false, fmt.Errorf("--version requires a value")
			}
			version = strings.TrimSpace(args[index+1])
			if version == "" {
				return "", "", false, fmt.Errorf("--version requires a value")
			}
			index++
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", "", false, fmt.Errorf("%s", capabilitiesUsage)
			}
			if id != "" {
				return "", "", false, fmt.Errorf("%s", capabilitiesUsage)
			}
			id = strings.TrimSpace(args[index])
		}
	}
	if id == "" {
		return "", "", false, fmt.Errorf("%s", capabilitiesUsage)
	}
	return id, version, jsonOutput, nil
}

func parseCapabilityKind(value string) (registry.Kind, error) {
	switch registry.Kind(strings.TrimSpace(value)) {
	case registry.KindAgent:
		return registry.KindAgent, nil
	case registry.KindSkill:
		return registry.KindSkill, nil
	case registry.KindWorkflow:
		return registry.KindWorkflow, nil
	case registry.KindCommand:
		return registry.KindCommand, nil
	case registry.KindTool:
		return registry.KindTool, nil
	default:
		return registry.KindUnknown, fmt.Errorf("unknown capability kind %q", value)
	}
}

func resolveCLICapabilityDescriptor(gateway *capabilities.Gateway, id, version string) (capabilities.Descriptor, error) {
	if strings.TrimSpace(version) != "" {
		descriptor, err := gateway.GetCapability(id, version)
		if err != nil {
			return capabilities.Descriptor{}, fmt.Errorf("capability %q version %q not found: %w", id, version, err)
		}
		return descriptor, nil
	}

	var matches []capabilities.CapabilityCard
	for _, card := range gateway.ListCapabilities(registry.KindUnknown, "") {
		if card.ID == id {
			matches = append(matches, card)
		}
	}

	switch len(matches) {
	case 0:
		return capabilities.Descriptor{}, fmt.Errorf("capability %q not found", id)
	case 1:
		return gateway.GetCapability(id, matches[0].Version)
	default:
		return capabilities.Descriptor{}, fmt.Errorf("capability %q requires --version", id)
	}
}

func capabilityCardToView(card capabilities.CapabilityCard) capabilityCardView {
	return capabilityCardView{
		ID:      card.ID,
		Kind:    string(card.Kind),
		Name:    card.Name,
		Title:   card.Title,
		Version: card.Version,
		Scope:   card.Scope,
		Summary: card.Summary,
		Status:  card.Status,
	}
}

func capabilityDescriptorToView(descriptor capabilities.Descriptor) capabilityDescriptorView {
	dependencies := make([]capabilityDependencyView, 0, len(descriptor.Dependencies))
	for _, dependency := range descriptor.Dependencies {
		dependencies = append(dependencies, capabilityDependencyView{
			Kind:    string(dependency.Kind),
			Name:    dependency.Name,
			Version: dependency.Version,
		})
	}

	return capabilityDescriptorView{
		ID:         descriptor.Key,
		APIVersion: descriptor.APIVersion,
		Kind:       string(descriptor.Kind),
		Name:       descriptor.Name,
		Title:      descriptor.Title,
		Version:    descriptor.Version,
		Summary:    descriptor.Summary,
		Status:     descriptor.Status,
		Availability: capabilityAvailabilityView{
			Scope: descriptor.Availability.Scope,
			Mode:  descriptor.Availability.Mode,
		},
		Permissions: append([]string(nil), descriptor.Permissions...),
		InputSchema: capabilitySchemaView{
			Ref:  descriptor.InputSchema.Ref,
			Type: descriptor.InputSchema.Type,
		},
		OutputSchema: capabilitySchemaView{
			Ref:  descriptor.OutputSchema.Ref,
			Type: descriptor.OutputSchema.Type,
		},
		Dependencies: dependencies,
		Execution: capabilityExecutionView{
			Mode:    descriptor.Execution.Mode,
			Timeout: descriptor.Execution.Timeout,
		},
		Implementation: capabilityImplementationView{
			Kind: descriptor.Implementation.Kind,
			Ref:  descriptor.Implementation.Ref,
			Path: descriptor.Implementation.Path,
		},
		Scopes: append([]string(nil), descriptor.Scopes...),
		Tags:   append([]string(nil), descriptor.Tags...),
		Owners: append([]string(nil), descriptor.Owners...),
		Source: capabilityDescriptorSourceView{
			Path:         descriptor.Source.Path,
			RelativePath: descriptor.Source.RelativePath,
		},
	}
}

func runScope(app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin scope [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	view := commands.ScopeView{Scope: scopeLabel(state.Scope)}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}

	_, err = fmt.Fprintf(stdout, "scope=%s\n", view.Scope)
	return err
}

func runJobs(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin jobs [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	views, err := jobs.Service{Store: app.Store}.List(ctx, state.Scope)
	if err != nil {
		return err
	}
	if jsonOutput {
		jobViews := make([]commands.JobView, 0, len(views))
		for _, view := range views {
			jobViews = append(jobViews, commands.JobView{
				ProjectKey:            view.ProjectKey,
				ProjectID:             view.ProjectID,
				TaskID:                view.TaskID,
				TaskKey:               view.TaskKey,
				Status:                view.Status,
				WorkKind:              view.WorkKind,
				ExecutionIntent:       view.ExecutionIntent,
				ExecutionIntentSource: view.ExecutionIntentSource,
				BlockedReason:         view.BlockedReason,
				CurrentRunID:          view.CurrentRunID,
				CurrentRunStatus:      view.CurrentRunStatus,
			})
		}
		return commands.WriteJSON(stdout, commands.JobsView{Jobs: jobViews})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no jobs")
		return err
	}

	for _, view := range views {
		if _, err := fmt.Fprintf(stdout, "%s %s %s\n", view.ProjectKey, view.TaskKey, view.Status); err != nil {
			return err
		}
	}
	return nil
}

func runDoctor(ctx context.Context, app bootstrap.App, cfg appconfig.Config, args []string, stdout io.Writer) error {
	if err := bootstrap.RefreshReadinessSamples(ctx, app, len(app.RegistryDiagnostics) == 0); err != nil {
		return err
	}

	report, _, err := newHealthService(app, healthsvc.DefaultConfig(), cfg).Readiness(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	workspaceCheck := coreworkspace.DoctorCheck(ctx, app.Registry, os.Getenv, exec.LookPath)
	report.Checks = append(report.Checks, workspaceCheck)
	report.Status = combineDoctorStatus(report.Status, workspaceCheck.Status)

	if len(args) > 0 && args[0] == "--json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	_, err = fmt.Fprintf(stdout, "status=%s checks=%d\n", report.Status, len(report.Checks))
	return err
}

func combineDoctorStatus(current healthsvc.Status, next healthsvc.Status) healthsvc.Status {
	if current == healthsvc.StatusFailed || next == healthsvc.StatusFailed {
		return healthsvc.StatusFailed
	}
	if current == healthsvc.StatusDegraded || next == healthsvc.StatusDegraded {
		return healthsvc.StatusDegraded
	}
	return healthsvc.StatusHealthy
}

func runRuns(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		if strings.EqualFold(remaining[0], "show") {
			if jsonOutput || len(remaining) != 2 {
				return fmt.Errorf(runsUsage)
			}
			runID, err := strconv.ParseInt(remaining[1], 10, 64)
			if err != nil || runID <= 0 {
				return fmt.Errorf("run id must be a positive integer")
			}
			state, err := loadCLIState(app)
			if err != nil {
				return err
			}
			detail, err := runs.Service{DB: app.Store.DB(), Store: app.Store}.Show(ctx, state.Scope, runID)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(stdout, clirender.RenderRunDetail(detail))
			return err
		}
		if strings.EqualFold(remaining[0], "routing") {
			return runRunsRouting(ctx, app, remaining[1:], jsonOutput, stdout)
		}
		return fmt.Errorf(runsUsage)
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	views, err := runs.Service{DB: app.Store.DB()}.List(ctx, state.Scope)
	if err != nil {
		return err
	}
	if jsonOutput {
		runViews := make([]commands.RunView, 0, len(views))
		for _, view := range views {
			runViews = append(runViews, commands.RunView{
				RunID:                 view.RunID,
				TaskID:                view.TaskID,
				TaskKey:               view.TaskKey,
				ProjectKey:            view.ProjectKey,
				RepoRoot:              view.RepoRoot,
				WorktreePath:          view.WorktreePath,
				BranchName:            view.BranchName,
				ExecutionIntent:       view.ExecutionIntent,
				ExecutionIntentSource: view.ExecutionIntentSource,
				Executor:              view.Executor,
				Status:                view.Status,
				Attempt:               view.Attempt,
			})
		}
		return commands.WriteJSON(stdout, commands.RunsView{Runs: runViews})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no runs")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(stdout, "%s %s %s\n", view.TaskKey, view.Executor, view.Status); err != nil {
			return err
		}
	}
	return nil
}

func runRunsRouting(ctx context.Context, app bootstrap.App, args []string, jsonOutput bool, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf(runsUsage)
	}
	state, err := loadCLIState(app)
	if err != nil {
		return err
	}
	service := runs.Service{DB: app.Store.DB(), Store: app.Store}
	var readback runs.ModelRoutingReadback
	switch args[0] {
	case "--run":
		runID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || runID <= 0 {
			return fmt.Errorf("run id must be a positive integer")
		}
		readback, err = service.ModelRoutingForRun(ctx, state.Scope, runID)
		if err != nil {
			return err
		}
	case "--task":
		readback, err = service.ModelRoutingForTask(ctx, state.Scope, args[1])
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf(runsUsage)
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, readback)
	}
	_, err = fmt.Fprint(stdout, clirender.RenderModelRouting(readback))
	return err
}

func runLeases(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "cleanup" {
		return fmt.Errorf("usage: odin leases cleanup [--dry-run|confirm]")
	}
	return runLeasesCleanup(ctx, app, args[1:], stdout)
}

func runLeasesCleanup(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	dryRun := true
	switch len(args) {
	case 0:
	case 1:
		switch strings.ToLower(args[0]) {
		case "--dry-run", "dry-run":
			dryRun = true
		case "confirm":
			dryRun = false
		default:
			return fmt.Errorf("usage: odin leases cleanup [--dry-run|confirm]")
		}
	default:
		return fmt.Errorf("usage: odin leases cleanup [--dry-run|confirm]")
	}

	manager := worktrees.Manager{
		Store:        app.Store,
		Git:          gitadapter.Adapter{},
		WorktreeRoot: worktrees.DefaultRoot(),
	}
	logger, logCloser, err := openServiceLogger(app.RuntimeRoot)
	if err != nil {
		return err
	}
	if logCloser != nil {
		defer logCloser.Close()
	}
	manager.Logger = logger
	staleBefore := time.Now().UTC().Add(-defaultServeLoopConfig.leaseStaleAfter)
	preview, err := manager.PreviewCleanup(ctx, staleBefore)
	if err != nil {
		return err
	}
	if err := renderLeaseCleanupPreview(ctx, app, preview, stdout); err != nil {
		return err
	}
	if dryRun {
		return nil
	}

	cleanupLeases := make([]sqlite.WorktreeLease, 0)
	for _, decision := range preview.Leases {
		if decision.Action == worktrees.CleanupActionCleanup {
			cleanupLeases = append(cleanupLeases, decision.Lease)
		}
	}
	if len(cleanupLeases) == 0 {
		_, err := fmt.Fprintln(stdout, "no cleanup-eligible leases")
		return err
	}

	result, err := manager.CleanupLeases(ctx, cleanupLeases)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "cleaned %d lease(s)\n", len(result.Removed))
	return err
}

func runProvider(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	if len(args) < 3 || !strings.EqualFold(args[0], "openrouter") || !strings.EqualFold(args[1], "smoke") {
		return fmt.Errorf(openRouterSmokeUsage)
	}
	if manifest, ok := app.Registry.Lookup("odin-core"); ok {
		if _, err := (projects.Service{Store: app.Store}).RegisterManagedProject(ctx, manifest); err != nil {
			return err
		}
	}
	service := openroutersmoke.Service{
		Store:         app.Store,
		ModelRegistry: app.ModelRegistry,
		ProjectKey:    "odin-core",
		Getenv:        os.Getenv,
	}
	switch strings.ToLower(strings.TrimSpace(args[2])) {
	case "prepare":
		modelKey, jsonOutput, err := parseOpenRouterSmokePrepareArgs(args[3:])
		if err != nil {
			return err
		}
		result, err := service.Prepare(ctx, openroutersmoke.PrepareParams{ModelKey: modelKey})
		if err != nil {
			return err
		}
		view := openRouterSmokePrepareView{
			TaskID:           result.Task.ID,
			TaskKey:          result.Task.Key,
			PrepareRunID:     result.Run.ID,
			ApprovalID:       result.Approval.ID,
			Status:           result.Approval.Status,
			ProviderKey:      openroutersmoke.ProviderKey,
			ModelKey:         modelKeyOrDefault(modelKey),
			ProviderModelID:  result.ProviderModelID,
			RequestSHA256:    result.RequestSHA256,
			NetworkAccess:    false,
			FixtureTransport: true,
			ApprovalRequired: true,
			ExactRunCommand:  result.ExactRunCommand,
			MaxOutputTokens:  result.MaxOutputTokens,
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "approval=%d task=%s run=%d provider=openrouter model=%s network_access=false fixture_transport=true next=\"%s\"\n", result.Approval.ID, result.Task.Key, result.Run.ID, view.ModelKey, result.ExactRunCommand)
		return err
	case "run":
		parsed, err := parseOpenRouterSmokeRunArgs(args[3:])
		if err != nil {
			return err
		}
		result, err := service.Run(ctx, openroutersmoke.RunParams{
			ApprovalID:  parsed.ApprovalID,
			ModelKey:    parsed.ModelKey,
			Live:        parsed.Live,
			ConfirmLive: parsed.ConfirmLive,
		})
		if err != nil {
			return err
		}
		view := openRouterSmokeRunView{
			TaskID:           result.Task.ID,
			TaskKey:          result.Task.Key,
			RunID:            result.Run.ID,
			ApprovalID:       result.Approval.ID,
			Status:           result.Run.Status,
			ProviderKey:      openroutersmoke.ProviderKey,
			ModelKey:         modelKeyOrDefault(parsed.ModelKey),
			ProviderModelID:  result.ProviderModelID,
			RequestSHA256:    result.RequestSHA256,
			ResponseID:       result.ResponseID,
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.OutputTokens,
			NetworkAccess:    result.NetworkAccess,
			FixtureTransport: false,
			Redaction:        "applied",
		}
		if parsed.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "run=%d approval=%d provider=openrouter model=%s status=%s network_access=true redaction=applied\n", result.Run.ID, result.Approval.ID, view.ModelKey, result.Run.Status)
		return err
	case "evidence":
		parsed, err := parseOpenRouterSmokeEvidenceArgs(args[3:])
		if err != nil {
			return err
		}
		result, err := service.Evidence(ctx, openroutersmoke.EvidenceParams{
			ApprovalID: parsed.ApprovalID,
			RunID:      parsed.RunID,
		})
		if err != nil {
			return err
		}
		if parsed.JSON {
			return commands.WriteJSON(stdout, result)
		}
		liveRun := "none"
		if result.LiveRunID != nil {
			liveRun = strconv.FormatInt(*result.LiveRunID, 10)
		}
		_, err = fmt.Fprintf(stdout, "approval=%d prepare_run=%d live_run=%s status=%s provider=%s model=%s network_access=%t redaction_proven=%t secret_leak_detected=%t raw_prompt_leak_detected=%t events=%d\n",
			result.ApprovalID,
			result.PrepareRunID,
			liveRun,
			result.Status,
			result.ProviderKey,
			result.ModelKey,
			result.NetworkAccess,
			result.RedactionProven,
			result.SecretLeakDetected,
			result.RawPromptLeakDetected,
			result.EventCount,
		)
		return err
	default:
		return fmt.Errorf(openRouterSmokeUsage)
	}
}

const openRouterSmokeUsage = "usage: odin provider openrouter smoke prepare --model <model-key> [--json] | odin provider openrouter smoke run --approval <approval-id> --model <model-key> --live --confirm-live-provider-call [--json] | odin provider openrouter smoke evidence (--approval <approval-id>|--run <run-id>) [--json]"

type openRouterSmokePrepareView struct {
	TaskID           int64  `json:"task_id"`
	TaskKey          string `json:"task_key"`
	PrepareRunID     int64  `json:"prepare_run_id"`
	ApprovalID       int64  `json:"approval_id"`
	Status           string `json:"status"`
	ProviderKey      string `json:"provider_key"`
	ModelKey         string `json:"model_key"`
	ProviderModelID  string `json:"provider_model_id"`
	RequestSHA256    string `json:"request_sha256"`
	NetworkAccess    bool   `json:"network_access"`
	FixtureTransport bool   `json:"fixture_transport"`
	ApprovalRequired bool   `json:"approval_required"`
	ExactRunCommand  string `json:"exact_run_command"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
}

type openRouterSmokeRunView struct {
	TaskID           int64  `json:"task_id"`
	TaskKey          string `json:"task_key"`
	RunID            int64  `json:"run_id"`
	ApprovalID       int64  `json:"approval_id"`
	Status           string `json:"status"`
	ProviderKey      string `json:"provider_key"`
	ModelKey         string `json:"model_key"`
	ProviderModelID  string `json:"provider_model_id"`
	RequestSHA256    string `json:"request_sha256"`
	ResponseID       string `json:"response_id"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	NetworkAccess    bool   `json:"network_access"`
	FixtureTransport bool   `json:"fixture_transport"`
	Redaction        string `json:"redaction"`
}

type openRouterSmokeRunArgs struct {
	ApprovalID  int64
	ModelKey    string
	Live        bool
	ConfirmLive bool
	JSON        bool
}

type openRouterSmokeEvidenceArgs struct {
	ApprovalID int64
	RunID      int64
	JSON       bool
}

func parseOpenRouterSmokePrepareArgs(args []string) (string, bool, error) {
	modelKey := "openrouter-kimi-k2-6"
	jsonOutput := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			jsonOutput = true
		case "--model":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return "", false, fmt.Errorf("usage: odin provider openrouter smoke prepare --model <model-key> [--json]")
			}
			modelKey = strings.TrimSpace(args[index])
		default:
			return "", false, fmt.Errorf("unknown provider openrouter smoke prepare argument: %s", args[index])
		}
	}
	return modelKey, jsonOutput, nil
}

func parseOpenRouterSmokeRunArgs(args []string) (openRouterSmokeRunArgs, error) {
	parsed := openRouterSmokeRunArgs{ModelKey: "openrouter-kimi-k2-6"}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			parsed.JSON = true
		case "--live":
			parsed.Live = true
		case "--confirm-live-provider-call":
			parsed.ConfirmLive = true
		case "--model":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return openRouterSmokeRunArgs{}, fmt.Errorf("usage: odin provider openrouter smoke run --approval <approval-id> --model <model-key> --live --confirm-live-provider-call [--json]")
			}
			parsed.ModelKey = strings.TrimSpace(args[index])
		case "--approval":
			index++
			if index >= len(args) {
				return openRouterSmokeRunArgs{}, fmt.Errorf("usage: odin provider openrouter smoke run --approval <approval-id> --model <model-key> --live --confirm-live-provider-call [--json]")
			}
			approvalID, err := strconv.ParseInt(args[index], 10, 64)
			if err != nil || approvalID <= 0 {
				return openRouterSmokeRunArgs{}, fmt.Errorf("approval id must be a positive integer")
			}
			parsed.ApprovalID = approvalID
		default:
			return openRouterSmokeRunArgs{}, fmt.Errorf("unknown provider openrouter smoke run argument: %s", args[index])
		}
	}
	if parsed.ApprovalID <= 0 || !parsed.Live || !parsed.ConfirmLive {
		return openRouterSmokeRunArgs{}, fmt.Errorf("usage: odin provider openrouter smoke run --approval <approval-id> --model <model-key> --live --confirm-live-provider-call [--json]")
	}
	return parsed, nil
}

func parseOpenRouterSmokeEvidenceArgs(args []string) (openRouterSmokeEvidenceArgs, error) {
	parsed := openRouterSmokeEvidenceArgs{}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			parsed.JSON = true
		case "--approval":
			index++
			if index >= len(args) {
				return openRouterSmokeEvidenceArgs{}, fmt.Errorf("usage: odin provider openrouter smoke evidence (--approval <approval-id>|--run <run-id>) [--json]")
			}
			approvalID, err := strconv.ParseInt(args[index], 10, 64)
			if err != nil || approvalID <= 0 {
				return openRouterSmokeEvidenceArgs{}, fmt.Errorf("approval id must be a positive integer")
			}
			parsed.ApprovalID = approvalID
		case "--run":
			index++
			if index >= len(args) {
				return openRouterSmokeEvidenceArgs{}, fmt.Errorf("usage: odin provider openrouter smoke evidence (--approval <approval-id>|--run <run-id>) [--json]")
			}
			runID, err := strconv.ParseInt(args[index], 10, 64)
			if err != nil || runID <= 0 {
				return openRouterSmokeEvidenceArgs{}, fmt.Errorf("run id must be a positive integer")
			}
			parsed.RunID = runID
		default:
			return openRouterSmokeEvidenceArgs{}, fmt.Errorf("unknown provider openrouter smoke evidence argument: %s", args[index])
		}
	}
	if (parsed.ApprovalID <= 0 && parsed.RunID <= 0) || (parsed.ApprovalID > 0 && parsed.RunID > 0) {
		return openRouterSmokeEvidenceArgs{}, fmt.Errorf("usage: odin provider openrouter smoke evidence (--approval <approval-id>|--run <run-id>) [--json]")
	}
	return parsed, nil
}

func modelKeyOrDefault(modelKey string) string {
	if strings.TrimSpace(modelKey) == "" {
		return "openrouter-kimi-k2-6"
	}
	return strings.TrimSpace(modelKey)
}

type openRouterSmokeApprovalView struct {
	ProviderKey      string `json:"provider_key"`
	ModelKey         string `json:"model_key"`
	ProviderModelID  string `json:"provider_model_id"`
	RequestSHA256    string `json:"request_sha256"`
	NetworkAccess    bool   `json:"network_access"`
	FixtureTransport bool   `json:"fixture_transport"`
	ApprovalRequired bool   `json:"approval_required"`
	ExactRunCommand  string `json:"exact_run_command"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
}

func openRouterSmokeApprovalContext(ctx context.Context, store *sqlite.Store, approval sqlite.Approval) *openRouterSmokeApprovalView {
	if store == nil || approval.RunID == nil {
		return nil
	}
	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: *approval.RunID, ArtifactType: openroutersmoke.RequestType})
	if err != nil || len(artifacts) == 0 {
		return nil
	}
	var details struct {
		ProviderKey      string `json:"provider_key"`
		ModelKey         string `json:"model_key"`
		ProviderModelID  string `json:"provider_model_id"`
		RequestSHA256    string `json:"request_sha256"`
		NetworkAccess    bool   `json:"network_access"`
		FixtureTransport bool   `json:"fixture_transport"`
		ApprovalRequired bool   `json:"approval_required"`
		ExactRunCommand  string `json:"exact_run_command"`
		MaxOutputTokens  int    `json:"max_output_tokens"`
	}
	if err := json.Unmarshal([]byte(artifacts[len(artifacts)-1].DetailsJSON), &details); err != nil {
		return nil
	}
	return &openRouterSmokeApprovalView{
		ProviderKey:      details.ProviderKey,
		ModelKey:         details.ModelKey,
		ProviderModelID:  details.ProviderModelID,
		RequestSHA256:    details.RequestSHA256,
		NetworkAccess:    true,
		FixtureTransport: details.FixtureTransport,
		ApprovalRequired: details.ApprovalRequired,
		ExactRunCommand:  strings.Replace(details.ExactRunCommand, "<approval-id>", strconv.FormatInt(approval.ID, 10), 1),
		MaxOutputTokens:  details.MaxOutputTokens,
	}
}

func renderLeaseCleanupPreview(ctx context.Context, app bootstrap.App, preview worktrees.CleanupPreview, stdout io.Writer) error {
	if len(preview.Leases) == 0 {
		_, err := fmt.Fprintln(stdout, "no worktree leases")
		return err
	}
	projectKeyByID := map[int64]string{}
	for _, decision := range preview.Leases {
		projectKey, err := projectKeyForID(ctx, app.Store, projectKeyByID, decision.Lease.ProjectID)
		if err != nil {
			return err
		}
		cleanup := "pending"
		if decision.Lease.CleanedUpAt != nil || decision.Lease.State == "cleaned" {
			cleanup = "complete"
		}
		dirty := "unknown"
		if decision.Dirty != nil {
			dirty = strconv.FormatBool(*decision.Dirty)
		}
		if _, err := fmt.Fprintf(stdout, "lease_id=%d project=%s state=%s cleanup=%s action=%s reason=%s dirty=%s task=%d run=%d branch=%s worktree=%s\n", decision.Lease.ID, projectKey, decision.Lease.State, cleanup, decision.Action, decision.Reason, dirty, decision.Lease.TaskID, decision.Lease.RunID, decision.Lease.BranchName, decision.Lease.WorktreePath); err != nil {
			return err
		}
	}
	return nil
}

func projectKeyForID(ctx context.Context, store *sqlite.Store, cache map[int64]string, projectID int64) (string, error) {
	if projectKey := cache[projectID]; projectKey != "" {
		return projectKey, nil
	}
	project, err := store.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	cache[projectID] = project.Key
	return project.Key, nil
}

func runApprovals(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) > 0 && strings.EqualFold(remaining[0], "show") {
		if len(remaining) != 2 {
			return fmt.Errorf("usage: odin approvals show <approval-id> [--json]")
		}
		approvalID, err := strconv.ParseInt(remaining[1], 10, 64)
		if err != nil || approvalID <= 0 {
			return fmt.Errorf("approval id must be a positive integer")
		}
		detail, err := approvalsvc.Service{Store: app.Store}.Detail(ctx, approvalID)
		if err != nil {
			return err
		}
		smokeContext := openRouterSmokeApprovalContext(ctx, app.Store, detail.Approval)
		if jsonOutput {
			resolverSupport := string(detail.ResolverSupport)
			reason := approvalOperatorReason(detail.Approval.Status, resolverSupport)
			if storedReason := strings.TrimSpace(detail.Approval.Reason); storedReason != "" {
				reason = storedReason
			}
			allowedActions := approvalOperatorAllowedActions(detail.Approval.Status, resolverSupport)
			nextSteps := approvalOperatorNextSteps(detail.Approval.ID, detail.Approval.Status, resolverSupport)
			onApprove := approvalOperatorOnApprove(resolverSupport)
			return commands.WriteJSON(stdout, struct {
				ID              int64                        `json:"id"`
				Status          string                       `json:"status"`
				TaskID          int64                        `json:"task_id"`
				TaskKey         string                       `json:"task_key"`
				TaskStatus      string                       `json:"task_status"`
				RunID           *int64                       `json:"run_id,omitempty"`
				DecisionBy      string                       `json:"decision_by,omitempty"`
				Reason          string                       `json:"reason,omitempty"`
				ResolverSupport string                       `json:"resolver_support"`
				Source          string                       `json:"source,omitempty"`
				Risk            string                       `json:"risk,omitempty"`
				AllowedActions  []string                     `json:"allowed_actions,omitempty"`
				NextSteps       string                       `json:"next_steps,omitempty"`
				OnApprove       string                       `json:"on_approve,omitempty"`
				OpenRouterSmoke *openRouterSmokeApprovalView `json:"openrouter_live_smoke,omitempty"`
			}{
				ID:              detail.Approval.ID,
				Status:          detail.Approval.Status,
				TaskID:          detail.Task.ID,
				TaskKey:         detail.Task.Key,
				TaskStatus:      detail.Task.Status,
				RunID:           detail.Approval.RunID,
				DecisionBy:      detail.Approval.DecisionBy,
				Reason:          reason,
				ResolverSupport: resolverSupport,
				Source:          "approval_requests",
				Risk:            "governance",
				AllowedActions:  allowedActions,
				NextSteps:       nextSteps,
				OnApprove:       onApprove,
				OpenRouterSmoke: smokeContext,
			})
		}
		resolverSupport := string(detail.ResolverSupport)
		reason := approvalOperatorReason(detail.Approval.Status, resolverSupport)
		if storedReason := strings.TrimSpace(detail.Approval.Reason); storedReason != "" {
			reason = storedReason
		}
		_, err = fmt.Fprintf(stdout,
			"approval=%d source=approval_requests risk=governance reason=%s task=%s run=%s status=%s task_status=%s resolver=%s actions=%s\n",
			detail.Approval.ID,
			reason,
			detail.Task.Key,
			approvalRunIDLabel(detail.Approval.RunID),
			detail.Approval.Status,
			detail.Task.Status,
			resolverSupport,
			strings.Join(approvalOperatorAllowedActions(detail.Approval.Status, resolverSupport), ","),
		)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "next_steps=%s\n", approvalOperatorNextSteps(detail.Approval.ID, detail.Approval.Status, resolverSupport)); err != nil {
			return err
		}
		if smokeContext != nil {
			if _, err := fmt.Fprintf(stdout, "openrouter_live_smoke provider=%s model=%s provider_model_id=%s network_access=true request_sha256=%s max_output_tokens=%d exact_run_command=%s\n", smokeContext.ProviderKey, smokeContext.ModelKey, smokeContext.ProviderModelID, smokeContext.RequestSHA256, smokeContext.MaxOutputTokens, smokeContext.ExactRunCommand); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(stdout, "on_approve=%s\n", approvalOperatorOnApprove(resolverSupport))
		return err
	}
	if len(remaining) > 0 && strings.EqualFold(remaining[0], "resolve") {
		command := commands.ApprovalResolveCommand{Name: "resolve"}
		if len(remaining) > 1 && !strings.HasPrefix(remaining[1], "--") {
			if len(remaining) < 4 {
				return fmt.Errorf("usage: odin approvals resolve <approval-id> <approve|deny> <reason...>")
			}
			approvalID, err := strconv.ParseInt(remaining[1], 10, 64)
			if err != nil || approvalID <= 0 {
				return fmt.Errorf("approval id must be a positive integer")
			}
			command.ApprovalID = approvalID
			command.Decision = strings.ToLower(strings.TrimSpace(remaining[2]))
			command.Reason = strings.Join(remaining[3:], " ")
			command.By = "operator"
		} else {
			parsed, err := commands.ParseApprovalResolve(remaining)
			if err != nil {
				return err
			}
			command = parsed
		}
		action := approvalResolveAction(command.Decision)
		result, err := approvalsvc.Service{Store: app.Store}.Resolve(ctx, approvalsvc.ResolveParams{
			ApprovalID: command.ApprovalID,
			Action:     action,
			DecisionBy: command.By,
			Reason:     command.Reason,
		})
		if err != nil {
			if !errors.Is(err, approvalsvc.ErrUnsupportedResolver) {
				return err
			}
		}
		if jsonOutput || command.JSON {
			receipt, receiptErr := approvalsvc.FormatReceipt(result)
			if receiptErr != nil {
				return receiptErr
			}
			var submitRunID *int64
			if result.SubmitRun != nil {
				submitRunID = &result.SubmitRun.ID
			}
			return commands.WriteJSON(stdout, struct {
				ID              int64  `json:"id"`
				Status          string `json:"status"`
				DecisionBy      string `json:"decision_by"`
				Reason          string `json:"reason"`
				ResolverSupport string `json:"resolver_support"`
				Result          string `json:"result"`
				SubmitRunID     *int64 `json:"submit_run_id,omitempty"`
				Summary         string `json:"summary"`
			}{
				ID:              result.Approval.ID,
				Status:          result.Approval.Status,
				DecisionBy:      result.Approval.DecisionBy,
				Reason:          result.Approval.Reason,
				ResolverSupport: string(result.ResolverSupport),
				Result:          approvalResolveResultLabel(result),
				SubmitRunID:     submitRunID,
				Summary:         strings.TrimPrefix(receipt.Summary, "summary="),
			})
		}
		receipt, err := approvalsvc.FormatReceipt(result)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout, receipt.Line); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, receipt.Summary)
		return err
	}
	filter, err := commands.ParseApprovalSupportFilter(remaining)
	if err != nil {
		return fmt.Errorf("usage: odin approvals [all|supported|unsupported] [--json] | odin approvals show <approval-id> [--json] | odin approvals resolve <approval-id> <approve|deny> <reason...> [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	approvals, err := listPendingApprovals(ctx, app.Store, state.Scope, filter)
	if err != nil {
		return err
	}
	if jsonOutput {
		if filter == commands.ApprovalSupportAll {
			approvals, err = listAllApprovals(ctx, app.Store, state.Scope)
			if err != nil {
				return err
			}
		}
		return commands.WriteJSON(stdout, commands.ApprovalsView{Approvals: approvals})
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(stdout, "no approvals waiting")
		return err
	}
	for _, approval := range approvals {
		if _, err := fmt.Fprintf(
			stdout,
			"approval=%d source=%s risk=%s reason=%s task=%s run=%s status=%s resolver=%s actions=%s\n",
			approval.ApprovalID,
			approval.Source,
			approval.Risk,
			approval.Reason,
			approval.TaskKey,
			approvalRunIDLabel(approval.RunID),
			approval.Status,
			approval.ResolverSupport,
			strings.Join(approval.AllowedActions, ","),
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "next_steps=%s\n", approval.NextSteps); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "on_approve=%s\n", approval.OnApprove); err != nil {
			return err
		}
	}
	return nil
}

func runIntake(ctx context.Context, app bootstrap.App, stdin io.Reader, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}

	command, err := commands.ParseIntake(remaining)
	if err != nil {
		return err
	}

	if command.Name == "help" {
		_, err := fmt.Fprintln(stdout, commands.IntakeUsage)
		return err
	}
	if command.Name == "raw" {
		return runRawIntake(ctx, app, stdin, command, jsonOutput || command.JSON, stdout)
	}
	if command.Name == "process" {
		return runProcessIntake(ctx, app, command, jsonOutput || command.JSON, stdout)
	}
	if command.Name == "review" {
		return runReviewIntake(ctx, app, command, jsonOutput || command.JSON, stdout)
	}
	if command.Name == "approval" {
		return runApprovalIntake(ctx, app, command, jsonOutput || command.JSON, stdout)
	}

	payloadJSON, err := loadIntakePayloadJSON(command.PayloadFile, stdin)
	if err != nil {
		return err
	}

	manifest, ok := app.Registry.Lookup(command.ProjectKey)
	if !ok {
		return fmt.Errorf("unknown project %q", command.ProjectKey)
	}

	resolved := scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})

	jobService := jobs.Service{
		Store:         app.Store,
		Registry:      app.Registry,
		ModelRegistry: app.ModelRegistry,
		Transitions:   projects.Service{Store: app.Store},
		Now:           time.Now,
	}
	task, err := jobService.CreateTaskFromActWithAction(ctx, resolved, command.Title, command.ActionKey)
	if err != nil {
		return err
	}

	intake, intakeErr := app.Store.CreateTaskIntake(ctx, sqlite.CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      command.Source,
		IntakeType:  command.Type,
		DedupKey:    command.DedupKey,
		RequestedBy: command.RequestedBy,
		PayloadJSON: payloadJSON,
	})
	if intakeErr != nil {
		if errors.Is(intakeErr, sqlite.ErrTaskIntakeConflict) {
			if _, err := app.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
				TaskID:         task.ID,
				Status:         "failed",
				Summary:        intakeErr.Error(),
				TerminalReason: "intake_dedup_conflict",
				ArtifactsJSON:  "[]",
			}); err != nil {
				return errors.Join(intakeErr, err)
			}
		}
		return intakeErr
	}

	view := struct {
		Task struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"task"`
		Intake struct {
			Source   string `json:"source"`
			Type     string `json:"type"`
			DedupKey string `json:"dedup_key"`
		} `json:"intake"`
	}{}
	view.Task.ID = task.ID
	view.Task.Key = task.Key
	view.Task.Status = task.Status
	view.Intake.Source = intake.Source
	view.Intake.Type = intake.IntakeType
	view.Intake.DedupKey = intake.DedupKey

	if jsonOutput || command.JSON {
		return commands.WriteJSON(stdout, view)
	}

	_, err = fmt.Fprintf(stdout, "queued intake task id=%d key=%s source=%s type=%s\n", task.ID, task.Key, intake.Source, intake.IntakeType)
	return err
}

const rawIntakePayloadPolicy = "stored_in_source_facts_json"

type rawIntakeItemView struct {
	ID                     int64                  `json:"id"`
	Key                    string                 `json:"key"`
	Status                 string                 `json:"status"`
	Source                 string                 `json:"source"`
	IntakeType             string                 `json:"intake_type"`
	DedupKey               string                 `json:"dedup_key"`
	RequestedBy            string                 `json:"requested_by"`
	ReceivedAt             string                 `json:"received_at"`
	CreatedAt              string                 `json:"created_at"`
	UpdatedAt              string                 `json:"updated_at"`
	PayloadPolicy          string                 `json:"payload_policy"`
	ProjectKey             string                 `json:"project_key,omitempty"`
	Title                  string                 `json:"title,omitempty"`
	Summary                string                 `json:"summary,omitempty"`
	CanonicalIntakeKey     string                 `json:"canonical_intake_key,omitempty"`
	GoalID                 int64                  `json:"goal_id,omitempty"`
	SuppressionReason      string                 `json:"suppression_reason,omitempty"`
	AcceptedWorkItemID     int64                  `json:"accepted_work_item_id,omitempty"`
	AcceptedWorkItemKey    string                 `json:"accepted_work_item_key,omitempty"`
	AcceptedWorkItemStatus string                 `json:"accepted_work_item_status,omitempty"`
	ApprovalRequired       bool                   `json:"approval_required,omitempty"`
	BlockedPendingApproval bool                   `json:"blocked_pending_approval,omitempty"`
	PolicyReason           string                 `json:"policy_reason,omitempty"`
	PolicyDecision         string                 `json:"policy_decision,omitempty"`
	Classification         string                 `json:"classification,omitempty"`
	DedupeResult           string                 `json:"dedupe_result,omitempty"`
	DedupeBasis            string                 `json:"dedupe_basis,omitempty"`
	Risk                   string                 `json:"risk,omitempty"`
	SuggestedRoute         string                 `json:"suggested_route,omitempty"`
	Evidence               *rawIntakeEvidenceView `json:"evidence,omitempty"`
	Payload                json.RawMessage        `json:"payload,omitempty"`
	Processing             json.RawMessage        `json:"processing,omitempty"`
}

type rawIntakeEvidenceView struct {
	PayloadPolicy        string `json:"payload_policy"`
	SourceFactsAvailable bool   `json:"source_facts_available"`
	PayloadAvailable     bool   `json:"payload_available"`
	PayloadIncluded      bool   `json:"payload_included"`
}

type rawIntakeItemEnvelope struct {
	IntakeItem rawIntakeItemView `json:"intake_item"`
}

type rawIntakeItemListView struct {
	IntakeItems []rawIntakeItemView `json:"intake_items"`
}

type intakeProcessView struct {
	IntakeItem     rawIntakeItemView   `json:"intake_item"`
	Outcome        string              `json:"outcome"`
	Classification string              `json:"classification"`
	DedupeResult   string              `json:"dedupe_result"`
	RoutedOutcome  string              `json:"routed_outcome"`
	GoalID         int64               `json:"goal_id,omitempty"`
	AutoPromoted   bool                `json:"auto_promoted,omitempty"`
	WorkCreated    bool                `json:"work_created,omitempty"`
	PolicyReason   string              `json:"policy_reason,omitempty"`
	PolicyDecision string              `json:"policy_decision,omitempty"`
	WorkItem       *reviewWorkItemView `json:"work_item,omitempty"`
}

type intakeReviewQueueView struct {
	IntakeItems []rawIntakeItemView `json:"intake_items"`
}

type intakeReviewDecisionView struct {
	IntakeItem             rawIntakeItemView   `json:"intake_item"`
	Decision               string              `json:"decision"`
	WorkCreated            bool                `json:"work_created"`
	ApprovalRequired       bool                `json:"approval_required,omitempty"`
	BlockedPendingApproval bool                `json:"blocked_pending_approval,omitempty"`
	PolicyReason           string              `json:"policy_reason,omitempty"`
	PolicyDecision         string              `json:"policy_decision,omitempty"`
	WorkItem               *reviewWorkItemView `json:"work_item,omitempty"`
	GoalID                 int64               `json:"goal_id,omitempty"`
	GoalStatus             string              `json:"goal_status,omitempty"`
}

type reviewWorkItemView struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Status string `json:"status"`
}

type intakeProcessingNotes struct {
	ProcessingStarted bool                  `json:"processing_started"`
	Classification    intakeClassification  `json:"classification"`
	Dedupe            intakeDedupeReview    `json:"dedupe"`
	Routing           intakeRoutingResult   `json:"routing"`
	DraftArtifact     *intakeDraftArtifact  `json:"draft_artifact,omitempty"`
	Goal              *intakeGoalConversion `json:"goal,omitempty"`
	Clarification     *intakeClarification  `json:"clarification,omitempty"`
	Review            *intakeReviewDecision `json:"review,omitempty"`
}

type intakeClassification struct {
	Result         string `json:"result"`
	Reason         string `json:"reason"`
	SourceType     string `json:"source_type,omitempty"`
	Intent         string `json:"intent,omitempty"`
	Risk           string `json:"risk,omitempty"`
	Confidence     string `json:"confidence,omitempty"`
	Category       string `json:"category,omitempty"`
	PriorityScore  int    `json:"priority_score,omitempty"`
	SuggestedRoute string `json:"suggested_route,omitempty"`
}

type intakeDedupeReview struct {
	Result             string `json:"result"`
	Basis              string `json:"basis,omitempty"`
	CanonicalIntakeKey string `json:"canonical_intake_key,omitempty"`
	MatchReason        string `json:"match_reason,omitempty"`
}

type intakeRoutingResult struct {
	Outcome               string                `json:"outcome"`
	ProjectKey            string                `json:"project_key,omitempty"`
	ExecutionIntent       string                `json:"execution_intent,omitempty"`
	ExecutionIntentSource string                `json:"execution_intent_source,omitempty"`
	SkillInvocation       *skillbinding.Binding `json:"skill_invocation,omitempty"`
	GoalID                int64                 `json:"goal_id,omitempty"`
}

type intakeGoalConversion struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	SourceIntakeKey string `json:"source_intake_key"`
	ReviewState     string `json:"review_state"`
}

type intakeDraftArtifact struct {
	Kind                  string `json:"kind"`
	Title                 string `json:"title"`
	ReviewState           string `json:"review_state"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

type intakeClarification struct {
	State   string   `json:"state"`
	Prompts []string `json:"prompts"`
}

type intakeReviewDecision struct {
	Decision               string            `json:"decision"`
	WorkCreated            bool              `json:"work_created"`
	ApprovalRequired       bool              `json:"approval_required,omitempty"`
	BlockedPendingApproval bool              `json:"blocked_pending_approval,omitempty"`
	PolicyReason           string            `json:"policy_reason,omitempty"`
	PolicyDecision         string            `json:"policy_decision,omitempty"`
	WorkItem               *intakeReviewWork `json:"work_item,omitempty"`
	Goal                   *intakeReviewGoal `json:"goal,omitempty"`
}

type intakeReviewWork struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Status string `json:"status"`
}

type intakeReviewGoal struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

func runRawIntake(ctx context.Context, app bootstrap.App, stdin io.Reader, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	switch command.RawAction {
	case "create":
		return runRawIntakeCreate(ctx, app, stdin, command, jsonOutput, stdout)
	case "list":
		return runRawIntakeList(ctx, app, command, jsonOutput, stdout)
	case "show":
		return runRawIntakeShow(ctx, app, command, jsonOutput, stdout)
	default:
		return errors.New(commands.IntakeUsage)
	}
}

func runRawIntakeCreate(ctx context.Context, app bootstrap.App, stdin io.Reader, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	payloadJSON, err := loadIntakePayloadJSON(command.PayloadFile, stdin)
	if err != nil {
		return err
	}

	scopeKind := ""
	scopeKey := ""
	if command.ProjectKey != "" {
		if _, ok := app.Registry.Lookup(command.ProjectKey); !ok {
			return fmt.Errorf("unknown project %q", command.ProjectKey)
		}
		scopeKind = "project"
		scopeKey = command.ProjectKey
	}

	sourceFactsJSON, err := rawIntakeSourceFactsJSON(command, payloadJSON)
	if err != nil {
		return err
	}

	item, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        command.Source,
		ExternalObjectID:    command.ActionKey,
		EventKind:           command.Type,
		Subject:             command.Title,
		DedupeKey:           command.DedupKey,
		DedupeRecipeVersion: "raw-cli-v1",
		SourceFactsJSON:     sourceFactsJSON,
		Status:              "received",
		Scope:               scopeKind,
		ScopeKey:            scopeKey,
		Summary:             command.Title,
	})
	if err != nil {
		return err
	}

	view, err := rawIntakeView(item, true)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, rawIntakeItemEnvelope{IntakeItem: view})
	}
	_, err = fmt.Fprintf(
		stdout,
		"raw_intake=%s status=%s source=%s type=%s dedup_key=%s requested_by=%s payload_policy=%s\n",
		view.Key,
		view.Status,
		view.Source,
		view.IntakeType,
		view.DedupKey,
		view.RequestedBy,
		view.PayloadPolicy,
	)
	return err
}

func runRawIntakeList(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	params := sqlite.ListIntakeItemsParams{
		WorkspaceID: workspaces.DefaultWorkspaceKey,
		Status:      command.Type,
	}
	if command.ProjectKey != "" {
		if _, ok := app.Registry.Lookup(command.ProjectKey); !ok {
			return fmt.Errorf("unknown project %q", command.ProjectKey)
		}
		params.Scope = "project"
		params.ScopeKey = command.ProjectKey
	}
	items, err := app.Store.ListIntakeItems(ctx, params)
	if err != nil {
		return err
	}
	views := make([]rawIntakeItemView, 0, len(items))
	for _, item := range items {
		view, err := rawIntakeView(item, false)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, rawIntakeItemListView{IntakeItems: views})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no raw intake items")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(stdout, "raw_intake=%s status=%s source=%s type=%s dedup_key=%s requested_by=%s created_at=%s payload_policy=%s\n", view.Key, view.Status, view.Source, view.IntakeType, view.DedupKey, view.RequestedBy, view.CreatedAt, view.PayloadPolicy); err != nil {
			return err
		}
	}
	return nil
}

func runRawIntakeShow(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	view, err := rawIntakeView(item, true)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, rawIntakeItemEnvelope{IntakeItem: view})
	}
	_, err = fmt.Fprintf(stdout, "raw_intake=%s status=%s source=%s type=%s dedup_key=%s requested_by=%s created_at=%s payload_policy=%s\n", view.Key, view.Status, view.Source, view.IntakeType, view.DedupKey, view.RequestedBy, view.CreatedAt, view.PayloadPolicy)
	return err
}

func runProcessIntake(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	outcome, err := buildIntakeProcessOutcome(ctx, app.Store, item)
	if err != nil {
		return err
	}
	if outcome.createGoal {
		goal, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{
			Title:       item.Subject,
			Description: "Created from raw intake " + rawIntakeKey(item.ID) + ". " + item.Summary,
			CreatedBy:   "intake:" + rawIntakeKey(item.ID),
			Source:      "intake",
		})
		if err != nil {
			return err
		}
		goalID := goal.ID
		outcome.goalID = &goalID
		outcome.notes.Routing.GoalID = goal.ID
		outcome.notes.Goal = &intakeGoalConversion{
			ID:              goal.ID,
			Title:           goal.Title,
			Status:          string(goal.Status),
			SourceIntakeKey: rawIntakeKey(item.ID),
			ReviewState:     "created_not_approved",
		}
	}
	outcome.events = intakeProcessingEvents(item.ID, outcome.status, outcome.notes, outcome.canonicalIntakeItemID)
	notesJSON, err := json.Marshal(outcome.notes)
	if err != nil {
		return err
	}
	processed, err := app.Store.ProcessIntakeItem(ctx, sqlite.ProcessIntakeItemParams{
		ID:                    item.ID,
		Status:                outcome.status,
		Summary:               outcome.summary,
		CanonicalIntakeItemID: outcome.canonicalIntakeItemID,
		GoalID:                outcome.goalID,
		SuppressionReason:     outcome.suppressionReason,
		RoutingNotes:          string(notesJSON),
		Events:                outcome.events,
	})
	if err != nil {
		return err
	}
	var promotion *intakeAutoPromotionResult
	if promoted, result, err := autoPromoteProcessedIntake(ctx, app, processed); err != nil {
		return err
	} else if result != nil {
		processed = promoted
		promotion = result
	}
	view, err := rawIntakeView(processed, true)
	if err != nil {
		return err
	}
	processView := intakeProcessView{
		IntakeItem:     view,
		Outcome:        outcome.status,
		Classification: outcome.notes.Classification.Result,
		DedupeResult:   outcome.notes.Dedupe.Result,
		RoutedOutcome:  outcome.notes.Routing.Outcome,
	}
	if outcome.goalID != nil {
		processView.GoalID = *outcome.goalID
	}
	if promotion != nil {
		processView.Outcome = processed.Status
		processView.AutoPromoted = true
		processView.WorkCreated = promotion.WorkCreated
		processView.PolicyReason = promotion.PolicyReason
		processView.PolicyDecision = promotion.PolicyDecision
		processView.WorkItem = promotion.WorkItem
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, processView)
	}
	_, err = fmt.Fprintf(stdout, "raw_intake=%s status=%s classification=%s dedupe=%s routed_outcome=%s\n", view.Key, view.Status, processView.Classification, processView.DedupeResult, processView.RoutedOutcome)
	return err
}

func runReviewIntake(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	switch command.ReviewAction {
	case "list":
		return runIntakeReviewList(ctx, app, jsonOutput, stdout)
	case "show":
		return runIntakeReviewShow(ctx, app, command, jsonOutput, stdout)
	case "accept", "reject", "clarify", "archive":
		return runIntakeReviewDecision(ctx, app, command, jsonOutput, stdout)
	default:
		return errors.New(commands.IntakeUsage)
	}
}

func runApprovalIntake(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	switch command.ApprovalAction {
	case "list":
		return runIntakeApprovalList(ctx, app, jsonOutput, stdout)
	case "show":
		return runIntakeApprovalShow(ctx, app, command, jsonOutput, stdout)
	case "approve", "deny":
		return runIntakeApprovalDecision(ctx, app, command, jsonOutput, stdout)
	default:
		return errors.New(commands.IntakeUsage)
	}
}

func runIntakeApprovalList(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	items, err := app.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey, Status: "approval_required"})
	if err != nil {
		return err
	}
	views := make([]rawIntakeItemView, 0, len(items))
	for _, item := range items {
		view, err := rawIntakeView(item, false)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, intakeReviewQueueView{IntakeItems: views})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no intake approvals waiting")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(stdout, "approval_intake=%s status=%s policy_reason=%s title=%s\n", view.Key, view.Status, valueOrNone(view.PolicyReason), view.Title); err != nil {
			return err
		}
	}
	return nil
}

func runIntakeApprovalShow(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	view, err := rawIntakeView(item, true)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, rawIntakeItemEnvelope{IntakeItem: view})
	}
	_, err = fmt.Fprintf(stdout, "approval_intake=%s status=%s policy_reason=%s title=%s\n", view.Key, view.Status, valueOrNone(view.PolicyReason), view.Title)
	return err
}

func runIntakeApprovalDecision(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	notes, err := intakeNotesFromItem(item)
	if err != nil {
		return err
	}

	status := item.Status
	summary := item.Summary
	decision := ""
	eventType := runtimeevents.EventIntakeApprovalDenied
	policyDecision := ""
	policyReason := ""
	var task *sqlite.Task
	workCreated := false

	switch command.ApprovalAction {
	case "approve":
		if item.Status == "accepted" && notes.Review != nil && notes.Review.WorkItem != nil {
			existing := sqlite.Task{ID: notes.Review.WorkItem.ID, Key: notes.Review.WorkItem.Key, Status: notes.Review.WorkItem.Status}
			if existing.ID > 0 {
				if loaded, err := app.Store.GetTask(ctx, existing.ID); err == nil {
					existing = loaded
				}
			}
			task = &existing
			decision = "approved"
			eventType = runtimeevents.EventIntakeApprovalApproved
			status = "accepted"
			summary = "Risky intake approval reused existing linked work item"
			policyDecision = "approved"
			policyReason = "operator_approved_risky_intake"
			break
		}
		if item.Status != "approval_required" || notes.Review == nil || !notes.Review.ApprovalRequired {
			return fmt.Errorf("intake %s is not pending approval", rawIntakeKey(item.ID))
		}
		created, createdNow, err := createTaskFromReviewedIntake(ctx, app, item)
		if err != nil {
			return err
		}
		task = &created
		workCreated = createdNow
		decision = "approved"
		eventType = runtimeevents.EventIntakeApprovalApproved
		status = "accepted"
		summary = "Risky intake approved by operator and promoted to real work item"
		policyDecision = "approved"
		policyReason = "operator_approved_risky_intake"
	case "deny":
		if item.Status == "approval_denied" {
			decision = "denied"
			eventType = runtimeevents.EventIntakeApprovalDenied
			status = "approval_denied"
			summary = "Risky intake approval denied; no work item created"
			policyDecision = "denied"
			policyReason = "operator_denied_risky_intake"
			break
		}
		if item.Status != "approval_required" || notes.Review == nil || !notes.Review.ApprovalRequired {
			return fmt.Errorf("intake %s is not pending approval", rawIntakeKey(item.ID))
		}
		decision = "denied"
		eventType = runtimeevents.EventIntakeApprovalDenied
		status = "approval_denied"
		summary = "Risky intake approval denied; no work item created"
		policyDecision = "denied"
		policyReason = "operator_denied_risky_intake"
	default:
		return errors.New(commands.IntakeUsage)
	}

	review := intakeReviewDecision{
		Decision:               decision,
		WorkCreated:            workCreated,
		ApprovalRequired:       false,
		BlockedPendingApproval: false,
		PolicyDecision:         policyDecision,
		PolicyReason:           policyReason,
	}
	var workItemID *int64
	workItemKey := ""
	if task != nil {
		id := task.ID
		workItemID = &id
		workItemKey = task.Key
		review.WorkItem = &intakeReviewWork{ID: task.ID, Key: task.Key, Status: task.Status}
	}
	notes.Review = &review
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return err
	}
	updated, err := app.Store.ReviewIntakeItem(ctx, sqlite.ReviewIntakeItemParams{
		ID:               item.ID,
		Status:           status,
		Summary:          summary,
		RoutingNotes:     string(notesJSON),
		EventType:        eventType,
		Decision:         decision,
		WorkCreated:      workCreated,
		ApprovalRequired: false,
		PolicyDecision:   policyDecision,
		PolicyReason:     policyReason,
		WorkItemID:       workItemID,
		WorkItemKey:      workItemKey,
	})
	if err != nil {
		return err
	}
	view, err := rawIntakeView(updated, true)
	if err != nil {
		return err
	}
	result := intakeReviewDecisionView{
		IntakeItem:             view,
		Decision:               decision,
		WorkCreated:            workCreated,
		ApprovalRequired:       false,
		BlockedPendingApproval: false,
		PolicyDecision:         policyDecision,
		PolicyReason:           policyReason,
	}
	if task != nil {
		result.WorkItem = &reviewWorkItemView{ID: task.ID, Key: task.Key, Status: task.Status}
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, result)
	}
	workKey := "none"
	if task != nil {
		workKey = task.Key
	}
	_, err = fmt.Fprintf(stdout, "approval_intake=%s decision=%s status=%s work_created=%t work_item=%s\n", view.Key, decision, view.Status, workCreated, workKey)
	return err
}

func runIntakeReviewList(ctx context.Context, app bootstrap.App, jsonOutput bool, stdout io.Writer) error {
	items, err := app.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return err
	}
	views := make([]rawIntakeItemView, 0)
	for _, item := range items {
		if !isReviewableIntakeStatus(item.Status) {
			continue
		}
		view, err := rawIntakeView(item, false)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, intakeReviewQueueView{IntakeItems: views})
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "no intake review items")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(stdout, "review_intake=%s status=%s source=%s type=%s dedup_key=%s title=%s\n", view.Key, view.Status, view.Source, view.IntakeType, view.DedupKey, view.Title); err != nil {
			return err
		}
	}
	return nil
}

func runIntakeReviewShow(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	view, err := rawIntakeView(item, true)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, rawIntakeItemEnvelope{IntakeItem: view})
	}
	_, err = fmt.Fprintf(stdout, "review_intake=%s status=%s source=%s type=%s dedup_key=%s title=%s\n", view.Key, view.Status, view.Source, view.IntakeType, view.DedupKey, view.Title)
	return err
}

func runIntakeReviewDecision(ctx context.Context, app bootstrap.App, command commands.IntakeCommand, jsonOutput bool, stdout io.Writer) error {
	item, err := findRawIntakeItem(ctx, app.Store, command.ShowRef)
	if err != nil {
		return err
	}
	notes, err := intakeNotesFromItem(item)
	if err != nil {
		return err
	}

	status := item.Status
	summary := item.Summary
	decision := ""
	eventType := runtimeevents.EventIntakeReviewRejected
	var task *sqlite.Task
	var goal *sqlite.Goal
	workCreated := false
	policyDecision := "direct_work_allowed"
	policyReason := "low_risk_review_acceptance"
	approvalRequired := false

	switch command.ReviewAction {
	case "accept":
		if item.Status == "approval_required" && notes.Review != nil && notes.Review.ApprovalRequired {
			decision = "approval_required"
			eventType = runtimeevents.EventIntakeReviewApprovalRequired
			status = "approval_required"
			summary = "Risk policy requires operator approval before work promotion"
			approvalRequired = true
			policyDecision = notes.Review.PolicyDecision
			if policyDecision == "" {
				policyDecision = "approval_required"
			}
			policyReason = notes.Review.PolicyReason
			if policyReason == "" {
				policyReason = "risky_intake_requires_operator_approval"
			}
			break
		}
		if item.Status == "accepted" && notes.Review != nil && notes.Review.WorkItem != nil {
			existing := sqlite.Task{
				ID:     notes.Review.WorkItem.ID,
				Key:    notes.Review.WorkItem.Key,
				Status: notes.Review.WorkItem.Status,
			}
			if existing.ID > 0 {
				if loaded, err := app.Store.GetTask(ctx, existing.ID); err == nil {
					existing = loaded
				}
			}
			task = &existing
			workCreated = false
			decision = "accepted"
			eventType = runtimeevents.EventIntakeReviewAccepted
			status = "accepted"
			summary = "Draft task accepted by operator and linked to existing work item"
			break
		}
		if item.Status == "duplicate_linked_or_suppressed" {
			decision = "duplicate_acknowledged"
			eventType = runtimeevents.EventIntakeReviewDuplicateAcknowledged
			status = "duplicate_linked_or_suppressed"
			summary = "Duplicate raw intake acknowledged; no duplicate work item created"
			break
		}
		if item.Status != "review_required" || !isAcceptableIntakeDraftArtifact(notes.DraftArtifact) {
			return fmt.Errorf("intake %s cannot be accepted into work from status %s", rawIntakeKey(item.ID), item.Status)
		}
		if isDraftGoalArtifact(notes.DraftArtifact) {
			createdGoal, err := ensureGoalForIntakeGoalReview(ctx, app.Store, item)
			if err != nil {
				return err
			}
			approvedGoal, _, err := approveGoalThroughReview(ctx, app.Store, createdGoal, fmt.Sprintf("intake-goal:%d", item.ID))
			if err != nil {
				return err
			}
			goal = &approvedGoal
			decision = "accepted"
			eventType = runtimeevents.EventIntakeReviewAccepted
			status = "accepted"
			summary = "Draft goal accepted by operator and promoted to goal review"
			policyDecision = "goal_review_accepted"
			policyReason = "operator_accepted_draft_goal"
			break
		}
		policy := intakePromotionPolicy(item)
		if policy.ApprovalRequired {
			decision = "approval_required"
			eventType = runtimeevents.EventIntakeReviewApprovalRequired
			status = "approval_required"
			summary = "Risk policy requires operator approval before work promotion"
			approvalRequired = true
			policyDecision = policy.Decision
			policyReason = policy.Reason
			break
		}
		created, createdNow, err := createTaskFromReviewedIntake(ctx, app, item)
		if err != nil {
			return err
		}
		task = &created
		workCreated = createdNow
		decision = "accepted"
		eventType = runtimeevents.EventIntakeReviewAccepted
		status = "accepted"
		summary = "Draft task accepted by operator and promoted to real work item"
	case "reject":
		decision = "rejected"
		eventType = runtimeevents.EventIntakeReviewRejected
		status = "rejected"
		summary = "Intake review rejected by operator; no work item created"
	case "clarify":
		decision = "clarification_requested"
		eventType = runtimeevents.EventIntakeReviewClarificationRequested
		status = "needs_clarification"
		summary = "Operator requested clarification before work promotion"
		notes.Clarification = &intakeClarification{
			State: "needs_clarification",
			Prompts: []string{
				"What exact outcome should Odin prepare?",
				"Which acceptance criteria make this ready for work?",
			},
		}
	case "archive":
		decision = "archived"
		eventType = runtimeevents.EventIntakeReviewArchived
		status = "archived"
		summary = "Intake archived by operator; no work item created"
	default:
		return errors.New(commands.IntakeUsage)
	}

	review := intakeReviewDecision{
		Decision:               decision,
		WorkCreated:            workCreated,
		ApprovalRequired:       approvalRequired,
		BlockedPendingApproval: approvalRequired,
		PolicyDecision:         policyDecision,
		PolicyReason:           policyReason,
	}
	var workItemID *int64
	workItemKey := ""
	if task != nil {
		id := task.ID
		workItemID = &id
		workItemKey = task.Key
		review.WorkItem = &intakeReviewWork{ID: task.ID, Key: task.Key, Status: task.Status}
	}
	var goalID *int64
	goalStatus := ""
	if goal != nil {
		id := goal.ID
		goalID = &id
		goalStatus = string(goal.Status)
		review.Goal = &intakeReviewGoal{ID: goal.ID, Status: string(goal.Status)}
	}
	notes.Review = &review
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return err
	}
	updated, err := app.Store.ReviewIntakeItem(ctx, sqlite.ReviewIntakeItemParams{
		ID:               item.ID,
		Status:           status,
		Summary:          summary,
		RoutingNotes:     string(notesJSON),
		EventType:        eventType,
		Decision:         decision,
		WorkCreated:      workCreated,
		ApprovalRequired: approvalRequired,
		PolicyDecision:   policyDecision,
		PolicyReason:     policyReason,
		WorkItemID:       workItemID,
		WorkItemKey:      workItemKey,
		GoalID:           goalID,
		GoalStatus:       goalStatus,
	})
	if err != nil {
		return err
	}
	view, err := rawIntakeView(updated, true)
	if err != nil {
		return err
	}
	result := intakeReviewDecisionView{
		IntakeItem:             view,
		Decision:               decision,
		WorkCreated:            workCreated,
		ApprovalRequired:       approvalRequired,
		BlockedPendingApproval: approvalRequired,
		PolicyDecision:         policyDecision,
		PolicyReason:           policyReason,
	}
	if task != nil {
		result.WorkItem = &reviewWorkItemView{ID: task.ID, Key: task.Key, Status: task.Status}
	}
	if goal != nil {
		result.GoalID = goal.ID
		result.GoalStatus = string(goal.Status)
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, result)
	}
	workKey := "none"
	if task != nil {
		workKey = task.Key
	}
	_, err = fmt.Fprintf(stdout, "review_intake=%s decision=%s status=%s work_created=%t work_item=%s\n", view.Key, decision, view.Status, workCreated, workKey)
	return err
}

func isDraftGoalArtifact(artifact *intakeDraftArtifact) bool {
	return artifact != nil && strings.TrimSpace(artifact.Kind) == "draft_goal"
}

func createTaskFromReviewedIntake(ctx context.Context, app bootstrap.App, item sqlite.IntakeItem) (sqlite.Task, bool, error) {
	if item.Scope != "project" || strings.TrimSpace(item.ScopeKey) == "" {
		return sqlite.Task{}, false, fmt.Errorf("intake %s has no project scope for work promotion", rawIntakeKey(item.ID))
	}
	manifest, ok := app.Registry.Lookup(item.ScopeKey)
	if !ok {
		return sqlite.Task{}, false, fmt.Errorf("unknown project %q", item.ScopeKey)
	}
	resolved := scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})
	intent := intakeExecutionIntentForTask(item)
	workKind := ""
	artifactsJSON := ""
	if notes, err := intakeNotesFromItem(item); err == nil && notes.Routing.SkillInvocation != nil {
		binding := *notes.Routing.SkillInvocation
		binding.ReviewState = "accepted"
		if encoded, err := skillbinding.EncodeArtifacts(binding); err != nil {
			return sqlite.Task{}, false, err
		} else {
			workKind = skillbinding.WorkKind
			artifactsJSON = encoded
			intent.ExecutionIntent = binding.ExecutionIntent
			intent.ExecutionIntentSource = binding.ExecutionIntentSource
		}
	}
	result, err := jobs.Service{
		Store:       app.Store,
		Registry:    app.Registry,
		Transitions: projects.Service{Store: app.Store},
		Now:         time.Now,
	}.CreateTaskOnce(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 item.Subject,
		RequestedBy:           "intake_review:" + rawIntakeKey(item.ID),
		Key:                   reviewedIntakeWorkItemKey(item.ID),
		WorkKind:              workKind,
		ArtifactsJSON:         artifactsJSON,
		ExecutionIntent:       intent.ExecutionIntent,
		ExecutionIntentSource: intent.ExecutionIntentSource,
	})
	return result.Task, result.Created, err
}

func reviewedIntakeWorkItemKey(id int64) string {
	return fmt.Sprintf("intake-review-%d", id)
}

type intakePromotionPolicyDecision struct {
	ApprovalRequired bool
	Decision         string
	Reason           string
}

type intakeAutoPromotionResult struct {
	WorkCreated    bool
	PolicyReason   string
	PolicyDecision string
	WorkItem       *reviewWorkItemView
}

func intakePromotionPolicy(item sqlite.IntakeItem) intakePromotionPolicyDecision {
	intent := intakeExecutionIntentForTask(item)
	switch intent.ExecutionIntent {
	case "governance", "destructive":
		return intakePromotionPolicyDecision{
			ApprovalRequired: true,
			Decision:         "approval_required",
			Reason:           "intake_intent_requires_operator_approval",
		}
	}

	text := strings.ToLower(strings.Join([]string{item.Subject, item.Summary, item.SourceFactsJSON}, " "))
	for _, marker := range []string{"delete", "production", "prod", "credential", "secret", "payment", "deploy"} {
		if strings.Contains(text, marker) {
			return intakePromotionPolicyDecision{
				ApprovalRequired: true,
				Decision:         "approval_required",
				Reason:           "risky_intake_requires_operator_approval",
			}
		}
	}
	return intakePromotionPolicyDecision{
		ApprovalRequired: false,
		Decision:         "direct_work_allowed",
		Reason:           "low_risk_review_acceptance",
	}
}

func autoPromoteProcessedIntake(ctx context.Context, app bootstrap.App, item sqlite.IntakeItem) (sqlite.IntakeItem, *intakeAutoPromotionResult, error) {
	notes, err := intakeNotesFromItem(item)
	if err != nil {
		return item, nil, err
	}
	if !shouldAutoPromoteProcessedIntake(item, notes) {
		return item, nil, nil
	}
	policy := intakePromotionPolicy(item)
	if policy.ApprovalRequired {
		return item, nil, nil
	}
	created, createdNow, err := createTaskFromReviewedIntake(ctx, app, item)
	if err != nil {
		return item, nil, err
	}
	policyDecision := "direct_work_allowed"
	policyReason := "low_risk_autonomous_intake"
	review := intakeReviewDecision{
		Decision:       "auto_accepted",
		WorkCreated:    createdNow,
		PolicyDecision: policyDecision,
		PolicyReason:   policyReason,
		WorkItem:       &intakeReviewWork{ID: created.ID, Key: created.Key, Status: created.Status},
	}
	notes.Review = &review
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return item, nil, err
	}
	workItemID := created.ID
	updated, err := app.Store.ReviewIntakeItem(ctx, sqlite.ReviewIntakeItemParams{
		ID:             item.ID,
		Status:         "accepted",
		Summary:        "Low-risk intake auto-promoted to real work item",
		RoutingNotes:   string(notesJSON),
		EventType:      runtimeevents.EventIntakeReviewAccepted,
		Decision:       "auto_accepted",
		WorkCreated:    createdNow,
		PolicyDecision: policyDecision,
		PolicyReason:   policyReason,
		WorkItemID:     &workItemID,
		WorkItemKey:    created.Key,
	})
	if err != nil {
		return item, nil, err
	}
	return updated, &intakeAutoPromotionResult{
		WorkCreated:    createdNow,
		PolicyReason:   policyReason,
		PolicyDecision: policyDecision,
		WorkItem:       &reviewWorkItemView{ID: created.ID, Key: created.Key, Status: created.Status},
	}, nil
}

func shouldAutoPromoteProcessedIntake(item sqlite.IntakeItem, notes intakeProcessingNotes) bool {
	if item.Status != "review_required" || item.Scope != "project" || strings.TrimSpace(item.ScopeKey) == "" {
		return false
	}
	if notes.Routing.SkillInvocation != nil {
		return false
	}
	if !isAcceptableIntakeDraftArtifact(notes.DraftArtifact) || isDraftGoalArtifact(notes.DraftArtifact) {
		return false
	}
	if strings.TrimSpace(notes.Routing.ExecutionIntent) != "read_only" {
		return false
	}
	switch strings.TrimSpace(notes.Routing.Outcome) {
	case "draft_task", "draft_idea", "draft_research", "draft_incident_review", "draft_routine", "draft_follow_up":
		return true
	default:
		return false
	}
}

type intakeDerivedRoute struct {
	RoutingOutcome        string
	DraftArtifactKind     string
	ExecutionIntent       string
	ExecutionIntentSource string
}

func intakeExecutionIntentForTask(item sqlite.IntakeItem) intakeDerivedRoute {
	notes, err := intakeNotesFromItem(item)
	if err == nil {
		if intent := strings.TrimSpace(notes.Routing.ExecutionIntent); intent != "" {
			source := strings.TrimSpace(notes.Routing.ExecutionIntentSource)
			if source == "" {
				source = "intake_type:" + normalizedIntakeType(item.EventKind)
			}
			return intakeDerivedRoute{
				ExecutionIntent:       intent,
				ExecutionIntentSource: source,
			}
		}
		if notes.DraftArtifact != nil {
			if intent := strings.TrimSpace(notes.DraftArtifact.ExecutionIntent); intent != "" {
				source := strings.TrimSpace(notes.DraftArtifact.ExecutionIntentSource)
				if source == "" {
					source = "intake_type:" + normalizedIntakeType(item.EventKind)
				}
				return intakeDerivedRoute{
					ExecutionIntent:       intent,
					ExecutionIntentSource: source,
				}
			}
		}
	}
	route := deriveIntakeRoute(item, classifyIntakeItem(item))
	return intakeDerivedRoute{
		ExecutionIntent:       route.ExecutionIntent,
		ExecutionIntentSource: route.ExecutionIntentSource,
	}
}

func isAcceptableIntakeDraftArtifact(artifact *intakeDraftArtifact) bool {
	if artifact == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(artifact.Kind), "draft_")
}

func intakeNotesFromItem(item sqlite.IntakeItem) (intakeProcessingNotes, error) {
	var notes intakeProcessingNotes
	if strings.TrimSpace(item.RoutingNotes) == "" {
		return notes, nil
	}
	if err := json.Unmarshal([]byte(item.RoutingNotes), &notes); err != nil {
		return intakeProcessingNotes{}, fmt.Errorf("intake routing notes: %w", err)
	}
	return notes, nil
}

func isReviewableIntakeStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "review_required", "needs_clarification", "duplicate_linked_or_suppressed", "approval_required":
		return true
	default:
		return false
	}
}

type intakeProcessOutcome struct {
	status                string
	summary               string
	canonicalIntakeItemID *int64
	goalID                *int64
	createGoal            bool
	suppressionReason     string
	notes                 intakeProcessingNotes
	events                []sqlite.IntakeItemProcessingEvent
}

func buildIntakeProcessOutcome(ctx context.Context, store *sqlite.Store, item sqlite.IntakeItem) (intakeProcessOutcome, error) {
	notes := intakeProcessingNotes{ProcessingStarted: true}
	notes.Classification = classifyIntakeItem(item)

	duplicate, err := findCanonicalDuplicate(ctx, store, item)
	if err != nil {
		return intakeProcessOutcome{}, err
	}
	notes.Dedupe = intakeDedupeReview{Result: "unique"}
	if duplicate != nil {
		notes.Dedupe = intakeDedupeReview{
			Result:             duplicate.Result,
			Basis:              duplicate.Basis,
			CanonicalIntakeKey: rawIntakeKey(duplicate.CanonicalIntakeItemID),
			MatchReason:        duplicate.Basis,
		}
		notes.Routing = intakeRoutingResult{Outcome: "duplicate_linked_or_suppressed", ProjectKey: item.ScopeKey}
		populateIntakeClassificationRoute(&notes, item)
		notes.DraftArtifact = &intakeDraftArtifact{
			Kind:        "duplicate_review",
			Title:       item.Subject,
			ReviewState: "duplicate_linked_or_suppressed",
		}
		outcome := intakeProcessOutcome{
			status:                "duplicate_linked_or_suppressed",
			summary:               "Duplicate raw intake linked to " + rawIntakeKey(duplicate.CanonicalIntakeItemID),
			canonicalIntakeItemID: &duplicate.CanonicalIntakeItemID,
			suppressionReason:     duplicate.SuppressionReason,
			notes:                 notes,
		}
		return outcome, nil
	}

	if notes.Classification.Result == "ambiguous" {
		notes.Routing = intakeRoutingResult{Outcome: "needs_clarification", ProjectKey: item.ScopeKey}
		populateIntakeClassificationRoute(&notes, item)
		notes.DraftArtifact = &intakeDraftArtifact{
			Kind:        "clarification_request",
			Title:       item.Subject,
			ReviewState: "needs_clarification",
		}
		notes.Clarification = &intakeClarification{
			State: "needs_clarification",
			Prompts: []string{
				"What outcome should Odin prepare for review?",
				"Which project or operator surface owns this intake?",
			},
		}
		outcome := intakeProcessOutcome{
			status:  "needs_clarification",
			summary: "Raw intake needs operator clarification before drafting work",
			notes:   notes,
		}
		return outcome, nil
	}

	if notes.Classification.Category == "project" || isGoalLikeIntake(item) {
		notes.Routing = intakeRoutingResult{
			Outcome:               "draft_goal",
			ProjectKey:            item.ScopeKey,
			ExecutionIntent:       "governance",
			ExecutionIntentSource: "intake_goal_rule:v1",
		}
		populateIntakeClassificationRoute(&notes, item)
		notes.DraftArtifact = &intakeDraftArtifact{
			Kind:                  "draft_goal",
			Title:                 item.Subject,
			ReviewState:           "review_required",
			ExecutionIntent:       notes.Routing.ExecutionIntent,
			ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
		}
		outcome := intakeProcessOutcome{
			status:  "review_required",
			summary: "draft_goal prepared for human review; no goal created",
			goalID:  item.GoalID,
			notes:   notes,
		}
		return outcome, nil
	}

	route := deriveIntakeRoute(item, notes.Classification)
	notes.Routing = intakeRoutingResult{
		Outcome:               route.RoutingOutcome,
		ProjectKey:            item.ScopeKey,
		ExecutionIntent:       route.ExecutionIntent,
		ExecutionIntentSource: route.ExecutionIntentSource,
	}
	if binding, ok, err := skillInvocationBindingFromIntake(item, route); err != nil {
		return intakeProcessOutcome{}, err
	} else if ok {
		notes.Routing.SkillInvocation = &binding
		notes.Routing.ExecutionIntent = binding.ExecutionIntent
		notes.Routing.ExecutionIntentSource = binding.ExecutionIntentSource
	}
	populateIntakeClassificationRoute(&notes, item)
	notes.DraftArtifact = &intakeDraftArtifact{
		Kind:                  route.DraftArtifactKind,
		Title:                 item.Subject,
		ReviewState:           "review_required",
		ExecutionIntent:       notes.Routing.ExecutionIntent,
		ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
	}
	outcome := intakeProcessOutcome{
		status:  "review_required",
		summary: route.DraftArtifactKind + " prepared for human review; no work item created",
		notes:   notes,
	}
	return outcome, nil
}

func skillInvocationBindingFromIntake(item sqlite.IntakeItem, route intakeDerivedRoute) (skillbinding.Binding, bool, error) {
	intakeType := normalizedIntakeType(item.EventKind)
	text := strings.ToLower(strings.Join([]string{item.Subject, item.Summary, item.SourceFactsJSON}, " "))
	if intakeType != "skill" && !(strings.Contains(text, "skill system") && strings.Contains(text, "triage")) {
		return skillbinding.Binding{}, false, nil
	}
	input, err := json.Marshal(map[string]any{
		"message": item.Subject,
		"scope":   item.Scope,
	})
	if err != nil {
		return skillbinding.Binding{}, false, err
	}
	binding := skillbinding.Binding{
		SkillKey:              "triage-skill",
		InputJSON:             input,
		SourceType:            "intake",
		SourceID:              strconv.FormatInt(item.ID, 10),
		SourceKey:             rawIntakeKey(item.ID),
		Scope:                 item.Scope,
		ProjectKey:            item.ScopeKey,
		ExecutionIntent:       defaultIntakeBindingString(route.ExecutionIntent, "read_only"),
		ExecutionIntentSource: "skill_binding:intake",
		ReviewState:           "review_required",
	}
	normalized, err := skillbinding.Normalize(binding)
	return normalized, true, err
}

func defaultIntakeBindingString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func isGoalLikeIntake(item sqlite.IntakeItem) bool {
	text := strings.ToLower(strings.TrimSpace(item.Subject + " " + item.Summary))
	for _, marker := range []string{"goal", "research goals", "long-running", "long running", "initiative", "roadmap", "multi-step", "project plan"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return strings.Contains(text, "plan the ") && strings.Contains(text, " project")
}

func deriveIntakeRoute(item sqlite.IntakeItem, classification intakeClassification) intakeDerivedRoute {
	intakeType := normalizedIntakeType(item.EventKind)
	source := "intake_type:" + intakeType
	switch intakeType {
	case "research":
		return intakeDerivedRoute{RoutingOutcome: "draft_research", DraftArtifactKind: "draft_research", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "writing":
		return intakeDerivedRoute{RoutingOutcome: "draft_document", DraftArtifactKind: "draft_document", ExecutionIntent: "mutation", ExecutionIntentSource: source}
	case "admin":
		return intakeDerivedRoute{RoutingOutcome: "draft_admin_task", DraftArtifactKind: "draft_admin_task", ExecutionIntent: "mutation", ExecutionIntentSource: source}
	case "bug", "incident":
		return intakeDerivedRoute{RoutingOutcome: "draft_incident_review", DraftArtifactKind: "draft_incident_review", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "governance":
		return intakeDerivedRoute{RoutingOutcome: "draft_policy_change", DraftArtifactKind: "draft_policy_change", ExecutionIntent: "governance", ExecutionIntentSource: source}
	case "destructive":
		return intakeDerivedRoute{RoutingOutcome: "draft_destructive_action", DraftArtifactKind: "draft_destructive_action", ExecutionIntent: "destructive", ExecutionIntentSource: source}
	}

	if classification.Category != "" && (intakeType == "request" || intakeType == "prompt") {
		source = "classification_category:" + classification.Category
	}
	switch classification.Category {
	case "idea":
		return intakeDerivedRoute{RoutingOutcome: "draft_idea", DraftArtifactKind: "draft_idea", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "bug":
		return intakeDerivedRoute{RoutingOutcome: "draft_incident_review", DraftArtifactKind: "draft_incident_review", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "research_item":
		return intakeDerivedRoute{RoutingOutcome: "draft_research", DraftArtifactKind: "draft_research", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "writing_request":
		return intakeDerivedRoute{RoutingOutcome: "draft_document", DraftArtifactKind: "draft_document", ExecutionIntent: "mutation", ExecutionIntentSource: source}
	case "admin_item":
		return intakeDerivedRoute{RoutingOutcome: "draft_admin_task", DraftArtifactKind: "draft_admin_task", ExecutionIntent: "mutation", ExecutionIntentSource: source}
	case "routine":
		return intakeDerivedRoute{RoutingOutcome: "draft_routine", DraftArtifactKind: "draft_routine", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "waiting_for_item":
		return intakeDerivedRoute{RoutingOutcome: "draft_follow_up", DraftArtifactKind: "draft_follow_up", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	case "archive_worthy_noise":
		return intakeDerivedRoute{RoutingOutcome: "archive_candidate", DraftArtifactKind: "archive_candidate", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	default:
		return intakeDerivedRoute{RoutingOutcome: "draft_task", DraftArtifactKind: "draft_task", ExecutionIntent: "read_only", ExecutionIntentSource: source}
	}
}

func normalizedIntakeType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "bug/incident", "bug_incident", "incident":
		return "incident"
	case "":
		return "request"
	default:
		return normalized
	}
}

func classifyIntakeItem(item sqlite.IntakeItem) intakeClassification {
	title := strings.ToLower(strings.TrimSpace(item.Subject))
	if title == "" || title == "help" || title == "help with this" || title == "fix this" || len(strings.Fields(title)) < 3 {
		return intakeClassification{
			Result:     "ambiguous",
			Reason:     "intake title is too vague to draft reviewable work",
			SourceType: normalizedIntakeSourceType(item.SourceFamily),
			Confidence: "low",
			Category:   "clarification_needed",
		}
	}
	category, priority := classifyIntakeCategory(item)
	return intakeClassification{
		Result:        "actionable_request",
		Reason:        "intake has enough subject detail for a draft review artifact",
		SourceType:    normalizedIntakeSourceType(item.SourceFamily),
		Confidence:    "high",
		Category:      category,
		PriorityScore: priority,
	}
}

func classifyIntakeCategory(item sqlite.IntakeItem) (string, int) {
	intakeType := normalizedIntakeType(item.EventKind)
	switch intakeType {
	case "research":
		return "research_item", 40
	case "writing":
		return "writing_request", 50
	case "admin":
		return "admin_item", 45
	case "bug", "incident":
		return "bug", 80
	}

	text := strings.ToLower(strings.TrimSpace(item.Subject + " " + item.Summary))
	switch {
	case hasAnyPhrase(text, "no action needed", "fyi", "newsletter", "unsubscribe", "spam"):
		return "archive_worthy_noise", 10
	case hasAnyPhrase(text, "waiting for", "wait for", "follow up", "follow-up"):
		return "waiting_for_item", 35
	case hasAnyPhrase(text, "remind me", "every day", "every week", "every friday", "every monday", "recurring"):
		return "routine", 35
	case hasAnyPhrase(text, "bug", "error", "failed", "failure", "incident", "broken", "fix "):
		return "bug", 80
	case hasAnyPhrase(text, "idea", "someday", "maybe"):
		return "idea", 30
	case isGoalLikeIntake(item):
		return "project", 70
	case hasAnyPhrase(text, "research", "investigate", "analyze", "compare", "options"):
		return "research_item", 40
	case hasAnyPhrase(text, "write", "draft", "memo", "document"):
		return "writing_request", 50
	case hasAnyPhrase(text, "organize", "book", "file", "labels", "admin"):
		return "admin_item", 45
	default:
		return "task", 50
	}
}

func hasAnyPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func normalizedIntakeSourceType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func populateIntakeClassificationRoute(notes *intakeProcessingNotes, item sqlite.IntakeItem) {
	if notes.Classification.SourceType == "" {
		notes.Classification.SourceType = normalizedIntakeSourceType(item.SourceFamily)
	}
	notes.Classification.SuggestedRoute = notes.Routing.Outcome
	notes.Classification.Intent = notes.Routing.ExecutionIntent
	if notes.Classification.Intent == "" {
		switch notes.Routing.Outcome {
		case "needs_clarification":
			notes.Classification.Intent = "clarification"
		case "duplicate_linked_or_suppressed":
			notes.Classification.Intent = "suppress_duplicate"
		default:
			notes.Classification.Intent = "review"
		}
	}
	notes.Classification.Risk = intakeClassificationRisk(notes.Routing.ExecutionIntent, notes.Routing.Outcome)
}

func intakeClassificationRisk(intent string, outcome string) string {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "destructive":
		return "destructive"
	case "governance":
		return "governance"
	case "mutation":
		return "medium"
	case "read_only":
		return "low"
	}
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "needs_clarification":
		return "unknown"
	case "duplicate_linked_or_suppressed":
		return "low"
	default:
		return "low"
	}
}

type intakeDuplicateMatch struct {
	CanonicalIntakeItemID int64
	Result                string
	Basis                 string
	SuppressionReason     string
}

func findCanonicalDuplicate(ctx context.Context, store *sqlite.Store, item sqlite.IntakeItem) (*intakeDuplicateMatch, error) {
	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: item.WorkspaceID})
	if err != nil {
		return nil, err
	}
	for _, candidate := range items {
		if candidate.ID >= item.ID {
			continue
		}
		if strings.TrimSpace(item.DedupeKey) != "" && candidate.DedupeKey == item.DedupeKey {
			return &intakeDuplicateMatch{
				CanonicalIntakeItemID: candidate.ID,
				Result:                "duplicate_linked",
				Basis:                 "dedupe_key",
				SuppressionReason:     "duplicate_dedupe_key",
			}, nil
		}
	}

	subjectKey := normalizedIntakeSubjectDedupeKey(item)
	if subjectKey == "" {
		return nil, nil
	}
	for _, candidate := range items {
		if candidate.ID >= item.ID {
			continue
		}
		if !sameIntakeRoutingScope(candidate, item) {
			continue
		}
		if normalizedIntakeSubjectDedupeKey(candidate) == subjectKey {
			return &intakeDuplicateMatch{
				CanonicalIntakeItemID: candidate.ID,
				Result:                "semantic_duplicate_linked",
				Basis:                 "normalized_subject",
				SuppressionReason:     "near_duplicate_subject",
			}, nil
		}
	}
	return nil, nil
}

func sameIntakeRoutingScope(left sqlite.IntakeItem, right sqlite.IntakeItem) bool {
	return left.WorkspaceID == right.WorkspaceID &&
		left.Scope == right.Scope &&
		left.ScopeKey == right.ScopeKey &&
		normalizedIntakeType(left.EventKind) == normalizedIntakeType(right.EventKind)
}

func normalizedIntakeSubjectDedupeKey(item sqlite.IntakeItem) string {
	if classifyIntakeItem(item).Result == "ambiguous" {
		return ""
	}
	var builder strings.Builder
	lastWasSpace := true
	for _, r := range strings.ToLower(item.Subject) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastWasSpace = false
			continue
		}
		if !lastWasSpace {
			builder.WriteByte(' ')
			lastWasSpace = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func intakeProcessingEvents(itemID int64, status string, notes intakeProcessingNotes, canonical *int64) []sqlite.IntakeItemProcessingEvent {
	events := []sqlite.IntakeItemProcessingEvent{
		{
			Type:    runtimeevents.EventIntakeProcessingStarted,
			Stage:   "processing_started",
			Result:  "started",
			Payload: intakeProcessingAuditPayload(itemID, status, "processing_started", "started", notes, canonical),
		},
		{
			Type:    runtimeevents.EventIntakeClassified,
			Stage:   "classification",
			Result:  notes.Classification.Result,
			Payload: intakeProcessingAuditPayload(itemID, status, "classification", notes.Classification.Result, notes, canonical),
		},
		{
			Type:    runtimeevents.EventIntakeDedupeReviewed,
			Stage:   "dedupe",
			Result:  notes.Dedupe.Result,
			Payload: intakeProcessingAuditPayload(itemID, status, "dedupe", notes.Dedupe.Result, notes, canonical),
		},
		{
			Type:    runtimeevents.EventIntakeRouted,
			Stage:   "routing",
			Result:  notes.Routing.Outcome,
			Payload: intakeProcessingAuditPayload(itemID, status, "routing", notes.Routing.Outcome, notes, canonical),
		},
		{
			Type:    runtimeevents.EventIntakeProcessed,
			Stage:   "processed",
			Result:  status,
			Payload: intakeProcessingAuditPayload(itemID, status, "processed", status, notes, canonical),
		},
	}
	switch {
	case notes.Goal != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:    runtimeevents.EventIntakeRoutedToGoal,
			Stage:   "goal",
			Result:  "goal_created",
			Payload: intakeProcessingAuditPayload(itemID, status, "goal", "goal_created", notes, canonical),
		})
	case notes.Clarification != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:    runtimeevents.EventIntakeClarificationNeeded,
			Stage:   "clarification",
			Result:  notes.Clarification.State,
			Payload: intakeProcessingAuditPayload(itemID, status, "clarification", notes.Clarification.State, notes, canonical),
		})
	case canonical != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:    runtimeevents.EventIntakeDuplicateLinkedOrSuppressed,
			Stage:   "duplicate",
			Result:  notes.Dedupe.Result,
			Payload: intakeProcessingAuditPayload(itemID, status, "duplicate", notes.Dedupe.Result, notes, canonical),
		})
	case notes.DraftArtifact != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:    runtimeevents.EventIntakeDraftArtifactCreated,
			Stage:   "draft_artifact",
			Result:  notes.DraftArtifact.Kind,
			Payload: intakeProcessingAuditPayload(itemID, status, "draft_artifact", notes.DraftArtifact.Kind, notes, canonical),
		})
	}
	return events
}

func intakeGoalIDPtr(notes intakeProcessingNotes) *int64 {
	if notes.Goal == nil {
		return nil
	}
	id := notes.Goal.ID
	return &id
}

func intakeProcessingAuditPayload(itemID int64, status string, stage string, result string, notes intakeProcessingNotes, canonical *int64) runtimeevents.IntakeProcessingPayload {
	payload := runtimeevents.IntakeProcessingPayload{
		IntakeItemID:          itemID,
		Status:                status,
		Stage:                 stage,
		Result:                result,
		RoutedOutcome:         notes.Routing.Outcome,
		ExecutionIntent:       notes.Routing.ExecutionIntent,
		ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
		CanonicalIntakeID:     canonical,
		GoalID:                intakeGoalIDPtr(notes),
		Classification: &runtimeevents.IntakeClassification{
			Result:         notes.Classification.Result,
			Reason:         notes.Classification.Reason,
			SourceType:     notes.Classification.SourceType,
			Intent:         notes.Classification.Intent,
			Risk:           notes.Classification.Risk,
			Confidence:     notes.Classification.Confidence,
			SuggestedRoute: notes.Classification.SuggestedRoute,
		},
		Dedupe: &runtimeevents.IntakeDedupeReview{
			Result:             notes.Dedupe.Result,
			Basis:              notes.Dedupe.Basis,
			CanonicalIntakeKey: notes.Dedupe.CanonicalIntakeKey,
		},
		Routing: &runtimeevents.IntakeRoutingResult{
			Outcome:               notes.Routing.Outcome,
			ProjectKey:            notes.Routing.ProjectKey,
			ExecutionIntent:       notes.Routing.ExecutionIntent,
			ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
			GoalID:                intakeGoalIDPtr(notes),
		},
	}
	if notes.DraftArtifact != nil {
		payload.DraftArtifactKind = notes.DraftArtifact.Kind
		payload.DraftArtifact = &runtimeevents.IntakeDraftArtifact{
			Kind:                  notes.DraftArtifact.Kind,
			Title:                 notes.DraftArtifact.Title,
			ReviewState:           notes.DraftArtifact.ReviewState,
			ExecutionIntent:       notes.DraftArtifact.ExecutionIntent,
			ExecutionIntentSource: notes.DraftArtifact.ExecutionIntentSource,
		}
	}
	if notes.Clarification != nil {
		payload.ClarificationState = notes.Clarification.State
		payload.Clarification = &runtimeevents.IntakeClarification{
			State:   notes.Clarification.State,
			Prompts: notes.Clarification.Prompts,
		}
	}
	return payload
}

func rawIntakeSourceFactsJSON(command commands.IntakeCommand, payloadJSON string) (string, error) {
	var payload json.RawMessage
	if strings.TrimSpace(payloadJSON) == "" {
		payloadJSON = "{}"
	}
	if command.RawText != "" && payloadJSON == "{}" {
		rawTextPayload, err := json.Marshal(map[string]string{"text": command.RawText})
		if err != nil {
			return "", err
		}
		payloadJSON = string(rawTextPayload)
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", fmt.Errorf("raw intake payload json: %w", err)
	}
	facts := map[string]any{
		"source":         command.Source,
		"intake_type":    command.Type,
		"dedup_key":      command.DedupKey,
		"requested_by":   command.RequestedBy,
		"payload_policy": rawIntakePayloadPolicy,
		"payload":        payload,
	}
	if command.ProjectKey != "" {
		facts["project_key"] = command.ProjectKey
	}
	if command.ActionKey != "" {
		facts["external_object_id"] = command.ActionKey
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func rawIntakeView(item sqlite.IntakeItem, includePayload bool) (rawIntakeItemView, error) {
	var facts map[string]json.RawMessage
	if err := json.Unmarshal([]byte(item.SourceFactsJSON), &facts); err != nil {
		return rawIntakeItemView{}, fmt.Errorf("raw intake source facts json: %w", err)
	}
	view := rawIntakeItemView{
		ID:            item.ID,
		Key:           rawIntakeKey(item.ID),
		Status:        item.Status,
		Source:        item.SourceFamily,
		IntakeType:    item.EventKind,
		DedupKey:      item.DedupeKey,
		ReceivedAt:    item.ReceivedAt.UTC().Format(time.RFC3339Nano),
		CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:     item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		PayloadPolicy: rawIntakePayloadPolicy,
		Title:         item.Subject,
		Summary:       item.Summary,
	}
	if item.GoalID != nil {
		view.GoalID = *item.GoalID
	}
	if item.Scope == "project" {
		view.ProjectKey = item.ScopeKey
	}
	if item.CanonicalIntakeItemID != nil {
		view.CanonicalIntakeKey = rawIntakeKey(*item.CanonicalIntakeItemID)
	}
	view.SuppressionReason = item.SuppressionReason
	_, payloadAvailable := facts["payload"]
	view.Evidence = &rawIntakeEvidenceView{
		PayloadPolicy:        rawIntakePayloadPolicy,
		SourceFactsAvailable: strings.TrimSpace(item.SourceFactsJSON) != "",
		PayloadAvailable:     payloadAvailable,
		PayloadIncluded:      includePayload && payloadAvailable,
	}
	if strings.TrimSpace(item.RoutingNotes) != "" && json.Valid([]byte(item.RoutingNotes)) {
		view.Processing = json.RawMessage(item.RoutingNotes)
		var notes intakeProcessingNotes
		if err := json.Unmarshal([]byte(item.RoutingNotes), &notes); err == nil {
			view.Classification = notes.Classification.Result
			view.DedupeResult = notes.Dedupe.Result
			view.DedupeBasis = notes.Dedupe.Basis
			view.Risk = notes.Classification.Risk
			view.SuggestedRoute = notes.Classification.SuggestedRoute
			if view.SuggestedRoute == "" {
				view.SuggestedRoute = notes.Routing.Outcome
			}
			if view.GoalID == 0 && notes.Goal != nil {
				view.GoalID = notes.Goal.ID
			}
			if view.GoalID == 0 && notes.Review != nil && notes.Review.Goal != nil {
				view.GoalID = notes.Review.Goal.ID
			}
			if notes.Review != nil && notes.Review.WorkItem != nil {
				view.AcceptedWorkItemID = notes.Review.WorkItem.ID
				view.AcceptedWorkItemKey = notes.Review.WorkItem.Key
				view.AcceptedWorkItemStatus = notes.Review.WorkItem.Status
			}
			if notes.Review != nil {
				view.ApprovalRequired = notes.Review.ApprovalRequired
				view.BlockedPendingApproval = notes.Review.BlockedPendingApproval
				view.PolicyReason = notes.Review.PolicyReason
				view.PolicyDecision = notes.Review.PolicyDecision
			}
		}
	}
	view.RequestedBy = rawStringFact(facts, "requested_by")
	if view.RequestedBy == "" {
		view.RequestedBy = item.SourceFamily
	}
	if policy := rawStringFact(facts, "payload_policy"); policy != "" {
		view.PayloadPolicy = policy
	}
	if includePayload {
		if payload, ok := facts["payload"]; ok {
			view.Payload = payload
		}
	}
	return view, nil
}

func rawStringFact(facts map[string]json.RawMessage, key string) string {
	raw, ok := facts[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}

func findRawIntakeItem(ctx context.Context, store *sqlite.Store, ref string) (sqlite.IntakeItem, error) {
	ref = strings.TrimSpace(ref)
	idRef := strings.TrimPrefix(ref, "intake-")
	if id, err := strconv.ParseInt(idRef, 10, 64); err == nil && id > 0 {
		items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
		if err != nil {
			return sqlite.IntakeItem{}, err
		}
		for _, item := range items {
			if item.ID == id {
				return item, nil
			}
		}
		return sqlite.IntakeItem{}, fmt.Errorf("raw intake item %q not found", ref)
	}
	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return sqlite.IntakeItem{}, err
	}
	var matches []sqlite.IntakeItem
	for _, item := range items {
		if item.DedupeKey == ref {
			matches = append(matches, item)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return sqlite.IntakeItem{}, fmt.Errorf("raw intake key %q matched %d items; use intake-<id>", ref, len(matches))
	}
	return sqlite.IntakeItem{}, fmt.Errorf("raw intake item %q not found", ref)
}

func rawIntakeKey(id int64) string {
	return fmt.Sprintf("intake-%d", id)
}

func loadIntakePayloadJSON(payloadFile string, stdin io.Reader) (string, error) {
	if payloadFile == "" {
		return "{}", nil
	}

	var content []byte
	var err error
	if payloadFile == "-" {
		content, err = io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin payload: %w", err)
		}
	} else {
		content, err = os.ReadFile(payloadFile)
		if err != nil {
			return "", fmt.Errorf("read --payload-file: %w", err)
		}
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return "", fmt.Errorf("payload must contain a JSON object")
	}
	if !json.Valid([]byte(trimmed)) {
		return "", fmt.Errorf("payload must contain valid JSON")
	}

	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", fmt.Errorf("payload must contain valid JSON: %w", err)
	}
	if _, ok := payload.(map[string]any); !ok {
		return "", fmt.Errorf("payload must contain a JSON object")
	}

	return trimmed, nil
}

func approvalResolveAction(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved":
		return "approve"
	case "reject", "rejected", "deny", "denied":
		return "deny"
	case "clarify", "clarification_requested":
		return "clarify"
	default:
		return strings.ToLower(strings.TrimSpace(decision))
	}
}

func approvalResolveResultLabel(result approvalsvc.ResolveResult) string {
	if result.ResolverSupport == approvalsvc.ResolverUnsupported {
		return "not_resolved"
	}
	switch result.Approval.Status {
	case "approved":
		return "approved"
	case "denied":
		return "denied"
	case "clarification_requested":
		return "clarification_requested"
	default:
		return result.Approval.Status
	}
}

func runAgenda(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer, now func() time.Time) error {
	command, err := commands.ParseAgenda(args)
	if err != nil {
		return err
	}

	view, err := projections.GetAgendaView(ctx, app.Store.DB(), workspaces.DefaultWorkspaceKey, now().UTC())
	if err != nil {
		return err
	}
	if command.JSON {
		return commands.WriteJSON(stdout, view)
	}
	return commands.WriteAgendaText(stdout, view)
}

const logsUsage = "usage: odin logs [--json] | odin logs show <event-id> [--json] | odin logs trail (--task <id|key> | --run <id> | --approval <id>) [--json]"

func runLogs(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	records, err := listLogs(ctx, app.Store, state.Scope)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		switch remaining[0] {
		case "show":
			return runLogsShow(ctx, app.Store, records, remaining[1:], jsonOutput, stdout)
		case "trail":
			return runLogsTrail(ctx, app.Store, records, remaining[1:], jsonOutput, stdout)
		default:
			return fmt.Errorf(logsUsage)
		}
	}
	if jsonOutput {
		logViews := make([]commands.LogView, 0, len(records))
		for _, record := range records {
			logViews = append(logViews, commands.LogView{
				ID:         record.ID,
				StreamType: string(record.StreamType),
				StreamID:   record.StreamID,
				Type:       string(record.Type),
				Scope:      record.Scope,
				ProjectID:  record.ProjectID,
				TaskID:     record.TaskID,
				RunID:      record.RunID,
				OccurredAt: record.OccurredAt.UTC().Format(time.RFC3339),
				Payload:    record.Payload,
			})
		}
		return commands.WriteJSON(stdout, commands.LogsView{Logs: logViews})
	}
	if len(records) == 0 {
		_, err := fmt.Fprintln(stdout, "no logs")
		return err
	}
	for _, record := range records {
		if _, err := fmt.Fprintf(stdout, "%d %s %s\n", record.ID, record.Type, record.Scope); err != nil {
			return err
		}
	}
	return nil
}

func runLogsShow(ctx context.Context, store *sqlite.Store, records []runtimeevents.Record, args []string, jsonOutput bool, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf(logsUsage)
	}
	eventID, err := parsePositiveInt64Arg(args[0], "event id")
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.ID != eventID {
			continue
		}
		items, err := runtimeoverview.BuildActivityEventSummaries(ctx, store, []runtimeevents.Record{record}, true)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return fmt.Errorf("event %d not found", eventID)
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, struct {
				Event runtimeoverview.ActivityEventSummary `json:"event"`
			}{Event: items[0]})
		}
		return writeActivityEventDetail(stdout, items[0])
	}
	return fmt.Errorf("event %d not found", eventID)
}

func runLogsTrail(ctx context.Context, store *sqlite.Store, records []runtimeevents.Record, args []string, jsonOutput bool, stdout io.Writer) error {
	taskRef, runRef, approvalRef, err := parseLogsTrailArgs(args)
	if err != nil {
		return err
	}
	var filtered []runtimeevents.Record
	switch {
	case taskRef != "":
		taskID, err := resolveLogsTaskID(ctx, store, taskRef)
		if err != nil {
			return err
		}
		filtered = filterActivityEventsByTask(records, taskID)
	case runRef != "":
		runID, err := parsePositiveInt64Arg(runRef, "run id")
		if err != nil {
			return err
		}
		run, err := store.GetRun(ctx, runID)
		if err != nil {
			return err
		}
		filtered = filterActivityEventsByRun(records, runID, run.TaskID)
	case approvalRef != "":
		approvalID, err := parsePositiveInt64Arg(approvalRef, "approval id")
		if err != nil {
			return err
		}
		detail, err := approvalsvc.Service{Store: store}.Detail(ctx, approvalID)
		if err != nil {
			return err
		}
		filtered = filterActivityEventsByApproval(records, approvalID, detail.Task.ID, detail.Approval.RunID)
	default:
		return fmt.Errorf(logsUsage)
	}
	items, err := runtimeoverview.BuildActivityEventSummaries(ctx, store, filtered, jsonOutput)
	if err != nil {
		return err
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, struct {
			Items []runtimeoverview.ActivityEventSummary `json:"items"`
		}{Items: items})
	}
	return writeActivityEventTrail(stdout, items)
}

func parseLogsTrailArgs(args []string) (taskRef, runRef, approvalRef string, err error) {
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--task":
			if taskRef != "" || index+1 >= len(args) {
				return "", "", "", fmt.Errorf(logsUsage)
			}
			index++
			taskRef = strings.TrimSpace(args[index])
		case "--run":
			if runRef != "" || index+1 >= len(args) {
				return "", "", "", fmt.Errorf(logsUsage)
			}
			index++
			runRef = strings.TrimSpace(args[index])
		case "--approval":
			if approvalRef != "" || index+1 >= len(args) {
				return "", "", "", fmt.Errorf(logsUsage)
			}
			index++
			approvalRef = strings.TrimSpace(args[index])
		default:
			return "", "", "", fmt.Errorf(logsUsage)
		}
	}
	selected := 0
	for _, value := range []string{taskRef, runRef, approvalRef} {
		if value != "" {
			selected++
		}
	}
	if selected != 1 {
		return "", "", "", fmt.Errorf(logsUsage)
	}
	return taskRef, runRef, approvalRef, nil
}

func resolveLogsTaskID(ctx context.Context, store *sqlite.Store, ref string) (int64, error) {
	if id, err := strconv.ParseInt(strings.TrimSpace(ref), 10, 64); err == nil && id > 0 {
		if _, err := store.GetTask(ctx, id); err != nil {
			return 0, err
		}
		return id, nil
	}
	taskViews, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		return 0, err
	}
	for _, task := range taskViews {
		if task.TaskKey == ref {
			return task.TaskID, nil
		}
	}
	return 0, fmt.Errorf("work item %q not found", ref)
}

func filterActivityEventsByTask(records []runtimeevents.Record, taskID int64) []runtimeevents.Record {
	filtered := make([]runtimeevents.Record, 0, len(records))
	for _, record := range records {
		if recordMatchesTask(record, taskID) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func filterActivityEventsByRun(records []runtimeevents.Record, runID, taskID int64) []runtimeevents.Record {
	filtered := make([]runtimeevents.Record, 0, len(records))
	for _, record := range records {
		if recordMatchesRun(record, runID) || recordMatchesTask(record, taskID) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func filterActivityEventsByApproval(records []runtimeevents.Record, approvalID, taskID int64, runID *int64) []runtimeevents.Record {
	filtered := make([]runtimeevents.Record, 0, len(records))
	for _, record := range records {
		if record.StreamType == runtimeevents.StreamApproval && record.StreamID == approvalID {
			filtered = append(filtered, record)
			continue
		}
		if recordMatchesTask(record, taskID) {
			filtered = append(filtered, record)
			continue
		}
		if runID != nil && recordMatchesRun(record, *runID) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func recordMatchesTask(record runtimeevents.Record, taskID int64) bool {
	if taskID <= 0 {
		return false
	}
	if record.StreamType == runtimeevents.StreamTask && record.StreamID == taskID {
		return true
	}
	if record.TaskID != nil && *record.TaskID == taskID {
		return true
	}
	return eventPayloadInt64(record.Payload, "task_id") == taskID
}

func recordMatchesRun(record runtimeevents.Record, runID int64) bool {
	if runID <= 0 {
		return false
	}
	if record.StreamType == runtimeevents.StreamRun && record.StreamID == runID {
		return true
	}
	if record.RunID != nil && *record.RunID == runID {
		return true
	}
	return eventPayloadInt64(record.Payload, "run_id") == runID
}

func eventPayloadInt64(raw json.RawMessage, key string) int64 {
	if len(raw) == 0 {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	switch value := payload[key].(type) {
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed
	default:
		return 0
	}
}

func parsePositiveInt64Arg(raw, label string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", label)
	}
	return id, nil
}

func writeActivityEventDetail(stdout io.Writer, item runtimeoverview.ActivityEventSummary) error {
	if _, err := fmt.Fprintln(stdout, activityEventLine(item)); err != nil {
		return err
	}
	if len(item.Payload) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(stdout, "  payload=%s\n", strings.TrimSpace(string(item.Payload)))
	return err
}

func writeActivityEventTrail(stdout io.Writer, items []runtimeoverview.ActivityEventSummary) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(stdout, "no logs")
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintln(stdout, activityEventLine(item)); err != nil {
			return err
		}
	}
	return nil
}

func activityEventLine(item runtimeoverview.ActivityEventSummary) string {
	return fmt.Sprintf(
		"event=%d type=%s scope=%s project=%s work_item=%s run=%s approval=%s summary=%s",
		item.EventID,
		valueOrNone(item.EventType),
		valueOrNone(item.Scope),
		valueOrNone(item.ProjectKey),
		valueOrNone(item.WorkItemKey),
		activityInt64PtrLabel(item.RunID),
		activityInt64PtrLabel(item.ApprovalID),
		valueOrNone(item.Summary),
	)
}

func activityInt64PtrLabel(value *int64) string {
	if value == nil {
		return "none"
	}
	return fmt.Sprintf("%d", *value)
}

func runGoal(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseGoal(args)
	if err != nil {
		return err
	}
	if command.Name == "help" {
		_, err := fmt.Fprintln(stdout, commands.GoalUsage)
		return err
	}

	switch command.Name {
	case "create":
		goal, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{
			Title:       command.Title,
			Description: command.Description,
			CreatedBy:   command.CreatedBy,
			Source:      command.Source,
		})
		if err != nil {
			return err
		}
		view := newGoalView(goal)
		if command.JSON {
			return commands.WriteJSON(stdout, commands.GoalEnvelope{Goal: view})
		}
		_, err = fmt.Fprintf(stdout, "goal=%d status=%s title=%q\n", goal.ID, goal.Status, goal.Title)
		return err
	case "list":
		goals, err := app.Store.ListGoals(ctx, sqlite.ListGoalsParams{Status: sqlite.GoalStatus(command.Status), Limit: command.Limit})
		if err != nil {
			return err
		}
		views := make([]commands.GoalView, 0, len(goals))
		for _, goal := range goals {
			views = append(views, newGoalView(goal))
		}
		if command.JSON {
			return commands.WriteJSON(stdout, commands.GoalListView{Goals: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no goals")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "goal=%d status=%s title=%q\n", view.ID, view.Status, view.Title); err != nil {
				return err
			}
		}
		return nil
	case "show":
		goal, err := app.Store.GetGoal(ctx, command.ID)
		if err != nil {
			return err
		}
		if command.JSON {
			envelope, err := newGoalEnvelopeWithEvidence(ctx, app.Store, goal)
			if err != nil {
				return err
			}
			return commands.WriteJSON(stdout, envelope)
		}
		_, err = fmt.Fprintf(stdout, "goal=%d status=%s title=%q\n", goal.ID, goal.Status, goal.Title)
		return err
	case "update":
		goal, err := app.Store.UpdateGoal(ctx, sqlite.UpdateGoalParams{
			GoalID:         command.ID,
			Title:          command.Title,
			TitleSet:       command.TitleSet,
			Description:    command.Description,
			DescriptionSet: command.DescriptionSet,
			Actor:          command.Actor,
			Reason:         command.Reason,
		})
		if err != nil {
			return err
		}
		view := newGoalView(goal)
		if command.JSON {
			return commands.WriteJSON(stdout, commands.GoalEnvelope{Goal: view})
		}
		_, err = fmt.Fprintf(stdout, "goal=%d status=%s title=%q\n", goal.ID, goal.Status, goal.Title)
		return err
	case "transition":
		goal, err := app.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: command.ID,
			Status: sqlite.GoalStatus(command.Status),
			Actor:  command.Actor,
			Reason: command.Reason,
		})
		if err != nil {
			return err
		}
		view := newGoalView(goal)
		if command.JSON {
			return commands.WriteJSON(stdout, commands.GoalEnvelope{Goal: view})
		}
		_, err = fmt.Fprintf(stdout, "goal=%d status=%s title=%q\n", goal.ID, goal.Status, goal.Title)
		return err
	case "tick":
		result, err := goalruntime.NewService(app.Store).Tick(ctx)
		if err != nil {
			return err
		}
		if command.JSON {
			return commands.WriteJSON(stdout, result)
		}
		_, err = fmt.Fprintf(stdout, "observed=%d planned=%d started=%d blocked=%d skipped=%d\n", result.Observed, result.Planned, result.Started, result.Blocked, result.Skipped)
		return err
	default:
		return fmt.Errorf(commands.GoalUsage)
	}
}

func newGoalView(goal sqlite.Goal) commands.GoalView {
	return commands.GoalView{
		ID:           goal.ID,
		Title:        goal.Title,
		Description:  goal.Description,
		Status:       string(goal.Status),
		CreatedBy:    goal.CreatedBy,
		Source:       goal.Source,
		CurrentRunID: goal.CurrentRunID,
		CreatedAt:    goal.CreatedAt,
		UpdatedAt:    goal.UpdatedAt,
	}
}

func newGoalEnvelopeWithEvidence(ctx context.Context, store *sqlite.Store, goal sqlite.Goal) (commands.GoalEnvelope, error) {
	evidence, err := store.ListGoalEvidence(ctx, sqlite.ListGoalEvidenceParams{GoalID: goal.ID})
	if err != nil {
		return commands.GoalEnvelope{}, err
	}
	view := commands.GoalEnvelope{
		Goal:     newGoalView(goal),
		Evidence: make([]commands.GoalEvidenceView, 0, len(evidence)),
	}
	for _, item := range evidence {
		view.Evidence = append(view.Evidence, newGoalEvidenceView(item))
	}
	return view, nil
}

func newGoalEvidenceView(evidence sqlite.GoalEvidence) commands.GoalEvidenceView {
	return commands.GoalEvidenceView{
		ID:           evidence.ID,
		GoalID:       evidence.GoalID,
		GoalRunID:    evidence.GoalRunID,
		EvidenceType: evidence.EvidenceType,
		Summary:      evidence.Summary,
		URI:          evidence.URI,
		PayloadJSON:  []byte(evidence.PayloadJSON),
		CreatedBy:    evidence.CreatedBy,
		CreatedAt:    evidence.CreatedAt,
	}
}

func runInitiative(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseInitiative(args)
	if err != nil {
		return err
	}

	workspace, err := workspaces.Service{Store: app.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return err
	}

	service := initiatives.Service{Store: app.Store}

	switch command.Name {
	case "create":
		initiative, err := service.UpsertNonProject(ctx, workspace.ID, initiatives.UpsertInput{
			Key:   command.Key,
			Title: command.Title,
			Kind:  initiatives.Kind(command.Kind),
		})
		if err != nil {
			return err
		}

		view := commands.InitiativeView{
			ID:      initiative.ID,
			Key:     initiative.Key,
			Title:   initiative.Title,
			Kind:    string(initiative.Kind),
			Status:  initiative.Status,
			Summary: initiative.Summary,
		}
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "created initiative key=%s kind=%s status=%s\n", view.Key, view.Kind, view.Status)
		return err
	case "list":
		initiativesList, err := service.ListInitiatives(ctx, workspace.ID)
		if err != nil {
			return err
		}

		views := make([]commands.InitiativeView, 0, len(initiativesList))
		for _, initiative := range initiativesList {
			views = append(views, commands.InitiativeView{
				ID:      initiative.ID,
				Key:     initiative.Key,
				Title:   initiative.Title,
				Kind:    string(initiative.Kind),
				Status:  initiative.Status,
				Summary: initiative.Summary,
			})
		}

		if command.JSON {
			return commands.WriteJSON(stdout, commands.InitiativeListView{Initiatives: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no initiatives")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "%s kind=%s status=%s title=%s\n", view.Key, view.Kind, view.Status, view.Title); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported initiative subcommand: %s", command.Name)
	}
}

func runCompanion(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseCompanion(args)
	if err != nil {
		return err
	}

	workspace, err := workspaces.Service{Store: app.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return err
	}

	service := companions.Service{Store: app.Store}

	switch command.Name {
	case "create":
		companion, err := service.CreateOrUpdateCompanion(ctx, companions.Companion{
			WorkspaceID:         workspace.ID,
			Key:                 command.Key,
			Title:               command.Title,
			Kind:                companions.Kind(command.Kind),
			Charter:             "",
			Status:              "",
			InitiativeScopeJSON: "",
			ToolPolicyJSON:      "",
			MemoryPolicyJSON:    "",
			PlanningPolicyJSON:  "",
		})
		if err != nil {
			return err
		}

		view := commands.CompanionView{
			ID:     companion.ID,
			Key:    companion.Key,
			Title:  companion.Title,
			Kind:   string(companion.Kind),
			Status: companion.Status,
		}
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "created companion key=%s kind=%s status=%s\n", view.Key, view.Kind, view.Status)
		return err
	case "list":
		companionList, err := service.ListCompanions(ctx, workspace.ID)
		if err != nil {
			return err
		}

		views := make([]commands.CompanionView, 0, len(companionList))
		for _, companion := range companionList {
			views = append(views, commands.CompanionView{
				ID:     companion.ID,
				Key:    companion.Key,
				Title:  companion.Title,
				Kind:   string(companion.Kind),
				Status: companion.Status,
			})
		}

		if command.JSON {
			return commands.WriteJSON(stdout, commands.CompanionListView{Companions: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no companions")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "%s kind=%s status=%s title=%s\n", view.Key, view.Kind, view.Status, view.Title); err != nil {
				return err
			}
		}
		return nil
	case "get":
		companion, err := service.GetCompanionByKey(ctx, workspace.ID, command.Key)
		if err != nil {
			return err
		}

		view := renderCompanionGetView(companion)
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "companion key=%s kind=%s status=%s title=%s\n", view.Key, view.Kind, view.Status, view.Title)
		return err
	case "state":
		companion, err := service.GetCompanionByKey(ctx, workspace.ID, command.Key)
		if err != nil {
			return err
		}

		view, err := renderCompanionStateView(ctx, app, workspace.Key, companion, command.Key)
		if err != nil {
			return err
		}
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "companion key=%s kind=%s status=%s open_work_items=%d active_runs=%d swarms=%d\n",
			view.Key,
			view.Kind,
			view.Status,
			view.TaskState.OpenWorkItemCount,
			view.TaskState.ActiveRunCount,
			len(view.Swarms),
		)
		return err
	case "capabilities":
		companion, err := service.GetCompanionByKey(ctx, workspace.ID, command.Key)
		if err != nil {
			return err
		}

		view := renderCompanionCapabilitiesView(companion)
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		maxChildren := 0
		if view.PlanningPolicy.Swarm != nil {
			maxChildren = view.PlanningPolicy.Swarm.MaxChildren
		}
		_, err = fmt.Fprintf(stdout, "companion key=%s kind=%s status=%s tool_allow=%d memory_mode=%s max_children=%d\n",
			view.Key,
			view.Kind,
			view.Status,
			len(view.ToolPolicy.Allow),
			view.MemoryPolicy.Mode,
			maxChildren,
		)
		return err
	case "run":
		resolved, err := companionRunScope(app)
		if err != nil {
			return err
		}

		companion, err := service.GetCompanionByKey(ctx, workspace.ID, command.Key)
		if err != nil {
			return err
		}

		task, err := jobs.Service{
			Store:       app.Store,
			Registry:    app.Registry,
			Transitions: projects.Service{Store: app.Store},
		}.CreateTaskFromCompanionRun(ctx, resolved, sqlite.Companion{
			ID:                  companion.ID,
			WorkspaceID:         companion.WorkspaceID,
			Key:                 companion.Key,
			Title:               companion.Title,
			Kind:                string(companion.Kind),
			Charter:             companion.Charter,
			Status:              companion.Status,
			InitiativeScopeJSON: companion.InitiativeScopeJSON,
			ToolPolicyJSON:      companion.ToolPolicyJSON,
			MemoryPolicyJSON:    companion.MemoryPolicyJSON,
			PlanningPolicyJSON:  companion.PlanningPolicyJSON,
			CreatedAt:           companion.CreatedAt,
			UpdatedAt:           companion.UpdatedAt,
		}, command.Objective, command.Trigger)
		if err != nil {
			return err
		}

		view := commands.CompanionRunView{
			CompanionKey:          companion.Key,
			Objective:             command.Objective,
			RequestedSwarmTrigger: task.ActionKey,
			Task: commands.TaskCreateView{
				ID:     task.ID,
				Key:    task.Key,
				Status: task.Status,
				Scope:  task.Scope,
			},
		}
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		if view.RequestedSwarmTrigger != "" {
			_, err = fmt.Fprintf(stdout, "created companion task key=%s companion=%s status=%s scope=%s trigger=%s\n",
				view.Task.Key,
				view.CompanionKey,
				view.Task.Status,
				view.Task.Scope,
				view.RequestedSwarmTrigger,
			)
			return err
		}
		_, err = fmt.Fprintf(stdout, "created companion task key=%s companion=%s status=%s scope=%s\n",
			view.Task.Key,
			view.CompanionKey,
			view.Task.Status,
			view.Task.Scope,
		)
		return err
	case "delegate":
		if command.DelegateAction == "list" {
			resolved, err := companionRunScope(app)
			if err != nil {
				return err
			}
			view, err := companionDelegationListView(ctx, app.Store, resolved)
			if err != nil {
				return err
			}
			if command.JSON {
				return commands.WriteJSON(stdout, view)
			}
			_, err = fmt.Fprintf(stdout, "delegations=%d\n", len(view.Delegations))
			return err
		}
		if command.DelegateAction == "show" {
			resolved, err := companionRunScope(app)
			if err != nil {
				return err
			}
			view, err := companionDelegationShowView(ctx, app.Store, resolved, command.Key)
			if err != nil {
				return err
			}
			if command.JSON {
				return commands.WriteJSON(stdout, view)
			}
			_, err = fmt.Fprintf(stdout, "delegation id=%d key=%s status=%s artifacts=%d\n",
				view.Delegation.ID,
				view.Delegation.DelegationKey,
				view.Delegation.Status,
				len(view.Artifacts),
			)
			return err
		}
		if command.DelegateAction == "retry" {
			resolved, err := companionRunScope(app)
			if err != nil {
				return err
			}
			delegation, err := lookupScopedDelegation(ctx, app.Store, resolved, command.Key)
			if err != nil {
				return err
			}
			delegationService := delegationsvc.Service{
				Store:            app.Store,
				Jobs:             newJobService(app).Service,
				Checkpoints:      checkpoints.Service{Store: app.Store},
				RegistrySnapshot: app.RegistrySnapshot,
			}
			result, err := delegationService.RetryDelegation(ctx, delegation.ID)
			if err != nil {
				return err
			}
			view, err := renderCompanionDelegationRetryView(ctx, app.Store, result)
			if err != nil {
				return err
			}
			if command.JSON {
				return commands.WriteJSON(stdout, view)
			}
			_, err = fmt.Fprintf(stdout, "delegation id=%d key=%s retried=%t reason=%s status=%s\n",
				view.Delegation.ID,
				view.Delegation.DelegationKey,
				view.Retried,
				view.Reason,
				view.Delegation.Status,
			)
			return err
		}

		resolved, err := companionRunScope(app)
		if err != nil {
			return err
		}

		companion, err := service.GetCompanionByKey(ctx, workspace.ID, command.Key)
		if err != nil {
			return err
		}

		jobService := newJobService(app).Service
		delegationService := delegationsvc.Service{
			Store:            app.Store,
			Jobs:             jobService,
			Checkpoints:      checkpoints.Service{Store: app.Store},
			RegistrySnapshot: app.RegistrySnapshot,
		}
		parentTask, parentRun, result, err := delegationService.RunAgent(ctx, delegationsvc.RunInput{
			ResolvedScope: resolved,
			AgentKey:      command.AgentKey,
			RequestedBy:   "companion:" + companion.Key,
			CompanionID:   companion.ID,
			Intent:        command.Intent,
			Inputs:        companionDelegateInputs(command),
		})
		if err != nil {
			return err
		}

		view := renderCompanionDelegationRunView(companion.Key, command, parentTask, parentRun, result)
		if command.JSON {
			return commands.WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "delegated companion=%s agent=%s parent_task=%s parent_run=%s child_delegations=%d\n",
			view.CompanionKey,
			view.AgentKey,
			view.ParentTask.Key,
			formatOptionalInt64(runIDPtr(parentRun)),
			len(view.ChildDelegations),
		)
		return err
	default:
		return fmt.Errorf("unsupported companion subcommand: %s", command.Name)
	}
}

func companionDelegateInputs(command commands.CompanionCommand) map[string]string {
	inputs := map[string]string{
		"portal_track": command.PortalTrack,
		"surface":      command.Surface,
		"goal":         command.Goal,
		"intent":       command.Intent,
	}
	if inputs["project_key"] == "" {
		inputs["project_key"] = command.PortalTrack
	}
	if inputs["launch_objective"] == "" {
		inputs["launch_objective"] = firstNonEmpty(command.Goal, command.Surface)
	}
	return inputs
}

func companionDelegationListView(ctx context.Context, store *sqlite.Store, resolved cliscope.Resolution) (commands.CompanionDelegationListView, error) {
	delegations, err := listScopedDelegations(ctx, store, resolved)
	if err != nil {
		return commands.CompanionDelegationListView{}, err
	}

	view := commands.CompanionDelegationListView{
		Delegations: make([]commands.CompanionDelegationView, 0, len(delegations)),
	}
	for _, delegation := range delegations {
		artifacts, err := store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{DelegationID: delegation.ID})
		if err != nil {
			return commands.CompanionDelegationListView{}, err
		}
		view.Delegations = append(view.Delegations, renderCompanionDelegationView(delegation, len(artifacts)))
	}
	return view, nil
}

func companionDelegationShowView(ctx context.Context, store *sqlite.Store, resolved cliscope.Resolution, identifier string) (commands.CompanionDelegationDetailView, error) {
	delegation, err := lookupScopedDelegation(ctx, store, resolved, identifier)
	if err != nil {
		return commands.CompanionDelegationDetailView{}, err
	}
	artifacts, err := store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{DelegationID: delegation.ID})
	if err != nil {
		return commands.CompanionDelegationDetailView{}, err
	}

	view := commands.CompanionDelegationDetailView{
		Delegation: renderCompanionDelegationView(delegation, len(artifacts)),
		Artifacts:  make([]commands.CompanionDelegationArtifact, 0, len(artifacts)),
	}
	for _, artifact := range artifacts {
		view.Artifacts = append(view.Artifacts, commands.CompanionDelegationArtifact{
			ID:           artifact.ID,
			DelegationID: artifact.DelegationID,
			ArtifactType: artifact.ArtifactType,
			Summary:      artifact.Summary,
			DetailsJSON:  artifact.DetailsJSON,
			CreatedAt:    artifact.CreatedAt,
		})
	}
	return view, nil
}

func renderCompanionDelegationRetryView(ctx context.Context, store *sqlite.Store, result delegationsvc.RetryResult) (commands.CompanionDelegationRetryView, error) {
	artifacts, err := store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{DelegationID: result.Delegation.ID})
	if err != nil {
		return commands.CompanionDelegationRetryView{}, err
	}
	view := commands.CompanionDelegationRetryView{
		Retried:    result.Retried,
		Reason:     result.Reason,
		Delegation: renderCompanionDelegationView(result.Delegation, len(artifacts)),
		Artifacts:  make([]commands.CompanionDelegationArtifact, 0, len(artifacts)),
	}
	if result.ParentTask != nil {
		view.ParentTask = &commands.TaskCreateView{
			ID:     result.ParentTask.ID,
			Key:    result.ParentTask.Key,
			Status: result.ParentTask.Status,
			Scope:  result.ParentTask.Scope,
		}
	}
	if result.ParentRun != nil && result.ParentTask != nil {
		view.ParentRun = renderRunView(result.ParentRun, result.ParentTask.Key)
	}
	if result.ChildTask != nil {
		view.ChildTask = &commands.TaskCreateView{
			ID:     result.ChildTask.ID,
			Key:    result.ChildTask.Key,
			Status: result.ChildTask.Status,
			Scope:  result.ChildTask.Scope,
		}
	}
	if result.ChildRun != nil && result.ChildTask != nil {
		view.ChildRun = renderRunView(result.ChildRun, result.ChildTask.Key)
	}
	for _, artifact := range artifacts {
		view.Artifacts = append(view.Artifacts, commands.CompanionDelegationArtifact{
			ID:           artifact.ID,
			DelegationID: artifact.DelegationID,
			ArtifactType: artifact.ArtifactType,
			Summary:      artifact.Summary,
			DetailsJSON:  artifact.DetailsJSON,
			CreatedAt:    artifact.CreatedAt,
		})
	}
	return view, nil
}

func listScopedDelegations(ctx context.Context, store *sqlite.Store, resolved cliscope.Resolution) ([]sqlite.Delegation, error) {
	projectID, err := projectIDForResolution(ctx, store, resolved)
	if err != nil {
		return nil, err
	}
	return store.ListDelegations(ctx, sqlite.ListDelegationsParams{ProjectID: projectID})
}

func lookupScopedDelegation(ctx context.Context, store *sqlite.Store, resolved cliscope.Resolution, identifier string) (sqlite.Delegation, error) {
	projectID, err := projectIDForResolution(ctx, store, resolved)
	if err != nil {
		return sqlite.Delegation{}, err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return sqlite.Delegation{}, fmt.Errorf("delegation id or key is required")
	}
	if id, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		delegation, err := store.GetDelegation(ctx, id)
		if err != nil {
			return sqlite.Delegation{}, err
		}
		if projectID != nil && delegation.ProjectID != *projectID {
			return sqlite.Delegation{}, sql.ErrNoRows
		}
		return delegation, nil
	}

	delegations, err := store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ProjectID:     projectID,
		DelegationKey: identifier,
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}
	if len(delegations) == 0 {
		return sqlite.Delegation{}, sql.ErrNoRows
	}
	if len(delegations) > 1 {
		return sqlite.Delegation{}, fmt.Errorf("multiple delegations match key %q; use id", identifier)
	}
	return delegations[0], nil
}

func renderRunView(run *sqlite.Run, taskKey string) *commands.RunView {
	if run == nil {
		return nil
	}
	return &commands.RunView{
		RunID:    run.ID,
		TaskID:   run.TaskID,
		TaskKey:  taskKey,
		Executor: run.Executor,
		Status:   run.Status,
		Attempt:  run.Attempt,
	}
}

func projectIDForResolution(ctx context.Context, store *sqlite.Store, resolved cliscope.Resolution) (*int64, error) {
	if resolved.Kind != cliscope.ScopeProject && resolved.Kind != cliscope.ScopeOdinCore {
		return nil, nil
	}
	project, err := store.GetProjectByKey(ctx, resolved.ProjectKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &project.ID, nil
}

func renderCompanionDelegationView(delegation sqlite.Delegation, artifactCount int) commands.CompanionDelegationView {
	return commands.CompanionDelegationView{
		ID:            delegation.ID,
		DelegationKey: delegation.DelegationKey,
		Role:          delegation.Role,
		Status:        delegation.Status,
		ParentTaskID:  delegation.ParentTaskID,
		ParentRunID:   delegation.ParentRunID,
		ChildTaskID:   delegation.ChildTaskID,
		ChildRunID:    delegation.ChildRunID,
		Executor:      delegation.Executor,
		MutationMode:  delegation.MutationMode,
		ExecutionIntent: func() string {
			return delegationExecutionIntentView(delegation.MutationMode)
		}(),
		ExecutionIntentSource: "companion_delegate",
		ArtifactCount:         artifactCount,
		DetailsJSON:           delegation.DetailsJSON,
	}
}

func delegationExecutionIntentView(mutationMode string) string {
	switch strings.ToLower(strings.TrimSpace(mutationMode)) {
	case "mutation", "governance", "destructive":
		return strings.ToLower(strings.TrimSpace(mutationMode))
	default:
		return "read_only"
	}
}

func renderCompanionDelegationRunView(companionKey string, command commands.CompanionCommand, parentTask sqlite.Task, parentRun *sqlite.Run, result delegationsvc.RunResult) commands.CompanionDelegationRunView {
	var parentRunView *commands.RunView
	if parentRun != nil {
		parentRunView = &commands.RunView{
			RunID:    parentRun.ID,
			TaskID:   parentRun.TaskID,
			TaskKey:  parentTask.Key,
			Executor: parentRun.Executor,
			Status:   parentRun.Status,
			Attempt:  parentRun.Attempt,
		}
	}

	delegations := make([]commands.CompanionDelegationView, 0, len(result.ChildDelegations))
	for _, delegation := range result.ChildDelegations {
		delegations = append(delegations, commands.CompanionDelegationView{
			ID:            delegation.ID,
			DelegationKey: delegation.DelegationKey,
			Role:          delegation.Role,
			Status:        delegation.Status,
			ParentTaskID:  delegation.ParentTaskID,
			ParentRunID:   delegation.ParentRunID,
			ChildTaskID:   delegation.ChildTaskID,
			ChildRunID:    delegation.ChildRunID,
			Executor:      delegation.Executor,
			MutationMode:  delegation.MutationMode,
			ExecutionIntent: func() string {
				return delegationExecutionIntentView(delegation.MutationMode)
			}(),
			ExecutionIntentSource: "companion_delegate",
		})
	}

	return commands.CompanionDelegationRunView{
		Reused:       result.Reused,
		Reason:       result.Reason,
		CompanionKey: companionKey,
		AgentKey:     command.AgentKey,
		PortalTrack:  command.PortalTrack,
		Surface:      command.Surface,
		Goal:         command.Goal,
		Intent:       command.Intent,
		ParentTask: commands.TaskCreateView{
			ID:     parentTask.ID,
			Key:    parentTask.Key,
			Status: parentTask.Status,
			Scope:  parentTask.Scope,
		},
		ParentRun:           parentRunView,
		ChildDelegations:    delegations,
		LearningProposalIDs: append([]int64(nil), result.LearningProposalIDs...),
	}
}

func runIDPtr(run *sqlite.Run) *int64 {
	if run == nil {
		return nil
	}
	return &run.ID
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatInt(*value, 10)
}

func companionRunScope(app bootstrap.App) (cliscope.Resolution, error) {
	state, err := loadCLIState(app)
	if err != nil {
		return cliscope.Resolution{}, err
	}
	if state.Scope.Kind != cliscope.ScopeGlobal {
		return state.Scope, nil
	}

	manifest, ok := app.Registry.SystemProject()
	if !ok {
		return cliscope.Resolution{}, fmt.Errorf("odin-core scope is required")
	}
	return cliscope.Resolve(cliscope.ResolveInput{
		ExplicitTarget: &cliscope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	}), nil
}

func renderCompanionGetView(companion companions.Companion) commands.CompanionGetView {
	return commands.CompanionGetView{
		ID:                  companion.ID,
		WorkspaceID:         companion.WorkspaceID,
		Key:                 companion.Key,
		Title:               companion.Title,
		Kind:                string(companion.Kind),
		Charter:             companion.Charter,
		Status:              companion.Status,
		InitiativeScopeJSON: companion.InitiativeScopeJSON,
		ToolPolicyJSON:      companion.ToolPolicyJSON,
		MemoryPolicyJSON:    companion.MemoryPolicyJSON,
		PlanningPolicyJSON:  companion.PlanningPolicyJSON,
		CreatedAt:           companion.CreatedAt,
		UpdatedAt:           companion.UpdatedAt,
	}
}

func renderCompanionStateView(ctx context.Context, app bootstrap.App, workspaceKey string, companion companions.Companion, companionKey string) (commands.CompanionStateView, error) {
	assignmentViews, err := projections.ListCompanionAssignmentViews(ctx, app.Store.DB(), workspaceKey)
	if err != nil {
		return commands.CompanionStateView{}, err
	}
	var assignment *projections.CompanionAssignmentView
	for index := range assignmentViews {
		if assignmentViews[index].CompanionKey == companionKey {
			assignment = &assignmentViews[index]
			break
		}
	}
	if assignment == nil {
		return commands.CompanionStateView{}, fmt.Errorf("companion assignment projection missing for %s", companionKey)
	}

	swarmViews, err := projections.ListCompanionSwarmViews(ctx, app.Store.DB(), workspaceKey)
	if err != nil {
		return commands.CompanionStateView{}, err
	}
	filteredSwarms := make([]projections.CompanionSwarmView, 0)
	for _, swarm := range swarmViews {
		if swarm.CompanionKey != nil && *swarm.CompanionKey == companionKey {
			filteredSwarms = append(filteredSwarms, swarm)
		}
	}

	return commands.CompanionStateView{
		ID:     companion.ID,
		Key:    companion.Key,
		Title:  companion.Title,
		Kind:   string(companion.Kind),
		Status: companion.Status,
		TaskState: commands.CompanionTaskStateView{
			WorkspaceID:          assignment.WorkspaceID,
			WorkspaceKey:         assignment.WorkspaceKey,
			CompanionKey:         assignment.CompanionKey,
			OwnedInitiativeCount: assignment.OwnedInitiativeCount,
			OpenWorkItemCount:    assignment.OpenWorkItemCount,
			ActiveRunCount:       assignment.ActiveRunCount,
			PendingApprovalCount: assignment.PendingApprovalCount,
			BlockedWorkItemCount: assignment.BlockedWorkItemCount,
			OverdueFollowUpCount: assignment.OverdueFollowUpCount,
		},
		Swarms: filteredSwarms,
	}, nil
}

func renderCompanionCapabilitiesView(companion companions.Companion) commands.CompanionCapabilitiesView {
	return commands.CompanionCapabilitiesView{
		ID:     companion.ID,
		Key:    companion.Key,
		Title:  companion.Title,
		Kind:   string(companion.Kind),
		Status: companion.Status,
		ToolPolicy: commands.CompanionToolPolicyView{
			Allow: parseToolPolicyAllow(companion.ToolPolicyJSON),
		},
		MemoryPolicy: commands.CompanionMemoryPolicyView{
			Mode: parseMemoryPolicyMode(companion.MemoryPolicyJSON),
		},
		PlanningPolicy: commands.CompanionPlanningPolicyView{
			Mode:  parsePlanningPolicyMode(companion.PlanningPolicyJSON),
			Swarm: parsePlanningPolicySwarm(companion.PlanningPolicyJSON),
		},
	}
}

func parseToolPolicyAllow(raw string) []string {
	type toolPolicy struct {
		Allow []string `json:"allow"`
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return []string{}
	}

	var policy toolPolicy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return []string{}
	}

	allowed := make([]string, 0, len(policy.Allow))
	seen := make(map[string]struct{}, len(policy.Allow))
	for _, tool := range policy.Allow {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		allowed = append(allowed, tool)
	}
	return allowed
}

func parseMemoryPolicyMode(raw string) string {
	type memoryPolicy struct {
		Mode string `json:"mode"`
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return "companion"
	}

	var policy memoryPolicy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return "companion"
	}
	policy.Mode = strings.TrimSpace(policy.Mode)
	if policy.Mode == "" {
		return "companion"
	}
	return policy.Mode
}

func parsePlanningPolicyMode(raw string) string {
	type planningPolicy struct {
		Mode  string `json:"mode"`
		Swarm struct {
			MaxChildren int `json:"max_children"`
		} `json:"swarm"`
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return ""
	}

	var policy planningPolicy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return ""
	}
	return strings.TrimSpace(policy.Mode)
}

func parsePlanningPolicySwarm(raw string) *commands.CompanionPlanningSwarmView {
	type planningPolicy struct {
		Mode  string `json:"mode"`
		Swarm struct {
			MaxChildren int `json:"max_children"`
		} `json:"swarm"`
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}

	var policy planningPolicy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return nil
	}
	if policy.Swarm.MaxChildren <= 0 {
		return nil
	}
	return &commands.CompanionPlanningSwarmView{MaxChildren: policy.Swarm.MaxChildren}
}

func parseFollowUpCadence(value string) (followups.Cadence, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(followups.CadenceModeOnce):
		return followups.Cadence{Mode: followups.CadenceModeOnce}, nil
	case string(followups.CadenceIntervalDaily):
		return followups.Cadence{Mode: followups.CadenceModeRecurring, Interval: followups.CadenceIntervalDaily}, nil
	case string(followups.CadenceIntervalWeekly):
		return followups.Cadence{Mode: followups.CadenceModeRecurring, Interval: followups.CadenceIntervalWeekly}, nil
	case string(followups.CadenceIntervalMonthly):
		return followups.Cadence{Mode: followups.CadenceModeRecurring, Interval: followups.CadenceIntervalMonthly}, nil
	case string(followups.CadenceIntervalQuarterly):
		return followups.Cadence{Mode: followups.CadenceModeRecurring, Interval: followups.CadenceIntervalQuarterly}, nil
	default:
		return followups.Cadence{}, fmt.Errorf("unsupported follow-up cadence: %s", value)
	}
}

func renderFollowUpView(ctx context.Context, store *sqlite.Store, obligation followups.FollowUpObligation) (commands.FollowUpView, error) {
	view := commands.FollowUpView{
		ID:                 obligation.ID,
		InitiativeID:       obligation.InitiativeID,
		CompanionID:        obligation.CompanionID,
		TargetProjectID:    obligation.TargetProjectID,
		Title:              obligation.Title,
		Status:             string(obligation.Status),
		Cadence:            followupCadenceLabel(obligation.Cadence),
		NextDueAt:          obligation.NextDueAt,
		LastMaterializedAt: obligation.LastMaterializedAt,
		LastCompletedAt:    obligation.LastCompletedAt,
	}
	if obligation.InitiativeID != nil {
		initiative, err := store.GetInitiativeByID(ctx, *obligation.InitiativeID)
		if err != nil {
			return commands.FollowUpView{}, err
		}
		view.InitiativeKey = initiative.Key
	}
	if obligation.TargetProjectID > 0 {
		project, err := store.GetProject(ctx, obligation.TargetProjectID)
		if err != nil {
			return commands.FollowUpView{}, err
		}
		view.TargetProjectKey = project.Key
	}
	return view, nil
}

func followupCadenceLabel(cadence followups.Cadence) string {
	switch cadence.Mode {
	case followups.CadenceModeOnce:
		return string(followups.CadenceModeOnce)
	case followups.CadenceModeRecurring:
		return string(cadence.Interval)
	default:
		return string(cadence.Mode)
	}
}

func followUpTargetProjectID(ctx context.Context, app bootstrap.App, initiative sqlite.Initiative) (int64, error) {
	if initiative.Kind == "managed_project" && initiative.LinkedProjectID != nil {
		return *initiative.LinkedProjectID, nil
	}
	return bootstrap.ResolveFollowUpTargetProjectID(ctx, app.Store, app.RepoRoot)
}

func runFollowup(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseFollowUp(args)
	if err != nil {
		return err
	}

	workspace, err := workspaces.Service{Store: app.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return err
	}

	service := followups.Service{Store: app.Store}

	switch command.Name {
	case "add":
		initiative, err := app.Store.GetInitiativeByKey(ctx, workspace.ID, command.Initiative)
		if err != nil {
			return err
		}

		targetProjectID, err := followUpTargetProjectID(ctx, app, initiative)
		if err != nil {
			return err
		}

		cadence, err := parseFollowUpCadence(command.Cadence)
		if err != nil {
			return err
		}
		nextDue, err := cadence.NextDueAfter(time.Now().UTC())
		if err != nil {
			return err
		}

		obligation, err := service.Create(ctx, followups.CreateParams{
			WorkspaceID:     workspace.ID,
			InitiativeID:    &initiative.ID,
			TargetProjectID: &targetProjectID,
			Title:           command.Title,
			Cadence:         cadence,
			NextDueAt:       nextDue,
		})
		if err != nil {
			return err
		}

		view, err := renderFollowUpView(ctx, app.Store, obligation)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "created follow-up id=%d initiative=%s status=%s next_due_at=%s\n", view.ID, view.InitiativeKey, view.Status, view.NextDueAt.UTC().Format(time.RFC3339))
		return err
	case "list":
		obligations, err := service.ListByWorkspace(ctx, workspace.ID)
		if err != nil {
			return err
		}

		views := make([]commands.FollowUpView, 0, len(obligations))
		for _, obligation := range obligations {
			view, err := renderFollowUpView(ctx, app.Store, obligation)
			if err != nil {
				return err
			}
			views = append(views, view)
		}

		if command.JSON {
			return commands.WriteJSON(stdout, commands.FollowUpListView{Obligations: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no follow-ups")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "%d initiative=%s status=%s title=%s next_due_at=%s\n", view.ID, view.InitiativeKey, view.Status, view.Title, view.NextDueAt.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		return nil
	case "complete":
		obligation, err := service.Complete(ctx, workspace.ID, command.ID)
		if err != nil {
			return err
		}
		view, err := renderFollowUpView(ctx, app.Store, obligation)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "completed follow-up id=%d initiative=%s status=%s next_due_at=%s\n", view.ID, view.InitiativeKey, view.Status, view.NextDueAt.UTC().Format(time.RFC3339))
		return err
	case "snooze":
		obligation, err := service.Snooze(ctx, workspace.ID, command.ID, command.Until)
		if err != nil {
			return err
		}
		view, err := renderFollowUpView(ctx, app.Store, obligation)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "snoozed follow-up id=%d initiative=%s status=%s next_due_at=%s\n", view.ID, view.InitiativeKey, view.Status, view.NextDueAt.UTC().Format(time.RFC3339))
		return err
	default:
		return fmt.Errorf("unsupported followup subcommand: %s", command.Name)
	}
}

type schedulerTickView struct {
	Now               string                         `json:"now"`
	DryRun            bool                           `json:"dry_run"`
	Mutates           bool                           `json:"mutates"`
	TriggerEvaluation schedulerTriggerEvaluationView `json:"trigger_evaluation"`
	Supervision       schedulerSupervisionView       `json:"supervision"`
	RecoveryRan       bool                           `json:"recovery_ran"`
	Recovery          *schedulerRecoveryView         `json:"recovery,omitempty"`
	WouldRun          int                            `json:"would_run,omitempty"`
	WouldDefer        int                            `json:"would_defer,omitempty"`
	WouldBatch        int                            `json:"would_batch,omitempty"`
	ApprovalRequired  int                            `json:"approval_required,omitempty"`
	Decisions         []schedulerDecisionView        `json:"decisions,omitempty"`
}

type schedulerTriggerEvaluationView struct {
	Evaluated    int `json:"evaluated"`
	Materialized int `json:"materialized"`
	Deferred     int `json:"deferred"`
	Errored      int `json:"errored"`
}

type schedulerSupervisionView struct {
	Promoted   int `json:"promoted"`
	Reconciled int `json:"reconciled"`
}

type schedulerRecoveryView struct {
	Observations int `json:"observations"`
	Decisions    int `json:"decisions"`
	Outcomes     int `json:"outcomes"`
}

type schedulerDecisionView struct {
	Key              string  `json:"key"`
	Decision         string  `json:"decision"`
	Reason           string  `json:"reason"`
	TriggerType      string  `json:"trigger_type"`
	Schedule         string  `json:"schedule"`
	DueAt            *string `json:"due_at,omitempty"`
	NextRun          *string `json:"next_run,omitempty"`
	LastRun          *string `json:"last_run,omitempty"`
	QuietHours       string  `json:"quiet_hours"`
	QuietHourEffect  string  `json:"quiet_hour_effect"`
	BatchKey         string  `json:"batch_key,omitempty"`
	BatchWindow      string  `json:"batch_window,omitempty"`
	BatchGroup       string  `json:"batch_group,omitempty"`
	ApprovalRequired bool    `json:"approval_required"`
	RecoveryState    string  `json:"recovery_state"`
	Mutates          bool    `json:"mutates"`
	Error            string  `json:"error,omitempty"`
}

func runScheduler(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, schedulerUsage)
		return err
	}
	switch args[0] {
	case "tick":
		return runSchedulerTick(ctx, app, args[1:], stdout)
	default:
		return fmt.Errorf("unsupported scheduler subcommand: %s", args[0])
	}
}

func runSchedulerTick(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	nowFunc, err := runtimeNow()
	if err != nil {
		return err
	}
	now := nowFunc().UTC()
	recoveryEnabled := false
	jsonOutput := false
	dryRun := parseBoolEnv(os.Getenv("ODIN_DRY_RUN"))
	for _, arg := range args {
		switch {
		case arg == "--help":
			_, err := fmt.Fprintln(stdout, schedulerUsage)
			return err
		case arg == "--json":
			jsonOutput = true
		case arg == "--dry-run":
			dryRun = true
		case strings.HasPrefix(arg, "dry_run="):
			parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, "dry_run="))
			if err != nil {
				return fmt.Errorf("scheduler tick dry_run must be true or false: %w", err)
			}
			dryRun = parsed
		case strings.HasPrefix(arg, "now="):
			parsed, err := time.Parse(time.RFC3339, strings.TrimPrefix(arg, "now="))
			if err != nil {
				return fmt.Errorf("scheduler tick now must be RFC3339: %w", err)
			}
			now = parsed.UTC()
		case strings.HasPrefix(arg, "recovery="):
			parsed, err := strconv.ParseBool(strings.TrimPrefix(arg, "recovery="))
			if err != nil {
				return fmt.Errorf("scheduler tick recovery must be true or false: %w", err)
			}
			recoveryEnabled = parsed
		default:
			return fmt.Errorf("unknown scheduler tick argument: %s", arg)
		}
	}

	if dryRun {
		preview, err := (triggers.Service{Store: app.Store, Registry: app.Registry}).PreviewDue(ctx, now)
		if err != nil {
			return err
		}
		view := schedulerPreviewTickView(now, preview)
		auditScope, auditProjectID := schedulerTickAuditTarget(ctx, app)
		if err := app.Store.RecordSchedulerTick(ctx, sqlite.RecordSchedulerTickParams{
			Now:              now,
			Scope:            auditScope,
			ProjectID:        auditProjectID,
			DryRun:           true,
			Mutates:          false,
			Evaluated:        view.TriggerEvaluation.Evaluated,
			Materialized:     view.TriggerEvaluation.Materialized,
			Deferred:         view.TriggerEvaluation.Deferred,
			Errored:          view.TriggerEvaluation.Errored,
			WouldRun:         view.WouldRun,
			WouldDefer:       view.WouldDefer,
			WouldBatch:       view.WouldBatch,
			ApprovalRequired: view.ApprovalRequired,
			RecoveryRan:      false,
		}); err != nil {
			return err
		}
		if jsonOutput {
			return commands.WriteJSON(stdout, view)
		}
		if _, err := fmt.Fprintf(stdout,
			"scheduler tick dry_run=true now=%s evaluated=%d materialized=0 would_run=%d would_defer=%d would_batch=%d approval_required=%d errored=%d recovery=false mutates=false\n",
			view.Now,
			view.TriggerEvaluation.Evaluated,
			view.WouldRun,
			view.WouldDefer,
			view.WouldBatch,
			view.ApprovalRequired,
			view.TriggerEvaluation.Errored,
		); err != nil {
			return err
		}
		for _, decision := range view.Decisions {
			if _, err := fmt.Fprintf(stdout, "trigger=%s decision=%s reason=%s type=%s schedule=%s next_run=%s quiet_hour_effect=%s batch=%s approval_required=%t recovery_state=%s\n",
				decision.Key,
				decision.Decision,
				decision.Reason,
				decision.TriggerType,
				decision.Schedule,
				defaultSchedulerStringPtr(decision.NextRun, "none"),
				defaultSchedulerString(decision.QuietHourEffect, "none"),
				formatSchedulerBatch(decision.BatchKey, decision.BatchWindow),
				decision.ApprovalRequired,
				decision.RecoveryState,
			); err != nil {
				return err
			}
		}
		return nil
	}
	triggerResult, err := (triggers.Service{Store: app.Store, Registry: app.Registry}).EvaluateDue(ctx, now)
	if err != nil {
		return err
	}
	supervisionResult, err := (supervision.Service{
		Store: app.Store,
		Now:   func() time.Time { return now },
	}).Tick(ctx)
	if err != nil {
		return err
	}

	view := schedulerTickView{
		Now:     now.Format(time.RFC3339),
		DryRun:  false,
		Mutates: true,
		TriggerEvaluation: schedulerTriggerEvaluationView{
			Evaluated:    triggerResult.Evaluated,
			Materialized: triggerResult.Materialized,
			Deferred:     triggerResult.Deferred,
			Errored:      triggerResult.Errored,
		},
		Supervision: schedulerSupervisionView{
			Promoted:   supervisionResult.Promoted,
			Reconciled: supervisionResult.Reconciled,
		},
		RecoveryRan: recoveryEnabled,
	}
	if recoveryEnabled {
		result, err := (recovery.Service{
			Store:           app.Store,
			RegistryRoot:    filepath.Join(app.RepoRoot, "registry"),
			ExecutorCatalog: app.Executors,
			HealthConfig:    healthsvc.DefaultConfig(),
			ExecutorKeys:    enabledExecutorKeys(app.ExecutorConfig),
			Now:             func() time.Time { return now },
		}).RunCycle(ctx)
		if err != nil {
			return err
		}
		view.Recovery = &schedulerRecoveryView{
			Observations: len(result.Observations),
			Decisions:    len(result.Decisions),
			Outcomes:     len(result.Outcomes),
		}
	}
	auditScope, auditProjectID := schedulerTickAuditTarget(ctx, app)
	if err := app.Store.RecordSchedulerTick(ctx, sqlite.RecordSchedulerTickParams{
		Now:          now,
		Scope:        auditScope,
		ProjectID:    auditProjectID,
		DryRun:       false,
		Mutates:      true,
		Evaluated:    view.TriggerEvaluation.Evaluated,
		Materialized: view.TriggerEvaluation.Materialized,
		Deferred:     view.TriggerEvaluation.Deferred,
		Errored:      view.TriggerEvaluation.Errored,
		RecoveryRan:  view.RecoveryRan,
	}); err != nil {
		return err
	}

	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout,
		"scheduler tick now=%s evaluated=%d materialized=%d deferred=%d errored=%d promoted=%d reconciled=%d recovery=%t\n",
		view.Now,
		view.TriggerEvaluation.Evaluated,
		view.TriggerEvaluation.Materialized,
		view.TriggerEvaluation.Deferred,
		view.TriggerEvaluation.Errored,
		view.Supervision.Promoted,
		view.Supervision.Reconciled,
		view.RecoveryRan,
	)
	return err
}

func schedulerPreviewTickView(now time.Time, preview triggers.PreviewResult) schedulerTickView {
	decisions := make([]schedulerDecisionView, 0, len(preview.Decisions))
	for _, decision := range preview.Decisions {
		decisions = append(decisions, newSchedulerDecisionView(decision))
	}
	return schedulerTickView{
		Now:     now.UTC().Format(time.RFC3339),
		DryRun:  true,
		Mutates: false,
		TriggerEvaluation: schedulerTriggerEvaluationView{
			Evaluated:    preview.Evaluated,
			Materialized: 0,
			Deferred:     0,
			Errored:      preview.Errored,
		},
		Supervision:      schedulerSupervisionView{},
		RecoveryRan:      false,
		WouldRun:         preview.WouldRun,
		WouldDefer:       preview.WouldDefer,
		WouldBatch:       preview.WouldBatch,
		ApprovalRequired: preview.ApprovalRequired,
		Decisions:        decisions,
	}
}

func newSchedulerDecisionView(decision triggers.PreviewDecision) schedulerDecisionView {
	schedule, quietHours, batchKey, batchWindow := schedulerTriggerRuleDetails(decision.Trigger)
	return schedulerDecisionView{
		Key:              decision.Trigger.Key,
		Decision:         decision.Decision,
		Reason:           decision.Reason,
		TriggerType:      decision.Trigger.Kind,
		Schedule:         schedule,
		DueAt:            formatSchedulerOptionalTime(decision.DueAt),
		NextRun:          formatSchedulerOptionalTime(decision.NextEligibleAt),
		LastRun:          formatSchedulerOptionalTime(decision.Trigger.LastMaterializedAt),
		QuietHours:       defaultSchedulerString(decision.QuietHours, quietHours),
		QuietHourEffect:  defaultSchedulerString(decision.QuietHourEffect, "none"),
		BatchKey:         defaultSchedulerString(decision.BatchKey, batchKey),
		BatchWindow:      defaultSchedulerString(decision.BatchWindow, batchWindow),
		BatchGroup:       decision.BatchGroup,
		ApprovalRequired: decision.ApprovalRequired,
		RecoveryState:    decision.RecoveryState,
		Mutates:          false,
		Error:            decision.Error,
	}
}

func schedulerTriggerRuleDetails(trigger sqlite.AutomationTrigger) (string, string, string, string) {
	var rule struct {
		Cadence     string `json:"cadence"`
		Cron        string `json:"cron"`
		QuietHours  string `json:"quiet_hours"`
		BatchKey    string `json:"batch_key"`
		BatchWindow string `json:"batch_window"`
		EventType   string `json:"event_type"`
	}
	_ = json.Unmarshal([]byte(trigger.RuleJSON), &rule)
	schedule := "manual"
	switch {
	case strings.TrimSpace(rule.Cron) != "":
		schedule = "cron:" + strings.TrimSpace(rule.Cron)
	case strings.TrimSpace(rule.Cadence) != "":
		schedule = "cadence:" + strings.TrimSpace(rule.Cadence)
	case strings.EqualFold(trigger.Kind, "event") && strings.TrimSpace(rule.EventType) != "":
		schedule = "event:" + strings.TrimSpace(rule.EventType)
	}
	return schedule, strings.TrimSpace(rule.QuietHours), strings.TrimSpace(rule.BatchKey), strings.TrimSpace(rule.BatchWindow)
}

func schedulerTickAuditTarget(ctx context.Context, app bootstrap.App) (string, *int64) {
	state, err := loadCLIState(app)
	if err != nil {
		return "runtime", nil
	}
	if state.Scope.Kind != cliscope.ScopeProject && state.Scope.Kind != cliscope.ScopeOdinCore {
		return "runtime", nil
	}
	project, err := app.Store.GetProjectByKey(ctx, state.Scope.ProjectKey)
	if err != nil {
		return state.Scope.ProjectKey, nil
	}
	return project.Scope, &project.ID
}

func parseBoolEnv(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func formatSchedulerOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func defaultSchedulerString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "none"
}

func defaultSchedulerStringPtr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func formatSchedulerBatch(key string, window string) string {
	key = strings.TrimSpace(key)
	window = strings.TrimSpace(window)
	if key == "" {
		return "none"
	}
	if window == "" {
		return key
	}
	return key + " window=" + window
}
func runStatus(ctx context.Context, app bootstrap.App, cfg appconfig.Config, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin status [--json]")
	}

	summary, err := newHealthService(app, healthsvc.DefaultConfig(), cfg).Summary(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}
	healthService := newHealthService(app, healthsvc.DefaultConfig(), cfg)
	readinessReport, ready, err := healthService.Readiness(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}
	runtimeStatus := "unknown"
	runtimeState, err := app.Store.GetRuntimeState(ctx)
	if err == nil {
		runtimeStatus = runtimeState.Status
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	workerDispatch := healthsvc.NewWorkerDispatchStatus(ready, runtimeStatus, readinessReport.Status)

	snapshotCtx, cancelSnapshot := context.WithTimeout(ctx, 5*time.Second)
	defer cancelSnapshot()
	snapshot, err := conversationsvc.Service{
		DB:             app.Store.DB(),
		StalledTimeout: 30 * time.Minute,
	}.Snapshot(snapshotCtx)
	statusSnapshotError := ""
	if err != nil && jsonOutput {
		statusSnapshotError = err.Error()
		snapshot = conversationsvc.Snapshot{GeneratedAt: time.Now().UTC()}
	} else if err != nil {
		return err
	}

	if jsonOutput {
		companionSwarmCounts := struct {
			Active  int `json:"active"`
			Blocked int `json:"blocked"`
			Backlog int `json:"backlog"`
		}{}
		for _, swarm := range snapshot.CompanionSwarms {
			if strings.EqualFold(swarm.Status, "blocked") {
				companionSwarmCounts.Blocked++
			} else if strings.EqualFold(swarm.Status, "running") || swarm.ActiveChildRunCount > 0 || swarm.BacklogCount > 0 || swarm.BudgetBacklogCount > 0 {
				companionSwarmCounts.Active++
			}
			companionSwarmCounts.Backlog += swarm.BacklogCount + swarm.BudgetBacklogCount
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{
			"health":                       string(readinessReport.Status),
			"pending_approvals":            len(snapshot.ApprovalsWaiting),
			"registry_healthy":             summary.RegistryHealthy,
			"generated_at":                 snapshot.GeneratedAt,
			"status_snapshot_error":        statusSnapshotError,
			"approvals_waiting":            snapshot.ApprovalsWaiting,
			"stalled_runs":                 snapshot.StalledRuns,
			"active_runs":                  snapshot.ActiveRuns,
			"project_transitions":          snapshot.ProjectTransitions,
			"project_transition_ownership": snapshot.ProjectTransitionOwnership,
			"companion_swarm_counts":       companionSwarmCounts,
			"companion_swarms":             snapshot.CompanionSwarms,
			"worker_dispatch":              workerDispatch,
		})
	}

	companionSwarmCount := len(snapshot.CompanionSwarms)
	_, err = fmt.Fprintf(stdout, "health=%s pending_approvals=%d stalled_runs=%d active_runs=%d project_transitions=%d companion_swarms=%d registry_healthy=%t worker_dispatch=%s dry_run=%t read_only=%t\n",
		readinessReport.Status,
		len(snapshot.ApprovalsWaiting),
		len(snapshot.StalledRuns),
		len(snapshot.ActiveRuns),
		len(snapshot.ProjectTransitions),
		companionSwarmCount,
		summary.RegistryHealthy,
		workerDispatch.Mode,
		workerDispatch.DryRun,
		workerDispatch.ReadOnly,
	)
	return err
}

func runProject(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	switch len(args) {
	case 0:
		return runShellCommand(ctx, app, "/project", stdout)
	case 2:
		if strings.EqualFold(args[0], "select") {
			return runShellCommand(ctx, app, "/project "+args[1], stdout)
		}
	}
	return commands.RunProject(ctx, app.Store, app.Registry, args, stdout)
}

func runTransition(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command := "/transition"
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}
	return runShellCommand(ctx, app, command, stdout)
}

func runTask(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := clicommands.ParseTask(args)
	if err != nil {
		return err
	}

	manifest, ok := app.Registry.Lookup(command.ProjectKey)
	if !ok {
		return fmt.Errorf("unknown project: %s", command.ProjectKey)
	}
	resolved := cliscope.Resolve(cliscope.ResolveInput{
		ExplicitTarget: &cliscope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})

	jobService := newJobService(app)
	task, err := jobService.Service.CreateTask(ctx, jobs.CreateTaskParams{
		Resolved:           resolved,
		Title:              command.Title,
		AcceptanceCriteria: command.AcceptanceCriteria,
		RequestedBy:        "operator",
	})
	if err != nil {
		return err
	}

	if command.Name == "create" {
		return clicommands.WriteJSON(stdout, clicommands.TaskCreateView{
			ID:     task.ID,
			Key:    task.Key,
			Status: task.Status,
			Scope:  task.Scope,
		})
	}

	if err := bootstrap.RefreshReadinessSamples(ctx, app, len(app.RegistryDiagnostics) == 0); err != nil {
		return err
	}
	if err := jobService.Service.ExecuteNextQueued(ctx); err != nil {
		return err
	}

	task, err = app.Store.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	run, err := latestRunForTask(ctx, app.Store, task.ID)
	if err != nil {
		return err
	}

	return clicommands.WriteJSON(stdout, clicommands.TaskRunView{
		Task: clicommands.TaskCreateView{
			ID:     task.ID,
			Key:    task.Key,
			Status: task.Status,
			Scope:  task.Scope,
		},
		Run: &clicommands.TaskRunResultView{
			ID:       run.ID,
			Executor: run.Executor,
			Status:   run.Status,
			Summary:  run.Summary,
		},
	})
}

func runShellCommand(ctx context.Context, app bootstrap.App, line string, stdout io.Writer) error {
	shell, err := newShell(app)
	if err != nil {
		return err
	}
	return shell.HandleLine(ctx, line, stdout)
}

type servedJobService struct {
	jobs.Service
	Supervisor any
}

func newJobService(app bootstrap.App) servedJobService {
	return servedJobService{
		Service: jobs.Service{
			Store:              app.Store,
			Registry:           app.Registry,
			Executors:          app.Executors,
			ExecutorConfig:     app.ExecutorConfig,
			ModelRegistry:      app.ModelRegistry,
			PromptRenderer:     app.PromptRenderer,
			PromptTemplateName: app.PromptTemplateName,
			Transitions:        projects.Service{Store: app.Store},
			Leases: leases.Manager{
				Store:        app.Store,
				Git:          gitadapter.Adapter{},
				WorktreeRoot: worktrees.DefaultRoot(),
			},
			Now: time.Now,
		},
		Supervisor: supervision.Service{
			Store: app.Store,
			Now:   time.Now,
		},
	}
}

type servedCommandService struct {
	app bootstrap.App
}

func (service servedCommandService) Execute(ctx context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	switch request.CapabilityID {
	case "project.status":
		return invokeServedProjectStatus(ctx, service.app, request)
	default:
		return capabilities.InvokeResponse{}, fmt.Errorf("unsupported capability: %s", request.CapabilityID)
	}
}

func invokeServedProjectStatus(ctx context.Context, app bootstrap.App, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	mode := strings.TrimSpace(request.Execution.Mode)
	if mode == "" {
		mode = "local"
	}

	scopeLabel := strings.TrimSpace(request.Scope.Kind)
	if request.Scope.Kind == "project" || request.Scope.Kind == "odin-core" {
		if request.Scope.ProjectKey != "" {
			scopeLabel = request.Scope.ProjectKey
		}
		if manifest, ok := app.Registry.Lookup(request.Scope.ProjectKey); ok && app.Store != nil {
			project, err := projects.Service{Store: app.Store}.RegisterManagedProject(ctx, manifest)
			if err == nil {
				record, err := app.Store.GetProjectTransition(ctx, project.ID)
				if err == nil {
					return capabilities.InvokeResponse{
						Status: "ok",
						Output: json.RawMessage(fmt.Sprintf(
							"project=%s state=%s controller=%s mutation_authority=%s odin_can_mutate=%t limited_actions=%s\n",
							manifest.Key,
							record.State,
							record.Controller,
							record.Controller,
							record.Controller == string(projects.TransitionControllerOdinOS),
							formatLimitedActions(record.LimitedActionsJSON),
						)),
					}, nil
				}
			}
		}
	}
	if scopeLabel == "" {
		scopeLabel = "global"
	}

	return capabilities.InvokeResponse{
		Status: "ok",
		Output: json.RawMessage(fmt.Sprintf("scope=%s mode=%s\n", scopeLabel, mode)),
	}, nil
}

func formatLimitedActions(raw string) string {
	values := strings.TrimSpace(raw)
	if values == "" || values == "[]" {
		return "none"
	}
	return strings.Trim(values, "[]\"")
}

func newShell(app bootstrap.App, nowOverride ...func() time.Time) (*repl.Shell, error) {
	var now func() time.Time
	if len(nowOverride) > 0 {
		now = nowOverride[0]
	}
	return repl.New(repl.Environment{
		Store:               app.Store,
		Registry:            app.Registry,
		RegistrySnapshot:    app.RegistrySnapshot,
		RegistryDiagnostics: app.RegistryDiagnostics,
		SessionStore:        app.SessionStore,
		CommandService:      servedCommandService{app: app},
		ExecutorConfig:      app.ExecutorConfig,
		ModelRegistry:       app.ModelRegistry,
		Executors:           app.Executors,
		ReviewQueueProjection: func(ctx context.Context) (reviewqueue.Projection, error) {
			return readReviewQueueProjection(ctx, app)
		},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
		},
		Now: now,
	})
}

func latestRunForTask(ctx context.Context, store *sqlite.Store, taskID int64) (sqlite.Run, error) {
	row := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM runs
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, taskID)

	var runID int64
	if err := row.Scan(&runID); err != nil {
		return sqlite.Run{}, err
	}
	return store.GetRun(ctx, runID)
}

func runHealthcheck(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
	if reason, active, err := readReadinessFlag(cfg.RuntimeRoot); err != nil {
		return err
	} else if active {
		_, _ = fmt.Fprintf(stdout, "not ready: %s\n", reason)
		return errRuntimeNotReady
	}

	state, err := app.Store.GetRuntimeState(ctx)
	switch err {
	case nil:
		if state.Status != "ready" {
			_, _ = fmt.Fprintln(stdout, "not ready: runtime not ready")
			return errRuntimeNotReady
		}
	case sql.ErrNoRows:
		_, _ = fmt.Fprintln(stdout, "not ready: runtime not ready")
		return errRuntimeNotReady
	default:
		return err
	}

	healthConfig := healthsvc.DefaultConfig()
	healthConfig.RuntimeHeartbeatTTL = runtimeHeartbeatTTL(serveLoopConfigFromContext(ctx).healthInterval)
	report, ready, err := newHealthService(app, healthConfig, cfg).Readiness(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	if !ready {
		reason := string(report.Status)
		if report.Status == healthsvc.StatusHealthy {
			reason = "runtime not ready"
		}
		_, _ = fmt.Fprintf(stdout, "not ready: %s\n", reason)
		return errRuntimeNotReady
	}

	lockHeld, err := bootstrap.ServiceLockHeld(cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	if !lockHeld {
		_, _ = fmt.Fprintln(stdout, "not ready: no live odin serve process owns runtime root")
		return errRuntimeNotReady
	}

	_, err = fmt.Fprintln(stdout, "ready")
	return err
}

func runtimeEnv() map[string]string {
	fileEnv := runtimeEnvFile()
	applyRuntimeEnvDefaults(fileEnv)
	return map[string]string{
		"ODIN_ROOT":                       runtimeEnvValue(fileEnv, "ODIN_ROOT"),
		"ODIN_HTTP_ADDR":                  runtimeEnvValue(fileEnv, "ODIN_HTTP_ADDR"),
		"ODIN_ADMIN_TOKEN":                runtimeEnvValue(fileEnv, "ODIN_ADMIN_TOKEN"),
		"ODIN_NOW":                        runtimeEnvValue(fileEnv, "ODIN_NOW"),
		"ODIN_MEDIA_CONFIG":               runtimeEnvValue(fileEnv, "ODIN_MEDIA_CONFIG"),
		"ODIN_EMAIL_ACTION_SECRET":        runtimeEnvValue(fileEnv, "ODIN_EMAIL_ACTION_SECRET"),
		"ODIN_EMAIL_ACTION_BASE_URL":      runtimeEnvValue(fileEnv, "ODIN_EMAIL_ACTION_BASE_URL"),
		"ODIN_EMAIL_ACTION_RECIPIENT":     runtimeEnvValue(fileEnv, "ODIN_EMAIL_ACTION_RECIPIENT"),
		"ODIN_EMAIL_ACTION_SENDMAIL_PATH": runtimeEnvValue(fileEnv, "ODIN_EMAIL_ACTION_SENDMAIL_PATH"),
		"ODIN_EMAIL_ACTION_FROM":          runtimeEnvValue(fileEnv, "ODIN_EMAIL_ACTION_FROM"),
	}
}

func runtimeEnvValue(fileEnv map[string]string, key string) string {
	if value := os.Getenv(key); strings.TrimSpace(value) != "" {
		return value
	}
	return fileEnv[key]
}

func applyRuntimeEnvDefaults(fileEnv map[string]string) {
	for _, key := range []string{
		"ODIN_ROOT",
		"ODIN_HTTP_ADDR",
		"ODIN_ADMIN_TOKEN",
		"ODIN_NOW",
		"ODIN_MEDIA_CONFIG",
		"ODIN_EMAIL_ACTION_SECRET",
		"ODIN_EMAIL_ACTION_BASE_URL",
		"ODIN_EMAIL_ACTION_RECIPIENT",
		"ODIN_EMAIL_ACTION_SENDMAIL_PATH",
		"ODIN_EMAIL_ACTION_FROM",
		"ODIN_PROJECTS_OVERLAY",
		"ODIN_CODEX_DRIVER",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		if value := strings.TrimSpace(fileEnv[key]); value != "" {
			_ = os.Setenv(key, value)
		}
	}
}

func runtimeEnvFile() map[string]string {
	env := make(map[string]string)
	path := runtimeEnvFilePath()
	if path == "" {
		return env
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := parseRuntimeEnvLine(line)
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func runtimeEnvFilePath() string {
	if parseBoolEnv(os.Getenv("ODIN_DISABLE_ENV_FILE")) {
		return ""
	}
	if path := strings.TrimSpace(os.Getenv("ODIN_ENV_FILE")); path != "" {
		return path
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home := strings.TrimSpace(os.Getenv("HOME"))
		if home == "" {
			return ""
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "odin", "odin-os.env")
}

func parseRuntimeEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if !runtimeEnvKeyAllowed(key) {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

func runtimeEnvKeyAllowed(key string) bool {
	switch key {
	case "ODIN_ROOT",
		"ODIN_HTTP_ADDR",
		"ODIN_ADMIN_TOKEN",
		"ODIN_NOW",
		"ODIN_MEDIA_CONFIG",
		"ODIN_EMAIL_ACTION_SECRET",
		"ODIN_EMAIL_ACTION_BASE_URL",
		"ODIN_EMAIL_ACTION_RECIPIENT",
		"ODIN_EMAIL_ACTION_SENDMAIL_PATH",
		"ODIN_EMAIL_ACTION_FROM",
		"ODIN_PROJECTS_OVERLAY",
		"ODIN_CODEX_DRIVER":
		return true
	default:
		return false
	}
}

func runtimeNow() (func() time.Time, error) {
	raw := strings.TrimSpace(os.Getenv("ODIN_NOW"))
	if raw == "" {
		return func() time.Time {
			return time.Now().UTC()
		}, nil
	}

	fixed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, fmt.Errorf("invalid ODIN_NOW %q: %w", raw, err)
	}
	fixed = fixed.UTC()
	return func() time.Time {
		return fixed
	}, nil
}

func saveCLIState(app bootstrap.App, state clistate.State) error {
	cache := clistate.Cache{
		Mode: state.Mode,
	}
	if state.Scope.Kind == scope.ScopeProject || state.Scope.Kind == scope.ScopeOdinCore {
		cache.ProjectKey = state.Scope.ProjectKey
	}
	return app.SessionStore.Save(cache)
}

func scopeLabel(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return resolved.ProjectKey
	default:
		return string(resolved.Kind)
	}
}

func listPendingApprovals(ctx context.Context, store *sqlite.Store, resolved scope.Resolution, filter commands.ApprovalSupportFilter) ([]commands.ApprovalView, error) {
	views, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		return nil, err
	}

	approvalService := approvalsvc.Service{Store: store}
	approvals := make([]commands.ApprovalView, 0, len(views))
	for _, view := range views {
		if !matchesTaskProjectionScope(view.ProjectKey, view.TaskScope, resolved) {
			continue
		}
		detail, err := approvalService.Detail(ctx, view.ApprovalID)
		if err != nil {
			return nil, err
		}
		resolverSupport := string(detail.ResolverSupport)
		if !filter.Matches(resolverSupport) {
			continue
		}
		approvals = append(approvals, commands.ApprovalView{
			ApprovalID:      view.ApprovalID,
			TaskKey:         view.TaskKey,
			RunID:           detail.Approval.RunID,
			Status:          view.Status,
			ResolverSupport: resolverSupport,
			Source:          "approval_requests",
			Risk:            "governance",
			Reason:          approvalOperatorReason(view.Status, resolverSupport),
			AllowedActions:  approvalOperatorAllowedActions(view.Status, resolverSupport),
			NextSteps:       approvalOperatorNextSteps(view.ApprovalID, view.Status, resolverSupport),
			OnApprove:       approvalOperatorOnApprove(resolverSupport),
		})
	}
	return approvals, nil
}

func approvalOperatorReason(status string, resolverSupport string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "pending" {
		if resolverSupport == string(approvalsvc.ResolverUnsupported) {
			return "approval_required_no_registered_resolver"
		}
		return "approval_required"
	}
	if status == "" {
		return "approval_state_unknown"
	}
	return "approval_" + status
}

func approvalOperatorAllowedActions(status string, resolverSupport string) []string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "pending" {
		return []string{"inspect"}
	}
	if resolverSupport == string(approvalsvc.ResolverSupported) {
		return []string{"approve", "deny"}
	}
	return []string{"inspect"}
}

func approvalOperatorNextSteps(approvalID int64, status string, resolverSupport string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "pending" && resolverSupport == string(approvalsvc.ResolverSupported) {
		return fmt.Sprintf("inspect with odin approvals show %d; resolve with odin approvals resolve %d <approve|deny> <reason...>", approvalID, approvalID)
	}
	if status == "pending" {
		return fmt.Sprintf("inspect with odin approvals show %d; no supported resolver is registered", approvalID)
	}
	return fmt.Sprintf("inspect with odin approvals show %d; already %s", approvalID, status)
}

func approvalOperatorOnApprove(resolverSupport string) string {
	if resolverSupport == string(approvalsvc.ResolverSupported) {
		return "task unblocked or registered continuation starts"
	}
	return "not resolved; inspect only"
}

func listAllApprovals(ctx context.Context, store *sqlite.Store, resolved scope.Resolution) ([]commands.ApprovalView, error) {
	rows, err := store.DB().QueryContext(ctx, `
		SELECT a.id
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	approvalIDs := []int64{}
	for rows.Next() {
		var approvalID int64
		if err := rows.Scan(&approvalID); err != nil {
			rows.Close()
			return nil, err
		}
		approvalIDs = append(approvalIDs, approvalID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	approvalService := approvalsvc.Service{Store: store}
	approvals := make([]commands.ApprovalView, 0, len(approvalIDs))
	for _, approvalID := range approvalIDs {
		detail, err := approvalService.Detail(ctx, approvalID)
		if err != nil {
			return nil, err
		}
		project, err := store.GetProject(ctx, detail.Task.ProjectID)
		if err != nil {
			return nil, err
		}
		if !matchesTaskProjectionScope(project.Key, detail.Task.Scope, resolved) {
			continue
		}
		reason := strings.TrimSpace(detail.Approval.Reason)
		if reason == "" {
			reason = approvalOperatorReason(detail.Approval.Status, string(detail.ResolverSupport))
		}
		approvals = append(approvals, commands.ApprovalView{
			ApprovalID:      detail.Approval.ID,
			TaskKey:         detail.Task.Key,
			RunID:           detail.Approval.RunID,
			Status:          detail.Approval.Status,
			ResolverSupport: string(detail.ResolverSupport),
			DecisionBy:      detail.Approval.DecisionBy,
			Source:          "approval_requests",
			Risk:            "governance",
			Reason:          reason,
			AllowedActions:  approvalOperatorAllowedActions(detail.Approval.Status, string(detail.ResolverSupport)),
			NextSteps:       approvalOperatorNextSteps(detail.Approval.ID, detail.Approval.Status, string(detail.ResolverSupport)),
			OnApprove:       approvalOperatorOnApprove(string(detail.ResolverSupport)),
		})
	}
	return approvals, nil
}

func approvalRunIDLabel(runID *int64) string {
	if runID == nil {
		return "none"
	}
	return fmt.Sprintf("%d", *runID)
}

func listLogs(ctx context.Context, store *sqlite.Store, resolved scope.Resolution) ([]runtimeevents.Record, error) {
	params := sqlite.ListEventsParams{}
	if resolved.Kind == scope.ScopeProject || resolved.Kind == scope.ScopeOdinCore {
		project, err := store.GetProjectByKey(ctx, resolved.ProjectKey)
		switch err {
		case nil:
			params.ProjectID = &project.ID
		case sql.ErrNoRows:
			return nil, nil
		default:
			return nil, err
		}
	}

	records, err := store.ListEvents(ctx, params)
	if err != nil {
		return nil, err
	}

	filtered := make([]runtimeevents.Record, 0, len(records))
	for _, record := range records {
		if !matchesEventScope(record.Scope, resolved) {
			continue
		}
		filtered = append(filtered, record)
	}
	return filtered, nil
}

func matchesTaskProjectionScope(projectKey, taskScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeNewProject:
		return taskScope == string(scope.ScopeNewProject)
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	default:
		return false
	}
}

func matchesEventScope(eventScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeProject:
		return eventScope == string(scope.ScopeProject)
	case scope.ScopeOdinCore:
		return eventScope == string(scope.ScopeOdinCore) || eventScope == string(scope.ScopeProject)
	case scope.ScopeNewProject:
		return eventScope == string(scope.ScopeNewProject)
	default:
		return false
	}
}

const transitionUsage = "transition [status] | transition set <state> [allow=<csv>] [confirm] because <reason...>"

type transitionStatus struct {
	State             projects.TransitionState
	Controller        projects.TransitionController
	MutationAuthority projects.TransitionController
	OdinCanMutate     bool
	LimitedActions    []string
	Notes             string
}

type transitionSetRequest struct {
	State          projects.TransitionState
	LimitedActions []string
	Reason         string
	Confirmed      bool
}

func scopedManifest(registry projects.Registry, resolved scope.Resolution) (projects.Manifest, error) {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		manifest, ok := registry.Lookup(resolved.ProjectKey)
		if !ok {
			return projects.Manifest{}, fmt.Errorf("unknown project: %s", resolved.ProjectKey)
		}
		return manifest, nil
	default:
		return projects.Manifest{}, fmt.Errorf("transition commands require project scope")
	}
}

func currentTransitionStatus(ctx context.Context, store *sqlite.Store, manifest projects.Manifest) (transitionStatus, error) {
	project, err := store.GetProjectByKey(ctx, manifest.Key)
	if err != nil {
		if err == sql.ErrNoRows {
			return transitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return transitionStatus{}, err
	}

	record, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return transitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return transitionStatus{}, err
	}

	controller := projects.TransitionController(record.Controller)
	return transitionStatus{
		State:             projects.TransitionState(record.State),
		Controller:        controller,
		MutationAuthority: controller,
		OdinCanMutate:     controller == projects.TransitionControllerOdinOS,
		LimitedActions:    decodeCSVOrJSON(record.LimitedActionsJSON),
		Notes:             record.Notes,
	}, nil
}

func parseTransitionSetRequest(args []string) (transitionSetRequest, error) {
	if len(args) == 0 {
		return transitionSetRequest{}, fmt.Errorf("transition target state is required")
	}

	state := projects.TransitionState(strings.ToLower(args[0]))
	validState := map[projects.TransitionState]bool{
		projects.TransitionStateInventory:      true,
		projects.TransitionStateShadow:         true,
		projects.TransitionStateCompare:        true,
		projects.TransitionStateLimitedAction:  true,
		projects.TransitionStateCutover:        true,
		projects.TransitionStateDecommissioned: true,
	}
	if !validState[state] {
		return transitionSetRequest{}, fmt.Errorf("unsupported transition state: %s", args[0])
	}

	becauseIndex := -1
	for index := 1; index < len(args); index++ {
		if strings.EqualFold(args[index], "because") {
			becauseIndex = index
			break
		}
	}
	if becauseIndex == -1 || becauseIndex == len(args)-1 {
		return transitionSetRequest{}, fmt.Errorf("transition changes require a reason: use 'because <reason...>'")
	}

	request := transitionSetRequest{
		State:  state,
		Reason: strings.Join(args[becauseIndex+1:], " "),
	}
	for _, token := range args[1:becauseIndex] {
		switch {
		case strings.EqualFold(token, "confirm"):
			request.Confirmed = true
		case strings.HasPrefix(strings.ToLower(token), "allow="):
			raw := strings.TrimSpace(token[len("allow="):])
			if raw == "" {
				return transitionSetRequest{}, fmt.Errorf("limited_action requires allow=<csv>")
			}
			for _, action := range strings.Split(raw, ",") {
				action = strings.TrimSpace(action)
				if action != "" {
					request.LimitedActions = append(request.LimitedActions, action)
				}
			}
		default:
			return transitionSetRequest{}, fmt.Errorf("unknown transition option: %s", token)
		}
	}

	switch state {
	case projects.TransitionStateLimitedAction:
		if len(request.LimitedActions) == 0 {
			return transitionSetRequest{}, fmt.Errorf("limited_action requires allow=<csv>")
		}
		if !request.Confirmed {
			return transitionSetRequest{}, fmt.Errorf("limited_action requires confirm")
		}
	case projects.TransitionStateCutover, projects.TransitionStateDecommissioned:
		if !request.Confirmed {
			return transitionSetRequest{}, fmt.Errorf("%s requires confirm", state)
		}
	default:
		if len(request.LimitedActions) != 0 {
			return transitionSetRequest{}, fmt.Errorf("allow=<csv> is only valid for limited_action")
		}
	}

	return request, nil
}

func renderTransitionStatus(projectKey string, status transitionStatus) string {
	limitedActions := "none"
	if len(status.LimitedActions) > 0 {
		limitedActions = strings.Join(status.LimitedActions, ",")
	}

	if status.Notes != "" {
		return fmt.Sprintf(
			"project=%s state=%s controller=%s mutation_authority=%s odin_can_mutate=%t limited_actions=%s notes=%s",
			projectKey,
			status.State,
			status.Controller,
			status.MutationAuthority,
			status.OdinCanMutate,
			limitedActions,
			status.Notes,
		)
	}

	return fmt.Sprintf(
		"project=%s state=%s controller=%s mutation_authority=%s odin_can_mutate=%t limited_actions=%s",
		projectKey,
		status.State,
		status.Controller,
		status.MutationAuthority,
		status.OdinCanMutate,
		limitedActions,
	)
}

func decodeCSVOrJSON(raw string) []string {
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		var decoded []string
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			return decoded
		}
	}

	parts := strings.Split(raw, ",")
	decoded := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			decoded = append(decoded, part)
		}
	}
	return decoded
}

func serveLoadContext(parent context.Context) (context.Context, context.CancelFunc) {
	if errors.Is(parent.Err(), context.DeadlineExceeded) {
		return context.WithTimeout(parent, serveOperationTimeout)
	}
	if deadline, ok := parent.Deadline(); ok {
		return context.WithDeadline(context.Background(), deadline)
	}
	return context.WithCancel(context.Background())
}

func runServe(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer, now func() time.Time) error {
	operationCtx := context.WithoutCancel(ctx)
	bootID := app.BootID
	stateService := app.RuntimeState
	if bootID == "" {
		return fmt.Errorf("boot_id is required")
	}

	if cfg.Service.StartupRecovery {
		result, err := recovery.Service{Store: app.Store}.RunStartupRecovery(operationCtx)
		if err != nil {
			return recordServeStopped(operationCtx, stateService, bootID, "startup recovery failed", err)
		}
		if result.RecoveredRuns > 0 {
			if _, err := fmt.Fprintf(stdout, "startup recovery recovered %d run(s)\n", result.RecoveredRuns); err != nil {
				return recordServeStopped(operationCtx, stateService, bootID, "startup recovery output failed", err)
			}
		}
		if _, err := stateService.MarkRecovering(operationCtx, runtimestate.TransitionInput{
			BootID: bootID,
			Reason: "startup recovery complete",
		}); err != nil {
			return recordServeStopped(operationCtx, stateService, bootID, "recovering state write failed", err)
		}
	}

	logger, logCloser, err := openServiceLogger(cfg.RuntimeRoot)
	if err != nil {
		return recordServeStopped(operationCtx, stateService, bootID, "service logger failed", err)
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	var shutdownRequested atomic.Bool
	jobService := jobs.Service{
		Store:              app.Store,
		RuntimeRoot:        app.RuntimeRoot,
		Registry:           app.Registry,
		Executors:          app.Executors,
		ExecutorConfig:     app.ExecutorConfig,
		ModelRegistry:      app.ModelRegistry,
		PromptRenderer:     app.PromptRenderer,
		PromptTemplateName: app.PromptTemplateName,
		Transitions:        projects.Service{Store: app.Store},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
			Logger:       logger,
		},
		ShutdownRequested: &shutdownRequested,
		Now:               now,
	}
	followUpService := followups.Service{Store: app.Store, Now: now}
	recoveryService := recovery.Service{
		Store:             app.Store,
		RegistryRoot:      filepath.Join(app.RepoRoot, "registry"),
		ExecutorCatalog:   app.Executors,
		HealthConfig:      healthsvc.DefaultConfig(),
		ExecutorKeys:      enabledExecutorKeys(app.ExecutorConfig),
		Logger:            logger,
		ShutdownRequested: &shutdownRequested,
	}
	mediaService := newMediaService(app, cfg)
	schedulerService := supervision.Service{
		Store:             app.Store,
		Now:               now,
		ShutdownRequested: &shutdownRequested,
	}
	goalService := goalruntime.NewService(app.Store)
	socialService := socialcopilot.Service{
		Store:    app.Store,
		Registry: app.Registry,
		Now:      now,
	}
	leaseService := leases.Maintenance{
		Store: app.Store,
		Cleanup: worktrees.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
			Logger:       logger,
		},
		Now: time.Now,
	}
	loopConfig := serveLoopConfigFromContext(ctx)
	healthConfig := healthsvc.DefaultConfig()
	healthConfig.RuntimeHeartbeatTTL = runtimeHeartbeatTTL(loopConfig.healthInterval)
	var immediateNotReady atomic.Bool
	immediateNotReady.Store(true)
	healthService := newHealthService(app, healthConfig, cfg)
	healthService.ImmediateNotReady = &immediateNotReady
	metricsService := newMetricsService(app, healthConfig)
	healthDeps := healthLoopDeps{
		Store:              app.Store,
		RuntimeState:       stateService,
		Health:             healthService,
		Notifications:      runtimenotifications.Service{Store: app.Store},
		Executors:          app.Executors,
		ExecutorConfig:     app.ExecutorConfig,
		RegistryHealthy:    len(app.RegistryDiagnostics) == 0,
		ProjectionSurfaces: bootstrap.ServiceOwnedProjectionSurfaces(),
		ShutdownRequested:  &shutdownRequested,
		BootID:             bootID,
		RuntimeRoot:        cfg.RuntimeRoot,
	}
	if cfg.Service.SocialCopilot.Enabled {
		socialCtx, cancel := serveOperationContext(operationCtx)
		if err := runSocialCopilotStartupCheck(socialCtx, socialService, cfg.Service.SocialCopilot); err != nil {
			cancel()
			return recordServeStopped(operationCtx, stateService, bootID, "social copilot startup failed", err)
		}
		cancel()
	}
	listener, err := serveListen("tcp", cfg.Service.HTTPAddr)
	if err != nil {
		return recordServeStopped(operationCtx, stateService, bootID, "listener binding failed", err)
	}
	defer listener.Close()

	server := &stdhttp.Server{
		Handler: apihttp.NewCapabilitiesHandler(apihttp.CapabilitiesDependencies{
			Gateway:    newServeCapabilityGateway(app),
			AdminToken: cfg.AdminToken,
			Fallback: apihttp.NewOperationalHandler(apihttp.Dependencies{
				Health:               healthService,
				Metrics:              metricsService,
				Store:                app.Store,
				ReadModels:           app.Store.DB(),
				RegistryHealthy:      healthDeps.RegistryHealthy,
				RegistrySnapshot:     app.RegistrySnapshot,
				Registry:             app.Registry,
				Now:                  now,
				EmailActionSecret:    cfg.EmailActionSecret,
				EmailActionBaseURL:   cfg.Service.EmailActions.BaseURL,
				EmailActionRecipient: cfg.Service.EmailActions.Recipient,
				EmailActionFrom:      cfg.Service.EmailActions.From,
				EmailActionSendmail:  cfg.Service.EmailActions.SendmailPath,
				Tmux: apihttp.WorkspaceTmuxStatusProvider{
					Workspaces: coreworkspace.Service{
						Store:    app.Store,
						Registry: app.Registry,
					},
				},
				AdminToken: cfg.AdminToken,
				Admin: serveDashboardAdmin{
					ImmediateNotReady: &immediateNotReady,
					RuntimeState:      stateService,
					Jobs: jobs.Service{
						Store: app.Store,
						Now:   now,
					},
					BootID:      bootID,
					RuntimeRoot: cfg.RuntimeRoot,
					Logger:      logger,
				},
				GitHubWebhookSecret: os.Getenv("ODIN_GITHUB_WEBHOOK_SECRET"),
				GitHubIssueIngester: triggers.Service{Store: app.Store, Registry: app.Registry},
			}),
		}),
	}

	runLeaseMaintenanceCycle(operationCtx, leaseService, logger, loopConfig.leaseStaleAfter)

	var background sync.WaitGroup
	loopCount := 7
	if mediaService != nil {
		loopCount++
	}
	if cfg.Service.SocialCopilot.Enabled {
		loopCount++
	}
	background.Add(loopCount)
	loopCtx, stopLoops := context.WithCancel(context.Background())
	dispatchNudges := make(chan struct{}, 32)
	go runSchedulerLoop(loopCtx, operationCtx, &background, schedulerService, dispatchNudges, logger, loopConfig.schedulerInterval)
	go runGoalLoop(loopCtx, operationCtx, &background, goalService, logger, loopConfig.goalInterval)
	go runTaskLoop(loopCtx, operationCtx, &background, healthService, healthDeps.RegistryHealthy, jobService, dispatchNudges, logger, loopConfig.taskInterval)
	go runSelfHealLoop(loopCtx, operationCtx, &background, recoveryService, logger, loopConfig.selfHealInterval)
	go runLeaseLoop(loopCtx, operationCtx, &background, leaseService, logger, loopConfig.leaseInterval, loopConfig.leaseStaleAfter)
	go runHealthLoop(loopCtx, operationCtx, &background, healthDeps, logger, loopConfig.healthInterval)
	go runFollowUpLoop(loopCtx, &background, followUpService, logger, now)
	if mediaService != nil {
		go runMediaLoop(loopCtx, operationCtx, &background, *mediaService, logger)
	}
	if cfg.Service.SocialCopilot.Enabled {
		go runSocialCopilotLoop(loopCtx, operationCtx, &background, socialService, cfg.Service.SocialCopilot, logger)
	}
	defer func() {
		stopLoops()
		background.Wait()
	}()

	runStartupHealthSynchronously := ctx.Err() != nil
	if runStartupHealthSynchronously {
		runHealthCycle(operationCtx, healthDeps, logger)
	}
	go func() {
		startupCtx, cancel := serveStartupContext(operationCtx)
		defer cancel()
		if !runStartupHealthSynchronously {
			runHealthCycle(startupCtx, healthDeps, logger)
		}
		if _, err := runFollowUpCycle(startupCtx, followUpService, now()); err != nil {
			logBackgroundError(logger, "follow_up", err)
		}
		runGoalTickCycle(startupCtx, goalService, logger)
		if _, err := recoveryService.RunCycle(startupCtx); err != nil {
			logBackgroundError(logger, "self_heal", err)
		}
		if mediaService != nil {
			if _, err := mediaService.RunCycle(startupCtx); err != nil {
				logBackgroundError(logger, "media_supervisor", err)
			}
		}
		if err := attemptDispatchIfReady(startupCtx, healthService, healthDeps.RegistryHealthy, jobService); err != nil {
			logBackgroundError(logger, "task_runner", err)
		}
	}()

	shutdownControlCtx, cancelShutdown := context.WithCancel(context.Background())
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			reason := "shutdown requested"
			if ctx.Err() != nil {
				reason = ctx.Err().Error()
			}
			shutdownRequested.Store(true)
			stopLoops()
			immediateNotReady.Store(true)
			if err := writeReadinessFlag(cfg.RuntimeRoot, reason); err != nil {
				logBackgroundError(logger, "readiness_flag", err)
			}
			if _, err := stateService.MarkDraining(operationCtx, runtimestate.TransitionInput{
				BootID: bootID,
				Reason: reason,
			}); err != nil {
				logBackgroundError(logger, "runtime_state", err)
			}
			shutdownCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-shutdownControlCtx.Done():
		}
	}()
	defer func() {
		cancelShutdown()
		<-shutdownDone
	}()

	if _, err := fmt.Fprintf(stdout, "serving on %s\n", listener.Addr().String()); err != nil {
		return recordServeStopped(operationCtx, stateService, bootID, "stdout write failed", err)
	}

	err = server.Serve(listener)
	if errors.Is(err, stdhttp.ErrServerClosed) {
		reason := "shutdown complete"
		if ctxErr := ctx.Err(); ctxErr != nil {
			reason = ctxErr.Error()
		}
		if stopErr := recordServeStopped(operationCtx, stateService, bootID, reason, nil); stopErr != nil {
			return stopErr
		}
		return ctx.Err()
	}
	return recordServeStopped(operationCtx, stateService, bootID, "server error", err)
}

func newServeCapabilityGateway(app bootstrap.App) *capabilities.Gateway {
	definitions := catalog.BuiltinDefinitions()
	snapshot := newCapabilitySnapshot(app, definitions, "serve-capabilities")
	service, err := capabilities.NewService(snapshot)
	if err != nil {
		return nil
	}

	var runLookup capabilities.RunLookup
	if app.Store != nil {
		runLookup = runs.Service{
			DB:    app.Store.DB(),
			Store: app.Store,
		}
	}

	return capabilities.NewGateway(service, func(ctx context.Context, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
		if descriptor.Kind == registry.KindTool {
			return capabilities.InvokeBuiltinToolCapability(ctx, definitions, request, descriptor)
		}
		return servedCommandService{app: app}.Execute(ctx, request)
	}, runLookup)
}

func newReadOnlyCapabilityGateway(app bootstrap.App) *capabilities.Gateway {
	definitions := catalog.BuiltinDefinitions()
	snapshot := newCapabilitySnapshot(app, definitions, capabilityCommandSource)
	service, err := capabilities.NewService(snapshot)
	if err != nil {
		return nil
	}

	return capabilities.NewGateway(service, nil, nil)
}

func newCapabilitySnapshot(app bootstrap.App, definitions map[string]catalog.ToolDefinition, digest string) capabilities.Snapshot {
	snapshot := capabilities.FromRegistrySnapshot(digest, app.RegistrySnapshot)
	return capabilities.WithBuiltinToolDescriptors(snapshot, definitions)
}

func recordServeStopped(ctx context.Context, service runtimestate.Service, bootID string, reason string, cause error) error {
	if bootID == "" || service.Store == nil {
		return cause
	}

	errorText := ""
	if cause != nil {
		errorText = cause.Error()
	}

	if _, err := service.MarkStopped(ctx, runtimestate.TransitionInput{
		BootID: bootID,
		Reason: reason,
		Error:  errorText,
	}); err != nil {
		if cause != nil {
			return errors.Join(cause, err)
		}
		return err
	}

	return cause
}

func openServiceLogger(runtimeRoot string) (*logs.Logger, io.Closer, error) {
	logDir := filepath.Join(runtimeRoot, "runs", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, err
	}

	file, err := os.OpenFile(filepath.Join(logDir, "odin-service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}

	return &logs.Logger{
		Writer: file,
		Now:    time.Now,
	}, file, nil
}

func runTaskLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, healthService healthsvc.Service, registryHealthy bool, service jobs.Service, nudges <-chan struct{}, logger *logs.Logger, interval time.Duration) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-nudges:
			if err := attemptDispatchIfReady(operationCtx, healthService, registryHealthy, service); err != nil {
				logBackgroundError(logger, "task_runner", err)
			}
		case <-ticker.C:
			if err := attemptDispatchIfReady(operationCtx, healthService, registryHealthy, service); err != nil {
				logBackgroundError(logger, "task_runner", err)
			}
		}
	}
}

func runSchedulerLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service supervision.Service, nudges chan<- struct{}, logger *logs.Logger, interval time.Duration) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if result, err := service.Tick(operationCtx); err != nil {
				logBackgroundError(logger, "scheduler", err)
			} else if (result.Promoted > 0 || result.Reconciled > 0) && logger != nil {
				for promoted := 0; promoted < result.Promoted; promoted++ {
					select {
					case nudges <- struct{}{}:
					default:
					}
				}
				_ = logger.Log(logs.Record{
					Level:         logs.LevelInfo,
					Component:     "scheduler",
					Message:       "scheduler dispatched delayed task candidates",
					CorrelationID: "scheduler",
					Scope:         "global",
					Fields: map[string]any{
						"promoted":   result.Promoted,
						"reconciled": result.Reconciled,
					},
				})
			}
		}
	}
}

func runGoalLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service goalruntime.Service, logger *logs.Logger, interval time.Duration) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			goalCtx, cancel := serveOperationContext(operationCtx)
			runGoalTickCycle(goalCtx, service, logger)
			cancel()
		}
	}
}

func runGoalTickCycle(ctx context.Context, service goalruntime.Service, logger *logs.Logger) {
	result, err := service.Tick(ctx)
	if err != nil {
		logBackgroundError(logger, "goal_runner", err)
		return
	}
	if logger == nil || result.Observed == 0 {
		return
	}
	_ = logger.Log(logs.Record{
		Level:         logs.LevelInfo,
		Component:     "goal_runner",
		Message:       "goal runner tick completed",
		CorrelationID: "goal_runner",
		Scope:         "global",
		Fields: map[string]any{
			"observed": result.Observed,
			"started":  result.Started,
			"blocked":  result.Blocked,
			"skipped":  result.Skipped,
		},
	})
}

func runSelfHealLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service recovery.Service, logger *logs.Logger, interval time.Duration) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := service.RunCycle(operationCtx); err != nil {
				logBackgroundError(logger, "self_heal", err)
			}
		}
	}
}

func runLeaseLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service leases.Maintenance, logger *logs.Logger, interval time.Duration, staleAfter time.Duration) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runLeaseMaintenanceCycle(operationCtx, service, logger, staleAfter)
		}
	}
}

func runHealthLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, deps healthLoopDeps, logger *logs.Logger, interval time.Duration) {
	defer wg.Done()

	firstInterval := interval
	if serveInitialHealthRetry > 0 && serveInitialHealthRetry < firstInterval {
		firstInterval = serveInitialHealthRetry
	}
	timer := time.NewTimer(firstInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			healthCtx, cancel := serveOperationContext(operationCtx)
			runHealthCycle(healthCtx, deps, logger)
			cancel()
			timer.Reset(interval)
		}
	}
}

func runLeaseMaintenanceCycle(ctx context.Context, service leases.Maintenance, logger *logs.Logger, staleAfter time.Duration) {
	if _, err := service.CleanupExpired(ctx, staleAfter); err != nil {
		logBackgroundError(logger, "worktree_lease_cleanup", err)
	}
	if _, err := service.HeartbeatActive(ctx); err != nil {
		logBackgroundError(logger, "worktree_lease_heartbeat", err)
	}
}

func runHealthCycle(ctx context.Context, deps healthLoopDeps, logger *logs.Logger) {
	if err := deps.Health.SampleConfiguredExecutors(ctx, deps.Store, deps.ExecutorConfig, deps.Executors, "serve.health_loop"); err != nil {
		setImmediateNotReady(deps.Health, true)
		writeNotReadyFlag(logger, deps.RuntimeRoot, "executor health sampling failed")
		logBackgroundError(logger, "executor_health", err)
		markRuntimeDegraded(ctx, deps, logger, "executor health sampling failed", err)
		return
	}
	if err := deps.Health.RefreshProjectionFreshness(ctx, deps.Store, deps.ProjectionSurfaces, "serve.health_loop"); err != nil {
		setImmediateNotReady(deps.Health, true)
		writeNotReadyFlag(logger, deps.RuntimeRoot, "projection freshness refresh failed")
		logBackgroundError(logger, "projection_freshness", err)
		markRuntimeDegraded(ctx, deps, logger, "projection freshness refresh failed", err)
		return
	}
	if _, err := deps.RuntimeState.Heartbeat(ctx, runtimestate.HeartbeatInput{BootID: deps.BootID}); err != nil {
		setImmediateNotReady(deps.Health, true)
		writeNotReadyFlag(logger, deps.RuntimeRoot, "runtime heartbeat failed")
		logBackgroundError(logger, "runtime_state", err)
		markRuntimeDegraded(ctx, deps, logger, "runtime heartbeat failed", err)
		return
	}

	report, safeToDispatch, err := deps.Health.DispatchReport(ctx, deps.RegistryHealthy)
	if err != nil {
		setImmediateNotReady(deps.Health, true)
		writeNotReadyFlag(logger, deps.RuntimeRoot, "health evaluation failed")
		logBackgroundError(logger, "health", err)
		markRuntimeDegraded(ctx, deps, logger, "health evaluation failed", err)
		return
	}

	state, err := deps.Store.GetRuntimeState(ctx)
	if err != nil {
		setImmediateNotReady(deps.Health, true)
		logBackgroundError(logger, "runtime_state", err)
		return
	}
	if state.Status == "draining" || state.Status == "stopped" {
		setImmediateNotReady(deps.Health, true)
		preserveNotReadyFlag(logger, deps.RuntimeRoot, state.Status)
		return
	}
	if deps.ShutdownRequested != nil && deps.ShutdownRequested.Load() {
		setImmediateNotReady(deps.Health, true)
		preserveNotReadyFlag(logger, deps.RuntimeRoot, "shutdown requested")
		return
	}

	if safeToDispatch {
		if state.Status == "booting" || state.Status == "recovering" || state.Status == "degraded" {
			if _, err := deps.RuntimeState.MarkReady(ctx, runtimestate.TransitionInput{
				BootID: deps.BootID,
				Reason: "health checks passed",
			}); err != nil {
				setImmediateNotReady(deps.Health, true)
				logBackgroundError(logger, "runtime_state", err)
				return
			}
		}
		setImmediateNotReady(deps.Health, false)
		clearNotReadyFlag(logger, deps.RuntimeRoot)
		routePendingNotifications(ctx, deps, logger)
		return
	}

	setImmediateNotReady(deps.Health, true)
	writeNotReadyFlag(logger, deps.RuntimeRoot, fmt.Sprintf("dispatch paused: %s", report.Status))
	markRuntimeDegraded(ctx, deps, logger, fmt.Sprintf("dispatch paused: %s", report.Status), nil)
	routePendingNotifications(ctx, deps, logger)
}

func routePendingNotifications(ctx context.Context, deps healthLoopDeps, logger *logs.Logger) {
	if deps.Notifications == nil {
		return
	}
	if _, err := deps.Notifications.RoutePendingEvents(ctx); err != nil {
		logBackgroundError(logger, "notifications", err)
	}
}

func markRuntimeDegraded(ctx context.Context, deps healthLoopDeps, logger *logs.Logger, reason string, cause error) {
	state, err := deps.Store.GetRuntimeState(ctx)
	if err != nil {
		logBackgroundError(logger, "runtime_state", err)
		return
	}
	if state.Status == "degraded" || state.Status == "draining" || state.Status == "stopped" {
		return
	}

	errorText := ""
	if cause != nil {
		errorText = cause.Error()
	}
	if _, err := deps.RuntimeState.MarkDegraded(ctx, runtimestate.TransitionInput{
		BootID: deps.BootID,
		Reason: reason,
		Error:  errorText,
	}); err != nil {
		logBackgroundError(logger, "runtime_state", err)
	}
}

func attemptDispatchIfReady(ctx context.Context, healthService healthsvc.Service, registryHealthy bool, service jobs.Service) error {
	_, ready, err := healthService.Readiness(ctx, registryHealthy)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	outcome, err := service.ExecuteNextDispatchedRun(ctx)
	if err != nil {
		return err
	}
	if outcome.Executed || outcome.Reason == "run_already_executing" || outcome.Reason == "stale_executing_run_recovered" {
		return nil
	}
	return service.ExecuteNextQueued(ctx)
}

func enabledExecutorKeys(config executorrouter.Config) []string {
	keys := make([]string, 0, len(config.Executors))
	for _, executor := range config.Executors {
		if executor.Enabled {
			keys = append(keys, executor.Key)
		}
	}
	return keys
}
func runFollowUpLoop(ctx context.Context, wg *sync.WaitGroup, followUpService followups.Service, logger *logs.Logger, now func() time.Time) {
	defer wg.Done()

	logBackgroundEvent(logger, logs.LevelInfo, "follow_up", "follow-up loop started", map[string]any{
		"interval_ms": serveFollowUpLoopInterval.Milliseconds(),
	})

	ticker := time.NewTicker(serveFollowUpLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			followUpCtx, cancel := serveOperationContext(ctx)
			if _, err := runFollowUpCycle(followUpCtx, followUpService, now()); err != nil {
				cancel()
				logBackgroundError(logger, "follow_up", err)
			}
			cancel()
		}
	}
}

func runFollowUpCycle(ctx context.Context, followUpService followups.Service, now time.Time) (int, error) {
	if followUpService.Store == nil {
		return 0, fmt.Errorf("follow-up store is required")
	}

	workspace, err := workspaces.Service{Store: followUpService.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return 0, err
	}

	obligations, err := followUpService.ListByWorkspace(ctx, workspace.ID)
	if err != nil {
		return 0, err
	}

	mutated := 0
	for _, obligation := range obligations {
		if obligation.InitiativeID != nil {
			initiative, err := followUpService.Store.GetInitiativeByID(ctx, *obligation.InitiativeID)
			if err != nil {
				return mutated, err
			}
			if initiative.Status == "paused" || initiative.Status == "archived" {
				if obligation.Status != followups.StatusPaused {
					if _, err := followUpService.PauseForInitiativeStatus(ctx, workspace.ID, obligation.ID, initiative.Status); err != nil {
						return mutated, err
					}
					mutated++
				}
				continue
			}
		}

		if obligation.DueStatus(now) != followups.StatusDue {
			continue
		}

		taskKey := followUpTaskKey(obligation)
		_, err := followUpService.Materialize(ctx, followups.MaterializeParams{
			ObligationID: obligation.ID,
			TaskKey:      taskKey,
			Title:        obligation.Title,
			Scope:        "project",
			RequestedBy:  "operator",
			TaskStatus:   "blocked",
		})
		if err != nil {
			return mutated, err
		}
		mutated++
	}

	return mutated, nil
}

func followUpTaskKey(obligation followups.FollowUpObligation) string {
	return fmt.Sprintf("followup-%d-%s", obligation.ID, obligation.NextDueAt.UTC().Format("20060102-150405Z0700"))
}

func runSocialCopilotStartupCheck(ctx context.Context, service socialcopilot.Service, cfg appconfig.SocialCopilotSettings) error {
	cadence := time.Duration(cfg.CadenceSeconds) * time.Second
	if _, err := service.EnsurePollingJob(ctx, socialcopilot.EnsureJobParams{
		WorkflowKey: cfg.WorkflowKey,
		Cadence:     cadence,
	}); err != nil {
		return err
	}
	_, err := service.Wake(ctx, socialcopilot.WakeParams{
		WorkflowKey: cfg.WorkflowKey,
		Trigger:     "serve",
		Reason:      "startup-due-check",
	})
	return err
}

func runSocialCopilotLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service socialcopilot.Service, cfg appconfig.SocialCopilotSettings, logger *logs.Logger) {
	defer wg.Done()

	interval := time.Duration(cfg.CadenceSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	logBackgroundEvent(logger, logs.LevelInfo, "social_copilot", "social copilot loop started", map[string]any{
		"interval_ms": interval.Milliseconds(),
	})

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			socialCtx, cancel := serveOperationContext(operationCtx)
			if err := runSocialCopilotStartupCheck(socialCtx, service, cfg); err != nil {
				logBackgroundError(logger, "social_copilot", err)
			}
			cancel()
		}
	}
}

func serveOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, serveOperationTimeout)
}

func serveTaskContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func serveStartupContext(parent context.Context) (context.Context, context.CancelFunc) {
	if errors.Is(parent.Err(), context.DeadlineExceeded) {
		return context.WithTimeout(parent, serveOperationTimeout)
	}
	if deadline, ok := parent.Deadline(); ok {
		timeoutDeadline := time.Now().Add(serveOperationTimeout)
		if deadline.Before(timeoutDeadline) {
			return context.WithDeadline(parent, deadline)
		}
		return context.WithTimeout(parent, serveOperationTimeout)
	}
	return context.WithTimeout(parent, serveOperationTimeout)
}

func newHealthService(app bootstrap.App, config healthsvc.Config, cfg appconfig.Config) healthsvc.Service {
	service := healthsvc.Service{
		DB:           app.Store.DB(),
		Config:       config,
		ExecutorKeys: enabledExecutorKeys(app.ExecutorConfig),
	}
	if cfg.Media != nil {
		service.Media = &healthsvc.MediaChecks{
			Config:       cfg.Media,
			ProbeCommand: os.Getenv("ODIN_MEDIA_PROBE_COMMAND"),
		}
	}
	return service
}

func newMediaService(app bootstrap.App, cfg appconfig.Config) *mediasvc.Service {
	if cfg.Media == nil {
		return nil
	}

	systemProject, _ := app.Registry.SystemProject()
	return &mediasvc.Service{
		Store:         app.Store,
		Config:        cfg.Media,
		RuntimeRoot:   cfg.RuntimeRoot,
		SystemProject: systemProject,
		Checker: healthsvc.MediaChecks{
			Config:       cfg.Media,
			ProbeCommand: os.Getenv("ODIN_MEDIA_PROBE_COMMAND"),
		},
		Now: time.Now,
	}
}

func runMediaLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service mediasvc.Service, logger *logs.Logger) {
	defer wg.Done()

	ticker := time.NewTicker(serveMediaLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := service.RunCycle(operationCtx); err != nil {
				logBackgroundError(logger, "media_supervisor", err)
			}
		}
	}
}

func newMetricsService(app bootstrap.App, config healthsvc.Config) metricsvc.Service {
	return metricsvc.Service{
		DB: app.Store.DB(),
		Config: metricsvc.Config{
			ExecutorFreshnessTTL:   config.ExecutorFreshnessTTL,
			SourceFreshnessTTL:     config.SourceFreshnessTTL,
			ProjectionFreshnessTTL: config.ProjectionFreshnessTTL,
		},
		ExecutorKeys: enabledExecutorKeys(app.ExecutorConfig),
	}
}

func runtimeHeartbeatTTL(interval time.Duration) time.Duration {
	if interval <= 0 {
		return healthsvc.DefaultConfig().RuntimeHeartbeatTTL
	}
	ttl := interval * 2
	if ttl < time.Second {
		return time.Second
	}
	return ttl
}

func setImmediateNotReady(service healthsvc.Service, value bool) {
	if service.ImmediateNotReady != nil {
		service.ImmediateNotReady.Store(value)
	}
}

func writeNotReadyFlag(logger *logs.Logger, runtimeRoot string, reason string) {
	if err := writeReadinessFlag(runtimeRoot, reason); err != nil {
		logBackgroundError(logger, "readiness_flag", err)
	}
}

func clearNotReadyFlag(logger *logs.Logger, runtimeRoot string) {
	if err := clearReadinessFlag(runtimeRoot); err != nil {
		logBackgroundError(logger, "readiness_flag", err)
	}
}

func preserveNotReadyFlag(logger *logs.Logger, runtimeRoot string, reason string) {
	existing, active, err := readReadinessFlag(runtimeRoot)
	if err != nil {
		logBackgroundError(logger, "readiness_flag", err)
		return
	}
	if active && strings.TrimSpace(existing) != "" {
		return
	}
	writeNotReadyFlag(logger, runtimeRoot, reason)
}

func readReadinessFlag(runtimeRoot string) (string, bool, error) {
	path := readinessFlagPath(runtimeRoot)
	content, err := os.ReadFile(path)
	if err == nil {
		reason := strings.TrimSpace(string(content))
		if reason == "" {
			reason = "runtime not ready"
		}
		return reason, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	return "", false, err
}

func writeReadinessFlag(runtimeRoot string, reason string) error {
	if runtimeRoot == "" {
		return nil
	}
	if reason == "" {
		reason = "runtime not ready"
	}
	path := readinessFlagPath(runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(reason+"\n"), 0o644)
}

func clearReadinessFlag(runtimeRoot string) error {
	if runtimeRoot == "" {
		return nil
	}
	err := os.Remove(readinessFlagPath(runtimeRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func readinessFlagPath(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "state", "cache", "readiness.flag")
}

func logBackgroundError(logger *logs.Logger, component string, err error) {
	if logger == nil {
		return
	}
	logBackgroundEvent(logger, logs.LevelError, component, "background loop error", map[string]any{
		"error": err.Error(),
	})
}

func logBackgroundEvent(logger *logs.Logger, level logs.Level, component, message string, fields map[string]any) {
	if logger == nil {
		return
	}
	_ = logger.Log(logs.Record{
		Level:         level,
		Component:     component,
		Message:       message,
		CorrelationID: component,
		Scope:         "global",
		Fields:        fields,
	})
}

func runBackup(ctx context.Context, service appbackup.Service, args []string, stdout io.Writer) error {
	if isHelpArgs(args) {
		_, err := fmt.Fprintln(stdout, backupUsage)
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf(backupUsage)
	}
	if err := service.CreateArchive(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "backup written to %s\n", args[0])
	return err
}

func runRestore(ctx context.Context, service appbackup.Service, args []string, stdout io.Writer) error {
	if isHelpArgs(args) {
		_, err := fmt.Fprintln(stdout, restoreUsage)
		return err
	}
	if len(args) != 2 {
		return fmt.Errorf(restoreUsage)
	}
	if err := service.RestoreArchive(ctx, args[0], args[1]); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "restored backup into %s\n", args[1])
	return err
}

func runVerifyBackup(ctx context.Context, service appbackup.Service, args []string, stdout io.Writer) error {
	if isHelpArgs(args) {
		_, err := fmt.Fprintln(stdout, verifyBackupUsage)
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf(verifyBackupUsage)
	}
	if err := service.VerifyArchive(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout, "backup verified")
	return err
}

func isHelpArgs(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

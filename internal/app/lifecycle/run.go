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
	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/e2e"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/checkpoints"
	conversationsvc "odin-os/internal/runtime/conversation"
	delegationsvc "odin-os/internal/runtime/delegations"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	mediasvc "odin-os/internal/runtime/media"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/runtime/runs"
	"odin-os/internal/runtime/socialcopilot"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

const rootUsageBanner = "Usage: odin <command> [args]\n\nCommands: help repl overview tui doctor healthcheck serve backup restore verify-backup status legacy project workspace work scope jobs runs approvals review intake agenda logs knowledge task initiative companion profile followup trigger transition skills e2e"

var (
	serveTaskLoopInterval     = 1 * time.Second
	serveFollowUpLoopInterval = 1 * time.Second
	serveMediaLoopInterval    = 30 * time.Second
	serveSelfHealLoopInterval = 30 * time.Second
	serveMetricsLoopInterval  = 1 * time.Minute
	serveOperationTimeout     = 30 * time.Second
	serveHealthConfig         = healthsvc.DefaultConfig()
	serveListen               = net.Listen
	runTUI                    = tui.Run
)

type serveLoopConfig struct {
	taskInterval      time.Duration
	schedulerInterval time.Duration
	selfHealInterval  time.Duration
	leaseInterval     time.Duration
	leaseStaleAfter   time.Duration
	healthInterval    time.Duration
}

type serveLoopConfigKey struct{}

var defaultServeLoopConfig = serveLoopConfig{
	taskInterval:      1 * time.Second,
	schedulerInterval: 5 * time.Second,
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
	Executors          map[string]contract.Executor
	ExecutorConfig     executorrouter.Config
	RegistryHealthy    bool
	ProjectionSurfaces []string
	ShutdownRequested  *atomic.Bool
	BootID             string
	RuntimeRoot        string
}

type serveDashboardAdmin struct {
	ImmediateNotReady *atomic.Bool
	RuntimeState      runtimestate.Service
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

func (admin serveDashboardAdmin) PauseIssue(_ context.Context, issueID int64) error {
	logBackgroundEvent(admin.Logger, logs.LevelWarn, "dashboard_admin", "issue pause requested but not implemented", map[string]any{
		"external_issue_id": issueID,
	})
	return apihttp.ErrAdminActionNotImplemented
}

func (admin serveDashboardAdmin) ResumeIssue(_ context.Context, issueID int64) error {
	logBackgroundEvent(admin.Logger, logs.LevelWarn, "dashboard_admin", "issue resume requested but not implemented", map[string]any{
		"external_issue_id": issueID,
	})
	return apihttp.ErrAdminActionNotImplemented
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
	case "tui":
		return runTUI(ctx, args[1:], stdout)
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
				Store:          app.Store,
				RuntimeRoot:    app.RuntimeRoot,
				Registry:       app.Registry,
				Executors:      app.Executors,
				ExecutorConfig: app.ExecutorConfig,
				Transitions:    projects.Service{Store: app.Store},
				Now:            time.Now,
			},
		})
	case "scope":
		return runScope(app, args[1:], stdout)
	case "jobs":
		return runJobs(ctx, app, args[1:], stdout)
	case "runs":
		return runRuns(ctx, app, args[1:], stdout)
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
	case "skills":
		return runSkills(ctx, app, args[1:], stdout)
	case "doctor":
		return runDoctor(ctx, app, cfg, args[1:], stdout)
	case "healthcheck":
		return runHealthcheck(ctx, app, cfg, stdout)
	case "serve":
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

	view, err := clioverview.Service{
		Store:            app.Store,
		Registry:         app.Registry,
		RegistrySnapshot: app.RegistrySnapshot,
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

	report, err := newHealthService(app, healthsvc.DefaultConfig(), cfg).Doctor(ctx, len(app.RegistryDiagnostics) == 0)
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
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin runs [--json]")
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
		if jsonOutput {
			return commands.WriteJSON(stdout, struct {
				ID              int64  `json:"id"`
				Status          string `json:"status"`
				TaskID          int64  `json:"task_id"`
				TaskKey         string `json:"task_key"`
				TaskStatus      string `json:"task_status"`
				RunID           *int64 `json:"run_id,omitempty"`
				DecisionBy      string `json:"decision_by,omitempty"`
				Reason          string `json:"reason,omitempty"`
				ResolverSupport string `json:"resolver_support"`
			}{
				ID:              detail.Approval.ID,
				Status:          detail.Approval.Status,
				TaskID:          detail.Task.ID,
				TaskKey:         detail.Task.Key,
				TaskStatus:      detail.Task.Status,
				RunID:           detail.Approval.RunID,
				DecisionBy:      detail.Approval.DecisionBy,
				Reason:          detail.Approval.Reason,
				ResolverSupport: string(detail.ResolverSupport),
			})
		}
		_, err = fmt.Fprintf(
			stdout,
			"approval=%d task=%s run=%s status=%s task_status=%s resolver=%s\n",
			detail.Approval.ID,
			detail.Task.Key,
			approvalRunIDLabel(detail.Approval.RunID),
			detail.Approval.Status,
			detail.Task.Status,
			detail.ResolverSupport,
		)
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
		return commands.WriteJSON(stdout, commands.ApprovalsView{Approvals: approvals})
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(stdout, "no approvals waiting")
		return err
	}
	for _, approval := range approvals {
		if _, err := fmt.Fprintf(
			stdout,
			"approval=%d task=%s run=%s status=%s resolver=%s\n",
			approval.ApprovalID,
			approval.TaskKey,
			approvalRunIDLabel(approval.RunID),
			approval.Status,
			approval.ResolverSupport,
		); err != nil {
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
		Store:       app.Store,
		Registry:    app.Registry,
		Transitions: projects.Service{Store: app.Store},
		Now:         time.Now,
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
	ID                     int64           `json:"id"`
	Key                    string          `json:"key"`
	Status                 string          `json:"status"`
	Source                 string          `json:"source"`
	IntakeType             string          `json:"intake_type"`
	DedupKey               string          `json:"dedup_key"`
	RequestedBy            string          `json:"requested_by"`
	CreatedAt              string          `json:"created_at"`
	PayloadPolicy          string          `json:"payload_policy"`
	ProjectKey             string          `json:"project_key,omitempty"`
	Title                  string          `json:"title,omitempty"`
	Summary                string          `json:"summary,omitempty"`
	CanonicalIntakeKey     string          `json:"canonical_intake_key,omitempty"`
	SuppressionReason      string          `json:"suppression_reason,omitempty"`
	AcceptedWorkItemID     int64           `json:"accepted_work_item_id,omitempty"`
	AcceptedWorkItemKey    string          `json:"accepted_work_item_key,omitempty"`
	AcceptedWorkItemStatus string          `json:"accepted_work_item_status,omitempty"`
	ApprovalRequired       bool            `json:"approval_required,omitempty"`
	BlockedPendingApproval bool            `json:"blocked_pending_approval,omitempty"`
	PolicyReason           string          `json:"policy_reason,omitempty"`
	PolicyDecision         string          `json:"policy_decision,omitempty"`
	Payload                json.RawMessage `json:"payload,omitempty"`
	Processing             json.RawMessage `json:"processing,omitempty"`
}

type rawIntakeItemEnvelope struct {
	IntakeItem rawIntakeItemView `json:"intake_item"`
}

type rawIntakeItemListView struct {
	IntakeItems []rawIntakeItemView `json:"intake_items"`
}

type intakeProcessView struct {
	IntakeItem     rawIntakeItemView `json:"intake_item"`
	Outcome        string            `json:"outcome"`
	Classification string            `json:"classification"`
	DedupeResult   string            `json:"dedupe_result"`
	RoutedOutcome  string            `json:"routed_outcome"`
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
	Clarification     *intakeClarification  `json:"clarification,omitempty"`
	Review            *intakeReviewDecision `json:"review,omitempty"`
}

type intakeClassification struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

type intakeDedupeReview struct {
	Result             string `json:"result"`
	CanonicalIntakeKey string `json:"canonical_intake_key,omitempty"`
}

type intakeRoutingResult struct {
	Outcome               string `json:"outcome"`
	ProjectKey            string `json:"project_key,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
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
}

type intakeReviewWork struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
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
	notesJSON, err := json.Marshal(outcome.notes)
	if err != nil {
		return err
	}
	processed, err := app.Store.ProcessIntakeItem(ctx, sqlite.ProcessIntakeItemParams{
		ID:                    item.ID,
		Status:                outcome.status,
		Summary:               outcome.summary,
		CanonicalIntakeItemID: outcome.canonicalIntakeItemID,
		SuppressionReason:     outcome.suppressionReason,
		RoutingNotes:          string(notesJSON),
		Events:                outcome.events,
	})
	if err != nil {
		return err
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
	route := deriveIntakeRoute(item)
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
			Result:             "duplicate_linked",
			CanonicalIntakeKey: rawIntakeKey(*duplicate),
		}
		notes.Routing = intakeRoutingResult{Outcome: "duplicate_linked_or_suppressed", ProjectKey: item.ScopeKey}
		outcome := intakeProcessOutcome{
			status:                "duplicate_linked_or_suppressed",
			summary:               "Duplicate raw intake linked to " + rawIntakeKey(*duplicate),
			canonicalIntakeItemID: duplicate,
			suppressionReason:     "duplicate_dedupe_key",
			notes:                 notes,
		}
		outcome.events = intakeProcessingEvents(item.ID, outcome.status, notes, duplicate)
		return outcome, nil
	}

	if notes.Classification.Result == "ambiguous" {
		notes.Routing = intakeRoutingResult{Outcome: "needs_clarification", ProjectKey: item.ScopeKey}
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
		outcome.events = intakeProcessingEvents(item.ID, outcome.status, notes, nil)
		return outcome, nil
	}

	route := deriveIntakeRoute(item)
	notes.Routing = intakeRoutingResult{
		Outcome:               route.RoutingOutcome,
		ProjectKey:            item.ScopeKey,
		ExecutionIntent:       route.ExecutionIntent,
		ExecutionIntentSource: route.ExecutionIntentSource,
	}
	notes.DraftArtifact = &intakeDraftArtifact{
		Kind:                  route.DraftArtifactKind,
		Title:                 item.Subject,
		ReviewState:           "review_required",
		ExecutionIntent:       route.ExecutionIntent,
		ExecutionIntentSource: route.ExecutionIntentSource,
	}
	outcome := intakeProcessOutcome{
		status:  "review_required",
		summary: route.DraftArtifactKind + " prepared for human review; no work item created",
		notes:   notes,
	}
	outcome.events = intakeProcessingEvents(item.ID, outcome.status, notes, nil)
	return outcome, nil
}

func deriveIntakeRoute(item sqlite.IntakeItem) intakeDerivedRoute {
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
		return intakeClassification{Result: "ambiguous", Reason: "intake title is too vague to draft reviewable work"}
	}
	return intakeClassification{Result: "actionable_request", Reason: "intake has enough subject detail for a draft review artifact"}
}

func findCanonicalDuplicate(ctx context.Context, store *sqlite.Store, item sqlite.IntakeItem) (*int64, error) {
	if strings.TrimSpace(item.DedupeKey) == "" {
		return nil, nil
	}
	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: item.WorkspaceID})
	if err != nil {
		return nil, err
	}
	for _, candidate := range items {
		if candidate.ID >= item.ID {
			continue
		}
		if candidate.DedupeKey == item.DedupeKey {
			id := candidate.ID
			return &id, nil
		}
	}
	return nil, nil
}

func intakeProcessingEvents(itemID int64, status string, notes intakeProcessingNotes, canonical *int64) []sqlite.IntakeItemProcessingEvent {
	events := []sqlite.IntakeItemProcessingEvent{
		{
			Type:   runtimeevents.EventIntakeProcessingStarted,
			Stage:  "processing_started",
			Result: "started",
		},
		{
			Type:   runtimeevents.EventIntakeClassified,
			Stage:  "classification",
			Result: notes.Classification.Result,
		},
		{
			Type:   runtimeevents.EventIntakeDedupeReviewed,
			Stage:  "dedupe",
			Result: notes.Dedupe.Result,
		},
		{
			Type:   runtimeevents.EventIntakeRouted,
			Stage:  "routing",
			Result: notes.Routing.Outcome,
			Payload: runtimeevents.IntakeProcessingPayload{
				IntakeItemID:          itemID,
				Status:                status,
				Stage:                 "routing",
				RoutedOutcome:         notes.Routing.Outcome,
				ExecutionIntent:       notes.Routing.ExecutionIntent,
				ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
			},
		},
	}
	switch {
	case notes.DraftArtifact != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:   runtimeevents.EventIntakeDraftArtifactCreated,
			Stage:  "draft_artifact",
			Result: notes.DraftArtifact.Kind,
			Payload: runtimeevents.IntakeProcessingPayload{
				IntakeItemID:          itemID,
				Status:                status,
				Stage:                 "draft_artifact",
				RoutedOutcome:         notes.Routing.Outcome,
				ExecutionIntent:       notes.Routing.ExecutionIntent,
				ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
				DraftArtifactKind:     notes.DraftArtifact.Kind,
			},
		})
	case notes.Clarification != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:   runtimeevents.EventIntakeClarificationNeeded,
			Stage:  "clarification",
			Result: notes.Clarification.State,
			Payload: runtimeevents.IntakeProcessingPayload{
				IntakeItemID:          itemID,
				Status:                status,
				Stage:                 "clarification",
				RoutedOutcome:         notes.Routing.Outcome,
				ExecutionIntent:       notes.Routing.ExecutionIntent,
				ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
				ClarificationState:    notes.Clarification.State,
			},
		})
	case canonical != nil:
		events = append(events, sqlite.IntakeItemProcessingEvent{
			Type:   runtimeevents.EventIntakeDuplicateLinkedOrSuppressed,
			Stage:  "duplicate",
			Result: notes.Dedupe.Result,
			Payload: runtimeevents.IntakeProcessingPayload{
				IntakeItemID:          itemID,
				Status:                status,
				Stage:                 "duplicate",
				RoutedOutcome:         notes.Routing.Outcome,
				ExecutionIntent:       notes.Routing.ExecutionIntent,
				ExecutionIntentSource: notes.Routing.ExecutionIntentSource,
				CanonicalIntakeID:     canonical,
			},
		})
	}
	return events
}

func rawIntakeSourceFactsJSON(command commands.IntakeCommand, payloadJSON string) (string, error) {
	var payload json.RawMessage
	if strings.TrimSpace(payloadJSON) == "" {
		payloadJSON = "{}"
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
		CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339Nano),
		PayloadPolicy: rawIntakePayloadPolicy,
		Title:         item.Subject,
		Summary:       item.Summary,
	}
	if item.Scope == "project" {
		view.ProjectKey = item.ScopeKey
	}
	if item.CanonicalIntakeItemID != nil {
		view.CanonicalIntakeKey = rawIntakeKey(*item.CanonicalIntakeItemID)
	}
	view.SuppressionReason = item.SuppressionReason
	if strings.TrimSpace(item.RoutingNotes) != "" && json.Valid([]byte(item.RoutingNotes)) {
		view.Processing = json.RawMessage(item.RoutingNotes)
		var notes intakeProcessingNotes
		if err := json.Unmarshal([]byte(item.RoutingNotes), &notes); err == nil && notes.Review != nil && notes.Review.WorkItem != nil {
			view.AcceptedWorkItemID = notes.Review.WorkItem.ID
			view.AcceptedWorkItemKey = notes.Review.WorkItem.Key
			view.AcceptedWorkItemStatus = notes.Review.WorkItem.Status
		}
		if err := json.Unmarshal([]byte(item.RoutingNotes), &notes); err == nil && notes.Review != nil {
			view.ApprovalRequired = notes.Review.ApprovalRequired
			view.BlockedPendingApproval = notes.Review.BlockedPendingApproval
			view.PolicyReason = notes.Review.PolicyReason
			view.PolicyDecision = notes.Review.PolicyDecision
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

func runLogs(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin logs [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	records, err := listLogs(ctx, app.Store, state.Scope)
	if err != nil {
		return err
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
			Inputs: map[string]string{
				"portal_track": command.PortalTrack,
				"surface":      command.Surface,
				"goal":         command.Goal,
				"intent":       command.Intent,
			},
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

func runStatus(ctx context.Context, app bootstrap.App, cfg appconfig.Config, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin status [--json]")
	}

	snapshot, err := conversationsvc.Service{
		DB:             app.Store.DB(),
		StalledTimeout: 30 * time.Minute,
	}.Snapshot(ctx)
	if err != nil {
		return err
	}

	summary, err := newHealthService(app, healthsvc.DefaultConfig(), cfg).Summary(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
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
			"health":                       string(summary.Status),
			"pending_approvals":            len(snapshot.ApprovalsWaiting),
			"registry_healthy":             summary.RegistryHealthy,
			"generated_at":                 snapshot.GeneratedAt,
			"approvals_waiting":            snapshot.ApprovalsWaiting,
			"stalled_runs":                 snapshot.StalledRuns,
			"active_runs":                  snapshot.ActiveRuns,
			"project_transitions":          snapshot.ProjectTransitions,
			"project_transition_ownership": snapshot.ProjectTransitionOwnership,
			"companion_swarm_counts":       companionSwarmCounts,
			"companion_swarms":             snapshot.CompanionSwarms,
		})
	}

	companionSwarmCount := len(snapshot.CompanionSwarms)
	_, err = fmt.Fprintf(stdout, "health=%s pending_approvals=%d stalled_runs=%d active_runs=%d project_transitions=%d companion_swarms=%d registry_healthy=%t\n",
		summary.Status,
		len(snapshot.ApprovalsWaiting),
		len(snapshot.StalledRuns),
		len(snapshot.ActiveRuns),
		len(snapshot.ProjectTransitions),
		companionSwarmCount,
		summary.RegistryHealthy,
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
	task, err := jobService.CreateTaskFromAct(ctx, resolved, command.Title)
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
			Store:          app.Store,
			Registry:       app.Registry,
			Executors:      app.Executors,
			ExecutorConfig: app.ExecutorConfig,
			Transitions:    projects.Service{Store: app.Store},
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
		Executors:           app.Executors,
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
	return map[string]string{
		"ODIN_ROOT":         os.Getenv("ODIN_ROOT"),
		"ODIN_HTTP_ADDR":    os.Getenv("ODIN_HTTP_ADDR"),
		"ODIN_ADMIN_TOKEN":  os.Getenv("ODIN_ADMIN_TOKEN"),
		"ODIN_NOW":          os.Getenv("ODIN_NOW"),
		"ODIN_MEDIA_CONFIG": os.Getenv("ODIN_MEDIA_CONFIG"),
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
		Store:          app.Store,
		RuntimeRoot:    app.RuntimeRoot,
		Registry:       app.Registry,
		Executors:      app.Executors,
		ExecutorConfig: app.ExecutorConfig,
		Transitions:    projects.Service{Store: app.Store},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
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
		Logger:            logger,
		ShutdownRequested: &shutdownRequested,
	}
	mediaService := newMediaService(app, cfg)
	schedulerService := supervision.Service{
		Store:             app.Store,
		Now:               now,
		ShutdownRequested: &shutdownRequested,
	}
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
			Gateway: newServeCapabilityGateway(app),
			Fallback: apihttp.NewOperationalHandler(apihttp.Dependencies{
				Health:          healthService,
				Metrics:         metricsService,
				ReadModels:      app.Store.DB(),
				RegistryHealthy: healthDeps.RegistryHealthy,
				Now:             now,
				AdminToken:      cfg.AdminToken,
				Admin: serveDashboardAdmin{
					ImmediateNotReady: &immediateNotReady,
					RuntimeState:      stateService,
					BootID:            bootID,
					RuntimeRoot:       cfg.RuntimeRoot,
					Logger:            logger,
				},
				GitHubWebhookSecret: os.Getenv("ODIN_GITHUB_WEBHOOK_SECRET"),
				GitHubIssueIngester: triggers.Service{Store: app.Store, Registry: app.Registry},
			}),
		}),
	}

	runLeaseMaintenanceCycle(operationCtx, leaseService, logger, loopConfig.leaseStaleAfter)

	var background sync.WaitGroup
	loopCount := 6
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

	if _, err := runFollowUpCycle(operationCtx, followUpService, now()); err != nil {
		logBackgroundError(logger, "follow_up", err)
	}
	if _, err := recoveryService.RunCycle(operationCtx); err != nil {
		logBackgroundError(logger, "self_heal", err)
	}
	if mediaService != nil {
		if _, err := mediaService.RunCycle(operationCtx); err != nil {
			logBackgroundError(logger, "media_supervisor", err)
		}
	}
	runHealthCycle(operationCtx, healthDeps, logger)
	if err := attemptDispatchIfReady(operationCtx, healthService, healthDeps.RegistryHealthy, jobService); err != nil {
		logBackgroundError(logger, "task_runner", err)
	}

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
	return nil
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

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runHealthCycle(operationCtx, deps, logger)
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
		return
	}

	setImmediateNotReady(deps.Health, true)
	writeNotReadyFlag(logger, deps.RuntimeRoot, fmt.Sprintf("dispatch paused: %s", report.Status))
	markRuntimeDegraded(ctx, deps, logger, fmt.Sprintf("dispatch paused: %s", report.Status), nil)
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
	if outcome.Executed || outcome.Reason == "run_already_executing" {
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
	if len(args) != 1 {
		return fmt.Errorf("usage: odin backup <archive-path>")
	}
	if err := service.CreateArchive(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "backup written to %s\n", args[0])
	return err
}

func runRestore(ctx context.Context, service appbackup.Service, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: odin restore <archive-path> <destination-root>")
	}
	if err := service.RestoreArchive(ctx, args[0], args[1]); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "restored backup into %s\n", args[1])
	return err
}

func runVerifyBackup(ctx context.Context, service appbackup.Service, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: odin verify-backup <archive-path>")
	}
	if err := service.VerifyArchive(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout, "backup verified")
	return err
}

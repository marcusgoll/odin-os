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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	"odin-os/internal/cli/commands"
	"odin-os/internal/cli/repl"
	"odin-os/internal/cli/scope"
	clistate "odin-os/internal/cli/state"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/companions"
	"odin-os/internal/core/followups"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
	conversationsvc "odin-os/internal/runtime/conversation"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/runtime/runs"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

const rootUsageBanner = "Usage: odin <command> [args]\n\nCommands: help repl doctor healthcheck serve backup restore verify-backup status project scope jobs runs approvals agenda logs task initiative companion profile followup transition skills"

var (
	serveTaskLoopInterval     = 1 * time.Second
	serveFollowUpLoopInterval = 1 * time.Second
	serveSelfHealLoopInterval = 30 * time.Second
	serveMetricsLoopInterval  = 1 * time.Minute
	serveOperationTimeout     = 30 * time.Second
	serveHealthConfig         = healthsvc.DefaultConfig()
	serveListen               = net.Listen
)

// Run dispatches between root commands and the interactive shell.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
	rootCommand := commands.ParseRoot(args)

	switch rootCommand.Name {
	case "help":
		_, err := fmt.Fprintln(stdout, rootUsageBanner)
		return err
	}

	cfg, err := appconfig.Load(filepath.Join(root, "config", "odin.yaml"), root, runtimeEnv())
	if err != nil {
		return err
	}

	loadCtx := ctx
	if rootCommand.Name == "serve" {
		var cancelLoad context.CancelFunc
		loadCtx, cancelLoad = serveLoadContext(ctx)
		defer cancelLoad()
	}

	appLoader := bootstrap.Load
	if rootCommand.Name == "status" {
		appLoader = bootstrap.LoadReadOnly
	}

	app, err := appLoader(loadCtx, root, cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	defer app.Store.Close()

	switch rootCommand.Name {
	case "repl":
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runRepl(ctx, app, stdin, stdout, now)
	case "doctor":
		return runDoctor(ctx, app, rootCommand.Args, stdout)
	case "healthcheck":
		return runHealthcheck(ctx, app, stdout)
	case "serve":
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runServe(ctx, app, cfg, stdout, now)
	case "backup":
		return runBackup(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, rootCommand.Args, stdout)
	case "restore":
		return runRestore(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, rootCommand.Args, stdout)
	case "verify-backup":
		return runVerifyBackup(ctx, appbackup.Service{RepoRoot: root, RuntimeRoot: cfg.RuntimeRoot}, rootCommand.Args, stdout)
	case "status":
		return runStatus(ctx, app, rootCommand.Args, stdout)
	case "project":
		return runProject(app, rootCommand.Args, stdout)
	case "scope":
		return runScope(app, rootCommand.Args, stdout)
	case "jobs":
		return runJobs(ctx, app, rootCommand.Args, stdout)
	case "runs":
		return runRuns(ctx, app, rootCommand.Args, stdout)
	case "approvals":
		return runApprovals(ctx, app, rootCommand.Args, stdout)
	case "agenda":
		now, err := runtimeNow()
		if err != nil {
			return err
		}
		return runAgenda(ctx, app, rootCommand.Args, stdout, now)
	case "logs":
		return runLogs(ctx, app, rootCommand.Args, stdout)
	case "task":
		return runTask(ctx, app, rootCommand.Args, stdout)
	case "initiative":
		return runInitiative(ctx, app, rootCommand.Args, stdout)
	case "companion":
		return runCompanion(ctx, app, rootCommand.Args, stdout)
	case "profile":
		return commands.RunProfile(ctx, app.Store, rootCommand.Args, stdout)
	case "followup":
		return runFollowup(ctx, app, rootCommand.Args, stdout)
	case "transition":
		return runTransition(ctx, app, rootCommand.Args, stdout)
	case "skills":
		return runSkills(ctx, app, rootCommand.Args, stdout)
	default:
		return fmt.Errorf("unknown command: %s", rootCommand.Name)
	}
}

func runRepl(ctx context.Context, app bootstrap.App, stdin io.Reader, stdout io.Writer, now func() time.Time) error {
	shell, err := repl.New(repl.Environment{
		Store:               app.Store,
		Registry:            app.Registry,
		RegistryDiagnostics: app.RegistryDiagnostics,
		SessionStore:        app.SessionStore,
		CapabilityService:   app.CapabilityService,
		ExecutorConfig:      app.ExecutorConfig,
		Executors:           app.Executors,
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
		},
		Now: now,
	})
	if err != nil {
		return err
	}

	if err := shell.Run(ctx, stdin, stdout); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func runDoctor(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	report, err := healthsvc.Service{DB: app.Store.DB()}.Doctor(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "--json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	_, err = fmt.Fprintf(stdout, "status=%s checks=%d\n", report.Status, len(report.Checks))
	return err
}

func runProject(app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) > 2 || (len(remaining) == 1 && remaining[0] != "list") {
		return fmt.Errorf("usage: odin project [list | select <project>] [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	if len(remaining) == 2 && remaining[0] == "select" {
		project, ok := app.Registry.Lookup(remaining[1])
		if !ok {
			return fmt.Errorf("unknown project: %s", remaining[1])
		}

		state.Scope = scope.Resolve(scope.ResolveInput{
			ExplicitTarget: &scope.Target{
				ProjectKey:    project.Key,
				SystemProject: project.SystemProject,
			},
		})
		state.Mode = clistate.SanitizeMode(state.Mode, state.Scope)
		if err := saveCLIState(app, state); err != nil {
			return err
		}

		if jsonOutput {
			return commands.WriteJSON(stdout, commands.ScopeView{Scope: scopeLabel(state.Scope)})
		}
		_, err := fmt.Fprintf(stdout, "project=%s scope=%s\n", project.Key, scopeLabel(state.Scope))
		return err
	}

	current := state.Scope.ProjectKey
	if current == "" {
		current = "none"
	}

	projectKeys := make([]string, 0, len(app.Registry.Projects()))
	for _, project := range app.Registry.Projects() {
		projectKeys = append(projectKeys, project.Key)
	}
	sort.Strings(projectKeys)

	view := commands.ProjectListView{
		Current:  current,
		Projects: projectKeys,
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}

	_, err = fmt.Fprintf(stdout, "current=%s projects=%s\n", view.Current, strings.Join(view.Projects, ","))
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
				ProjectKey: view.ProjectKey,
				TaskKey:    view.TaskKey,
				Status:     view.Status,
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
				TaskKey:  view.TaskKey,
				Executor: view.Executor,
				Status:   view.Status,
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
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin approvals [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	approvals, err := listPendingApprovals(ctx, app.Store, state.Scope)
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
		if _, err := fmt.Fprintf(stdout, "%s %s\n", approval.TaskKey, approval.Status); err != nil {
			return err
		}
	}
	return nil
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
				ID:    record.ID,
				Type:  string(record.Type),
				Scope: record.Scope,
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

func runTask(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseTask(args)
	if err != nil {
		return err
	}

	jobService := jobs.Service{
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
	}

	task, err := jobService.CreateTaskFromProjectKey(ctx, command.ProjectKey, command.Title)
	if err != nil {
		return err
	}

	taskView := commands.TaskCreateView{
		ID:     task.ID,
		Key:    task.Key,
		Status: task.Status,
		Scope:  task.Scope,
	}
	if command.Name == "create" {
		return commands.WriteJSON(stdout, taskView)
	}

	outcome, err := jobService.ExecuteTask(ctx, task.ID)
	if err != nil {
		return err
	}

	payload := commands.TaskRunView{
		Task: commands.TaskCreateView{
			ID:     outcome.Task.ID,
			Key:    outcome.Task.Key,
			Status: outcome.Task.Status,
			Scope:  outcome.Task.Scope,
		},
	}
	if outcome.Run != nil {
		payload.Run = &commands.TaskRunResultView{
			ID:       outcome.Run.ID,
			Executor: outcome.Run.Executor,
			Status:   outcome.Run.Status,
			Summary:  outcome.Run.Summary,
		}
	}
	return commands.WriteJSON(stdout, payload)
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
	default:
		return fmt.Errorf("unsupported companion subcommand: %s", command.Name)
	}
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

func runTransition(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	if len(args) > 0 && strings.EqualFold(args[0], "help") {
		_, err := fmt.Fprintln(stdout, transitionUsage)
		return err
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}
	manifest, err := scopedManifest(app.Registry, state.Scope)
	if err != nil {
		_, writeErr := fmt.Fprintln(stdout, err.Error())
		return writeErr
	}

	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		status, err := currentTransitionStatus(ctx, app.Store, manifest)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, renderTransitionStatus(manifest.Key, status))
		return err
	}

	if !strings.EqualFold(args[0], "set") || len(args) < 2 {
		return fmt.Errorf("usage: %s", transitionUsage)
	}

	request, err := parseTransitionSetRequest(args[1:])
	if err != nil {
		_, writeErr := fmt.Fprintln(stdout, err.Error())
		return writeErr
	}

	jobService := jobs.Service{
		Store:    app.Store,
		Registry: app.Registry,
	}
	project, err := jobService.EnsureRuntimeProject(ctx, manifest)
	if err != nil {
		return err
	}

	record, err := projects.Service{Store: app.Store}.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    request.State,
		LimitedActions: request.LimitedActions,
		ChangedBy:      "operator",
		Notes:          request.Reason,
	})
	if err != nil {
		_, writeErr := fmt.Fprintf(stdout, "unable to change transition: %v\n", err)
		return writeErr
	}

	status := transitionStatus{
		State:             projects.TransitionState(record.State),
		Controller:        projects.TransitionController(record.Controller),
		MutationAuthority: projects.TransitionController(record.Controller),
		OdinCanMutate:     record.Controller == string(projects.TransitionControllerOdinOS),
		LimitedActions:    decodeCSVOrJSON(record.LimitedActionsJSON),
		Notes:             record.Notes,
	}
	_, err = fmt.Fprintln(stdout, renderTransitionStatus(manifest.Key, status))
	return err
}

func runHealthcheck(ctx context.Context, app bootstrap.App, stdout io.Writer) error {
	report, err := healthsvc.Service{DB: app.Store.DB()}.Doctor(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	if report.Status != healthsvc.StatusHealthy {
		_, _ = fmt.Fprintf(stdout, "not ready: %s\n", report.Status)
		return errRuntimeNotReady
	}

	_, err = fmt.Fprintln(stdout, "ready")
	return err
}

func runStatus(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
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

	summary, err := healthsvc.Service{DB: app.Store.DB()}.Summary(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	if jsonOutput {
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
		})
	}

	_, err = fmt.Fprintf(stdout, "health=%s pending_approvals=%d stalled_runs=%d active_runs=%d project_transitions=%d registry_healthy=%t\n",
		summary.Status,
		len(snapshot.ApprovalsWaiting),
		len(snapshot.StalledRuns),
		len(snapshot.ActiveRuns),
		len(snapshot.ProjectTransitions),
		summary.RegistryHealthy,
	)
	return err
}

func runtimeEnv() map[string]string {
	return map[string]string{
		"ODIN_ROOT":      os.Getenv("ODIN_ROOT"),
		"ODIN_HTTP_ADDR": os.Getenv("ODIN_HTTP_ADDR"),
		"ODIN_NOW":       os.Getenv("ODIN_NOW"),
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

func loadCLIState(app bootstrap.App) (clistate.State, error) {
	cache, err := app.SessionStore.Load()
	if err != nil {
		return clistate.State{}, err
	}
	return clistate.ResolveStartupState(cache, app.Registry), nil
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

func consumeJSONFlag(args []string) (bool, []string, error) {
	jsonOutput := false
	remaining := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			if jsonOutput {
				return false, nil, fmt.Errorf("duplicate --json flag")
			}
			jsonOutput = true
			continue
		}
		remaining = append(remaining, arg)
	}
	return jsonOutput, remaining, nil
}

func scopeLabel(resolved scope.Resolution) string {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		return resolved.ProjectKey
	default:
		return string(resolved.Kind)
	}
}

func listPendingApprovals(ctx context.Context, store *sqlite.Store, resolved scope.Resolution) ([]commands.ApprovalView, error) {
	views, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		return nil, err
	}

	approvals := make([]commands.ApprovalView, 0, len(views))
	for _, view := range views {
		if matchesTaskProjectionScope(view.ProjectKey, view.TaskScope, resolved) {
			approvals = append(approvals, commands.ApprovalView{
				TaskKey: view.TaskKey,
				Status:  view.Status,
			})
		}
	}
	return approvals, nil
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

	filtered := make([]runtimeevents.Record, 0, 10)
	for _, record := range records {
		if !matchesEventScope(record.Scope, resolved) {
			continue
		}
		filtered = append(filtered, record)
		if len(filtered) == 10 {
			break
		}
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
		return eventScope == string(scope.ScopeOdinCore)
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
	if cfg.Service.StartupRecovery {
		startupCtx, cancel := serveStartupContext(ctx)
		result, err := recovery.Service{Store: app.Store}.RunStartupRecovery(startupCtx)
		cancel()
		if err != nil {
			return err
		}
		if result.RecoveredRuns > 0 {
			if _, err := fmt.Fprintf(stdout, "startup recovery recovered %d run(s)\n", result.RecoveredRuns); err != nil {
				return err
			}
		}
	}

	logger, logCloser, err := openServiceLogger(cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	if logCloser != nil {
		defer logCloser.Close()
	}

	jobService := newJobService(app)
	followUpService := followups.Service{Store: app.Store}
	workItemService := workitems.Service{Store: app.Store}
	recoveryService := recovery.Service{
		Store:           app.Store,
		RegistryRoot:    filepath.Join(app.RepoRoot, "registry"),
		ExecutorCatalog: app.Executors,
		HealthConfig:    serveHealthConfig,
		Logger:          logger,
	}
	metricsService := metricsvc.Service{
		DB: app.Store.DB(),
	}

	serveCtx, cancelServe := serveServeContext(ctx)
	defer cancelServe()

	followUpCtx, cancel := serveOperationContext(serveCtx)
	if _, err := runFollowUpCycle(followUpCtx, followUpService, workItemService, now()); err != nil {
		cancel()
		logBackgroundError(logger, "follow_up", err)
	}
	cancel()

	taskCtx, cancel := serveOperationContext(serveCtx)
	if err := jobService.ExecuteNextQueued(taskCtx); err != nil {
		cancel()
		logBackgroundError(logger, "task_runner", err)
	}
	cancel()

	recoveryCtx, cancel := serveOperationContext(serveCtx)
	if _, err := recoveryService.RunCycle(recoveryCtx); err != nil {
		cancel()
		logBackgroundError(logger, "self_heal", err)
	}
	cancel()

	var background sync.WaitGroup
	background.Add(4)
	go runTaskLoop(serveCtx, &background, jobService, logger)
	go runSelfHealLoop(serveCtx, &background, recoveryService, logger)
	go runFollowUpLoop(serveCtx, &background, followUpService, workItemService, logger, now)
	go runMetricsLoop(serveCtx, &background, metricsService, logger)

	listener, err := serveListen("tcp", cfg.Service.HTTPAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &stdhttp.Server{
		Handler: apihttp.NewCapabilitiesHandler(apihttp.CapabilitiesDependencies{
			Gateway: newServeCapabilityGateway(app),
			Fallback: apihttp.NewOperationalHandler(apihttp.Dependencies{
				Health: healthsvc.Service{
					DB: app.Store.DB(),
				},
				Metrics: metricsvc.Service{
					DB: app.Store.DB(),
				},
				ReadModels:      app.Store.DB(),
				RegistryHealthy: len(app.RegistryDiagnostics) == 0,
				Now:             now,
			}),
		}),
	}

	shutdownDone := make(chan struct{})
	shutdownStop := make(chan struct{})
	var shutdownStopOnce sync.Once
	stopShutdown := func() {
		shutdownStopOnce.Do(func() {
			close(shutdownStop)
		})
	}
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), serveOperationTimeout)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-shutdownStop:
		}
	}()

	if _, err := fmt.Fprintf(stdout, "serving on %s\n", listener.Addr().String()); err != nil {
		return err
	}

	err = server.Serve(listener)
	if errors.Is(err, stdhttp.ErrServerClosed) {
		<-shutdownDone
		background.Wait()
		return ctx.Err()
	}
	stopShutdown()
	cancelServe()
	background.Wait()
	return err
}

func newServeCapabilityGateway(app bootstrap.App) *capabilities.Gateway {
	if app.CapabilityService == nil {
		return nil
	}

	return capabilities.NewGateway(
		app.CapabilityService,
		func(ctx context.Context, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
			return invokeServedCapability(ctx, app, request, descriptor)
		},
		runs.Service{DB: app.Store.DB(), Store: app.Store},
	)
}

func invokeServedCapability(ctx context.Context, app bootstrap.App, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
	switch descriptor.Key {
	case "project.status":
		return invokeServedProjectStatus(ctx, app, request)
	default:
		return capabilities.InvokeResponse{}, &capabilities.Error{
			CodeValue: "not_found",
			Message:   fmt.Sprintf("unsupported capability: %s", descriptor.Key),
		}
	}
}

func invokeServedProjectStatus(ctx context.Context, app bootstrap.App, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	scopeRef := request.Scope
	scopeKind := strings.TrimSpace(scopeRef.Kind)
	projectKey := strings.TrimSpace(scopeRef.ProjectKey)

	if (scopeKind == "project" || scopeKind == "odin-core") && projectKey != "" {
		if manifest, ok := app.Registry.Lookup(projectKey); ok {
			status, err := loadServedTransitionStatus(ctx, app, manifest.Key)
			if err != nil {
				return capabilities.InvokeResponse{}, err
			}
			return capabilities.InvokeResponse{
				Status: "ok",
				Output: json.RawMessage(renderServedTransitionStatus(manifest.Key, status)),
			}, nil
		}
	}

	return capabilities.InvokeResponse{
		Status: "ok",
		Output: json.RawMessage(fmt.Sprintf("scope=%s mode=%s\n", servedScopeLabel(scopeRef), servedMode(request))),
	}, nil
}

type servedTransitionStatus struct {
	State             projects.TransitionState
	Controller        projects.TransitionController
	MutationAuthority projects.TransitionController
	OdinCanMutate     bool
	LimitedActions    []string
	Notes             string
}

func loadServedTransitionStatus(ctx context.Context, app bootstrap.App, projectKey string) (servedTransitionStatus, error) {
	project, err := app.Store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return servedTransitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return servedTransitionStatus{}, err
	}

	record, err := app.Store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return servedTransitionStatus{
				State:             projects.TransitionStateInventory,
				Controller:        projects.TransitionControllerLegacyOdin,
				MutationAuthority: projects.TransitionControllerLegacyOdin,
				OdinCanMutate:     false,
			}, nil
		}
		return servedTransitionStatus{}, err
	}

	controller := projects.TransitionController(record.Controller)
	return servedTransitionStatus{
		State:             projects.TransitionState(record.State),
		Controller:        controller,
		MutationAuthority: controller,
		OdinCanMutate:     controller == projects.TransitionControllerOdinOS,
		LimitedActions:    decodeCSVList(record.LimitedActionsJSON),
		Notes:             record.Notes,
	}, nil
}

func renderServedTransitionStatus(projectKey string, status servedTransitionStatus) string {
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

func decodeCSVList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "[") {
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

func servedMode(request capabilities.InvokeRequest) string {
	mode := strings.TrimSpace(request.Execution.Mode)
	if mode == "" {
		return "local"
	}
	return mode
}

func servedScopeLabel(scopeRef capabilities.ScopeRef) string {
	switch strings.TrimSpace(scopeRef.Kind) {
	case "project", "odin-core":
		if strings.TrimSpace(scopeRef.ProjectKey) != "" {
			return strings.TrimSpace(scopeRef.ProjectKey)
		}
	}
	return strings.TrimSpace(scopeRef.Kind)
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

func newJobService(app bootstrap.App) jobs.Service {
	return jobs.Service{
		Store:          app.Store,
		Registry:       app.Registry,
		Executors:      app.Executors,
		ExecutorConfig: app.ExecutorConfig,
		RuntimeRoot:    app.RuntimeRoot,
		Transitions:    projects.Service{Store: app.Store},
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
		},
		Supervisor: supervision.Service{},
		Now:        time.Now,
	}
}

func runTaskLoop(ctx context.Context, wg *sync.WaitGroup, service jobs.Service, logger *logs.Logger) {
	defer wg.Done()

	logBackgroundEvent(logger, logs.LevelInfo, "task_runner", "task loop started", map[string]any{
		"interval_ms": serveTaskLoopInterval.Milliseconds(),
	})

	ticker := time.NewTicker(serveTaskLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			taskCtx, cancel := serveTaskContext(ctx)
			if err := service.ExecuteNextQueued(taskCtx); err != nil {
				cancel()
				logBackgroundError(logger, "task_runner", err)
			}
			cancel()
		}
	}
}

func runSelfHealLoop(ctx context.Context, wg *sync.WaitGroup, service recovery.Service, logger *logs.Logger) {
	defer wg.Done()

	logBackgroundEvent(logger, logs.LevelInfo, "self_heal", "self-heal loop started", map[string]any{
		"interval_ms": serveSelfHealLoopInterval.Milliseconds(),
	})

	ticker := time.NewTicker(serveSelfHealLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recoveryCtx, cancel := serveOperationContext(ctx)
			if _, err := service.RunCycle(recoveryCtx); err != nil {
				cancel()
				logBackgroundError(logger, "self_heal", err)
			}
			cancel()
		}
	}
}

func runMetricsLoop(ctx context.Context, wg *sync.WaitGroup, service metricsvc.Service, logger *logs.Logger) {
	defer wg.Done()

	logBackgroundEvent(logger, logs.LevelInfo, "metrics", "metrics loop started", map[string]any{
		"interval_ms": serveMetricsLoopInterval.Milliseconds(),
	})

	ticker := time.NewTicker(serveMetricsLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metricsCtx, cancel := serveOperationContext(ctx)
			snapshot, err := service.Collect(metricsCtx)
			cancel()
			if err != nil {
				logBackgroundError(logger, "metrics", err)
				continue
			}
			logBackgroundEvent(logger, logs.LevelInfo, "metrics", "metrics snapshot exported", map[string]any{
				"generated_at":        snapshot.GeneratedAt.Format(time.RFC3339Nano),
				"active_runs":         snapshot.ActiveRuns,
				"blocked_items":       snapshot.BlockedItems,
				"approvals_waiting":   snapshot.ApprovalsWaiting,
				"open_incidents":      snapshot.OpenIncidents,
				"escalated_incidents": snapshot.EscalatedIncidents,
				"active_recoveries":   snapshot.ActiveRecoveries,
				"queued_tasks":        snapshot.QueuedTasks,
				"stale_executors":     snapshot.StaleExecutors,
				"stale_sources":       snapshot.StaleSources,
				"stale_projections":   snapshot.StaleProjections,
			})
		}
	}
}

func runFollowUpLoop(ctx context.Context, wg *sync.WaitGroup, followUpService followups.Service, workItemService workitems.Service, logger *logs.Logger, now func() time.Time) {
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
			if _, err := runFollowUpCycle(followUpCtx, followUpService, workItemService, now()); err != nil {
				cancel()
				logBackgroundError(logger, "follow_up", err)
			}
			cancel()
		}
	}
}

func runFollowUpCycle(ctx context.Context, followUpService followups.Service, workItemService workitems.Service, now time.Time) (int, error) {
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
					if _, err := followUpService.Pause(ctx, workspace.ID, obligation.ID); err != nil {
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
		materialization, err := followUpService.Materialize(ctx, followups.MaterializeParams{
			ObligationID: obligation.ID,
			TaskKey:      taskKey,
			Title:        obligation.Title,
			Scope:        "project",
			RequestedBy:  "operator",
		})
		if err != nil {
			return mutated, err
		}
		mutated++

		task, err := workItemService.Get(ctx, materialization.TaskID)
		if err != nil {
			return mutated, err
		}
		if task.Status != "blocked" {
			if _, err := workItemService.Block(ctx, task.ID); err != nil {
				return mutated, err
			}
			mutated++
		}
	}

	return mutated, nil
}

func followUpTaskKey(obligation followups.FollowUpObligation) string {
	return fmt.Sprintf("followup-%d-%s", obligation.ID, obligation.NextDueAt.UTC().Format("20060102-150405Z0700"))
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

	base := context.Background()
	if deadline, ok := parent.Deadline(); ok {
		timeoutDeadline := time.Now().Add(serveOperationTimeout)
		if deadline.Before(timeoutDeadline) {
			return context.WithDeadline(base, deadline)
		}
	}
	return context.WithTimeout(base, serveOperationTimeout)
}

func serveServeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func logBackgroundError(logger *logs.Logger, component string, err error) {
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

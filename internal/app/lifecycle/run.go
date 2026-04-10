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
	"odin-os/internal/core/projects"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

const rootUsageBanner = "Usage: odin <command> [args]\n\nCommands: help repl doctor healthcheck serve backup restore verify-backup status project scope jobs runs approvals logs"

var (
	serveTaskLoopInterval     = 1 * time.Second
	serveSelfHealLoopInterval = 30 * time.Second
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
		loadCtx = context.WithoutCancel(ctx)
	}

	app, err := bootstrap.Load(loadCtx, root, cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	defer app.Store.Close()

	switch rootCommand.Name {
	case "repl":
		return runRepl(ctx, app, stdin, stdout)
	case "doctor":
		return runDoctor(ctx, app, rootCommand.Args, stdout)
	case "healthcheck":
		return runHealthcheck(ctx, app, stdout)
	case "serve":
		return runServe(ctx, app, cfg, stdout)
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
	case "logs":
		return runLogs(ctx, app, rootCommand.Args, stdout)
	default:
		return fmt.Errorf("unknown command: %s", rootCommand.Name)
	}
}

func runRepl(ctx context.Context, app bootstrap.App, stdin io.Reader, stdout io.Writer) error {
	shell, err := repl.New(repl.Environment{
		Store:               app.Store,
		Registry:            app.Registry,
		RegistryDiagnostics: app.RegistryDiagnostics,
		SessionStore:        app.SessionStore,
		ExecutorConfig:      app.ExecutorConfig,
		Executors:           app.Executors,
		Leases: leases.Manager{
			Store:        app.Store,
			Git:          gitadapter.Adapter{},
			WorktreeRoot: worktrees.DefaultRoot(),
		},
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

func runStatus(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("usage: odin status [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
		return err
	}

	pendingApprovals, err := listPendingApprovals(ctx, app.Store, state.Scope)
	if err != nil {
		return err
	}

	summary, err := healthsvc.Service{DB: app.Store.DB()}.Summary(ctx, len(app.RegistryDiagnostics) == 0)
	if err != nil {
		return err
	}

	view := commands.StatusView{
		Health:           string(summary.Status),
		PendingApprovals: len(pendingApprovals),
		RegistryHealthy:  summary.RegistryHealthy,
	}
	if jsonOutput {
		return commands.WriteStatusJSON(stdout, view)
	}

	_, err = fmt.Fprintf(
		stdout,
		"health=%s pending_approvals=%d registry_healthy=%t\n",
		view.Health,
		view.PendingApprovals,
		view.RegistryHealthy,
	)
	return err
}

func runProject(app bootstrap.App, args []string, stdout io.Writer) error {
	jsonOutput, remaining, err := consumeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(remaining) > 1 || (len(remaining) == 1 && remaining[0] != "list") {
		return fmt.Errorf("usage: odin project [list] [--json]")
	}

	state, err := loadCLIState(app)
	if err != nil {
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

func runtimeEnv() map[string]string {
	return map[string]string{
		"ODIN_ROOT":      os.Getenv("ODIN_ROOT"),
		"ODIN_HTTP_ADDR": os.Getenv("ODIN_HTTP_ADDR"),
	}
}

func loadCLIState(app bootstrap.App) (clistate.State, error) {
	cache, err := app.SessionStore.Load()
	if err != nil {
		return clistate.State{}, err
	}
	return clistate.ResolveStartupState(cache, app.Registry), nil
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
	rows, err := store.DB().QueryContext(ctx, `
		SELECT t.key, a.status, t.scope, p.key
		FROM approvals a
		JOIN tasks t ON t.id = a.task_id
		JOIN projects p ON p.id = t.project_id
		WHERE a.status = 'pending'
		ORDER BY a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []commands.ApprovalView
	for rows.Next() {
		var approval commands.ApprovalView
		var projectKey string
		var taskScope string
		if err := rows.Scan(&approval.TaskKey, &approval.Status, &taskScope, &projectKey); err != nil {
			return nil, err
		}
		if matchesTaskProjectionScope(projectKey, taskScope, resolved) {
			approvals = append(approvals, approval)
		}
	}

	return approvals, rows.Err()
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

func runServe(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
	operationCtx := context.WithoutCancel(ctx)

	if cfg.Service.StartupRecovery {
		result, err := recovery.Service{Store: app.Store}.RunStartupRecovery(operationCtx)
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
	recoveryService := recovery.Service{
		Store:           app.Store,
		RegistryRoot:    filepath.Join(app.RepoRoot, "registry"),
		ExecutorCatalog: app.Executors,
		HealthConfig:    healthsvc.DefaultConfig(),
		Logger:          logger,
	}

	if err := jobService.ExecuteNextQueued(operationCtx); err != nil {
		logBackgroundError(logger, "task_runner", err)
	}
	if _, err := recoveryService.RunCycle(operationCtx); err != nil {
		logBackgroundError(logger, "self_heal", err)
	}

	var background sync.WaitGroup
	background.Add(2)
	go runTaskLoop(ctx, operationCtx, &background, jobService, logger)
	go runSelfHealLoop(ctx, operationCtx, &background, recoveryService, logger)

	listener, err := net.Listen("tcp", cfg.Service.HTTPAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &stdhttp.Server{
		Handler: apihttp.NewOperationalHandler(apihttp.Dependencies{
			Health: healthsvc.Service{
				DB: app.Store.DB(),
			},
			Metrics: metricsvc.Service{
				DB: app.Store.DB(),
			},
			RegistryHealthy: len(app.RegistryDiagnostics) == 0,
		}),
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if _, err := fmt.Fprintf(stdout, "serving on %s\n", listener.Addr().String()); err != nil {
		return err
	}

	err = server.Serve(listener)
	<-shutdownDone
	background.Wait()
	if errors.Is(err, stdhttp.ErrServerClosed) {
		return ctx.Err()
	}
	return err
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

func runTaskLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service jobs.Service, logger *logs.Logger) {
	defer wg.Done()

	ticker := time.NewTicker(serveTaskLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.ExecuteNextQueued(operationCtx); err != nil {
				logBackgroundError(logger, "task_runner", err)
			}
		}
	}
}

func runSelfHealLoop(ctx context.Context, operationCtx context.Context, wg *sync.WaitGroup, service recovery.Service, logger *logs.Logger) {
	defer wg.Done()

	ticker := time.NewTicker(serveSelfHealLoopInterval)
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

func logBackgroundError(logger *logs.Logger, component string, err error) {
	if logger == nil {
		return
	}
	_ = logger.Log(logs.Record{
		Level:         logs.LevelError,
		Component:     component,
		Message:       "background loop error",
		CorrelationID: component,
		Scope:         "global",
		Fields: map[string]any{
			"error": err.Error(),
		},
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

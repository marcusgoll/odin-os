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
	"strings"
	"sync"
	"time"

	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	"odin-os/internal/cli/repl"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/projects"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/recovery"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

var (
	serveTaskLoopInterval     = 1 * time.Second
	serveSelfHealLoopInterval = 30 * time.Second
)

// Run dispatches between the interactive shell and machine-oriented operational commands.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := appconfig.Load(filepath.Join(root, "config", "odin.yaml"), root, runtimeEnv())
	if err != nil {
		return err
	}

	loadCtx := ctx
	if len(args) > 0 && args[0] == "serve" {
		loadCtx = context.WithoutCancel(ctx)
	}

	app, err := bootstrap.Load(loadCtx, root, cfg.RuntimeRoot)
	if err != nil {
		return err
	}
	defer app.Store.Close()

	if len(args) > 0 {
		switch args[0] {
		case "doctor":
			return runDoctor(ctx, app, args[1:], stdout)
		case "healthcheck":
			return runHealthcheck(ctx, app, stdout)
		case "serve":
			return runServe(ctx, app, cfg, stdout)
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

	shell, err := repl.New(repl.Environment{
		Store:               app.Store,
		Registry:            app.Registry,
		RegistryDiagnostics: app.RegistryDiagnostics,
		SessionStore:        app.SessionStore,
		CapabilityService:   app.CapabilityService,
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

	jobService := newJobService(app)
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
		Handler: apihttp.NewCapabilitiesHandler(apihttp.CapabilitiesDependencies{
			Gateway: newServeCapabilityGateway(app),
			Fallback: apihttp.NewOperationalHandler(apihttp.Dependencies{
				Health: healthsvc.Service{
					DB: app.Store.DB(),
				},
				Metrics: metricsvc.Service{
					DB: app.Store.DB(),
				},
				RegistryHealthy: len(app.RegistryDiagnostics) == 0,
			}),
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

func newServeCapabilityGateway(app bootstrap.App) *capabilities.Gateway {
	if app.CapabilityService == nil {
		return nil
	}

	return capabilities.NewGateway(
		app.CapabilityService,
		func(ctx context.Context, request capabilities.InvokeRequest, descriptor capabilities.Descriptor) (capabilities.InvokeResponse, error) {
			return invokeServedCapability(ctx, app, request, descriptor)
		},
		runsvc.Service{DB: app.Store.DB()},
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

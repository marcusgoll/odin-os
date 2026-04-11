package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	"odin-os/internal/cli/repl"
	"odin-os/internal/core/projects"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/recovery"
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
	serveMetricsLoopInterval  = 1 * time.Minute
	serveOperationTimeout     = 30 * time.Second
	serveListen               = net.Listen
)

// Run dispatches between the interactive shell and machine-oriented operational commands.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := appconfig.Load(filepath.Join(root, "config", "odin.yaml"), root, runtimeEnv())
	if err != nil {
		return err
	}

	loadCtx := ctx
	if len(args) > 0 && args[0] == "serve" {
		loadCtx = serveLoadContext(ctx)
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

func serveLoadContext(parent context.Context) context.Context {
	if !shouldDetachServeContext(parent) {
		return parent
	}
	return context.WithoutCancel(parent)
}

func runServe(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
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
	metricsService := metricsvc.Service{
		DB: app.Store.DB(),
	}

	serveCtx, cancelServe := serveServeContext(ctx)
	defer cancelServe()

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
	background.Add(3)
	go runTaskLoop(serveCtx, &background, jobService, logger)
	go runSelfHealLoop(serveCtx, &background, recoveryService, logger)
	go runMetricsLoop(serveCtx, &background, metricsService, logger)

	listener, err := serveListen("tcp", cfg.Service.HTTPAddr)
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
			taskCtx, cancel := serveOperationContext(ctx)
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

func serveOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, serveOperationTimeout)
}

func serveStartupContext(parent context.Context) (context.Context, context.CancelFunc) {
	if shouldDetachServeContext(parent) {
		parent = context.WithoutCancel(parent)
	}
	return context.WithTimeout(parent, serveOperationTimeout)
}

func serveServeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func shouldDetachServeContext(parent context.Context) bool {
	return errors.Is(parent.Err(), context.Canceled)
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

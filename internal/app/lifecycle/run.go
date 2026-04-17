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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	"odin-os/internal/cli/repl"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/runtime/recovery"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	metricsvc "odin-os/internal/telemetry/metrics"
	gitadapter "odin-os/internal/vcs/git"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

var errRuntimeNotReady = errors.New("runtime not ready")

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

// Run dispatches between the interactive shell and machine-oriented operational commands.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
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

	if len(args) > 0 {
		switch args[0] {
		case "doctor":
			return runDoctor(ctx, app, args[1:], stdout)
		case "healthcheck":
			return runHealthcheck(ctx, app, cfg, stdout)
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
	report, err := newHealthService(app, healthsvc.DefaultConfig()).Doctor(ctx, len(app.RegistryDiagnostics) == 0)
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

func runHealthcheck(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
	if reason, active, err := readReadinessFlag(cfg.RuntimeRoot); err != nil {
		return err
	} else if active {
		_, _ = fmt.Fprintf(stdout, "not ready: %s\n", reason)
		return errRuntimeNotReady
	}

	healthConfig := healthsvc.DefaultConfig()
	healthConfig.RuntimeHeartbeatTTL = runtimeHeartbeatTTL(serveLoopConfigFromContext(ctx).healthInterval)
	report, ready, err := newHealthService(app, healthConfig).Readiness(ctx, len(app.RegistryDiagnostics) == 0)
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
		"ODIN_ROOT":      os.Getenv("ODIN_ROOT"),
		"ODIN_HTTP_ADDR": os.Getenv("ODIN_HTTP_ADDR"),
	}
}

func runServe(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
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
		Now:               time.Now,
	}
	recoveryService := recovery.Service{
		Store:           app.Store,
		RegistryRoot:    filepath.Join(app.RepoRoot, "registry"),
		ExecutorCatalog: app.Executors,
		HealthConfig:    healthsvc.DefaultConfig(),
		Logger:          logger,
	}
	schedulerService := supervision.Service{
		Store: app.Store,
		Now:   time.Now,
	}
	leaseService := leases.Maintenance{
		Store: app.Store,
		Cleanup: worktrees.Manager{
			Store: app.Store,
			Git:   gitadapter.Adapter{},
		},
		Now: time.Now,
	}
	loopConfig := serveLoopConfigFromContext(ctx)
	healthConfig := healthsvc.DefaultConfig()
	healthConfig.RuntimeHeartbeatTTL = runtimeHeartbeatTTL(loopConfig.healthInterval)
	var immediateNotReady atomic.Bool
	immediateNotReady.Store(true)
	healthService := newHealthService(app, healthConfig)
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
	listener, err := net.Listen("tcp", cfg.Service.HTTPAddr)
	if err != nil {
		return recordServeStopped(operationCtx, stateService, bootID, "listener binding failed", err)
	}
	defer listener.Close()

	server := &stdhttp.Server{
		Handler: apihttp.NewOperationalHandler(apihttp.Dependencies{
			Health:          healthService,
			Metrics:         metricsService,
			RegistryHealthy: healthDeps.RegistryHealthy,
		}),
	}

	runLeaseMaintenanceCycle(operationCtx, leaseService, logger, loopConfig.leaseStaleAfter)

	var background sync.WaitGroup
	background.Add(5)
	loopCtx, stopLoops := context.WithCancel(context.Background())
	dispatchNudges := make(chan struct{}, 32)
	go runSchedulerLoop(loopCtx, operationCtx, &background, schedulerService, dispatchNudges, logger, loopConfig.schedulerInterval)
	go runTaskLoop(loopCtx, operationCtx, &background, healthService, healthDeps.RegistryHealthy, jobService, dispatchNudges, logger, loopConfig.taskInterval)
	go runSelfHealLoop(loopCtx, operationCtx, &background, recoveryService, logger, loopConfig.selfHealInterval)
	go runLeaseLoop(loopCtx, operationCtx, &background, leaseService, logger, loopConfig.leaseInterval, loopConfig.leaseStaleAfter)
	go runHealthLoop(loopCtx, operationCtx, &background, healthDeps, logger, loopConfig.healthInterval)
	defer func() {
		stopLoops()
		background.Wait()
	}()

	if _, err := recoveryService.RunCycle(operationCtx); err != nil {
		logBackgroundError(logger, "self_heal", err)
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
			} else if result.Promoted > 0 && logger != nil {
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
						"promoted": result.Promoted,
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

func newHealthService(app bootstrap.App, config healthsvc.Config) healthsvc.Service {
	return healthsvc.Service{
		DB:           app.Store.DB(),
		Config:       config,
		ExecutorKeys: enabledExecutorKeys(app.ExecutorConfig),
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

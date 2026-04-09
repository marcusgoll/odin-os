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

	apihttp "odin-os/internal/api/http"
	appbackup "odin-os/internal/app/backup"
	"odin-os/internal/app/bootstrap"
	appconfig "odin-os/internal/app/config"
	"odin-os/internal/cli/repl"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/recovery"
	metricsvc "odin-os/internal/telemetry/metrics"
)

var errRuntimeNotReady = errors.New("runtime not ready")

// Run dispatches between the interactive shell and machine-oriented operational commands.
func Run(ctx context.Context, root string, args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := appconfig.Load(filepath.Join(root, "config", "odin.yaml"), root, runtimeEnv())
	if err != nil {
		return err
	}

	app, err := bootstrap.Load(ctx, root, cfg.RuntimeRoot)
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

func runServe(ctx context.Context, app bootstrap.App, cfg appconfig.Config, stdout io.Writer) error {
	if cfg.Service.StartupRecovery {
		result, err := recovery.Service{Store: app.Store}.RunStartupRecovery(ctx)
		if err != nil {
			return err
		}
		if result.RecoveredRuns > 0 {
			if _, err := fmt.Fprintf(stdout, "startup recovery recovered %d run(s)\n", result.RecoveredRuns); err != nil {
				return err
			}
		}
	}

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
	if errors.Is(err, stdhttp.ErrServerClosed) {
		return ctx.Err()
	}
	return err
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

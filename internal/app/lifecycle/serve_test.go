package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
)

func TestRunDoctorJSONWritesStructuredReport(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid json: %v\n%s", err, stdout.String())
	}
	if report.Status == "" {
		t.Fatalf("doctor report status is empty")
	}
}

func TestRunHealthcheckHealthyReturnsNil(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	seedRuntimeState(t, root, "ready", time.Now().UTC())

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckFreshRuntimeReturnsError(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(healthcheck fresh runtime) error = nil, want readiness error")
	}
	if !strings.Contains(stdout.String(), "runtime not ready") {
		t.Fatalf("healthcheck output = %q, want runtime-not-ready message", stdout.String())
	}
}

func TestRunHealthcheckFailsWhenReadinessFlagIsPresent(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	seedRuntimeState(t, root, "ready", time.Now().UTC())
	if err := writeReadinessFlag(root, "runtime heartbeat failed"); err != nil {
		t.Fatalf("writeReadinessFlag() error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(healthcheck) error = nil, want readiness error")
	}
	if !strings.Contains(stdout.String(), "runtime heartbeat failed") {
		t.Fatalf("healthcheck output = %q, want readiness-flag reason", stdout.String())
	}
}

func TestRunHealthcheckDegradedReturnsError(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
`), 0o644); err != nil {
		t.Fatalf("write invalid projects config: %v", err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatalf("Run(healthcheck) error = nil, want readiness error")
	}
	if !strings.Contains(stdout.String(), "not ready") {
		t.Fatalf("healthcheck output = %q, want not ready message", stdout.String())
	}
}

func TestRunDoctorScopesEnabledExecutors(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeUnavailableExecutorsConfig(t, root)
	seedHealthyRuntime(t, root)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid json: %v\n%s", err, stdout.String())
	}
	if report.Status != "degraded" {
		t.Fatalf("doctor report status = %q, want %q", report.Status, "degraded")
	}
}

func TestRunServeExecutesStartupRecoveryBeforeShutdown(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)

	projectID, taskID, runID := seedRunningTask(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	gotRun, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}

	packet, err := store.GetLatestTaskWakePacket(context.Background(), projectID, taskID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}
}

func TestRunServeRecordsLifecycleTransitionsAndShutdown(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	got, err := store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "stopped")
	}
	if got.ReadyAt == nil {
		t.Fatal("RuntimeState.ReadyAt = nil, want ready transition before shutdown")
	}

	statuses, err := lifecycleStatuses(store)
	if err != nil {
		t.Fatalf("lifecycleStatuses() error = %v", err)
	}
	assertLifecycleSequence(t, statuses, []string{"booting", "recovering", "ready", "draining", "stopped"})
}

func TestRunServeStopsWithoutReadyWhenListenerBindingFails(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	_, taskID := seedQueuedTask(t, root)
	seedStaleProjection(t, root)

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer occupied.Close()

	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: `+occupied.Addr().String()+`
  startup_recovery: false
`)

	var stdout bytes.Buffer
	err = Run(context.Background(), root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(serve) error = nil, want listener binding failure")
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	got, err := store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if got.Status != "stopped" {
		t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "stopped")
	}
	if got.LastError == "" {
		t.Fatal("RuntimeState.LastError = empty, want listener binding error")
	}

	task, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "queued" {
		t.Fatalf("Task.Status = %q, want %q", task.Status, "queued")
	}

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == runtimeevents.EventRecoveryActionExecuted {
			t.Fatalf("events = %+v, want no recovery action on listener failure", events)
		}
	}

	statuses, err := lifecycleStatuses(store)
	if err != nil {
		t.Fatalf("lifecycleStatuses() error = %v", err)
	}
	assertLifecycleSequence(t, statuses, []string{"booting", "stopped"})
}

func TestRunServeFailsWhenServiceLockIsHeld(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	holdServiceLock(t, root)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(serve) error = nil, want service lock conflict")
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	if _, err := store.GetRuntimeState(context.Background()); err == nil {
		t.Fatal("GetRuntimeState() error = nil, want no runtime_state mutation")
	}

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == runtimeevents.EventServiceLifecycleChanged {
			t.Fatalf("events = %+v, want no lifecycle mutation while service lock is held", events)
		}
	}
}

func TestRunServeExecutesStartupRecoveryWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)

	projectID, taskID, runID := seedRunningTask(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, openErr := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if openErr != nil {
		t.Fatalf("sqlite.Open() error = %v", openErr)
	}
	defer store.Close()

	gotRun, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}

	packet, err := store.GetLatestTaskWakePacket(context.Background(), projectID, taskID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}

	statuses, err := lifecycleStatuses(store)
	if err != nil {
		t.Fatalf("lifecycleStatuses() error = %v", err)
	}
	assertLifecycleSequence(t, statuses, []string{"booting", "recovering", "ready", "draining", "stopped"})
}

func TestRunServeSkipsRecoveringWhenStartupRecoveryDisabled(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)
	seedHealthyRuntime(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	statuses, err := lifecycleStatuses(store)
	if err != nil {
		t.Fatalf("lifecycleStatuses() error = %v", err)
	}
	assertNoLifecycleStatus(t, statuses, "recovering")
	assertLifecycleSequence(t, statuses, []string{"booting", "ready", "draining", "stopped"})
}

func TestRunServeRunsSelfHealCycleBeforeShutdown(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)
	seedHealthyRuntime(t, root)

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "stale",
		DetailsJSON: `{"source":"serve-test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{selfHealInterval: 20 * time.Millisecond})
	actionObserved := make(chan struct{})
	time.AfterFunc(30*time.Millisecond, func() {
		staleAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
		if _, err := store.DB().ExecContext(context.Background(), `
			UPDATE projection_freshness
			SET refreshed_at = ?, updated_at = ?
			WHERE surface = 'doctor'
		`, staleAt, staleAt); err != nil {
			t.Errorf("force stale projection freshness error = %v", err)
		}
	})
	go func() {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-deadline.C:
				cancel()
				return
			case <-ticker.C:
				events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
				if err != nil {
					continue
				}
				for _, event := range events {
					if string(event.Type) == "recovery.action_executed" {
						close(actionObserved)
						cancel()
						return
					}
				}
			}
		}
	}()

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	select {
	case <-actionObserved:
	default:
	}

	found := false
	for _, event := range events {
		if string(event.Type) == "recovery.action_executed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("events = %+v, want recovery.action_executed from background self-heal cycle", events)
	}
}

func TestRunServeSchedulerDispatchesDelayedTasksBeforeTaskLoopWakeup(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)
	writeMutableProjectsConfig(t, root)
	initGitRepo(t, filepath.Join(root, "repos", "alpha"))
	seedHealthyRuntime(t, root)
	projectID, taskID := seedQueuedTask(t, root)

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if _, err := (projects.Service{Store: store}).SetTransitionState(context.Background(), projects.TransitionStateInput{
		ProjectID:   projectID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	delay := 150 * time.Millisecond
	if _, err := store.RequeueTaskAt(context.Background(), sqlite.RequeueTaskAtParams{
		TaskID:         taskID,
		NextEligibleAt: time.Now().Add(delay),
	}); err != nil {
		t.Fatalf("RequeueTaskAt() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		taskInterval:      5 * time.Second,
		schedulerInterval: 20 * time.Millisecond,
	})
	time.AfterFunc(700*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	gotTask, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		logPath := filepath.Join(root, "runs", "logs", "odin-service.log")
		logContent, _ := os.ReadFile(logPath)
		t.Fatalf("Task.Status = %q, want completed (last_error=%q retry_count=%d next_eligible_at=%v log=%s)", gotTask.Status, gotTask.LastError, gotTask.RetryCount, gotTask.NextEligibleAt, string(logContent))
	}
}

func TestRunHealthCycleDoesNotPromoteDrainingRuntimeToReady(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	ctx := bootstrap.WithBootID(context.Background(), "boot-test")
	app, err := bootstrap.Load(ctx, root, root)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, err := app.RuntimeState.MarkDraining(context.Background(), runtimestate.TransitionInput{
		BootID: "boot-test",
		Reason: "shutdown requested",
	}); err != nil {
		t.Fatalf("MarkDraining() error = %v", err)
	}

	runHealthCycle(context.Background(), healthLoopDeps{
		Store:              app.Store,
		RuntimeState:       app.RuntimeState,
		Health:             healthsvc.Service{DB: app.Store.DB(), Config: healthsvc.DefaultConfig(), ExecutorKeys: enabledExecutorKeys(app.ExecutorConfig)},
		Executors:          app.Executors,
		ExecutorConfig:     app.ExecutorConfig,
		RegistryHealthy:    len(app.RegistryDiagnostics) == 0,
		ProjectionSurfaces: bootstrap.ServiceOwnedProjectionSurfaces(),
		BootID:             "boot-test",
	}, nil)

	state, err := app.Store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if state.Status != "draining" {
		t.Fatalf("RuntimeState.Status = %q, want %q", state.Status, "draining")
	}
}

func TestRunHealthCycleKeepsRuntimeNotReadyWhenShutdownRequested(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	ctx := bootstrap.WithBootID(context.Background(), "boot-test")
	app, err := bootstrap.Load(ctx, root, root)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, err := app.RuntimeState.MarkReady(context.Background(), runtimestate.TransitionInput{
		BootID: "boot-test",
		Reason: "ready for shutdown guard test",
	}); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := writeReadinessFlag(root, "shutdown requested"); err != nil {
		t.Fatalf("writeReadinessFlag() error = %v", err)
	}

	var immediateNotReady atomic.Bool
	var shutdownRequested atomic.Bool
	shutdownRequested.Store(true)

	runHealthCycle(context.Background(), healthLoopDeps{
		Store:        app.Store,
		RuntimeState: app.RuntimeState,
		Health: healthsvc.Service{
			DB:                app.Store.DB(),
			Config:            healthsvc.DefaultConfig(),
			ExecutorKeys:      enabledExecutorKeys(app.ExecutorConfig),
			ImmediateNotReady: &immediateNotReady,
		},
		Executors:          app.Executors,
		ExecutorConfig:     app.ExecutorConfig,
		RegistryHealthy:    len(app.RegistryDiagnostics) == 0,
		ProjectionSurfaces: bootstrap.ServiceOwnedProjectionSurfaces(),
		ShutdownRequested:  &shutdownRequested,
		BootID:             "boot-test",
		RuntimeRoot:        root,
	}, nil)

	if !immediateNotReady.Load() {
		t.Fatal("ImmediateNotReady = false, want shutdown to keep readiness latched closed")
	}
	reason, active, err := readReadinessFlag(root)
	if err != nil {
		t.Fatalf("readReadinessFlag() error = %v", err)
	}
	if !active {
		t.Fatal("readiness flag missing after shutdown-requested health cycle")
	}
	if !strings.Contains(reason, "shutdown requested") {
		t.Fatalf("readiness flag reason = %q, want shutdown requested", reason)
	}
}

func TestRunHealthCyclePreservesReadinessReasonWhileDraining(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	ctx := bootstrap.WithBootID(context.Background(), "boot-test")
	app, err := bootstrap.Load(ctx, root, root)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if _, err := app.RuntimeState.MarkDraining(context.Background(), runtimestate.TransitionInput{
		BootID: "boot-test",
		Reason: "operator requested shutdown",
	}); err != nil {
		t.Fatalf("MarkDraining() error = %v", err)
	}
	if err := writeReadinessFlag(root, "operator requested shutdown"); err != nil {
		t.Fatalf("writeReadinessFlag() error = %v", err)
	}

	runHealthCycle(context.Background(), healthLoopDeps{
		Store:        app.Store,
		RuntimeState: app.RuntimeState,
		Health: healthsvc.Service{
			DB:           app.Store.DB(),
			Config:       healthsvc.DefaultConfig(),
			ExecutorKeys: enabledExecutorKeys(app.ExecutorConfig),
		},
		Executors:          app.Executors,
		ExecutorConfig:     app.ExecutorConfig,
		RegistryHealthy:    len(app.RegistryDiagnostics) == 0,
		ProjectionSurfaces: bootstrap.ServiceOwnedProjectionSurfaces(),
		BootID:             "boot-test",
		RuntimeRoot:        root,
	}, nil)

	reason, active, err := readReadinessFlag(root)
	if err != nil {
		t.Fatalf("readReadinessFlag() error = %v", err)
	}
	if !active {
		t.Fatal("readiness flag missing while draining")
	}
	if reason != "operator requested shutdown" {
		t.Fatalf("readiness flag reason = %q, want operator requested shutdown", reason)
	}
}

func TestRunServeHeartbeatsActiveWorktreeLeases(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)
	seedHealthyRuntime(t, root)

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFI Pros",
		Scope:         "project",
		GitRoot:       filepath.Join(root, "repos", "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "heartbeat-active-lease",
		Title:       "Heartbeat active lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(context.Background(), sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	lease, err := store.CreateWorktreeLease(context.Background(), sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "active")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	staleAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := store.DB().ExecContext(context.Background(), `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), staleAt.Format(time.RFC3339Nano), lease.ID); err != nil {
		t.Fatalf("force stale heartbeat error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		leaseInterval:   20 * time.Millisecond,
		leaseStaleAfter: 30 * time.Minute,
	})
	time.AfterFunc(120*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	updated, err := store.GetWorktreeLease(context.Background(), lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if !updated.HeartbeatAt.After(staleAt) {
		t.Fatalf("HeartbeatAt = %v, want later than %v", updated.HeartbeatAt, staleAt)
	}
}

func TestServeHealthLoopRefreshesExecutorHealthAndRuntimeHeartbeat(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		healthInterval: 80 * time.Millisecond,
	})
	time.AfterFunc(300*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	state, err := store.GetRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if state.ReadyAt == nil {
		t.Fatal("RuntimeState.ReadyAt = nil, want ready transition before periodic heartbeat")
	}
	if !state.LastHeartbeatAt.After(*state.ReadyAt) {
		t.Fatalf("RuntimeState.LastHeartbeatAt = %s, want later than ReadyAt = %s", state.LastHeartbeatAt.Format(time.RFC3339Nano), state.ReadyAt.Format(time.RFC3339Nano))
	}

	var samples int
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM executor_health
		WHERE executor = 'codex_headless'
	`).Scan(&samples); err != nil {
		t.Fatalf("count executor_health rows error = %v", err)
	}
	if samples < 3 {
		t.Fatalf("codex_headless samples = %d, want at least 3 after bootstrap, initial health cycle, and periodic refresh", samples)
	}
}

func TestServeDegradedRuntimePausesDispatch(t *testing.T) {
	root := createRuntimeRoot(t)
	addr := allocateHTTPAddr(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: `+addr+`
  startup_recovery: false
`)
	writeMutableProjectsConfig(t, root)
	writeUnavailableExecutorsConfig(t, root)
	initGitRepo(t, filepath.Join(root, "repos", "alpha"))
	projectID, taskID := seedQueuedTask(t, root)

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if _, err := (projects.Service{Store: store}).SetTransitionState(context.Background(), projects.TransitionStateInput{
		ProjectID:   projectID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		taskInterval:   20 * time.Millisecond,
		healthInterval: 20 * time.Millisecond,
	})
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, root, []string{"serve"}, strings.NewReader(""), io.Discard)
	}()

	if err := waitForServeHealthStatus(ctx, "http://"+addr, http.StatusOK, "degraded"); err != nil {
		t.Fatal(err)
	}
	if err := waitForServeHealthStatus(ctx, "http://"+addr, http.StatusServiceUnavailable, "degraded", "/readyz"); err != nil {
		t.Fatal(err)
	}

	cancel()

	err = <-runErr
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}

	task, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Status != "queued" {
		t.Fatalf("Task.Status = %q, want %q while runtime is degraded", task.Status, "queued")
	}

	statuses, err := lifecycleStatuses(store)
	if err != nil {
		t.Fatalf("lifecycleStatuses() error = %v", err)
	}
	for _, status := range statuses {
		if status == "ready" {
			t.Fatalf("statuses = %v, want degraded runtime without ready transition", statuses)
		}
	}
	foundDegraded := false
	for _, status := range statuses {
		if status == "degraded" {
			foundDegraded = true
			break
		}
	}
	if !foundDegraded {
		t.Fatalf("statuses = %v, want degraded transition", statuses)
	}
}

func TestRunLeaseMaintenanceCycleHeartbeatsLiveLeasesWhenCleanupFails(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFI Pros",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "repo"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	releasedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "released-cleanup",
		Title:       "Released cleanup candidate",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(released) error = %v", err)
	}
	releasedRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   releasedTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(released) error = %v", err)
	}
	releasedLease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       releasedTask.ID,
		RunID:        releasedRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "released")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(released) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: releasedLease.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	liveTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "live-heartbeat",
		Title:       "Live lease heartbeat",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(live) error = %v", err)
	}
	liveRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   liveTask.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(live) error = %v", err)
	}
	liveLease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       liveTask.ID,
		RunID:        liveRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-2/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "live")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(live) error = %v", err)
	}

	maint := leases.Maintenance{
		Store: store,
		Cleanup: worktrees.Manager{
			Store: store,
			Git: &cleanupFailureGit{
				err: errors.New("remove failed"),
			},
		},
		Now: func() time.Time {
			return liveLease.HeartbeatAt.Add(30 * time.Second)
		},
	}
	store.Now = maint.Now

	runLeaseMaintenanceCycle(ctx, maint, nil, 30*time.Minute)

	updated, err := store.GetWorktreeLease(ctx, liveLease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(live) error = %v", err)
	}
	if !updated.HeartbeatAt.After(liveLease.HeartbeatAt) {
		t.Fatalf("HeartbeatAt = %v, want later than %v", updated.HeartbeatAt, liveLease.HeartbeatAt)
	}
}

type cleanupFailureGit struct {
	err error
}

func (git *cleanupFailureGit) RemoveWorktree(context.Context, string, string) error {
	return git.err
}

func createRuntimeRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "registry"), 0o755); err != nil {
		t.Fatalf("mkdir registry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(`
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("write projects config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte(`
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [codex_headless]
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}

	return root
}

func writeUnavailableExecutorsConfig(t *testing.T, root string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte(`
version: 1
executors:
  - key: claude_code_headless
    adapter: claude_code_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [claude_code_headless]
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}
}

func writeMutableProjectsConfig(t *testing.T, root string) {
	t.Helper()

	repoRoot := filepath.Join(root, "repos", "alpha")

	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(fmt.Sprintf(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: %s
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: %s
    default_branch: main
    github:
      repo: acme/alpha
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`, root, repoRoot)), 0o644); err != nil {
		t.Fatalf("write projects config: %v", err)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	run := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(output))
		}
	}

	run("init", "-b", "main", ".")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("commit", "--allow-empty", "-m", "initial commit")
}

func seedHealthyRuntime(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-15",
		Notes:       "healthy test sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "active_runs",
		Status:      "current",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func seedRuntimeState(t *testing.T, root string, status string, heartbeatAt time.Time) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	readyAt := (*time.Time)(nil)
	if status == "ready" {
		readyValue := heartbeatAt.UTC()
		readyAt = &readyValue
	}
	if _, err := store.UpsertRuntimeState(ctx, sqlite.UpsertRuntimeStateParams{
		BootID:             "boot-test",
		Status:             status,
		PID:                1234,
		StartedAt:          heartbeatAt.UTC(),
		ReadyAt:            readyAt,
		LastHeartbeatAt:    heartbeatAt.UTC(),
		LastShutdownReason: "",
		LastError:          "",
		UpdatedAt:          heartbeatAt.UTC(),
	}, sqlite.RuntimeStateWriteOptions{}); err != nil {
		t.Fatalf("UpsertRuntimeState() error = %v", err)
	}
}

func seedQueuedTask(t *testing.T, root string) (int64, int64) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(root, "repos", "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Queued alpha work",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	return project.ID, task.ID
}

func seedStaleProjection(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "stale",
		DetailsJSON: `{"source":"serve-test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func holdServiceLock(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "state", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir state/cache: %v", err)
	}

	file, err := os.OpenFile(filepath.Join(root, "state", "cache", "service.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open service lock: %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock service lock: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	})
}

func allocateHTTPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	return listener.Addr().String()
}

func seedRunningTask(t *testing.T, root string) (int64, int64, int64) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(root, "repos", "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Resume alpha work",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return project.ID, task.ID, run.ID
}

func writeRuntimeConfig(t *testing.T, root string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}
}

func lifecycleStatuses(store *sqlite.Store) ([]string, error) {
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		return nil, err
	}

	statuses := make([]string, 0, len(events))
	for _, event := range events {
		if event.Type != runtimeevents.EventServiceLifecycleChanged {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.ServiceLifecyclePayload](event.Payload)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, payload.Status)
	}
	return statuses, nil
}

func waitForServeHealthStatus(ctx context.Context, baseURL string, wantCode int, wantStatus string, pathOverride ...string) error {
	path := "/healthz"
	if len(pathOverride) > 0 && pathOverride[0] != "" {
		path = pathOverride[0]
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s%s readiness probe", baseURL, path)
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s%s readiness probe", baseURL, path)
		case <-ticker.C:
			response, err := http.Get(baseURL + path)
			if err != nil {
				continue
			}

			var report struct {
				Status string `json:"status"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&report)
			_ = response.Body.Close()
			if decodeErr != nil {
				continue
			}
			if response.StatusCode == wantCode && report.Status == wantStatus {
				return nil
			}
		}
	}
}

func assertLifecycleSequence(t *testing.T, statuses []string, want []string) {
	t.Helper()

	if len(statuses) < len(want) {
		t.Fatalf("lifecycle statuses = %v, want sequence at least %v", statuses, want)
	}

	cursor := 0
	for _, status := range want {
		found := false
		for cursor < len(statuses) {
			if statuses[cursor] == status {
				found = true
				cursor++
				break
			}
			cursor++
		}
		if !found {
			t.Fatalf("lifecycle statuses = %v, missing %q in order %v", statuses, status, want)
		}
	}
}

func assertNoLifecycleStatus(t *testing.T, statuses []string, forbidden string) {
	t.Helper()

	for _, status := range statuses {
		if status == forbidden {
			t.Fatalf("lifecycle statuses = %v, want no %q", statuses, forbidden)
		}
	}
}

package lifecycle

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	"odin-os/internal/executors/contract"
	"odin-os/internal/runtime/checkpoints"
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/jobs"
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

func TestRunDoctorJSONIncludesMediaChecksWhenMediaConfigOverrideIsSet(t *testing.T) {
	root := createRuntimeRoot(t)
	mediaConfigPath := filepath.Join(t.TempDir(), "media-stack.yaml")
	if err := os.WriteFile(mediaConfigPath, []byte(`
enabled: true
services:
  - name: plex
    kind: plex
policies:
  auto_allowed:
    - media_probe_cycle
`), 0o644); err != nil {
		t.Fatalf("write media config: %v", err)
	}

	t.Setenv("ODIN_MEDIA_CONFIG", mediaConfigPath)
	t.Setenv("ODIN_MEDIA_PROBE_COMMAND", filepath.Join("..", "..", "..", "scripts", "tests", "fixtures", "media-probe-mount-mismatch.sh"))

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var report struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("doctor output is not valid json: %v\n%s", err, stdout.String())
	}

	for _, check := range report.Checks {
		if check.Name == "media.mounts" && check.Status == "failed" {
			return
		}
	}
	t.Fatalf("doctor report missing failed media.mounts check: %s", stdout.String())
}

func TestRunHealthcheckHealthyReturnsNil(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	seedRuntimeState(t, root, "ready", time.Now().UTC())
	holdServiceLock(t, root)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckUsesConfiguredEnvFileRuntimeRoot(t *testing.T) {
	repoRoot := createRuntimeRoot(t)
	runtimeRoot := createRuntimeRoot(t)
	seedHealthyRuntime(t, runtimeRoot)
	seedRuntimeState(t, runtimeRoot, "ready", time.Now().UTC())
	holdServiceLock(t, runtimeRoot)

	envFile := filepath.Join(t.TempDir(), "odin-os.env")
	if err := os.WriteFile(envFile, []byte("ODIN_ROOT="+runtimeRoot+"\nODIN_HTTP_ADDR=127.0.0.1:9444\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("ODIN_ENV_FILE", envFile)
	t.Setenv("ODIN_ROOT", "")
	t.Setenv("ODIN_HTTP_ADDR", "")

	var stdout bytes.Buffer
	if err := Run(context.Background(), repoRoot, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v\noutput=%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckReadyStateWithoutLiveServiceLockReturnsError(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)
	seedRuntimeState(t, root, "ready", time.Now().UTC())

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(healthcheck) error = nil, want readiness error without live service lock")
	}
	if !strings.Contains(stdout.String(), "no live odin serve process owns runtime root") {
		t.Fatalf("healthcheck output = %q, want missing-service-owner message", stdout.String())
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
	go func() {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		forcedStale := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-deadline.C:
				cancel()
				return
			case <-ticker.C:
				if !forcedStale {
					state, err := store.GetRuntimeState(context.Background())
					if err == nil && state.Status == "ready" {
						staleAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
						if _, err := store.DB().ExecContext(context.Background(), `
							UPDATE projection_freshness
							SET refreshed_at = ?, updated_at = ?
							WHERE surface = 'doctor'
						`, staleAt, staleAt); err != nil {
							t.Errorf("force stale projection freshness error = %v", err)
							cancel()
							return
						}
						forcedStale = true
					}
				}
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

func TestServeGoalTickStartsApprovedGoal(t *testing.T) {
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

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Serve approved goal"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{sqlite.GoalStatusPlanned, sqlite.GoalStatusApprovedForExecution} {
		if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: goal.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		goalInterval: time.Hour,
	})
	time.AfterFunc(150*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	got, err := store.GetGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if got.Status != sqlite.GoalStatusRunning || got.CurrentRunID == nil {
		t.Fatalf("goal after serve = %+v, want running with active run", got)
	}
	runs, err := store.ListGoalRunsByGoalID(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want one active run from serve tick", len(runs))
	}
	counts := countServeGoalEvents(t, store)
	if counts[string(runtimeevents.EventGoalRunnerObserved)] != 1 || counts[string(runtimeevents.EventGoalRunStarted)] != 1 {
		t.Fatalf("goal event counts = %#v, want serve observed and started", counts)
	}
}

func TestServeGoalTickDoesNotRunUnapprovedPlannedGoal(t *testing.T) {
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

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Serve planned goal"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusPlanned}); err != nil {
		t.Fatalf("TransitionGoal(planned) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		goalInterval: time.Hour,
	})
	time.AfterFunc(150*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	got, err := store.GetGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if got.Status != sqlite.GoalStatusPlanned || got.CurrentRunID != nil {
		t.Fatalf("goal after serve = %+v, want planned without active run", got)
	}
	runs, err := store.ListGoalRunsByGoalID(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want no run for unapproved planned goal", len(runs))
	}
}

func TestServeGoalTickDoesNotRunConvertedIntakeGoalWithoutApproval(t *testing.T) {
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

	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--text", "Build a browser executor for Odin research goals",
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake raw create --text) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		goalInterval: time.Hour,
	})
	time.AfterFunc(150*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	goals, err := store.ListGoals(context.Background(), sqlite.ListGoalsParams{})
	if err != nil {
		t.Fatalf("ListGoals() error = %v", err)
	}
	if len(goals) != 0 {
		t.Fatalf("goals after serve = %+v, want no goal before intake review approval", goals)
	}
	runs, err := store.ListGoalRunsByGoalID(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want no run before intake review approval", len(runs))
	}
}

func TestServeGoalTickDoesNotRetryBlockedGoal(t *testing.T) {
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

	goal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Serve blocked goal"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{sqlite.GoalStatusPlanned, sqlite.GoalStatusApprovedForExecution} {
		if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: goal.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
	}
	run, err := store.CreateGoalRun(context.Background(), sqlite.CreateGoalRunParams{GoalID: goal.ID, Status: sqlite.GoalRunStatusRunning})
	if err != nil {
		t.Fatalf("CreateGoalRun() error = %v", err)
	}
	if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusRunning}); err != nil {
		t.Fatalf("TransitionGoal(running) error = %v", err)
	}
	if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusBlocked}); err != nil {
		t.Fatalf("TransitionGoal(blocked) error = %v", err)
	}
	beforeCounts := countServeGoalEvents(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		goalInterval: 20 * time.Millisecond,
	})
	time.AfterFunc(120*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	runs, err := store.ListGoalRunsByGoalID(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %+v, want original blocked goal run only", runs)
	}
	afterCounts := countServeGoalEvents(t, store)
	if afterCounts[string(runtimeevents.EventGoalRunStarted)] != beforeCounts[string(runtimeevents.EventGoalRunStarted)] {
		t.Fatalf("goal_run.started count changed from %d to %d, want no retry", beforeCounts[string(runtimeevents.EventGoalRunStarted)], afterCounts[string(runtimeevents.EventGoalRunStarted)])
	}
	if afterCounts[string(runtimeevents.EventGoalBlockerRecorded)] != beforeCounts[string(runtimeevents.EventGoalBlockerRecorded)] {
		t.Fatalf("goal.blocker_recorded count changed from %d to %d, want no blocked retry", beforeCounts[string(runtimeevents.EventGoalBlockerRecorded)], afterCounts[string(runtimeevents.EventGoalBlockerRecorded)])
	}
}

func TestServeGoalTickSkipsCompletedAndWaitingGoals(t *testing.T) {
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

	waiting, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Serve waiting goal"})
	if err != nil {
		t.Fatalf("CreateGoal(waiting) error = %v", err)
	}
	if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: waiting.ID, Status: sqlite.GoalStatusWaitingForHuman}); err != nil {
		t.Fatalf("TransitionGoal(waiting) error = %v", err)
	}
	completed, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Serve completed goal"})
	if err != nil {
		t.Fatalf("CreateGoal(completed) error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{
		sqlite.GoalStatusPlanned,
		sqlite.GoalStatusApprovedForExecution,
		sqlite.GoalStatusRunning,
		sqlite.GoalStatusVerifying,
		sqlite.GoalStatusCompleted,
	} {
		if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: completed.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(completed %s) error = %v", status, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		goalInterval: time.Hour,
	})
	time.AfterFunc(150*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err = Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	for _, goalID := range []int64{waiting.ID, completed.ID} {
		runs, err := store.ListGoalRunsByGoalID(context.Background(), goalID)
		if err != nil {
			t.Fatalf("ListGoalRunsByGoalID(%d) error = %v", goalID, err)
		}
		if len(runs) != 0 {
			t.Fatalf("goal %d runs = %+v, want none for skipped serve goal", goalID, runs)
		}
	}
}

func TestRunServeCompletesAlreadyDispatchedIntakeRun(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare automatic execution proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "automatic execute test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "alpha-cli",
		"--title", "Prepare automatic execution proof",
		"--type", "request",
		"--dedup-key", "serve-execute-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake raw create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake review accept) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(work dispatch) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = withServeLoopConfig(ctx, serveLoopConfig{
		taskInterval: 20 * time.Millisecond,
	})
	time.AfterFunc(700*time.Millisecond, cancel)

	var stdout bytes.Buffer
	err := Run(ctx, root, []string{"serve"}, strings.NewReader(""), &stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v\n%s", err, stdout.String())
	}

	var runsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"runs", "--json"}, strings.NewReader(""), &runsOutput); err != nil {
		t.Fatalf("Run(runs --json) error = %v", err)
	}
	if output := runsOutput.String(); !strings.Contains(output, `"run_id": 1`) || !strings.Contains(output, `"status": "completed"`) {
		t.Fatalf("runs output = %s, want automatically completed dispatched run", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if output := jobsOutput.String(); !strings.Contains(output, `"task_id": 1`) || !strings.Contains(output, `"status": "completed"`) || strings.Contains(output, `"current_run_id"`) {
		t.Fatalf("jobs output = %s, want completed job without active run", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if output := logsOutput.String(); !strings.Contains(output, `"type": "run.execution_claimed"`) || !strings.Contains(output, `"actor": "serve.task_loop"`) || strings.Count(output, `"type": "run.finished"`) != 1 {
		t.Fatalf("logs output = %s, want one automatic execution claim and one terminal run event", output)
	}

	var statusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &statusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{"work_items=1", "open_work_items=0", "active_run_attempts=0", "dispatch=work_dispatch"} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("work status output = %s, want %s", statusOutput.String(), want)
		}
	}

	var manualOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "execute", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &manualOutput); err != nil {
		t.Fatalf("Run(work execute terminal) error = %v\n%s", err, manualOutput.String())
	}
	if output := manualOutput.String(); !strings.Contains(output, `"executed": false`) || !strings.Contains(output, `"reason": "task_not_running"`) {
		t.Fatalf("manual execute output = %s, want safe non-executing terminal response", output)
	}
}

func TestAttemptDispatchRecoversStaleExecutingRunWithoutDuplicateExecutorEntry(t *testing.T) {
	root := createRuntimeRoot(t)
	writeMutableProjectsConfig(t, root)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
`)

	now := time.Now().UTC()
	seedHealthyRuntime(t, root)
	seedRuntimeState(t, root, "ready", now)
	if err := os.MkdirAll(filepath.Join(root, "repos", "alpha", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir alpha git root: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.Now = func() time.Time { return now.Add(-2 * time.Hour) }
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
		Key:         "stale-executing-run",
		Title:       "Stale executing run",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   "codex_headless",
		Attempt:    1,
		Status:     "executing",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/alpha/task-1/run-1/try-1",
		WorktreePath: filepath.Join(root, ".worktrees", "stale-executing-run"),
		RepoRoot:     project.GitRoot,
		State:        "active",
	}); err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}
	store.Now = func() time.Time { return now }

	registry, diagnostics, err := projects.Register(filepath.Join(root, "config", "projects.yaml"))
	if err != nil {
		t.Fatalf("projects.Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("projects.Register() diagnostics = %#v", diagnostics)
	}

	executor := &countingLifecycleExecutor{key: "codex_headless"}
	err = attemptDispatchIfReady(
		ctx,
		healthsvc.Service{
			DB:           store.DB(),
			Now:          func() time.Time { return now },
			ExecutorKeys: []string{"codex_headless"},
		},
		true,
		jobs.Service{
			Store:           store,
			Registry:        registry,
			Executors:       map[string]contract.Executor{"codex_headless": executor},
			Now:             func() time.Time { return now },
			StaleRunTimeout: 30 * time.Minute,
		},
	)
	if err != nil {
		t.Fatalf("attemptDispatchIfReady() error = %v", err)
	}
	if calls := executor.calls.Load(); calls != 0 {
		t.Fatalf("executor calls = %d, want 0 after stale recovery tick", calls)
	}

	var runsOutput bytes.Buffer
	if err := Run(ctx, root, []string{"runs", "--json"}, strings.NewReader(""), &runsOutput); err != nil {
		t.Fatalf("Run(runs --json) error = %v", err)
	}
	if output := runsOutput.String(); !strings.Contains(output, `"run_id": 1`) || !strings.Contains(output, `"status": "interrupted"`) {
		t.Fatalf("runs output = %s, want interrupted stale recovery", output)
	}

	var statusOutput bytes.Buffer
	if err := Run(ctx, root, []string{"work", "status"}, strings.NewReader(""), &statusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{"work_items=1", "open_work_items=1", "active_run_attempts=0", "dispatch=work_dispatch"} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("work status output = %s, want %s", statusOutput.String(), want)
		}
	}

	var logsOutput bytes.Buffer
	if err := Run(ctx, root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if output := logsOutput.String(); !strings.Contains(output, `"type": "run.finished"`) || !strings.Contains(output, "stale executing run recovered by live service loop") {
		t.Fatalf("logs output = %s, want visible stale recovery decision", output)
	}
}

func TestRunServeGitHubIssueWebhookFeedsTriggerIngest(t *testing.T) {
	root := createRuntimeRoot(t)
	writeMutableProjectsConfig(t, root)
	addr := allocateHTTPAddr(t)
	writeRuntimeConfig(t, root, fmt.Sprintf(`
version: 1
runtime:
  root: .
service:
  http_addr: %s
  startup_recovery: true
`, addr))
	t.Setenv("ODIN_GITHUB_WEBHOOK_SECRET", "webhook-secret")

	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	extractTaskKey := func(output string, prefix string) string {
		t.Helper()
		var payload struct {
			Results []struct {
				CreatedWorkItem bool `json:"created_work_item"`
				WorkItem        struct {
					Key string `json:"key"`
				} `json:"work_item"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(trigger evaluate) error = %v\n%s", err, output)
		}
		for _, result := range payload.Results {
			if result.CreatedWorkItem && strings.HasPrefix(result.WorkItem.Key, prefix) {
				return result.WorkItem.Key
			}
		}
		t.Fatalf("trigger evaluate output = %s, want created task prefix %s", output, prefix)
		return ""
	}

	run("project", "select", "alpha")
	run("transition", "set", "cutover", "confirm", "because", "github webhook proof")
	run("trigger", "upsert", "github-low",
		"initiative=alpha",
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/alpha",
		"title=Review GitHub webhook event",
		"summary=github_webhook_event",
		"--json",
	)
	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "github webhook approval proof")
	run("trigger", "upsert", "github-risky",
		"initiative=odin-core",
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/odin-core",
		"title=Review risky GitHub webhook event",
		"summary=github_webhook_risky_event",
		"--json",
	)

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
	serveStopped := false
	stopServe := func() {
		t.Helper()
		if serveStopped {
			return
		}
		cancel()
		err := <-runErr
		serveStopped = true
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run(serve) error = %v", err)
		}
	}
	t.Cleanup(stopServe)
	if err := waitForServeHealthStatus(ctx, "http://"+addr, http.StatusOK, "degraded", "/healthz"); err != nil {
		t.Fatal(err)
	}

	lowBody := []byte(`{"action":"opened","repository":{"full_name":"acme/alpha"},"issue":{"number":77,"title":"Low risk GitHub issue","body":"prepare release checklist","html_url":"https://github.example/acme/alpha/issues/77"}}`)
	if status, body := postGitHubWebhook(t, "http://"+addr+"/webhooks/github/issues", lowBody, "bad-secret"); status != http.StatusUnauthorized {
		t.Fatalf("invalid signature status=%d body=%s, want %d", status, body, http.StatusUnauthorized)
	}
	if status, body := postGitHubWebhook(t, "http://"+addr+"/webhooks/github/issues", lowBody, "webhook-secret"); status != http.StatusAccepted {
		t.Fatalf("webhook status=%d body=%s, want %d", status, body, http.StatusAccepted)
	} else if !strings.Contains(body, `"external_event_key":"github:issue:acme/alpha:77:opened"`) {
		t.Fatalf("webhook body=%s, want stable external event key", body)
	}
	if status, body := postGitHubWebhook(t, "http://"+addr+"/webhooks/github/issues", lowBody, "webhook-secret"); status != http.StatusAccepted {
		t.Fatalf("webhook replay status=%d body=%s, want %d", status, body, http.StatusAccepted)
	}

	riskyBody := []byte(`{"action":"opened","repository":{"full_name":"acme/odin-core"},"issue":{"number":9,"title":"Governance mutation request","body":"change system policy","html_url":"https://github.example/acme/odin-core/issues/9"}}`)
	if status, body := postGitHubWebhook(t, "http://"+addr+"/webhooks/github/issues?project=odin-core", riskyBody, "webhook-secret"); status != http.StatusAccepted {
		t.Fatalf("risky webhook status=%d body=%s, want %d", status, body, http.StatusAccepted)
	}

	stopServe()

	evaluate := run("trigger", "evaluate", "source=events", "--json")
	lowTaskKey := extractTaskKey(evaluate, "automation-github-low-")
	riskyTaskKey := extractTaskKey(evaluate, "automation-github-risky-")
	for _, want := range []string{
		`"materialization_key": "default:github-low:event:external-github-issue-acme-alpha-77-opened"`,
		`"materialization_key": "default:github-risky:event:external-github-issue-acme-odin-core-9-opened"`,
	} {
		if !strings.Contains(evaluate, want) {
			t.Fatalf("trigger evaluate output = %s, want %s", evaluate, want)
		}
	}
	replayEvaluate := run("trigger", "evaluate", "source=events", "--json")
	if !strings.Contains(replayEvaluate, `"materialized": 0`) || !strings.Contains(replayEvaluate, lowTaskKey) || !strings.Contains(replayEvaluate, riskyTaskKey) {
		t.Fatalf("replay evaluate output = %s, want duplicate delivery suppressed", replayEvaluate)
	}
	dispatch := run("work", "dispatch", "--task", riskyTaskKey, "--json")
	if !strings.Contains(dispatch, `"reason": "task_not_queued"`) || !strings.Contains(dispatch, `"status": "blocked"`) {
		t.Fatalf("risky dispatch output = %s, want already-blocked approval gate", dispatch)
	}
	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "external.github.issue"`,
		`"external_event_key": "github:issue:acme/odin-core:9:opened"`,
		`"type": "automation_trigger.materialized"`,
		`"source_event_type": "external.github.issue"`,
		`"type": "approval.requested"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
	approvals := run("approvals", "all", "--json")
	if !strings.Contains(approvals, `"status": "pending"`) || !strings.Contains(approvals, riskyTaskKey) {
		t.Fatalf("approvals output = %s, want pending risky webhook approval", approvals)
	}
}

func TestServeEnsuresSocialCopilotJobWhenEnabled(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: false
  social_copilot:
    enabled: true
    workflow_key: marcus-social-growth-workflow
    cadence_seconds: 1800
`)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)

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

	project, err := store.GetProjectByKey(context.Background(), "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	task, err := store.GetTaskByProjectAndKey(context.Background(), project.ID, "workflow-marcus-social-growth-workflow-social-copilot-loop")
	if err != nil {
		t.Fatalf("GetTaskByProjectAndKey(social copilot) error = %v", err)
	}
	if task.Status != "scheduled" {
		t.Fatalf("Task.Status = %q, want scheduled", task.Status)
	}

	var completedRuns int
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM runs
		WHERE task_id = ? AND executor = 'social_copilot' AND status = 'completed'
	`, task.ID).Scan(&completedRuns); err != nil {
		t.Fatalf("count social copilot runs: %v", err)
	}
	if completedRuns == 0 {
		t.Fatal("social copilot completed run count = 0, want startup due check")
	}

	packets, err := store.ListContextPackets(context.Background(), sqlite.ListContextPacketsParams{
		TaskID:      &task.ID,
		PacketScope: "workflow_job_metadata",
	})
	if err != nil {
		t.Fatalf("ListContextPackets() error = %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("metadata packet count = 0, want account action metadata")
	}
	if latest := packets[len(packets)-1]; !strings.Contains(latest.PayloadJSON, `"account_actions":"none"`) {
		t.Fatalf("latest metadata payload = %s, want account_actions none", latest.PayloadJSON)
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
		healthInterval: 50 * time.Millisecond,
	})
	time.AfterFunc(600*time.Millisecond, cancel)

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
			Store:        store,
			WorktreeRoot: filepath.Dir(releasedLease.WorktreePath),
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

func (git *cleanupFailureGit) WorktreeDirty(context.Context, string) (bool, error) {
	return false, nil
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

type countingLifecycleExecutor struct {
	key   string
	calls atomic.Int64
}

func (executor *countingLifecycleExecutor) Key() string { return executor.key }

func (*countingLifecycleExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (*countingLifecycleExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (*countingLifecycleExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
	}, nil
}

func (executor *countingLifecycleExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	executor.calls.Add(1)
	return contract.ExecutionResult{Status: "completed", Output: "unexpected executor entry"}, nil
}

func (*countingLifecycleExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (*countingLifecycleExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (*countingLifecycleExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
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

func countServeGoalEvents(t *testing.T, store *sqlite.Store) map[string]int {
	t.Helper()
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[string]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamGoal {
			counts[string(event.Type)]++
		}
	}
	return counts
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

func postGitHubWebhook(t *testing.T, endpoint string, body []byte, secret string) (int, string) {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", "issues")
	request.Header.Set("X-GitHub-Delivery", "test-delivery")
	request.Header.Set("X-Hub-Signature-256", "sha256="+signGitHubWebhookBody(body, secret))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST webhook error = %v", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(webhook response) error = %v", err)
	}
	return response.StatusCode, string(content)
}

func signGitHubWebhookBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
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

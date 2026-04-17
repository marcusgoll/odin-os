package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
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

func TestRunDoctorMarkdownWritesOperatorReport(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"doctor", "--format", "markdown"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(doctor --format markdown) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "## Current Health Snapshot") {
		t.Fatalf("doctor markdown output = %q, want report heading", stdout.String())
	}
}

func TestRunDoctorRejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"doctor", "--format", "yaml"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatalf("Run(doctor --format yaml) error = nil, want rejection")
	}
}

func TestRunHealthcheckHealthyReturnsNil(t *testing.T) {
	configureServeHarnessDriver(t)
	root := createRuntimeRoot(t)
	seedHealthyRuntime(t, root)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck) error = %v", err)
	}

	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
	}
}

func TestRunHealthcheckFreshRuntimeReturnsNil(t *testing.T) {
	configureServeHarnessDriver(t)
	root := createRuntimeRoot(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"healthcheck"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(healthcheck fresh runtime) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("healthcheck output = %q, want readiness message", stdout.String())
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

func TestRunServeExecutesStartupRecoveryBeforeShutdown(t *testing.T) {
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

func TestRunServeExecutesStartupRecoveryWhenContextAlreadyCanceled(t *testing.T) {
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
}

func TestRunServeRunsSelfHealCycleBeforeShutdown(t *testing.T) {
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
	if err := os.RemoveAll(filepath.Join(worktrees.DefaultRoot(), "alpha", "task-1", "run-1", "try-1")); err != nil {
		t.Fatalf("RemoveAll(stale worktree path) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if _, err := store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "current",
		DetailsJSON: `{"source":"serve-test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	originalSelfHealInterval := serveSelfHealLoopInterval
	serveSelfHealLoopInterval = 20 * time.Millisecond
	defer func() {
		serveSelfHealLoopInterval = originalSelfHealInterval
	}()
	originalHealthConfig := serveHealthConfig
	serveHealthConfig = healthsvc.Config{
		QueuePressureThreshold: 10,
		ExecutorFreshnessTTL:   time.Hour,
		ProjectionFreshnessTTL: 25 * time.Millisecond,
		SourceFreshnessTTL:     time.Hour,
	}
	defer func() {
		serveHealthConfig = originalHealthConfig
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	selfHealResult := make(chan error, 1)
	go func() {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				selfHealResult <- ctx.Err()
				return
			case <-deadline.C:
				selfHealResult <- errors.New("timed out waiting for background self-heal cycle")
				cancel()
				return
			case <-ticker.C:
				events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
				if err != nil {
					continue
				}
				for _, event := range events {
					if string(event.Type) == "recovery.action_executed" {
						selfHealResult <- nil
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

	select {
	case err := <-selfHealResult:
		if err != nil {
			t.Fatalf("self-heal wait error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background self-heal cycle")
	}

	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
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

func TestRunServeDrainsQueuedTaskAfterStartup(t *testing.T) {
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

	ctx := context.Background()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(root, "repos", "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	transitionService := projects.Service{Store: store}
	if _, err := transitionService.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	originalTaskInterval := serveTaskLoopInterval
	serveTaskLoopInterval = 20 * time.Millisecond
	defer func() {
		serveTaskLoopInterval = originalTaskInterval
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &servedOutput{started: make(chan struct{}), marker: "serving on"}
	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, root, []string{"serve"}, strings.NewReader(""), stdout)
	}()

	stdout.waitStarted(t)

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "queued-task-after-start",
		Title:       "Queued after serve startup",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		gotTask, err := store.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if gotTask.Status != "queued" {
			break
		}

		select {
		case <-deadline.C:
			t.Fatal("task remained queued, want background task loop to drain it after serve started")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}
}

func TestRunServeAllowsQueuedTaskToOutliveOperationTimeout(t *testing.T) {
	root := createRuntimeRoot(t)
	writeRuntimeConfig(t, root, `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:0
  startup_recovery: true
`)
	projectKey := fmt.Sprintf("alpha-%d", time.Now().UnixNano())
	repoRoot := filepath.Join(root, "repos", "alpha")
	initServeGitRepo(t, repoRoot)
	writeServeProjectManifest(t, root, projectKey, repoRoot)
	seedHealthyRuntime(t, root)
	if err := os.RemoveAll(filepath.Join(worktrees.DefaultRoot(), projectKey, "task-1", "run-1", "try-1")); err != nil {
		t.Fatalf("RemoveAll(stale worktree path) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           projectKey,
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       repoRoot,
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	transitionService := projects.Service{Store: store}
	if _, err := transitionService.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	originalTaskInterval := serveTaskLoopInterval
	originalTimeout := serveOperationTimeout
	serveTaskLoopInterval = 20 * time.Millisecond
	serveOperationTimeout = 20 * time.Millisecond
	defer func() {
		serveTaskLoopInterval = originalTaskInterval
		serveOperationTimeout = originalTimeout
	}()

	configureServeHarnessDriverWithDelay(t, 50*time.Millisecond)

	serveCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &servedOutput{started: make(chan struct{}), marker: "serving on"}
	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(serveCtx, root, []string{"serve"}, strings.NewReader(""), stdout)
	}()

	stdout.waitStarted(t)

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "queued-task-outlives-timeout",
		Title:       "Queued task outlives serve operation timeout",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	for {
		gotTask, err := store.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if gotTask.Status == "completed" {
			break
		}
		if gotTask.Status == "failed" || gotTask.Status == "dead_letter" {
			t.Fatalf("task status = %q, want completed (summary=%q terminal_reason=%q)", gotTask.Status, gotTask.Summary, gotTask.TerminalReason)
		}

		select {
		case <-deadline.C:
			t.Fatal("task did not complete under serve after exceeding serveOperationTimeout once")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}
}

func TestRunServeEmitsMetricsLoopLogRecords(t *testing.T) {
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

	originalMetricsInterval := serveMetricsLoopInterval
	serveMetricsLoopInterval = 20 * time.Millisecond
	defer func() {
		serveMetricsLoopInterval = originalMetricsInterval
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &servedOutput{started: make(chan struct{}), marker: "serving on"}
	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, root, []string{"serve"}, strings.NewReader(""), stdout)
	}()

	stdout.waitStarted(t)

	logPath := filepath.Join(root, "runs", "logs", "odin-service.log")
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	var logOutput string
	for {
		encoded, err := os.ReadFile(logPath)
		if err == nil {
			logOutput = string(encoded)
			if strings.Contains(logOutput, `"component":"metrics"`) && strings.Contains(logOutput, `"message":"metrics snapshot exported"`) {
				break
			}
		}

		select {
		case <-deadline.C:
			t.Fatalf("log output = %q, want metrics loop record", logOutput)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	if err := <-runErr; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(serve) error = %v", err)
	}
}

func TestServeOperationContextHasDeadline(t *testing.T) {
	originalTimeout := serveOperationTimeout
	serveOperationTimeout = 20 * time.Millisecond
	defer func() {
		serveOperationTimeout = originalTimeout
	}()

	ctx, cancel := serveOperationContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("serveOperationContext() returned context without deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("deadline = %v, want future deadline", deadline)
	}

	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want context deadline exceeded", ctx.Err())
	}
}

func TestServeServeContextPropagatesCancellation(t *testing.T) {
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

	parent, cancelParent := context.WithCancel(context.Background())
	serveCtx, cancelServe := serveServeContext(parent)
	defer cancelServe()

	cancelParent()

	select {
	case <-serveCtx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("serve context did not cancel when parent context was canceled")
	}
	if !errors.Is(serveCtx.Err(), context.Canceled) {
		t.Fatalf("serveCtx.Err() = %v, want context canceled", serveCtx.Err())
	}
}

func TestServeLoadContextDetachesFromActiveParentCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())

	loadCtx, cancelLoad := serveLoadContext(parent)
	defer cancelLoad()

	cancelParent()
	select {
	case <-loadCtx.Done():
		if errors.Is(loadCtx.Err(), context.Canceled) {
			t.Fatal("serveLoadContext() should ignore parent cancellation during serve startup")
		}
	default:
	}
	select {
	case <-loadCtx.Done():
		t.Fatal("serveLoadContext() should not be canceled by the parent context")
	case <-time.After(100 * time.Millisecond):
	}
	if loadCtx.Err() != nil {
		t.Fatalf("serveLoadContext() Err() = %v, want nil after detaching from parent cancellation", loadCtx.Err())
	}
}

func TestServeLoadContextDetachesOnlyAfterCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	loadCtx, cancelLoad := serveLoadContext(parent)
	defer cancelLoad()
	if loadCtx == parent {
		t.Fatal("serveLoadContext() should detach from a canceled parent context")
	}
	if loadCtx.Err() != nil {
		t.Fatalf("serveLoadContext() Err() = %v, want nil after detaching", loadCtx.Err())
	}
	select {
	case <-loadCtx.Done():
		t.Fatal("detached load context should not be done immediately")
	default:
	}
}

func TestServeLoadContextDoesNotDetachOnDeadlineExceeded(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancelParent()
	<-parent.Done()

	loadCtx, cancelLoad := serveLoadContext(parent)
	defer cancelLoad()
	if !errors.Is(loadCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("serveLoadContext() Err() = %v, want deadline exceeded", loadCtx.Err())
	}
}

func TestServeStartupContextDetachesFromActiveParentCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	originalTimeout := serveOperationTimeout
	serveOperationTimeout = 20 * time.Millisecond
	defer func() {
		serveOperationTimeout = originalTimeout
	}()

	startupCtx, cancelStartup := serveStartupContext(parent)
	defer cancelStartup()

	if _, ok := startupCtx.Deadline(); !ok {
		t.Fatal("serveStartupContext() should provide a deadline when the parent is active")
	}

	cancelParent()
	select {
	case <-startupCtx.Done():
		if errors.Is(startupCtx.Err(), context.Canceled) {
			t.Fatal("serveStartupContext() should ignore parent cancellation during startup recovery")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("serveStartupContext() did not finish after startup timeout")
	}
	if !errors.Is(startupCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("serveStartupContext() Err() = %v, want deadline exceeded", startupCtx.Err())
	}
}

func TestServeStartupContextDetachesOnlyAfterCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	startupCtx, cancelStartup := serveStartupContext(parent)
	defer cancelStartup()

	if startupCtx == parent {
		t.Fatal("serveStartupContext() should detach from a canceled parent context")
	}
	if startupCtx.Err() != nil {
		t.Fatalf("serveStartupContext() Err() = %v, want nil after detaching", startupCtx.Err())
	}
	select {
	case <-startupCtx.Done():
		t.Fatal("detached startup context should not be done immediately")
	default:
	}
}

func TestServeStartupContextDoesNotDetachOnDeadlineExceeded(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancelParent()
	<-parent.Done()

	startupCtx, cancelStartup := serveStartupContext(parent)
	defer cancelStartup()

	if !errors.Is(startupCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("serveStartupContext() Err() = %v, want deadline exceeded", startupCtx.Err())
	}
}

func TestRunServeReturnsServerErrorWithoutWaitingForShutdown(t *testing.T) {
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

	originalListen := serveListen
	originalTimeout := serveOperationTimeout
	serveListen = func(string, string) (net.Listener, error) {
		return errTestListener{}, nil
	}
	serveOperationTimeout = 20 * time.Millisecond
	defer func() {
		serveListen = originalListen
		serveOperationTimeout = originalTimeout
	}()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), root, []string{"serve"}, strings.NewReader(""), &bytes.Buffer{})
	}()

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "listener exploded") {
			t.Fatalf("Run(serve) error = %v, want listener failure", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Run(serve) did not return after listener failure")
	}
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

func writeServeProjectManifest(t *testing.T, root string, projectKey string, repoRoot string) {
	t.Helper()

	content := fmt.Sprintf(`
version: 1
projects:
  - key: %s
    name: Alpha
    project_class: local_git_project
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
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`, projectKey, repoRoot)
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}

func initServeGitRepo(t *testing.T, repoRoot string) {
	t.Helper()

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# alpha\n"), 0o644); err != nil {
		t.Fatalf("write repo readme: %v", err)
	}

	runServeGit(t, repoRoot, "init", "-b", "main")
	runServeGit(t, repoRoot, "config", "user.name", "Serve Test")
	runServeGit(t, repoRoot, "config", "user.email", "serve-test@example.com")
	runServeGit(t, repoRoot, "add", "README.md")
	runServeGit(t, repoRoot, "commit", "-m", "init")
}

func runServeGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, string(output))
	}
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

func configureServeHarnessDriver(t *testing.T) {
	t.Helper()
	configureServeHarnessDriverWithDelay(t, 0)
}

func configureServeHarnessDriverWithDelay(t *testing.T, delay time.Duration) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
payload="$(cat)"
sleep_ms=%d
if [[ "${sleep_ms}" != "0" ]]; then
  python3 - <<'PY'
import time
time.sleep(%0.6f)
PY
fi
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"serve test driver healthy"}))
else:
    print(json.dumps({"status":"completed","output":"driver test ok","handle":{"external_id":"fixture-driver"}}))
PY
`, delay.Milliseconds(), delay.Seconds())
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}

type errTestListener struct{}

func (errTestListener) Accept() (net.Conn, error) { return nil, errors.New("listener exploded") }
func (errTestListener) Close() error              { return nil }
func (errTestListener) Addr() net.Addr            { return errTestAddr("test-listener") }

type errTestAddr string

func (addr errTestAddr) Network() string { return string(addr) }
func (addr errTestAddr) String() string  { return string(addr) }

type servedOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started chan struct{}
	marker  string
	once    sync.Once
}

func (output *servedOutput) Write(p []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()

	n, err := output.buffer.Write(p)
	if err != nil {
		return n, err
	}
	if output.marker != "" && strings.Contains(output.buffer.String(), output.marker) {
		output.once.Do(func() {
			close(output.started)
		})
	}
	return n, nil
}

func (output *servedOutput) String() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.buffer.String()
}

func (output *servedOutput) waitStarted(t *testing.T) {
	t.Helper()

	select {
	case <-output.started:
		return
	case <-time.After(2 * time.Second):
		t.Fatalf("serve output = %q, want startup marker %q", output.String(), output.marker)
	}
}

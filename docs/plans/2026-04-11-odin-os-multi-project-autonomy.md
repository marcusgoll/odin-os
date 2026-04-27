# Odin OS Multi-Project Autonomy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `odin-os` the primary unattended controller for normal multi-project work while preserving human approval for high-risk mutations.

**Architecture:** Build this as a runtime-composition program, not a rewrite. Keep SQLite, manifests, transitions, approvals, worktrees, and event projections as the control-plane authority; wire one real harness executor lane through the jobs and runs services; then prove the scheduler and operator surfaces under progressive dual-run cutover against the legacy stack.

**Tech Stack:** Go 1.25, SQLite, YAML config, repo-local driver scripts, runtime event projections, worktree/lease management, integration tests, and systemd service execution.

---

## Preconditions

Before starting Task 4, either land `docs/plans/2026-04-10-odin-os-harness-cli-cutover.md` or port its equivalent operational command surface into the same branch. Multi-project autonomy needs explicit machine CLI commands for status, projects, jobs, runs, approvals, and task creation.

### Task 1: Lock the production exit contract in docs and tests

**Files:**
- Create: `docs/contracts/operational-autonomy.md`
- Modify: `docs/contracts/phase-exit-criteria.md`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Create: `tests/integration/multi_project_autonomy_test.go`

**Step 1: Write the failing tests**

```go
func TestOperationalAutonomyFreshRuntimeBecomesHealthy(t *testing.T) {
	root := t.TempDir()
	result := runOdin(t, root, "doctor", "--json")
	if result.Health != "healthy" {
		t.Fatalf("health = %q, want healthy", result.Health)
	}
}

func TestOperationalAutonomyRequiresApprovalForHighRiskMutation(t *testing.T) {
	root := seededRuntime(t)
	out := runOdin(t, root, "task", "run", "--project", "odin-core", "--action", "repo_rewrite")
	if out.Status != "awaiting_approval" {
		t.Fatalf("status = %q, want awaiting_approval", out.Status)
	}
}

func TestOperationalAutonomySchedulesAcrossMultipleProjects(t *testing.T) {
	root := seededRuntimeWithProjects(t, "odin-core", "pbs", "odin-orchestrator")
	snapshot := runServeOnce(t, root)
	if len(snapshot.Projects) < 3 {
		t.Fatalf("projects = %d, want at least 3", len(snapshot.Projects))
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'TestOperationalAutonomy' -count=1`

Expected: FAIL because the contract doc does not exist, fresh runtime health is not yet guaranteed, and the multi-project autonomy behavior is not fully composed.

**Step 3: Write the minimal implementation**

Create `docs/contracts/operational-autonomy.md` with:

- definition of "primary controller"
- approval-required action classes
- required runtime invariants
- cutover gates
- rollback triggers

Update `phase-exit-criteria.md` so the repo cannot claim operational readiness without:

- healthy fresh bootstrap
- one real executor lane
- mandatory leased worktrees for mutations
- restart recovery
- multi-project queue control

**Step 4: Run the tests to verify they still fail for the right reasons**

Run: `go test ./tests/integration -run 'TestOperationalAutonomy' -count=1`

Expected: FAIL only on missing runtime behavior, not on missing docs or test harness setup.

**Step 5: Commit**

```bash
git add docs/contracts/operational-autonomy.md docs/contracts/phase-exit-criteria.md tests/integration/alpha_acceptance_test.go tests/integration/multi_project_autonomy_test.go
git commit -m "docs: define operational autonomy exit contract"
```

### Task 2: Make fresh bootstrap and health state truthful

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/bootstrap/bootstrap_test.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/runtime/health/service_test.go`
- Modify: `config/telemetry.yaml`

**Step 1: Write the failing tests**

```go
func TestBootstrapInitializesRegistryExecutorAndProjectionFreshness(t *testing.T) {
	app := openBootstrapTestApp(t)
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	status := mustDoctor(t, app)
	if status.Health != "healthy" {
		t.Fatalf("health = %q, want healthy", status.Health)
	}
	if len(status.Checks) == 0 {
		t.Fatal("checks empty, want populated readiness state")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health -run 'TestBootstrapInitializesRegistryExecutorAndProjectionFreshness' -count=1`

Expected: FAIL because bootstrap does not fully seed runtime freshness and health state on a fresh root.

**Step 3: Write the minimal implementation**

In `bootstrap.go`, add a composed bootstrap sequence:

```go
if err := seedRegistryVersion(ctx, deps.RegistryLoader, deps.Store); err != nil {
	return err
}
if err := seedExecutorHealth(ctx, deps.ExecutorCatalog, deps.Store); err != nil {
	return err
}
if err := seedProjectionFreshness(ctx, deps.Store, []string{
	"jobs",
	"runs",
	"approvals_waiting",
	"project_transitions",
}); err != nil {
	return err
}
```

In `health/service.go`, compute degraded state only from actual missing or stale rows after bootstrap rather than assuming they are absent by default.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/app/bootstrap/bootstrap.go internal/app/bootstrap/bootstrap_test.go internal/runtime/health/service.go internal/runtime/health/service_test.go config/telemetry.yaml
git commit -m "feat: make bootstrap readiness truthful on fresh runtimes"
```

### Task 3: Wire executor config and catalog into runtime startup

**Files:**
- Modify: `internal/executors/router/catalog.go`
- Modify: `internal/executors/router/catalog_test.go`
- Modify: `internal/executors/router/config.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/runtime/jobs/service.go`

**Step 1: Write the failing tests**

```go
func TestBootstrapRegistersConfiguredExecutors(t *testing.T) {
	app := openBootstrapTestApp(t)
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if got := app.ExecutorCatalog.Count(); got == 0 {
		t.Fatalf("catalog count = %d, want configured executors", got)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/executors/router ./internal/app/bootstrap -run 'TestBootstrapRegistersConfiguredExecutors' -count=1`

Expected: FAIL because runtime bootstrap does not yet materialize the configured executor catalog for job selection.

**Step 3: Write the minimal implementation**

In `catalog.go`, add a constructor that accepts config entries and returns a catalog keyed by executor name:

```go
func NewCatalog(entries []ConfigEntry, builtins map[string]contract.Executor) (*Catalog, error) {
	catalog := &Catalog{executors: make(map[string]contract.Executor)}
	for _, entry := range entries {
		exec, ok := builtins[entry.Adapter]
		if !ok {
			return nil, fmt.Errorf("unknown executor adapter %q", entry.Adapter)
		}
		catalog.executors[entry.Name] = exec
	}
	return catalog, nil
}
```

Have `bootstrap.go` load `config/executors.yaml` and store the resulting catalog on the app container used by `jobs.Service`.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/executors/router ./internal/app/bootstrap -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/executors/router/catalog.go internal/executors/router/catalog_test.go internal/executors/router/config.go internal/app/bootstrap/bootstrap.go internal/runtime/jobs/service.go
git commit -m "feat: load configured executor catalog at runtime"
```

### Task 4: Implement one real harness executor lane

**Files:**
- Modify: `internal/executors/contract/types.go`
- Modify: `internal/executors/contract/types_test.go`
- Modify: `internal/executors/codex/adapter.go`
- Create: `scripts/drivers/codex-headless.sh`
- Modify: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing tests**

```go
func TestCodexAdapterRunTaskUsesDriverProcess(t *testing.T) {
	adapter := openCodexAdapterForTest(t)
	result, err := adapter.RunTask(context.Background(), contract.RunTaskRequest{
		TaskKey: "runtime-smoke",
		Prompt:  "say ready",
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Status != contract.RunStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.Summary == "" {
		t.Fatal("summary empty, want driver output")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/executors/contract ./internal/executors/codex ./tests/integration -run 'TestCodexAdapterRunTaskUsesDriverProcess|TestLiveDriverScripts' -count=1`

Expected: FAIL because the contract methods are still partially inert or not yet using a durable driver process.

**Step 3: Write the minimal implementation**

In `adapter.go`, invoke a repo-local driver script and decode structured JSON:

```go
cmd := exec.CommandContext(ctx, driverPath, "--task-key", req.TaskKey)
cmd.Env = append(os.Environ(), "ODIN_PROMPT="+req.Prompt)
output, err := cmd.Output()
if err != nil {
	return contract.RunTaskResult{}, err
}
var payload driverResult
if err := json.Unmarshal(output, &payload); err != nil {
	return contract.RunTaskResult{}, err
}
return contract.RunTaskResult{
	Status:  contract.RunStatus(payload.Status),
	Summary: payload.Summary,
}, nil
```

The script should be deterministic in tests and support a real harness path in production via environment variables.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/executors/contract ./internal/executors/codex ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/executors/contract/types.go internal/executors/contract/types_test.go internal/executors/codex/adapter.go scripts/drivers/codex-headless.sh tests/integration/live_driver_scripts_test.go
git commit -m "feat: add durable codex harness executor lane"
```

### Task 5: Compose queued task to run to completion

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/runtime/runs/service.go`
- Modify: `internal/runtime/runs/service_test.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write the failing tests**

```go
func TestJobServiceCompletesQueuedTaskThroughExecutor(t *testing.T) {
	service := openJobServiceWithFakeExecutor(t)
	task := mustCreateQueuedTask(t, service.Store)
	if err := service.RunNext(context.Background()); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}
	got := mustGetTask(t, service.Store, task.ID)
	if got.Status != "completed" {
		t.Fatalf("status = %q, want completed", got.Status)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/jobs ./internal/runtime/runs ./internal/store/sqlite -run 'TestJobServiceCompletesQueuedTaskThroughExecutor' -count=1`

Expected: FAIL because queued tasks are not yet fully driven through a real executor-backed lifecycle.

**Step 3: Write the minimal implementation**

In `jobs/service.go`, add the composed control path:

```go
task, err := service.store.ClaimNextQueuedTask(ctx)
if err != nil {
	return err
}
run, err := service.runs.Start(ctx, task, selectedExecutor)
if err != nil {
	return err
}
result, err := service.executors.RunTask(ctx, task, run)
if err != nil {
	return service.runs.Fail(ctx, run.ID, err)
}
return service.runs.Complete(ctx, run.ID, result)
```

Persist summary, terminal reason, and artifact pointers on both task and run records.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/jobs ./internal/runtime/runs ./internal/store/sqlite -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/runtime/runs/service.go internal/runtime/runs/service_test.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go
git commit -m "feat: wire queued tasks through run completion lifecycle"
```

### Task 6: Enforce approvals, transition authority, and mandatory worktree isolation

**Files:**
- Modify: `internal/core/projects/service.go`
- Modify: `internal/core/projects/service_test.go`
- Modify: `internal/core/projects/transition.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/vcs/worktrees/paths.go`
- Modify: `internal/vcs/worktrees/paths_test.go`
- Modify: `internal/vcs/worktrees/manager.go`
- Modify: `internal/vcs/leases/manager.go`

**Step 1: Write the failing tests**

```go
func TestJobServiceRequestsApprovalForSystemProjectMutation(t *testing.T) {
	service := openMutatingJobService(t, "odin-core")
	err := service.RunNext(context.Background())
	if err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}
	if got := pendingApprovals(t, service.Store); len(got) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(got))
	}
}

func TestJobServiceRequiresLeasedWorktreeForMutableTask(t *testing.T) {
	service := openMutatingJobService(t, "pbs")
	run := mustRunNext(t, service)
	if run.WorktreePath == "" {
		t.Fatal("worktree path empty, want leased mutable worktree")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/projects ./internal/runtime/jobs ./internal/vcs/worktrees -run 'TestJobService(RequestsApprovalForSystemProjectMutation|RequiresLeasedWorktreeForMutableTask)' -count=1`

Expected: FAIL because policy enforcement and worktree requirements are not yet hard runtime boundaries for every mutating run.

**Step 3: Write the minimal implementation**

In `jobs/service.go`, separate read-only and mutating execution:

```go
decision := projects.AuthorizeTransitionAction(projects.TransitionAuthRequest{
	State:    transition.State,
	Actor:    projects.TransitionControllerOdinOS,
	Action:   mutationClass.Action,
	Mutation: mutationClass.Kind,
})
if !decision.Allowed {
	return service.requestApprovalOrFail(ctx, task, run, decision)
}
lease, err := service.worktrees.Acquire(ctx, task.ProjectKey, task.Key)
if err != nil {
	return service.runs.Fail(ctx, run.ID, err)
}
```

Update `paths.go` to expand `~` before resolving the worktree root.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core/projects ./internal/runtime/jobs ./internal/vcs/worktrees ./internal/vcs/leases -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/core/projects/service.go internal/core/projects/service_test.go internal/core/projects/transition.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/vcs/worktrees/paths.go internal/vcs/worktrees/paths_test.go internal/vcs/worktrees/manager.go internal/vcs/leases/manager.go
git commit -m "feat: enforce approvals and leased worktrees for mutable runs"
```

### Task 7: Replace canned tool behavior with runtime-backed invocation

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`
- Modify: `internal/tools/invocation/service.go`
- Modify: `internal/tools/invocation/service_test.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `tests/integration/live_driver_scripts_test.go`

**Step 1: Write the failing tests**

```go
func TestBuiltinToolInvokesRuntimeDriver(t *testing.T) {
	service := openInvocationServiceForTest(t)
	result, err := service.Invoke(context.Background(), "google_calendar_off_dates", invocation.Request{
		Args: map[string]string{"month": "2026-05"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Source != "driver" {
		t.Fatalf("source = %q, want driver", result.Source)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/catalog ./internal/tools/invocation ./tests/integration -run 'TestBuiltinToolInvokesRuntimeDriver|TestLiveDriverScripts' -count=1`

Expected: FAIL because at least one built-in tool still returns canned data instead of runtime-backed execution.

**Step 3: Write the minimal implementation**

In `builtin.go`, replace the selected canned tool with an invocation service call:

```go
return builtinTool{
	Name: "google_calendar_off_dates",
	Run: func(ctx context.Context, req Request) (Result, error) {
		return invoker.Invoke(ctx, "google_calendar_off_dates", invocation.Request{Args: req.Args})
	},
}
```

Use this pattern only for one tool first, then expand once the execution loop is stable.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/catalog ./internal/tools/invocation ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/catalog/builtin.go internal/tools/catalog/builtin_test.go internal/tools/invocation/service.go internal/tools/invocation/service_test.go internal/runtime/jobs/service.go tests/integration/live_driver_scripts_test.go
git commit -m "feat: wire runtime-backed tool invocation into jobs"
```

### Task 8: Make `serve` a real long-running autonomy loop

**Files:**
- Modify: `cmd/odin/main.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `internal/runtime/recovery/service.go`
- Modify: `internal/runtime/recovery/service_test.go`
- Modify: `internal/telemetry/logs/logger.go`
- Modify: `internal/telemetry/logs/logger_test.go`

**Step 1: Write the failing tests**

```go
func TestServeRunsBoundedExecutionAndRecoveryLoops(t *testing.T) {
	app := openServeTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if err := runServe(ctx, app); err != nil {
		t.Fatalf("runServe() error = %v", err)
	}
	if got := app.RecoveryCycles(); got == 0 {
		t.Fatalf("recovery cycles = %d, want > 0", got)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/lifecycle ./internal/runtime/recovery ./internal/telemetry/logs -run 'TestServeRunsBoundedExecutionAndRecoveryLoops' -count=1`

Expected: FAIL because `serve` does not yet behave like a production autonomy loop under signal-aware shutdown with operator-visible structured logs.

**Step 3: Write the minimal implementation**

In `main.go`, use signal-aware context:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
if err := lifecycle.Run(ctx, repoRoot, os.Args[1:], os.Stdin, os.Stdout); err != nil {
	os.Exit(1)
}
```

In `run.go`, schedule bounded tickers for:

- queue execution
- self-heal
- metrics flush

Make the logger emit newline-delimited JSON records for each cycle.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/lifecycle ./internal/runtime/recovery ./internal/telemetry/logs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add cmd/odin/main.go internal/app/lifecycle/run.go internal/app/lifecycle/serve_test.go internal/runtime/recovery/service.go internal/runtime/recovery/service_test.go internal/telemetry/logs/logger.go internal/telemetry/logs/logger_test.go
git commit -m "feat: make serve run production control loops"
```

### Task 9: Add explicit operator surfaces for status, runs, approvals, and project control

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/conversation/service_test.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `tests/integration/multi_project_autonomy_test.go`

**Step 1: Write the failing tests**

```go
func TestStatusJSONExplainsBlockedAndRunningWork(t *testing.T) {
	root := seededRuntimeWithApprovalsAndRuns(t)
	out := runOdinRaw(t, root, "status", "--json")
	if !bytes.Contains(out, []byte(`"approvals_waiting"`)) {
		t.Fatalf("status output = %s, want approvals_waiting", out)
	}
	if !bytes.Contains(out, []byte(`"stalled_runs"`)) {
		t.Fatalf("status output = %s, want stalled_runs", out)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/runtime/conversation ./internal/runtime/projections ./tests/integration -run 'TestStatusJSONExplainsBlockedAndRunningWork' -count=1`

Expected: FAIL because the operator surfaces do not yet provide a full autonomy control-plane summary.

**Step 3: Write the minimal implementation**

Expose structured projection-backed outputs for:

- queue summary
- running runs
- blocked approvals
- stalled runs
- per-project transition ownership
- budget pressure

Example projection shape:

```go
type StatusSnapshot struct {
	Health           string         `json:"health"`
	Projects         []ProjectView  `json:"projects"`
	RunsRunning      int            `json:"runs_running"`
	ApprovalsWaiting int            `json:"approvals_waiting"`
	StalledRuns      int            `json:"stalled_runs"`
}
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/runtime/conversation ./internal/runtime/projections ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/commands/commands_test.go internal/runtime/conversation/service.go internal/runtime/conversation/service_test.go internal/runtime/projections/projections.go tests/integration/multi_project_autonomy_test.go
git commit -m "feat: add operator status surfaces for autonomous control"
```

### Task 10: Add multi-project queue control, budgets, and stuck-run handling

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/tools/budgets/budgets.go`
- Modify: `internal/tools/budgets/budgets_test.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `config/projects.yaml`

**Step 1: Write the failing tests**

```go
func TestSchedulerRespectsPerProjectConcurrencyAndBudget(t *testing.T) {
	service := openMultiProjectJobService(t)
	snapshot := mustScheduleCycle(t, service)
	if snapshot.ProjectRuns["odin-core"] > 1 {
		t.Fatalf("odin-core runs = %d, want <= 1", snapshot.ProjectRuns["odin-core"])
	}
	if snapshot.ProjectRuns["pbs"] > 2 {
		t.Fatalf("pbs runs = %d, want <= 2", snapshot.ProjectRuns["pbs"])
	}
}

func TestSchedulerDemotesStalledRuns(t *testing.T) {
	service := openMultiProjectJobService(t)
	mustSeedStalledRun(t, service.Store)
	snapshot := mustScheduleCycle(t, service)
	if snapshot.StalledRuns == 0 {
		t.Fatal("stalled runs = 0, want detected stalled run")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/jobs ./internal/tools/budgets ./internal/store/sqlite -run 'TestScheduler(RespectsPerProjectConcurrencyAndBudget|DemotesStalledRuns)' -count=1`

Expected: FAIL because the scheduler is not yet a real multi-project controller with explicit resource controls.

**Step 3: Write the minimal implementation**

Add one scheduler pass that:

- groups runnable tasks by project
- skips projects over budget or concurrency
- prefers older ready work before new work
- marks stale runs for recovery or dead-letter after bounded retries

Minimal sketch:

```go
for _, project := range snapshot.Projects {
	if budgets.OverLimit(project.Key, snapshot) {
		continue
	}
	if snapshot.RunningByProject[project.Key] >= project.MaxConcurrentRuns {
		continue
	}
	if err := service.runNextForProject(ctx, project.Key); err != nil {
		recordSchedulerError(project.Key, err)
	}
}
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/jobs ./internal/tools/budgets ./internal/store/sqlite -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/tools/budgets/budgets.go internal/tools/budgets/budgets_test.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go config/projects.yaml
git commit -m "feat: add multi-project scheduler controls"
```

### Task 11: Prove dual-run cutover and retire legacy duties by capability

**Files:**
- Create: `docs/operations/odin-os-cutover.md`
- Create: `docs/operations/odin-os-rollback.md`
- Modify: `config/projects.yaml`
- Modify: `tests/integration/multi_project_autonomy_test.go`

**Step 1: Write the failing tests**

```go
func TestCutoverPilotProjectsStayRunnableWithoutLegacyPrimary(t *testing.T) {
	root := seededRuntimeWithPilotProjects(t)
	result := runPilotCutoverSimulation(t, root)
	if result.PrimaryController["pbs"] != "odin_os" {
		t.Fatalf("pbs primary controller = %q, want odin_os", result.PrimaryController["pbs"])
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'TestCutoverPilotProjectsStayRunnableWithoutLegacyPrimary' -count=1`

Expected: FAIL because cutover policy and pilot project evidence are not yet encoded.

**Step 3: Write the minimal implementation**

Document:

- pilot project selection rules
- shadow, limited-action, and cutover graduation criteria
- rollback triggers
- exact legacy duties to retire in order

Update `config/projects.yaml` with explicit pilot metadata and runtime ownership expectations for the first cutover projects.

**Step 4: Run the tests to verify they pass**

Run: `go test ./tests/integration -run 'TestCutoverPilotProjectsStayRunnableWithoutLegacyPrimary' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add docs/operations/odin-os-cutover.md docs/operations/odin-os-rollback.md config/projects.yaml tests/integration/multi_project_autonomy_test.go
git commit -m "docs: define odin-os cutover and rollback playbooks"
```

## Final Verification

After Tasks 1 through 11:

1. Run: `make test`
Expected: PASS

2. Run: `make test-alpha`
Expected: PASS

3. Run: `make build`
Expected: PASS

4. Run: `ODIN_ROOT=$(mktemp -d) ./bin/odin doctor --json`
Expected: healthy fresh bootstrap with populated readiness checks

5. Run: `ODIN_ROOT=$(mktemp -d) ./bin/odin serve`
Expected: clean startup, bounded loop activity, newline-delimited structured logs, and clean shutdown on SIGTERM

6. Run the live pilot cutover checklist from `docs/operations/odin-os-cutover.md`
Expected: `odin-os` is primary for normal work on pilot projects while high-risk actions still require approval

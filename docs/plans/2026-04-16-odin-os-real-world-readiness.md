# Odin OS Real-World Readiness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `odin-os` honest and usable for bounded real-world work by replacing the fake execution lane with one real driver-backed path, making readiness truthful, cleaning up mutable worktrees deterministically, and removing mandatory legacy-repo dependencies from live drivers.

**Architecture:** Keep SQLite, manifests, transitions, approvals, and worktree leases as the control-plane authority. Replace the in-process `codex_headless` stub with a driver-backed executor, thread expected-executor truth through bootstrap and health surfaces, clean up leased worktrees on run exit and in serve mode, and ensure operator-facing docs and catalogs expose only real capability.

**Tech Stack:** Go 1.25, SQLite, YAML config, repo-local shell drivers, standard library process execution, existing `internal/app`, `internal/runtime`, `internal/executors`, `internal/vcs`, and integration tests.

---

## Preconditions

- Use [2026-04-16-odin-os-real-world-readiness-design.md](/home/orchestrator/odin-os/docs/plans/2026-04-16-odin-os-real-world-readiness-design.md) as the design authority for this plan.
- Do not broaden scope into multi-provider execution or unattended multi-project scheduling during this pass.
- Keep one real lane as the alpha target: `codex_headless`.

### Task 1: Lock the real-world-readiness contract in docs and integration tests

**Files:**
- Create: `docs/contracts/real-world-readiness.md`
- Create: `tests/integration/real_world_readiness_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `README.md`
- Modify: `docs/operations/alpha-readiness.md`

**Step 1: Write the failing tests**

```go
func TestFreshRuntimeWithoutCodexDriverIsNotReady(t *testing.T) {
	root := t.TempDir()
	result := runOdin(t, root, "healthcheck")
	if result.ExitCode == 0 {
		t.Fatalf("healthcheck exit = %d, want non-zero without driver", result.ExitCode)
	}
}

func TestFreshRuntimeWithCodexDriverCanAnswerAndRun(t *testing.T) {
	root := t.TempDir()
	driver := writeFixtureCodexDriver(t)

	ask := runInteractiveOdin(t, root, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, "what can you do?\n")
	if strings.Contains(ask.Stdout, "codex_headless completed") {
		t.Fatalf("ask output = %q, want real answer instead of stub marker", ask.Stdout)
	}

	act := runInteractiveOdin(t, root, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, "/project odin-core\n/mode act\nprepare a release note\n")
	if !strings.Contains(act.Stdout, "run ") || strings.Contains(act.Stdout, "codex_headless completed") {
		t.Fatalf("act output = %q, want real run output", act.Stdout)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'Test(FreshRuntimeWithoutCodexDriverIsNotReady|FreshRuntimeWithCodexDriverCanAnswerAndRun)' -count=1`

Expected: FAIL because fresh runtime currently reports ready without a real driver and ask mode still emits stub-marker executor output.

**Step 3: Write the minimal implementation**

Create `docs/contracts/real-world-readiness.md` with:

- the definition of a "real" executor lane
- readiness rules for fresh runtimes with and without a configured driver
- worktree cleanup invariants
- allowed alpha claims and explicit non-goals

Update `README.md` and `docs/operations/alpha-readiness.md` so they state:

- one real lane is required before calling the system ready
- the repo is only ready for bounded alpha work when the driver-backed lane is configured
- placeholder or deferred surfaces are not presented as live capability

**Step 4: Run the tests to verify they still fail for runtime reasons only**

Run: `go test ./tests/integration -run 'Test(FreshRuntimeWithoutCodexDriverIsNotReady|FreshRuntimeWithCodexDriverCanAnswerAndRun)' -count=1`

Expected: FAIL only on runtime behavior, not on missing docs or test harness helpers.

**Step 5: Commit**

```bash
git add docs/contracts/real-world-readiness.md tests/integration/real_world_readiness_test.go tests/integration/alpha_acceptance_test.go README.md docs/operations/alpha-readiness.md
git commit -m "test: lock real-world-readiness contract"
```

### Task 2: Replace `codex_headless` with a real driver-backed executor

**Files:**
- Modify: `internal/executors/codex/adapter.go`
- Create: `internal/executors/codex/adapter_test.go`
- Modify: `internal/runtime/conversation/service_test.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Create: `scripts/drivers/codex-headless.sh`

**Step 1: Write the failing tests**

```go
func TestHeadlessHealthIsUnavailableWithoutDriver(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	report, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if report.Status != contract.HealthStatusUnavailable {
		t.Fatalf("status = %q, want unavailable", report.Status)
	}
}

func TestHeadlessRunTaskUsesConfiguredDriver(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", writeFixtureDriver(t))
	result, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "task-1",
		Kind:   contract.TaskKindGeneral,
		Scope:  "global",
		Prompt: "say ready",
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if !strings.Contains(result.Output, "ready") {
		t.Fatalf("output = %q, want driver result", result.Output)
	}
}

func TestHeadlessCapabilitiesOnlyClaimImplementedFeatures(t *testing.T) {
	caps, err := NewHeadless().Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if caps.SupportsResume || caps.SupportsCancel {
		t.Fatalf("caps = %+v, want resume/cancel disabled until implemented", caps)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/executors/codex ./internal/runtime/conversation ./internal/runtime/jobs -run 'TestHeadless|TestServiceRespond|TestExecuteNextQueued' -count=1`

Expected: FAIL because the current adapter reports `healthy` unconditionally, returns a formatted stub string from `RunTask`, and overclaims capabilities.

**Step 3: Write the minimal implementation**

Use a JSON-over-stdin/stdout driver contract keyed by `ODIN_CODEX_DRIVER`.

```go
type driverRequest struct {
	Action string            `json:"action"`
	Spec   contract.TaskSpec `json:"spec,omitempty"`
}

func (headlessExecutor) Health(ctx context.Context) (contract.HealthReport, error) {
	command := strings.TrimSpace(os.Getenv("ODIN_CODEX_DRIVER"))
	if command == "" {
		return contract.HealthReport{
			Status:  contract.HealthStatusUnavailable,
			Details: "ODIN_CODEX_DRIVER is not configured",
		}, nil
	}
	return probeDriver(ctx, command)
}

func (headlessExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	response, err := invokeDriver(ctx, "run", spec)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	return contract.ExecutionResult{
		Status: response.Status,
		Output: response.Output,
		Metadata: map[string]string{
			"lane": "driver_backed_alpha",
		},
	}, nil
}
```

Start `scripts/drivers/codex-headless.sh` with a minimal fixture-friendly contract:

```bash
#!/usr/bin/env bash
set -euo pipefail

request="$(cat)"
python3 - "$request" <<'PY'
import json, sys
req = json.loads(sys.argv[1])
if req["action"] == "health":
    print(json.dumps({"status": "healthy", "details": "fixture driver"}))
elif req["action"] == "run":
    prompt = req["spec"]["prompt"]
    print(json.dumps({"status": "completed", "output": f"ready: {prompt}"}))
else:
    print(json.dumps({"status": "failed", "output": "unsupported action"}))
PY
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/executors/codex ./internal/runtime/conversation ./internal/runtime/jobs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/executors/codex/adapter.go internal/executors/codex/adapter_test.go internal/runtime/conversation/service_test.go internal/runtime/jobs/service_test.go scripts/drivers/codex-headless.sh
git commit -m "feat: replace codex headless stub with driver-backed executor"
```

### Task 3: Make bootstrap, doctor, and healthcheck truthful against expected executors

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/bootstrap/bootstrap_test.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/runtime/health/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`

**Step 1: Write the failing tests**

```go
func TestBootstrapRecordsConfiguredExecutorStatus(t *testing.T) {
	app := openBootstrapTestApp(t)
	report := mustDoctor(t, app)
	executor := findCheck(report.Checks, "executor")
	if executor.Status != health.StatusDegraded {
		t.Fatalf("executor check = %q, want degraded without real driver", executor.Status)
	}
}

func TestHealthcheckRequiresExpectedExecutorToBeHealthy(t *testing.T) {
	root := t.TempDir()
	err := Run(context.Background(), repoRoot(t), []string{"healthcheck"}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("healthcheck error = nil, want runtime not ready")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health ./internal/api/http -run 'Test(BootstrapRecordsConfiguredExecutorStatus|HealthcheckRequiresExpectedExecutorToBeHealthy)' -count=1`

Expected: FAIL because bootstrap only records healthy rows, and health evaluates the latest executor sample instead of explicit expected executors.

**Step 3: Write the minimal implementation**

Thread expected executors into bootstrap and health.

```go
type Service struct {
	DB                *sql.DB
	ExpectedExecutors []string
	Config            Config
	Now               func() time.Time
}

for _, executorKey := range expectedExecutors(executorConfig) {
	executor := executors[executorKey]
	report, err := executor.Health(ctx)
	if err != nil {
		report = contract.HealthReport{Status: contract.HealthStatusUnavailable, Details: err.Error()}
	}
	_, _ = store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor: executorKey,
		Status:   string(report.Status),
	})
}
```

In `executorCheck`, load the latest row for each expected executor and degrade if any required lane is missing, stale, or not healthy.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health ./internal/api/http -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/app/bootstrap/bootstrap.go internal/app/bootstrap/bootstrap_test.go internal/runtime/health/service.go internal/runtime/health/service_test.go internal/app/lifecycle/run.go internal/api/http/operational.go internal/api/http/operational_test.go
git commit -m "feat: make runtime readiness depend on expected executor health"
```

### Task 4: Clean up mutable worktrees on run exit and in serve mode

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `internal/vcs/worktrees/manager_test.go`

**Step 1: Write the failing tests**

```go
func TestExecuteTaskRemovesWorktreeOnSuccess(t *testing.T) {
	service, git := newJobServiceWithMutableFixture(t)
	_, err := service.ExecuteTask(context.Background(), seededTaskID(t, service))
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if len(git.removedWorktrees) != 1 {
		t.Fatalf("removed = %d, want 1 cleaned up worktree", len(git.removedWorktrees))
	}
}

func TestServeLoopCleansReleasedAndStaleLeases(t *testing.T) {
	root := seededServeRuntime(t)
	result := runServeCycle(t, root)
	if result.CleanedWorktrees == 0 {
		t.Fatalf("CleanedWorktrees = %d, want > 0", result.CleanedWorktrees)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/jobs ./internal/app/lifecycle ./internal/vcs/worktrees -run 'Test(ExecuteTaskRemovesWorktreeOnSuccess|ServeLoopCleansReleasedAndStaleLeases)' -count=1`

Expected: FAIL because leases are only marked released in SQLite and no serve loop calls worktree cleanup.

**Step 3: Write the minimal implementation**

Add immediate cleanup after run exit:

```go
defer func() {
	releaseAssignment(ctx, service.Store, assignment)
	if assignment.LeaseID != nil && assignment.WorktreePath != project.GitRoot {
		_ = leaseManager.Git.RemoveWorktree(ctx, assignment.RepoRoot, assignment.WorktreePath)
	}
}()
```

Add a bounded cleanup loop in `runServe`:

```go
var serveWorktreeCleanupInterval = 60 * time.Second

go runWorktreeCleanupLoop(ctx, operationCtx, &background, worktrees.Manager{
	Store: app.Store,
	Git:   gitadapter.Adapter{},
}, logger)
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/jobs ./internal/app/lifecycle ./internal/vcs/worktrees -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/app/lifecycle/run.go internal/app/lifecycle/serve_test.go internal/vcs/worktrees/manager_test.go
git commit -m "fix: clean up mutable worktrees on run exit and in serve mode"
```

### Task 5: Make the live Google Calendar and Huginn drivers self-contained

**Files:**
- Create: `scripts/drivers/lib/google.sh`
- Create: `scripts/drivers/lib/browser-access.sh`
- Modify: `scripts/drivers/google-calendar-off-dates.sh`
- Modify: `scripts/drivers/huginn-pbs-session.sh`
- Modify: `tests/integration/live_driver_scripts_test.go`
- Modify: `docs/contracts/live-driver-tools.md`

**Step 1: Write the failing tests**

```go
func TestGoogleCalendarDriverUsesRepoLocalLibraryByDefault(t *testing.T) {
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_TEST_GOOGLE_RESPONSE": `{"items":[]}`,
	})
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed with repo-local library", response.Status)
	}
}

func TestHuginnDriverUsesRepoLocalLibraryByDefault(t *testing.T) {
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_TEST_HUGINN_HEALTH":   `{"ok":true,"browser":true,"page":true,"url":"https://jia.flica.net/online/mainmenu.cgi"}`,
		"ODIN_TEST_HUGINN_SNAPSHOT": "Main Menu",
	})
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed with repo-local library", response.Status)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'Test(GoogleCalendarDriverUsesRepoLocalLibraryByDefault|HuginnDriverUsesRepoLocalLibraryByDefault)' -count=1`

Expected: FAIL because the scripts still default to `/home/orchestrator/odin-orchestrator/...`.

**Step 3: Write the minimal implementation**

Move the default libs inside this repo:

```bash
driver_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
default_google_lib="${driver_dir}/lib/google.sh"
default_browser_lib="${driver_dir}/lib/browser-access.sh"
```

Copy only the minimal functions needed by these two drivers into `scripts/drivers/lib/` and keep:

- `ODIN_GOOGLE_LIB_PATH`
- `ODIN_BROWSER_ACCESS_LIB_PATH`
- `ODIN_ORCHESTRATOR_ROOT`

as optional override paths, not required defaults.

**Step 4: Run the tests to verify they pass**

Run: `go test ./tests/integration -run 'Test(GoogleCalendarDriverUsesRepoLocalLibraryByDefault|HuginnDriverUsesRepoLocalLibraryByDefault|TestGoogleCalendarDriverScriptNormalizesMonthOffDates|TestHuginnPBSSessionDriverScriptValidatesReadySession)' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add scripts/drivers/lib/google.sh scripts/drivers/lib/browser-access.sh scripts/drivers/google-calendar-off-dates.sh scripts/drivers/huginn-pbs-session.sh tests/integration/live_driver_scripts_test.go docs/contracts/live-driver-tools.md
git commit -m "fix: vendor live driver script dependencies into odin-os"
```

### Task 6: Remove or demote placeholder built-ins and align the acceptance suite with real surfaces

**Files:**
- Modify: `internal/tools/catalog/builtin.go`
- Modify: `internal/tools/catalog/builtin_test.go`
- Modify: `docs/contracts/capability-catalog.md`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `README.md`

**Step 1: Write the failing tests**

```go
func TestBuiltinCatalogDoesNotExposePlaceholderOperationalTools(t *testing.T) {
	definitions := BuiltinDefinitions()
	if _, ok := definitions["project_status"]; ok {
		t.Fatal("project_status should not be exposed until it is runtime-backed")
	}
	if _, ok := definitions["task_list"]; ok {
		t.Fatal("task_list should not be exposed until it is runtime-backed")
	}
	if _, ok := definitions["event_log"]; ok {
		t.Fatal("event_log should not be exposed until it is runtime-backed")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/catalog ./tests/integration -run 'TestBuiltinCatalogDoesNotExposePlaceholderOperationalTools|TestAlphaAcceptance' -count=1`

Expected: FAIL because the catalog still exports canned operational tools and the acceptance suite does not yet distinguish placeholder capability from real capability.

**Step 3: Write the minimal implementation**

Make the operator-facing built-in catalog truthful:

```go
definitions := []ToolDefinition{
	liveGoogleCalendarTool(),
	liveHuginnTool(),
}
```

If `project_status`, `task_list`, or `event_log` are still needed for later planner work, move them behind an explicit internal-only constructor or mark them as non-default test fixtures instead of exposing them to operators.

Update docs so the default catalog describes only:

- live driver-backed tools
- explicitly runtime-backed capabilities
- deferred items as deferred, not present

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/catalog ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/catalog/builtin.go internal/tools/catalog/builtin_test.go docs/contracts/capability-catalog.md tests/integration/alpha_acceptance_test.go README.md
git commit -m "fix: make built-in capability catalog truthful"
```

## Final Verification

Run all of these before calling the branch ready:

1. `go test ./internal/executors/codex ./internal/runtime/conversation ./internal/runtime/jobs ./internal/runtime/health ./internal/app/bootstrap ./internal/app/lifecycle ./internal/tools/catalog ./internal/api/http -count=1`
2. `go test ./tests/integration -count=1`
3. `make build`
4. `tmpdir=$(mktemp -d) && ODIN_ROOT="$tmpdir" ./bin/odin healthcheck`
5. `tmpdir=$(mktemp -d) && ODIN_ROOT="$tmpdir" ODIN_CODEX_DRIVER="$(pwd)/scripts/drivers/codex-headless.sh" ./bin/odin doctor --json`
6. `tmpdir=$(mktemp -d) && printf 'what can you do?\n' | ODIN_ROOT="$tmpdir" ODIN_CODEX_DRIVER="$(pwd)/scripts/drivers/codex-headless.sh" ./bin/odin`

Expected:

- without `ODIN_CODEX_DRIVER`, healthcheck is not ready
- with `ODIN_CODEX_DRIVER`, doctor shows healthy executor state
- ask mode and act mode return driver output, not stub markers
- no released or stale worktrees remain after the run completes

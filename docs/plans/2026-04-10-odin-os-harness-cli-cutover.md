# Odin OS Harness CLI Cutover Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `odin`'s default embedded REPL with an explicit machine-oriented CLI and add a real harness executor path for durable Codex/Claude-driven work inside `odin-os` only.

**Architecture:** `odin-os` remains the canonical runtime authority, worktree manager, policy engine, and executor router. The conversational surface moves outside the binary into the Codex/Claude harness, while `odin` exposes explicit commands with text and JSON output for inspection and mutation. Durable execution stops using stubbed in-process "headless" responses and instead routes through a harness driver process adapter so `odin-os` can manage state without owning the interactive agent session.

**Tech Stack:** Go 1.25, SQLite, YAML config, standard library JSON/process execution, existing `internal/app`, `internal/cli`, `internal/runtime`, `internal/executors`, and integration tests.

---

### Task 1: Replace the implicit REPL entrypoint with an explicit root dispatcher

**Files:**
- Create: `internal/cli/commands/root.go`
- Create: `internal/cli/commands/root_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Step 1: Write the failing tests**

```go
func TestParseRootDefaultsToHelp(t *testing.T) {
	cmd := ParseRoot(nil)
	if cmd.Name != "help" {
		t.Fatalf("Name = %q, want help", cmd.Name)
	}
}

func TestParseRootRoutesExplicitRepl(t *testing.T) {
	cmd := ParseRoot([]string{"repl"})
	if cmd.Name != "repl" {
		t.Fatalf("Name = %q, want repl", cmd.Name)
	}
}

func TestRunWithoutArgsPrintsUsageInsteadOfStartingShell(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, nil, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage: odin") {
		t.Fatalf("stdout = %q, want usage banner", stdout.String())
	}
	if strings.Contains(stdout.String(), "odin>") {
		t.Fatalf("stdout = %q, should not contain repl prompt", stdout.String())
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseRoot|RunWithoutArgsPrintsUsageInsteadOfStartingShell)' -count=1`

Expected: FAIL because `ParseRoot` does not exist and `Run()` still starts the shell when `args` is empty.

**Step 3: Write the minimal implementation**

```go
// internal/cli/commands/root.go
package commands

type RootCommand struct {
	Name string
	Args []string
}

func ParseRoot(args []string) RootCommand {
	if len(args) == 0 {
		return RootCommand{Name: "help"}
	}
	return RootCommand{
		Name: args[0],
		Args: args[1:],
	}
}
```

```go
// internal/app/lifecycle/run.go
rootCommand := commands.ParseRoot(args)

switch rootCommand.Name {
case "help":
	_, err := fmt.Fprintln(stdout, "Usage: odin <command> [args]\n\nCommands: help repl doctor healthcheck serve backup restore verify-backup status project scope transition jobs runs approvals logs task")
	return err
case "repl":
	return runRepl(ctx, app, stdin, stdout)
case "doctor":
	return runDoctor(ctx, app, rootCommand.Args, stdout)
// existing operational commands remain here
default:
	return fmt.Errorf("unknown command: %s", rootCommand.Name)
}
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseRoot|RunWithoutArgsPrintsUsageInsteadOfStartingShell)' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/root.go internal/cli/commands/root_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
git commit -m "feat: add explicit odin root command dispatcher"
```

### Task 2: Move CLI session state out of the REPL package and make it reusable

**Files:**
- Create: `internal/cli/state/session.go`
- Create: `internal/cli/state/session_test.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/cli/repl/session.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `docs/contracts/cli-session.md`

**Step 1: Write the failing tests**

```go
func TestSessionStoreLoadAndSave(t *testing.T) {
	store := state.SessionStore{Path: filepath.Join(t.TempDir(), "cli-session.json")}
	want := state.Cache{ProjectKey: "odin-core", Mode: state.ModeAsk}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Cache = %+v, want %+v", got, want)
	}
}

func TestResolveStartupStateFallsBackToGlobalAsk(t *testing.T) {
	got := state.ResolveStartupState(state.Cache{ProjectKey: "missing", Mode: state.ModeAct}, projects.Registry{})
	if got.Scope.Kind != scope.ScopeGlobal || got.Mode != state.ModeAsk {
		t.Fatalf("State = %+v, want global ask", got)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/state ./internal/cli/repl -run 'Test(SessionStore|ResolveStartupState)' -count=1`

Expected: FAIL because `internal/cli/state` does not exist.

**Step 3: Write the minimal implementation**

```go
// internal/cli/state/session.go
package state

type Mode string

const (
	ModeAsk Mode = "ask"
	ModeAct Mode = "act"
)

type Cache struct {
	ProjectKey string `json:"project_key,omitempty"`
	Mode       Mode   `json:"mode,omitempty"`
}

type SessionStore struct {
	Path string
}
```

```go
// internal/cli/repl/session.go
package repl

import clistate "odin-os/internal/cli/state"

type Mode = clistate.Mode
type Cache = clistate.Cache
type SessionStore = clistate.SessionStore
type State = clistate.State

const (
	ModeAsk = clistate.ModeAsk
	ModeAct = clistate.ModeAct
)

var ResolveStartupState = clistate.ResolveStartupState
```

```go
// internal/app/bootstrap/bootstrap.go
SessionStore: clistate.SessionStore{
	Path: filepath.Join(runtimeRoot, "state", "cache", "cli-session.json"),
},
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/state ./internal/cli/repl -run 'Test(SessionStore|ResolveStartupState)' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/state/session.go internal/cli/state/session_test.go internal/app/bootstrap/bootstrap.go internal/cli/repl/session.go internal/cli/repl/shell.go docs/contracts/cli-session.md
git commit -m "refactor: extract cli session state from repl package"
```

### Task 3: Add read-only operational commands with structured JSON output

**Files:**
- Create: `internal/cli/commands/operational.go`
- Create: `internal/cli/commands/operational_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing tests**

```go
func TestRunStatusJSON(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"status", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Health            string `json:"health"`
		PendingApprovals  int    `json:"pending_approvals"`
		RegistryHealthy   bool   `json:"registry_healthy"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status json = %v", err)
	}
}

func TestRunProjectListText(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"project", "list"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "odin-core") {
		t.Fatalf("stdout = %q, want project key", stdout.String())
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'TestRun(StatusJSON|ProjectListText)' -count=1`

Expected: FAIL because `status` and `project list` commands do not exist.

**Step 3: Write the minimal implementation**

```go
// internal/cli/commands/operational.go
type StatusView struct {
	Health           string `json:"health"`
	PendingApprovals int    `json:"pending_approvals"`
	RegistryHealthy  bool   `json:"registry_healthy"`
}

func WriteStatusJSON(w io.Writer, view StatusView) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(view)
}
```

```go
// internal/app/lifecycle/run.go
case "status":
	return runStatus(ctx, app, rootCommand.Args, stdout)
case "project":
	return runProject(ctx, app, rootCommand.Args, stdout)
case "scope":
	return runScope(ctx, app, rootCommand.Args, stdout)
case "jobs":
	return runJobs(ctx, app, rootCommand.Args, stdout)
case "runs":
	return runRuns(ctx, app, rootCommand.Args, stdout)
case "approvals":
	return runApprovals(ctx, app, rootCommand.Args, stdout)
case "logs":
	return runLogs(ctx, app, rootCommand.Args, stdout)
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'TestRun(StatusJSON|ProjectListText)' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/operational.go internal/cli/commands/operational_test.go internal/app/lifecycle/run.go tests/integration/alpha_acceptance_test.go
git commit -m "feat: add odin operational read commands"
```

### Task 4: Add explicit mutating commands for project selection, transitions, and task execution

**Files:**
- Create: `internal/cli/commands/task.go`
- Create: `internal/cli/commands/task_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing tests**

```go
func TestRunProjectSelectPersistsSession(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"project", "select", "odin-core"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "project=odin-core") {
		t.Fatalf("stdout = %q, want selection confirmation", stdout.String())
	}
}

func TestRunTaskCreateJSON(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"task", "create", "--project", "odin-core", "--title", "cutover smoke"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"status\": \"queued\"") {
		t.Fatalf("stdout = %q, want queued task json", stdout.String())
	}
}

func TestRunTaskRunJSON(t *testing.T) {
	root := testRepoRoot(t)
	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"task", "run", "--project", "odin-core", "--title", "run from cli", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"run\"") {
		t.Fatalf("stdout = %q, want run payload", stdout.String())
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle ./internal/runtime/jobs -run 'TestRun(ProjectSelectPersistsSession|Task(CreateJSON|RunJSON))' -count=1`

Expected: FAIL because the commands do not exist.

**Step 3: Write the minimal implementation**

```go
// internal/cli/commands/task.go
type TaskCreateView struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

type TaskRunView struct {
	Task TaskCreateView `json:"task"`
	Run  any            `json:"run,omitempty"`
}
```

```go
// internal/app/lifecycle/run.go
case "task":
	return runTask(ctx, app, rootCommand.Args, stdout)
case "transition":
	return runTransition(ctx, app, rootCommand.Args, stdout)
```

```go
// runTask create path
task, err := jobs.CreateTaskFromAct(ctx, resolvedScope, title)

// runTask run path
outcome, err := jobs.ExecuteTask(ctx, task.ID)
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle ./internal/runtime/jobs -run 'TestRun(ProjectSelectPersistsSession|Task(CreateJSON|RunJSON))' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/task.go internal/cli/commands/task_test.go internal/app/lifecycle/run.go internal/runtime/jobs/service.go tests/integration/alpha_acceptance_test.go
git commit -m "feat: add explicit odin mutating task commands"
```

### Task 5: Replace stubbed headless executors with a harness driver process adapter

**Files:**
- Create: `internal/executors/harness/adapter.go`
- Create: `internal/executors/harness/adapter_test.go`
- Modify: `internal/executors/codex/adapter.go`
- Modify: `internal/executors/claude_code/adapter.go`
- Modify: `internal/executors/router/catalog.go`
- Modify: `config/executors.yaml`
- Modify: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing tests**

```go
func TestDriverExecutorReturnsUnavailableWhenCommandMissing(t *testing.T) {
	executor := harness.NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
	report, err := executor.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if report.Status != contract.HealthStatusUnavailable {
		t.Fatalf("Status = %q, want unavailable", report.Status)
	}
}

func TestDriverExecutorRunsFixtureProcess(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >/tmp/request.json
printf '{"status":"completed","output":"driver ok","external_id":"fixture-1"}'
`)
	t.Setenv("ODIN_CODEX_DRIVER", script)

	executor := harness.NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{ID: "t-1", Kind: contract.TaskKindGeneral, Scope: "project", Prompt: "hi"})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Output != "driver ok" {
		t.Fatalf("Output = %q, want driver ok", result.Output)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/executors/harness ./internal/executors/codex ./internal/executors/claude_code -run 'TestDriverExecutor' -count=1`

Expected: FAIL because `internal/executors/harness` does not exist and the current codex adapter always returns a fake completed string.

**Step 3: Write the minimal implementation**

```go
// internal/executors/harness/adapter.go
type DriverRequest struct {
	ExecutorKey string            `json:"executor_key"`
	Backend     string            `json:"backend"`
	Task        contract.TaskSpec `json:"task"`
}

type DriverResponse struct {
	Status     string            `json:"status"`
	Output     string            `json:"output"`
	ExternalID string            `json:"external_id"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}
```

```go
func (executor driverExecutor) Health(context.Context) (contract.HealthReport, error) {
	command := strings.TrimSpace(os.Getenv(executor.envVar))
	if command == "" {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   "driver command not configured",
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		Details:   "external harness driver configured",
		CheckedAt: time.Now().UTC(),
	}, nil
}
```

```go
// internal/executors/codex/adapter.go
func NewHeadless() contract.Executor {
	return harness.NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
}
```

```go
// internal/executors/claude_code/adapter.go
func NewHeadless() contract.Executor {
	return harness.NewDriver("claude_code_headless", "ODIN_CLAUDE_DRIVER", "claude")
}
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/executors/harness ./internal/executors/codex ./internal/executors/claude_code ./internal/runtime/jobs -run 'TestDriverExecutor|TestExecuteTask' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/executors/harness/adapter.go internal/executors/harness/adapter_test.go internal/executors/codex/adapter.go internal/executors/claude_code/adapter.go internal/executors/router/catalog.go config/executors.yaml internal/runtime/jobs/service_test.go
git commit -m "feat: route headless execution through harness drivers"
```

### Task 6: Update acceptance, contracts, and cutover docs for the new harness-first CLI

**Files:**
- Create: `docs/contracts/harness-cli.md`
- Modify: `README.md`
- Modify: `docs/contracts/executor-contract.md`
- Modify: `docs/contracts/cli-session.md`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

```go
func TestAlphaAcceptanceUsesExplicitCommands(t *testing.T) {
	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "status", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(status) error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "\"health\"") {
		t.Fatalf("output = %q, want health json", output)
	}
}

func TestReplRequiresExplicitSubcommand(t *testing.T) {
	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "/help\n", "repl")
	if err != nil {
		t.Fatalf("runOdinCommand(repl) error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "odin>") {
		t.Fatalf("output = %q, want repl prompt", output)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance|TestReplRequiresExplicitSubcommand' -count=1`

Expected: FAIL because the current acceptance still assumes `odin` starts the shell directly.

**Step 3: Write the minimal implementation**

```md
<!-- docs/contracts/harness-cli.md -->
# Harness CLI Contract

- `odin` with no args prints usage and exits 0.
- `odin repl` is the only interactive entrypoint.
- Operators and external harnesses use explicit commands plus `--json`.
- Durable Codex/Claude execution flows through configured driver commands, not stubbed in-process completions.
```

```go
// internal/cli/repl/shell.go
case "help":
	_, err := fmt.Fprintln(output, "/help /mode /project /transition /jobs /runs /approvals /logs /doctor /self")
	return err
```

Update `README.md` so the local usage section uses:

```bash
odin help
odin status --json
odin task run --project odin-core --title "smoke"
odin repl
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./tests/integration ./internal/cli/repl ./internal/app/lifecycle ./internal/executors/... -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/harness-cli.md README.md docs/contracts/executor-contract.md docs/contracts/cli-session.md tests/integration/alpha_acceptance_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "docs: finalize odin harness-first cli cutover"
```

### Final verification

Run:

```bash
go test ./internal/app/lifecycle ./internal/cli/... ./internal/runtime/conversation ./internal/runtime/jobs ./internal/executors/... ./tests/integration -count=1
```

Expected: PASS

Run:

```bash
odin help
odin status --json
odin project list
odin task create --project odin-core --title "manual smoke" --json
```

Expected:
- `odin help` prints usage without starting a prompt
- `odin status --json` returns valid JSON
- `odin project list` includes `odin-core`
- `odin task create ... --json` returns a queued task payload

### Notes for execution

- Do not touch `/home/orchestrator/odin-orchestrator`.
- Do not delete the REPL until `odin repl` parity and command-surface acceptance both pass.
- Do not leave `codex_headless` or `claude_code_headless` reporting `healthy` when no harness driver is configured.
- Prefer explicit `--project` / `--json` usage in new tests; avoid rebuilding machine flows around persisted implicit state.

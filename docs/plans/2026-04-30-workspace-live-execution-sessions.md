# Workspace Live Execution Sessions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the adoption-first `odin workspace ...` surface for binding, inspecting, attaching to, handing off, and stopping Live Execution Sessions.

**Domain Source of Truth:** `CONTEXT.md`, `docs/plans/2026-04-30-workspace-live-execution-sessions-design.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/git-worktrees.md`

**Context:** Odin OS governed work-control plane and workspace/session control surface.

**Owns / Does Not Own:** Odin owns Live Execution Session identity, lifecycle state, adoption records, local tmux liveness probes, handoff evidence, stop semantics, and command proof. Odin does not own Codex internal process state, tmux as durable truth, remote SSH process control, or Delivery Gate proof without Odin verification.

**Invariants:**
- A Live Execution Session attaches to exactly one Run Attempt.
- A Run Attempt has at most one active Live Execution Session.
- Session keys are generated from Run Attempt identity and remain canonical; aliases are lookup aids only.
- Adopted sessions require explicit operator binding and are not auto-discovered.
- Pre-adoption work is handoff context, not proof.
- Mutating sessions require distinct worktree paths and cannot share branch, path, or active lease.
- `workspace list` uses cached state; `workspace status` and `workspace attach` are local tmux probe/attachment paths.
- `workspace attach` is bind-only.
- `workspace stop` marks Odin state by default; process termination requires `--terminate`.
- Mutating sessions require handoff before stop or terminate unless explicitly abandoned.

**Architecture:** Add a small `internal/runtime/workspace` service over existing task/run/project rows, worktree leases, runtime events, and projection freshness. Add a dedicated `live_execution_sessions` SQLite table for current session state and a top-level `odin workspace ...` command adapter. Keep tmux probing behind an injected local adapter so tests do not require a real tmux server.

**Tech Stack:** Go, SQLite migrations/store methods, existing lifecycle dispatch, existing task/run/project substrate, existing worktree lease store, existing projection freshness store, standard-library process execution for tmux adapter.

---

### Task 1: Add Live Execution Session persistence

**Domain Goal:** Persist current Live Execution Session identity and lifecycle state in SQLite without making projections authoritative.

**Domain Rules Enforced:**
- Session key is canonical.
- Alias is optional and unique only among active sessions.
- A Run Attempt has at most one active Live Execution Session.
- Current state lives in SQLite, consistent with ADR-0001.

**Why this matters:**
- `workspace adopt`, `list`, `status`, `attach`, `handoff`, and `stop` need a transactional source of truth for current session state.

**Files:**
- Create: `internal/store/sqlite/migrations/0020_live_execution_sessions.sql`
- Modify: `internal/store/sqlite/migrations_test.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/store/sqlite/live_execution_sessions_test.go`

**Step 1: Write the failing migration test**

Add checks in `internal/store/sqlite/migrations_test.go`:

```go
func TestMigrationsCreateLiveExecutionSessions(t *testing.T) {
	store := openTestStore(t)
	var count int
	err := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = 'live_execution_sessions'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("live_execution_sessions table query error = %v", err)
	}
	if count != 1 {
		t.Fatalf("live_execution_sessions table count = %d, want 1", count)
	}
}
```

**Step 2: Write failing store tests**

Create `internal/store/sqlite/live_execution_sessions_test.go` with tests:

```go
func TestCreateLiveExecutionSessionPersistsCanonicalIdentity(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project, task, run := seedProjectTaskRun(t, ctx, store)

	session, err := store.CreateLiveExecutionSession(ctx, sqlite.CreateLiveExecutionSessionParams{
		SessionKey:        "les-run-1",
		Alias:             sql.NullString{String: "codex-test", Valid: true},
		ProjectID:         project.ID,
		TaskID:            task.ID,
		RunID:             run.ID,
		Executor:          "codex",
		Host:              "local",
		SessionKind:       "tmux",
		ProviderSessionID: "codex-test",
		MutationMode:      "read_only",
		RepoRoot:          project.GitRoot,
		LifecycleState:    "active",
		CachedLiveness:    "unknown",
		MetadataJSON:      `{}`,
	})
	if err != nil {
		t.Fatalf("CreateLiveExecutionSession() error = %v", err)
	}
	if session.SessionKey != "les-run-1" || session.Alias.String != "codex-test" {
		t.Fatalf("session = %+v, want canonical key and alias", session)
	}
}

func TestCreateLiveExecutionSessionRejectsSecondActiveSessionForRun(t *testing.T) {
	// Create one active session for a run, then try a second active session for the same run.
	// Expected: ErrLiveExecutionSessionConflict.
}

func TestCreateLiveExecutionSessionRejectsActiveAliasCollision(t *testing.T) {
	// Create two active sessions with alias "codex".
	// Expected: ErrLiveExecutionSessionConflict.
}
```

**Step 3: Run tests to verify failure**

Run: `go test ./internal/store/sqlite -run 'Test(MigrationsCreateLiveExecutionSessions|CreateLiveExecutionSession)' -v`

Expected: FAIL because the migration, model, and store methods do not exist.

**Step 4: Add migration**

Create `internal/store/sqlite/migrations/0020_live_execution_sessions.sql`:

```sql
CREATE TABLE IF NOT EXISTS live_execution_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_key TEXT NOT NULL UNIQUE,
  alias TEXT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  executor TEXT NOT NULL,
  host TEXT NOT NULL,
  session_kind TEXT NOT NULL,
  provider_session_id TEXT NOT NULL,
  mutation_mode TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  worktree_path TEXT,
  branch_name TEXT,
  lease_id INTEGER REFERENCES worktree_leases(id) ON DELETE SET NULL,
  lifecycle_state TEXT NOT NULL,
  cached_liveness TEXT NOT NULL,
  last_probe_at TEXT,
  last_handoff_at TEXT,
  metadata_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_live_execution_sessions_active_run
ON live_execution_sessions(run_id)
WHERE lifecycle_state IN ('active', 'stale');

CREATE UNIQUE INDEX IF NOT EXISTS idx_live_execution_sessions_active_alias
ON live_execution_sessions(alias)
WHERE alias IS NOT NULL AND lifecycle_state IN ('active', 'stale');

CREATE INDEX IF NOT EXISTS idx_live_execution_sessions_project
ON live_execution_sessions(project_id, lifecycle_state, updated_at);

CREATE INDEX IF NOT EXISTS idx_live_execution_sessions_worktree
ON live_execution_sessions(worktree_path)
WHERE worktree_path IS NOT NULL AND lifecycle_state IN ('active', 'stale');
```

**Step 5: Add model and store methods**

Add `LiveExecutionSession` plus create/get/list/update parameter structs in `internal/store/sqlite/models.go`.

Add methods in `internal/store/sqlite/store.go`:

- `CreateLiveExecutionSession`
- `GetLiveExecutionSessionByKey`
- `GetActiveLiveExecutionSessionByAlias`
- `ListLiveExecutionSessions`
- `UpdateLiveExecutionSessionLiveness`
- `RecordLiveExecutionSessionHandoff`
- `UpdateLiveExecutionSessionState`

Map unique constraint errors to `ErrLiveExecutionSessionConflict`.

**Step 6: Run store tests**

Run: `go test ./internal/store/sqlite`

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/store/sqlite/migrations/0020_live_execution_sessions.sql internal/store/sqlite/migrations_test.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/live_execution_sessions_test.go
git commit -m "feat(store): add live execution session persistence"
```

### Task 2: Add Workspace runtime service for adoption and validation

**Domain Goal:** Enforce Live Execution Session identity, adoption, cardinality, alias, and mutating worktree rules in one runtime service.

**Domain Rules Enforced:**
- Adoption is explicit and operator-bound.
- Session key is generated from Run Attempt identity.
- One active session per Run Attempt.
- Mutating sessions require distinct worktree paths.
- `--new-run` bridge is explicit and temporary.

**Why this matters:**
- Command handlers should stay thin and should not duplicate lifecycle or worktree validation logic.

**Files:**
- Create: `internal/runtime/workspace/types.go`
- Create: `internal/runtime/workspace/service.go`
- Create: `internal/runtime/workspace/service_test.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/runtime/workspace/service_test.go`

**Step 1: Write failing service tests**

Create tests:

```go
func TestAdoptExistingRunCreatesSessionWithGeneratedKey(t *testing.T) {
	// Seed project/task/run.
	// Call Service.Adopt with RunID and Alias.
	// Assert SessionKey == "run-<id>" or the chosen stable format.
	// Assert event live_execution_session.adopted was recorded.
}

func TestAdoptMutatingSessionRequiresWorktree(t *testing.T) {
	// Call Service.Adopt with MutationModeMutating and no WorktreePath.
	// Expected: error includes "worktree".
}

func TestAdoptMutatingSessionRejectsSharedActiveWorktree(t *testing.T) {
	// Seed active mutating session on /tmp/wt.
	// Adopt sibling run with same /tmp/wt.
	// Expected: conflict.
}

func TestAdoptNewRunBridgeCreatesTaskRunThenSession(t *testing.T) {
	// Call Service.AdoptNewRun with ProjectKey and Title.
	// Assert task, run, and session exist.
}
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/runtime/workspace -run TestAdopt -v`

Expected: FAIL because package does not exist.

**Step 3: Add service types**

Define:

```go
type MutationMode string
const (
	MutationModeReadOnly MutationMode = "read_only"
	MutationModeMutating MutationMode = "mutating"
)

type AdoptInput struct {
	RunID             int64
	Host              string
	SessionKind       string
	ProviderSessionID string
	Executor          string
	MutationMode      MutationMode
	Alias             string
	RepoRoot          string
	WorktreePath      string
	BranchName        string
	LeaseID           *int64
	HandoffSummary    string
	Operator          string
}

type AdoptNewRunInput struct {
	ProjectKey string
	Title      string
	AdoptInput
}
```

**Step 4: Implement minimal adoption**

`Service.Adopt` should:

1. Load run, task, and project.
2. Generate session key from run id, for example `run-<run_id>`.
3. Validate required fields.
4. Validate mutating worktree path and active conflicts.
5. Create `live_execution_sessions` row.
6. Record `live_execution_session.adopted` in `events`.
7. Touch projection freshness for `workspace_sessions`.

`Service.AdoptNewRun` should create a task and run using existing store methods before calling `Adopt`.

**Step 5: Run service tests**

Run: `go test ./internal/runtime/workspace`

Expected: PASS.

**Step 6: Run impacted tests**

Run: `go test ./internal/runtime/workspace ./internal/store/sqlite ./internal/vcs/leases`

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/runtime/workspace internal/store/sqlite/store.go
git commit -m "feat(runtime): add workspace session adoption service"
```

### Task 3: Add local tmux liveness adapter and cached status behavior

**Domain Goal:** Make `workspace status` the live local probe path while keeping `workspace list` cached and non-probing.

**Domain Rules Enforced:**
- tmux is liveness truth only for local Live Execution Sessions.
- Remote status reports cached state and `remote_probe_unsupported`.
- Missing local tmux session becomes stale or missing, not stopped.

**Why this matters:**
- Operator views stay fast and predictable, while explicit status refreshes can update cached state.

**Files:**
- Create: `internal/runtime/workspace/tmux.go`
- Modify: `internal/runtime/workspace/types.go`
- Modify: `internal/runtime/workspace/service.go`
- Test: `internal/runtime/workspace/status_test.go`

**Step 1: Write failing status tests**

Add tests:

```go
func TestStatusLocalTmuxRefreshesCachedLiveness(t *testing.T) {
	// Fake prober returns running.
	// Assert session CachedLiveness == "running" and last_probe_at set.
}

func TestStatusRemoteDoesNotProbe(t *testing.T) {
	// Session host is "remote-host".
	// Fake prober should not be called.
	// Result includes "remote_probe_unsupported".
}

func TestStatusMissingLocalTmuxMarksMissingNotStopped(t *testing.T) {
	// Fake prober returns missing.
	// Assert cached liveness missing and lifecycle remains active/stale, not stopped.
}
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/runtime/workspace -run TestStatus -v`

Expected: FAIL for missing status behavior.

**Step 3: Add adapter interface**

Add:

```go
type LocalSessionProber interface {
	Probe(ctx context.Context, sessionKind string, providerSessionID string) (ProbeResult, error)
}

type ProbeResult struct {
	Liveness string
	AttachCommand []string
	Details map[string]string
}
```

Implement `TmuxProber` with `exec.CommandContext(ctx, "tmux", "has-session", "-t", providerSessionID)`.

**Step 4: Add `Service.Status`**

Behavior:

- Resolve key or alias.
- If host is not local, return cached state with `remote_probe_unsupported`.
- Probe local tmux.
- Update `cached_liveness`, `last_probe_at`, and projection freshness.
- Record `live_execution_session.status_refreshed` event.

**Step 5: Run status tests**

Run: `go test ./internal/runtime/workspace -run TestStatus -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/runtime/workspace
git commit -m "feat(runtime): add workspace session status probing"
```

### Task 4: Add command parser and top-level `odin workspace ...` dispatch

**Domain Goal:** Expose the canonical operator surface without routing users through sidecar scripts or direct SQLite edits.

**Domain Rules Enforced:**
- `workspace` is the canonical binding/adoption/attachment surface for Live Execution Sessions.
- The command adapter is thin over the runtime service.
- Missing `odin workspace ...` support stops being a product gap for the V1 verbs.

**Why this matters:**
- Verification is incomplete unless the real `odin` command path works.

**Files:**
- Create: `internal/cli/commands/workspace.go`
- Create: `internal/cli/commands/workspace_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Test: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing command parser tests**

Add parser tests:

```go
func TestParseWorkspaceAdoptExistingRun(t *testing.T) {
	cmd, err := ParseWorkspace([]string{"adopt", "--run", "7", "--host", "local", "--session-id", "codex", "--executor", "codex", "--mode", "read_only", "--alias", "codex-test"})
	if err != nil {
		t.Fatalf("ParseWorkspace() error = %v", err)
	}
	if cmd.Action != "adopt" || cmd.RunID != 7 || cmd.Alias != "codex-test" {
		t.Fatalf("cmd = %+v", cmd)
	}
}

func TestParseWorkspaceAdoptNewRunRequiresProjectAndTitle(t *testing.T) {
	_, err := ParseWorkspace([]string{"adopt", "--new-run", "--host", "local", "--session-id", "codex", "--executor", "codex", "--mode", "read_only"})
	if err == nil {
		t.Fatal("ParseWorkspace() error = nil, want missing project/title")
	}
}
```

**Step 2: Write failing lifecycle dispatch test**

Add a test in `internal/app/lifecycle/run_test.go` or extend existing command tests:

```go
func TestRunWorkspaceListDispatchesTopLevelCommand(t *testing.T) {
	output, err := runOdinForTest(t, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list error = %v output=%s", err, output)
	}
	if !strings.Contains(output, "session_key") && !strings.Contains(output, "no live execution sessions") {
		t.Fatalf("workspace list output = %q", output)
	}
}
```

**Step 3: Run tests to verify failure**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseWorkspace|RunWorkspace)' -v`

Expected: FAIL because the parser and dispatch do not exist.

**Step 4: Implement command adapter**

Implement `RunWorkspace(ctx, deps, args, stdout, stdin)` for:

- `list`
- `adopt`
- `status`
- `attach`
- `handoff`
- `stop`

Add `case "workspace":` in `internal/app/lifecycle/run.go` and construct the workspace service with store, registry, prober, and clock dependencies.

**Step 5: Run command tests**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/cli/commands/workspace.go internal/cli/commands/workspace_test.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
git commit -m "feat(cli): add workspace command surface"
```

### Task 5: Add handoff, stop, terminate, and attach semantics

**Domain Goal:** Preserve mutating session continuity and keep attachment separate from lifecycle mutation.

**Domain Rules Enforced:**
- `attach` is bind-only.
- Non-TTY attach prints the tmux command and does not hang.
- Stop marks Odin state only by default.
- `--terminate` is explicit destructive intent.
- Mutating stop/terminate requires handoff unless abandoned.

**Why this matters:**
- Long-running SSH/Codex sessions must not lose work or accidentally terminate because an operator intended only to update Odin state.

**Files:**
- Modify: `internal/runtime/workspace/service.go`
- Modify: `internal/runtime/workspace/types.go`
- Modify: `internal/cli/commands/workspace.go`
- Test: `internal/runtime/workspace/lifecycle_test.go`
- Test: `internal/cli/commands/workspace_test.go`

**Step 1: Write failing lifecycle tests**

Add tests:

```go
func TestAttachNonTTYPrintsCommandWithoutLifecycleMutation(t *testing.T) {
	// Session is local tmux.
	// Attach with Interactive=false.
	// Assert output contains "tmux attach -t" and lifecycle_state unchanged.
}

func TestStopMutatingSessionRequiresHandoff(t *testing.T) {
	// Mutating session has no last_handoff_at.
	// Stop without abandoned.
	// Expected: error includes required handoff fields.
}

func TestStopMutatingSessionAllowsAbandonedWithReason(t *testing.T) {
	// Stop with --abandoned --reason.
	// Assert lifecycle_state == "abandoned" and event recorded.
}

func TestStopDoesNotTerminateByDefault(t *testing.T) {
	// Fake terminator should not be called.
	// Assert lifecycle_state == "stopped".
}
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/runtime/workspace ./internal/cli/commands -run 'Test(Attach|Stop)' -v`

Expected: FAIL for missing lifecycle behavior.

**Step 3: Implement handoff and stop**

Add service methods:

- `Handoff(ctx, input)`
- `Attach(ctx, input)`
- `Stop(ctx, input)`

`Handoff` records required fields in metadata JSON or a structured handoff payload and sets `last_handoff_at`.

`Stop` enforces:

- read-only stop can mark stopped.
- mutating stop requires handoff or abandoned reason.
- `--terminate` requires explicit flag and supported local process handle.
- unsupported termination leaves lifecycle unchanged and returns a clear error.

**Step 4: Implement non-TTY attach behavior**

Make command adapter accept an `Interactive` flag or use a small TTY detector dependency. For tests, inject `Interactive=false`.

Non-TTY output must include:

```text
tmux attach -t <provider_session_id>
```

**Step 5: Run lifecycle tests**

Run: `go test ./internal/runtime/workspace ./internal/cli/commands -run 'Test(Attach|Stop|Handoff)' -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/runtime/workspace internal/cli/commands/workspace.go internal/cli/commands/workspace_test.go
git commit -m "feat(workspace): add handoff attach and stop semantics"
```

### Task 6: Prove the real `odin workspace ...` operator path

**Domain Goal:** Verify the user-visible operator surface end to end through the real binary.

**Domain Rules Enforced:**
- Real E2E verification for workspace behavior exercises `odin workspace ...`.
- `list` remains cached.
- `status` probes local tmux when available.
- `attach` is bind-only and non-TTY safe.
- Stop requires handoff for mutating sessions.

**Why this matters:**
- Internal tests do not satisfy the Odin operator proof requirement when a real command path exists.

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Test: `tests/integration/alpha_acceptance_test.go`
- Modify: `docs/plans/2026-04-30-workspace-live-execution-sessions.md`

**Step 1: Add integration test for workspace command path**

In `tests/integration/alpha_acceptance_test.go`, add a subtest that:

1. Builds or uses the test binary.
2. Starts with fresh `ODIN_ROOT`.
3. Runs `workspace adopt --new-run ... --mode read_only`.
4. Runs `workspace list`.
5. Runs `workspace status`.
6. Runs non-TTY `workspace attach` and asserts it prints the tmux command.
7. Runs `workspace handoff`.
8. Runs `workspace stop`.

Use a fake prober path where possible so integration does not require a real tmux daemon. If the real binary path cannot inject a fake prober, make the status proof use a local tmux session only when `tmux` is available and skip with an explicit message otherwise.

**Step 2: Run focused tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/workspace ./internal/cli/commands ./internal/app/lifecycle
```

Expected: PASS.

**Step 3: Build real binary**

Run: `go build -o ./bin/odin ./cmd/odin`

Expected: PASS.

**Step 4: Run real command proof**

Run:

```bash
runtimeRoot="$(mktemp -d)"
ODIN_ROOT="$runtimeRoot" ./bin/odin doctor
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace adopt --new-run --project odin-core --title "Adopt local Codex tmux" --host local --session-id codex-test --executor codex --mode read_only --alias codex-test
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace list
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace status codex-test
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace attach codex-test
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace handoff codex-test summary="adoption proof" changed_paths="none" last_status="ready" verification="doctor" next_action="stop"
ODIN_ROOT="$runtimeRoot" ./bin/odin workspace stop codex-test
```

Expected:

- `doctor` reports ready/healthy.
- `adopt` reports the canonical session key.
- `list` shows cached session state.
- `status` reports running, missing, or stale with explicit liveness.
- non-TTY `attach` prints `tmux attach -t codex-test` instead of hanging.
- `handoff` records handoff.
- `stop` marks the session stopped in Odin.

**Step 5: Run broader verification**

Run: `go test ./...`

Expected: PASS or document any unrelated known failures with evidence.

**Step 6: Commit**

```bash
git add tests/integration/alpha_acceptance_test.go docs/plans/2026-04-30-workspace-live-execution-sessions.md
git commit -m "test: prove workspace session operator path"
```

## Review Checklist

- Domain naming matches `CONTEXT.md`: **Live Execution Session**, **Live Execution Session Key**, **Adopted Live Execution Session**, **Work Item**, **Run Attempt**.
- Invariant coverage exists for session key identity, one active session per run, alias uniqueness, no auto-adoption, worktree isolation, cached list behavior, status probe behavior, bind-only attach, stop default, terminate explicitness, and handoff-before-stop.
- ADR-0001 is honored: current state is in SQLite, not YAML, JSON, tmux, or memory alone.
- Boundary crossings are explicit: local tmux is an adapter for liveness/attachment, not durable authority.
- Reused repo structures are named: tasks, runs, events, worktree leases, projection freshness, lifecycle command dispatch.
- `workspace start`, remote SSH probing, remote termination, overview/TUI integration, and Delivery Gate advancement are intentionally deferred.
- Real command proof uses `./bin/odin workspace ...`; direct store tests are not treated as sufficient operator proof.

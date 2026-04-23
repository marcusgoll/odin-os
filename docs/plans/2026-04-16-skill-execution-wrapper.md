# Skill Execution Wrapper Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace direct skill handler execution with a restricted wrapper that enforces an allowlisted handler subtree, a scrubbed execution environment, and auditable execution-profile metadata.

**Architecture:** Keep `registry/skills/*.md` as the source of truth, but harden the command-backed runtime path in `internal/skills`. Validate that resolved handlers stay under `scripts/skills/`, execute them through one wrapper with controlled cwd/env/stdin/stdout/stderr behavior, and record the wrapper profile in skill lifecycle events.

**Tech Stack:** Go 1.25, existing `internal/skills` service, existing CLI/runtime event path, SQLite runtime event store, Go unit and integration tests.

---

### Task 1: Define the handler allowlist contract

**Files:**
- Create: `internal/skills/execution_policy.go`
- Create: `internal/skills/execution_policy_test.go`
- Modify: `internal/skills/invoke.go`
- Modify: `docs/contracts/skill-lifecycle.md`

**Step 1: Write the failing tests**

Add tests for:

- handler under `scripts/skills/` is allowed
- handler elsewhere in the repo is denied
- symlinked handler that resolves outside `scripts/skills/` is denied

Example test shape:

```go
func TestResolveHandlerPathRejectsHandlerOutsideAllowedSkillRoot(t *testing.T) {
	service := newTestService(t)
	writeExecutable(t, filepath.Join(service.RepoRoot, "scripts", "other", "outside.sh"), "#!/usr/bin/env bash\n")

	_, err := service.resolveHandlerPath("scripts/other/outside.sh")
	if err == nil {
		t.Fatal("resolveHandlerPath() error = nil, want denial")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills -run 'TestResolveHandlerPath' -count=1`

Expected: FAIL because the handler subtree allowlist does not exist yet.

**Step 3: Write the minimal implementation**

- add a small execution-policy helper that defines the allowed handler subtree
- update handler resolution to require the resolved path to stay under `scripts/skills/`
- keep the existing repo-relative and symlink-escape checks
- document the allowed location contract

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills -run 'TestResolveHandlerPath' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/execution_policy.go internal/skills/execution_policy_test.go internal/skills/invoke.go docs/contracts/skill-lifecycle.md
git commit -m "feat: restrict skill handlers to allowlisted repo paths"
```

### Task 2: Add the restricted command wrapper

**Files:**
- Create: `internal/skills/runner.go`
- Create: `internal/skills/runner_test.go`
- Modify: `internal/skills/invoke.go`

**Step 1: Write the failing tests**

Add tests for:

- wrapper runs handlers with repo-root cwd
- wrapper does not expose arbitrary inherited env vars
- wrapper preserves enough env for `#!/usr/bin/env bash` handlers to execute
- wrapper still enforces timeout behavior

Example test shape:

```go
func TestRestrictedRunnerScrubsInheritedEnvironment(t *testing.T) {
	runner := newRestrictedRunner(testRepoRoot)
	t.Setenv("SHOULD_NOT_LEAK", "secret")

	result, err := runner.Run(context.Background(), handlerPath, payload)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strings.Contains(result.Stdout, "secret") {
		t.Fatal("stdout leaked scrubbed environment value")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills -run 'TestRestrictedRunner' -count=1`

Expected: FAIL because the restricted wrapper does not exist yet.

**Step 3: Write the minimal implementation**

- add a small runner abstraction for command-backed skills
- set cwd to repo root
- clear the inherited environment
- add back only the explicit allowlist (`PATH`, `TMPDIR`, optional `ODIN_ROOT`, and stable Odin skill metadata vars)
- preserve stdin/stdout/stderr and timeout handling
- replace direct `exec.CommandContext(...)` in invoke with the wrapper

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills -run 'TestRestrictedRunner' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/runner.go internal/skills/runner_test.go internal/skills/invoke.go
git commit -m "feat: run skills through a restricted command wrapper"
```

### Task 3: Record execution profile metadata in lifecycle events

**Files:**
- Modify: `internal/skills/observe.go`
- Modify: `internal/skills/service.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/app/lifecycle/skills.go`
- Modify: `internal/skills/hardening_test.go`
- Modify: `docs/contracts/runtime-events.md`

**Step 1: Write the failing tests**

Add tests for:

- allowed invokes record `execution_profile=restricted_command_v1`
- denied pre-exec invokes do not falsely claim a wrapper profile
- SQLite `skill.lifecycle_recorded` payloads carry the execution profile

Example test shape:

```go
func TestInvokeRecordsRestrictedExecutionProfile(t *testing.T) {
	observer := &recordingObserver{}
	service := newTestService(t)
	service.Observer = observer

	// seed an allowed skill and invoke it

	if observer.events[len(observer.events)-1].ExecutionProfile != "restricted_command_v1" {
		t.Fatalf("ExecutionProfile = %q", observer.events[len(observer.events)-1].ExecutionProfile)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills ./internal/store/sqlite -run 'Test(InvokeRecordsRestrictedExecutionProfile|RecordSkillLifecycleEvent)' -count=1`

Expected: FAIL because lifecycle events do not carry wrapper-profile metadata yet.

**Step 3: Write the minimal implementation**

- extend the in-memory skill event model with `ExecutionProfile`
- persist that field into `skill.lifecycle_recorded`
- wire CLI/runtime recording through SQLite unchanged except for the new field
- document the new payload field and when it is present

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills ./internal/store/sqlite -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/observe.go internal/skills/service.go internal/runtime/events/events.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go internal/app/lifecycle/skills.go internal/skills/hardening_test.go docs/contracts/runtime-events.md
git commit -m "feat: audit restricted skill execution profile"
```

### Task 4: Prove the wrapper behavior end to end

**Files:**
- Modify: `tests/integration/skill_lifecycle_test.go`
- Modify: `README.md`
- Modify: `docs/contracts/skill-lifecycle.md`

**Step 1: Write the failing test**

Extend binary-driven integration coverage so it proves:

- an allowed handler under `scripts/skills/` runs through the restricted wrapper
- the handler sees repo-root cwd
- a scrubbed env var from the parent process is absent inside the handler
- lifecycle events in SQLite record `execution_profile=restricted_command_v1`
- a handler outside `scripts/skills/` is rejected

Example test shape:

```go
func TestSkillInvocationRestrictedWrapperE2E(t *testing.T) {
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	binaryPath := buildOdinBinary(t, projectRoot(t))
	runtimeRoot := t.TempDir()

	// seed a probing skill handler that reports cwd and env presence
	// invoke via compiled binary
	// inspect SQLite event payloads
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./tests/integration -run 'TestSkillInvocationRestrictedWrapperE2E' -count=1 -v`

Expected: FAIL because the direct execution path still leaks inherited environment and does not record the wrapper profile.

**Step 3: Write the minimal implementation and docs**

- finish any missing wrapper wiring revealed by the e2e test
- update README and the skill lifecycle contract with the restricted-wrapper behavior
- keep docs explicit that this is not OS sandboxing

**Step 4: Run the test to verify it passes**

Run: `go test ./tests/integration -run 'TestSkillInvocationRestrictedWrapperE2E' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/skill_lifecycle_test.go README.md docs/contracts/skill-lifecycle.md
git commit -m "test: prove restricted skill execution wrapper end to end"
```

### Task 5: Run full verification

**Files:**
- Modify: none

**Step 1: Run targeted package verification**

Run:

```bash
go test ./internal/skills ./internal/app/lifecycle ./internal/store/sqlite -count=1
```

Expected: PASS

**Step 2: Run integration verification**

Run:

```bash
go test ./tests/integration -count=1 -v
```

Expected: PASS

**Step 3: Run full repo verification**

Run:

```bash
go test ./... -count=1
make build
```

Expected: PASS

**Step 4: Commit verification-only changes if needed**

If the implementation touched docs/tests during verification cleanup:

```bash
git add README.md docs/contracts/skill-lifecycle.md docs/contracts/runtime-events.md tests/integration/skill_lifecycle_test.go
git commit -m "docs: finalize restricted skill execution contract"
```

Otherwise skip this step.

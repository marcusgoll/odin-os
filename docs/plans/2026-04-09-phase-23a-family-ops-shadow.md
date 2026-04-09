# Phase 23a Family-Ops Shadow Onboarding Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Onboard `family-ops` as a second real shadow-only managed project on the operational line and verify that Odin can supervise multiple projects without mutation.

**Architecture:** This phase is a data-and-proof slice, not a runtime feature slice. Add one conservative project manifest entry, add one focused multi-project shell test for scope and lease visibility, then run a real `family-ops` shadow smoke and capture the results in an audit.

**Tech Stack:** Go, YAML project manifests, SQLite runtime store, Odin CLI shell

---

### Task 1: Add the Family-Ops Manifest Entry

**Files:**
- Modify: `config/projects.yaml`

**Step 1: Add `family-ops` as a conservative GitHub-backed project**

Keep `config/projects.yaml` canonical.

Implement local project-overlay support so machine-local projects can be loaded from:

- `ODIN_PROJECTS_OVERLAY`
- `config/projects.local.yaml`

**Step 2: Run the focused manifest validation test**

Run:

```bash
go test ./internal/core/projects ./internal/app/bootstrap -count=1
```

Expected: PASS

**Step 3: Commit**

```bash
git add .gitignore internal/core/projects/manifest.go internal/core/projects/register.go internal/app/bootstrap/bootstrap.go internal/core/projects/manifest_test.go internal/app/bootstrap/bootstrap_test.go docs/operations/project-overlays.md
git commit -m "feat: add local project overlay support"
```

### Task 2: Add Multi-Project Shadow Scope Coverage

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing shell test**

Add a test that:

- builds a registry with `odin-core`, `alpha`, and `family-ops`
- sets `alpha` to `shadow` and records an observation
- switches to `family-ops`
- verifies `/transition` still shows default `inventory`
- verifies `/leases` remains empty for `family-ops`

**Step 2: Run only that test to verify it fails if the assumption is wrong**

Run:

```bash
go test ./internal/cli/repl -run TestShellScopesShadowStatePerProject -count=1
```

Expected: PASS if existing behavior already supports it, otherwise FAIL for the intended scope leak and then fix minimally.

**Step 3: If the test needs harness changes, make the smallest change necessary**

Only adjust the test fixture or shell logic if the new test exposes a real scoping bug.

**Step 4: Run the shell test package**

Run:

```bash
go test ./internal/cli/repl -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/repl/shell_test.go
git commit -m "test: cover multi-project shadow scope behavior"
```

### Task 3: Run the Real Family-Ops Shadow Smoke Through a Local Overlay

**Files:**
- Create: `docs/audits/phase-23a-second-project-shadow-onboarding.md`

**Step 1: Build Odin on the branch**

Run:

```bash
make build
```

Expected: PASS

**Step 2: Create a fresh runtime root and run the CLI smoke**

Create a machine-local overlay manifest containing `pbs` and `family-ops`, then run a fresh `ODIN_ROOT` through:

- `./bin/odin doctor --json`
- `/project family-ops`
- `/transition`
- `/transition set shadow because observe only`
- `/observe ...`
- `/mode act`
- one bounded smoke task
- `./bin/odin serve`

**Step 3: Capture the verification evidence**

Record:

- manifest loading
- transition state
- observation report
- task/run denial summary
- `/leases` output
- proof that no mutation occurred

**Step 4: Write the audit**

Summarize:

- what succeeded
- what remained fail-closed
- portfolio readiness recommendation for additional shadow-only onboarding

**Step 5: Commit**

```bash
git add docs/audits/phase-23a-second-project-shadow-onboarding.md
git commit -m "docs: add family-ops shadow onboarding audit"
```

### Task 4: Final Verification

**Files:**
- Verify only

**Step 1: Run the repaired verification slice**

Run:

```bash
go test ./internal/core/projects ./internal/cli/repl -count=1
make build
```

Expected: PASS

**Step 2: Report exact runtime outcomes**

Include:

- `family-ops` stayed shadow-only
- no worktree leases were created
- operator surfaces remained usable

**Step 3: Decide branch integration later**

Do not merge automatically. Present the results and current branch state.

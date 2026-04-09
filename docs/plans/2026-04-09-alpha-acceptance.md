# Alpha Acceptance Suite Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a repeatable alpha acceptance suite that checks Odin OS against the full Global Definition of Done and then use it to produce a real alpha readiness verdict.

**Architecture:** Create a single integration harness under `tests/integration/` with one top-level acceptance test and criterion-specific subtests. Use the real repository layout and authored assets where appropriate, isolate runtime mutations under temporary runtime roots, and add a `make test-alpha` target so the verdict is easy to rerun after future changes.

**Tech Stack:** Go integration tests, standard library process execution, existing internal services, SQLite, Makefile

---

### Task 1: Add the alpha acceptance entrypoint

**Files:**
- Modify: `Makefile`
- Optionally modify: `README.md`

**Step 1: Add a dedicated acceptance target**

Add:
- `test-alpha`

Run only the acceptance suite, not the whole repo.

**Step 2: Optionally note the target in README**

Keep this short if added at all.

**Step 3: Verify the target exists**

Run: `make -n test-alpha`

Expected: it prints the exact `go test` command for the acceptance suite.

### Task 2: Write the failing alpha acceptance skeleton

**Files:**
- Create: `tests/integration/alpha_acceptance_test.go`
- Create: `tests/integration/helpers_test.go`

**Step 1: Write the top-level acceptance test**

Create:
- `TestAlphaAcceptance`
- one `t.Run` per Global Definition of Done item

**Step 2: Add only the minimal helper signatures**

Create helpers for:
- repo root lookup
- temporary runtime root setup
- small CLI smoke execution
- common store seeding

Do not implement helper bodies yet beyond what is needed to compile.

**Step 3: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: FAIL because the helpers and acceptance wiring are incomplete.

### Task 3: Implement the structural and authored-truth acceptance checks

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Implement checks for real repo structure**

Verify:
- canonical top-level folders exist
- the documented authored directories are present

**Step 2: Implement real registry and governance checks**

Verify:
- the real `registry/` loads cleanly
- the real `config/projects.yaml` registers `odin-core` as a system project
- temporary manifest fixtures validate local-only and GitHub-backed project classes

**Step 3: Implement executor and tool-broker checks**

Verify:
- the real executor config exposes plan-backed CLI and API lanes
- the broker plans from thin cards and expands only selected capabilities

**Step 4: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: FAIL later in runtime smoke or lifecycle checks, but the structural subtests pass.

### Task 4: Implement CLI, runtime, and wake-packet acceptance checks

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Add CLI smoke tests**

Verify with a temporary `ODIN_ROOT`:
- the prompt header shows explicit scope and mode
- Ask mode answers without creating tasks
- Act mode in `odin-core` creates a structured task
- `odin doctor --json` emits structured output

**Step 2: Add runtime authority and wake-packet checks**

Verify:
- SQLite persists runtime state under the temporary runtime root
- compaction creates wake packets
- resume loads from wake packets

**Step 3: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: FAIL later in worktree, self-heal, migration, or homelab checks if those are not wired yet.

### Task 5: Implement worktree, observability, and self-heal acceptance checks

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Add worktree isolation checks**

Verify:
- mutating work gets a task-owned branch name
- mutable worktree paths are isolated
- leases prevent conflicts

**Step 2: Add observability checks**

Verify:
- doctor report is structured and useful
- metrics can be collected
- incidents and recoveries appear in projections

**Step 3: Add self-heal checks**

Verify:
- a deterministic fault triggers a playbook
- the action is recorded in recoveries and events

**Step 4: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: FAIL later in migration, transitions, learning, or homelab checks if those are not yet wired.

### Task 6: Implement migration, transition, and self-improvement acceptance checks

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Add migration extraction check**

Verify:
- `/home/orchestrator/odin-orchestrator` exists
- the extractor runs against it into temporary docs/state roots
- inventory and duplicate reports are produced

**Step 2: Add transition ladder checks**

Verify:
- shadow is read-only
- limited-action allows only the allowlisted isolated mutation

**Step 3: Add self-improvement lifecycle checks**

Verify:
- proposals can be created, submitted, evaluated, promoted, and rolled back
- promotion without approval is rejected

**Step 4: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: FAIL only in the final homelab backup or restart-safety path if any real alpha gaps remain.

### Task 7: Implement homelab restart-safety and backup acceptance checks

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Add service restart-safety checks**

Verify:
- startup recovery converts running runs into interrupted and resumable state
- restart wake packets are created

**Step 2: Add backup and restore smoke checks**

Verify through real top-level commands or the real backup service:
- backup archive creation
- backup verification
- restore into a fresh target root

**Step 3: Run the acceptance suite**

Run: `go test ./tests/integration -run TestAlphaAcceptance -v`

Expected: PASS if the repo satisfies the alpha gate.

### Task 8: Run full verification and record the verdict

**Files:**
- Verify only

**Step 1: Run the acceptance suite through the Makefile**

Run: `make test-alpha`

Expected: exit 0

**Step 2: Run repo formatting check**

Run: `make fmtcheck`

Expected: exit 0

**Step 3: Run lint**

Run: `make lint`

Expected: exit 0

**Step 4: Run the full repo test matrix**

Run: `make test`

Expected: exit 0

**Step 5: Run the build**

Run: `make build`

Expected: exit 0

### Task 9: Commit the implementation

**Files:**
- Commit all acceptance-suite implementation files

**Step 1: Review the final diff**

Run: `git status --short && git diff --stat`

Expected: only acceptance-suite files and any minimal real-gap fixes are present.

**Step 2: Commit**

Run:

```bash
git add Makefile README.md docs/plans/2026-04-09-alpha-acceptance.md tests/integration
git commit -m "test: add alpha acceptance suite"
```

Expected: one implementation commit for the acceptance suite and any necessary acceptance-driven fixes.

# Alpha Acceptance Suite Design

## Goal

Add a repeatable end-to-end alpha acceptance suite that maps directly to the Odin OS Global Definition of Done, then use that suite to produce a real alpha readiness verdict instead of a one-off manual judgment.

## Current Context

Odin OS now has strong package-level coverage across the phase work:

- registry loading and validation
- SQLite runtime state and events
- CLI shell behavior
- executor routing
- tool broker behavior
- wake packets and restart recovery
- worktree leases
- observability
- self-heal
- migration extraction
- project transitions
- self-improvement
- homelab deployment surfaces

What the repository does not have is a single repeatable acceptance layer that checks the system against the actual alpha criteria listed by the operator.

Today, `tests/integration/`, `tests/replay/`, and `tests/unit/` are placeholders only.

## Approaches Considered

### 1. One-off manual alpha verification run

This is the fastest path to a single answer, but it is weak. It produces no durable acceptance harness, makes reruns harder after future changes, and depends too much on human memory.

### 2. Repeatable alpha acceptance suite plus a final run

This is the recommended approach. Add a small integration harness under `tests/integration/` that maps each alpha Definition of Done item to a concrete verification surface, then run it and report the real pass or fail outcome.

### 3. Full black-box homelab simulation

This would be the most realistic test environment, but it is too heavy for the first acceptance pass. It would add operational complexity before the real remaining gaps are even known.

## Recommendation

Add a repeatable alpha acceptance suite under `tests/integration/` and expose it through a dedicated `make test-alpha` target.

The suite should:

- use the real repository layout and authored assets where appropriate
- isolate runtime mutations under temporary runtime roots
- use package APIs for cross-cutting verification where that is the most honest seam
- use a small number of real CLI smoke tests for top-level operator paths

The acceptance output should stay binary and direct: if a Definition of Done item is not satisfied, the suite fails.

## Scope Of The Suite

The suite should cover each Global Definition of Done item:

1. repo structure matches the documented canonical layout
2. Markdown registry system works
3. SQLite is the canonical runtime authority
4. managed projects support local-only git and GitHub-backed modes
5. `odin-core` is handled as a special system project
6. CLI shell supports Ask and Act with explicit scope visibility
7. executor abstraction supports both headless CLI lanes and API lanes
8. dynamic tool loading is working
9. context compaction and wake packets work
10. mutating tasks use isolated worktrees and task-owned branches
11. observability and doctor surfaces are useful
12. self-heal playbooks run and are audited
13. migration extraction from `odin-orchestrator` works
14. projects can onboard in shadow mode and limited-action mode
15. self-improvement exists only through proposals, evaluation, and promotion
16. the system can run on the homelab with restart safety and backups

## Test Architecture

The suite should live in `tests/integration/alpha_acceptance_test.go` with a small helper file beside it.

Recommended structure:

- one top-level `TestAlphaAcceptance`
- one `t.Run(...)` per acceptance criterion
- shared helpers for:
  - locating the real repo root
  - creating isolated runtime roots
  - running small CLI smoke tests
  - creating temporary managed-project fixtures

This keeps the suite easy to read and directly traceable to the alpha gate.

## Verification Strategy Per Criterion

### Structural and authored-truth criteria

These should verify the real repo:

- canonical folders exist
- registry assets load cleanly from `registry/`
- the real `config/projects.yaml` registers `odin-core` as a system project
- the real executor config includes both plan-backed CLI lanes and API lanes

### Runtime behavior criteria

These should use isolated temporary runtime roots:

- CLI Ask and Act smoke behavior
- wake packet compaction and resume
- startup recovery and backup round-trips
- doctor and health surfaces

Using temporary runtime roots avoids mutating the real `data/odin.db` during acceptance.

### Governance and lifecycle criteria

These may use temporary fixture manifests and stores:

- local-only and GitHub-backed project manifest support
- transition shadow and limited-action enforcement
- self-improvement proposal, evaluation, promotion, and rollback lifecycle
- self-heal playbook execution and event audit

### Migration criteria

The suite should verify migration extraction against the real legacy source at `/home/orchestrator/odin-orchestrator` when it exists. If that source root is missing, the migration acceptance subtest should fail with a clear explanation because the alpha gate explicitly depends on that migration source being usable.

## CLI Smoke Coverage

The suite should include a small number of actual top-level CLI smoke tests using `go run ./cmd/odin` with a temporary `ODIN_ROOT`.

Recommended smoke cases:

- interactive shell header plus Ask and Act mode behavior
- `odin doctor --json`
- `odin healthcheck`
- `odin backup`, `odin verify-backup`, and `odin restore`

This keeps the suite grounded in real operator entrypoints without turning the whole acceptance layer into expensive black-box process orchestration.

## Makefile Integration

Add a dedicated target:

- `make test-alpha`

This should run only the acceptance suite, not the full repo test matrix.

The existing `make test` remains unchanged and still runs `go test ./...`.

## Non-Goals

The alpha acceptance suite should not:

- introduce a second policy or architecture layer
- duplicate all existing unit tests
- simulate a multi-node environment
- silently downgrade failing criteria into warnings
- rely on hidden preloaded context or manual operator interpretation

## Success Condition

This work succeeds when Odin OS has a rerunnable alpha acceptance harness that produces an explicit pass or fail result against the full Global Definition of Done, and the resulting verdict is grounded in actual repo behavior rather than narrative summaries.

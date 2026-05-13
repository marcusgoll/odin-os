# Odin Actual-Use Completion Roadmap

Generated: 2026-05-12

## Current State

This roadmap turns the current Odin OS codebase into a proven actual-use operator system without creating new runtime authorities. It is grounded in the installed `odin` binary for live truth and the repo-local `./bin/odin` binary for source-local truth.

Inputs reviewed:

- Present: `README.md`, `CONTEXT.md`, `AGENTS.md`, `WORKFLOW.md`, `Makefile`, `cmd/odin`, `internal/app/lifecycle`, `internal/store/sqlite`, `internal/runtime`, `internal/executors`, `internal/vcs`, `registry`
- Present in the dirty primary checkout and used as briefing evidence: `docs/briefings/2026-05-12-odin-os-current-state.md`, `docs/briefings/2026-05-12-odin-os-gap-analysis-briefing.md`
- Absent at `/home/orchestrator/odin-os`: `Current-State.txt`, `Gap-Analysis.txt`, `Product-Brief.txt`

Live proof snapshot from `/home/orchestrator/odin-os`:

```bash
which odin
realpath "$(which odin)"
odin status || true
odin doctor || true
odin healthcheck || true
odin overview --json || true
```

Observed:

- Installed `odin` resolves to `/home/orchestrator/odin-os/releases/current/bin/odin`.
- Live status/doctor are healthy enough for inspection, with two pending approvals and no active runs.
- Live `odin healthcheck` fails closed with `runtime not ready`.
- Live overview exposes workspace, initiatives, companions, work items, capability catalog, skill activity, delegation truth, approvals, observability, memory, knowledge context packs, intake inbox, and automation triggers.

Source-local proof from the clean worktree:

- `cmd/odin` is the canonical command entrypoint.
- `internal/app/lifecycle` owns command/service composition and review queue composition.
- `internal/store/sqlite` owns runtime authority, including work items, runs, approvals, intake items, automation triggers, execution intent, worktree leases, pull request handoffs, browser handoff state, and memory proposals.
- `internal/runtime` owns jobs, approvals, triggers, recovery, projections, supervision, browser handoff, memory proposals, and related runtime services.
- `internal/executors` is the canonical executor seam; `codex_headless` is the current live local alpha lane.
- `internal/vcs` is the canonical git/worktree lease seam.
- `registry` contains authored agents, skills, commands, and workflows.

## What Already Exists

Odin already has the important substrate for actual use:

- One intended operator surface: `odin ...`
- SQLite runtime authority with migrations for the key durable objects
- Live review queue via `odin review`
- Approval records and resolver-aware approval handling
- Raw intake and work intake/reconcile paths
- GitHub tracker package for issue intake and bounded mutation contracts
- Automation trigger storage and materialization paths
- Work dispatch/execute/retry command paths over `internal/runtime/jobs`
- Mandatory mutable-worktree policy in the runtime execution path
- Executor routing and `codex_headless` alpha execution
- Operational HTTP health/readiness/metrics surfaces behind `odin serve`
- Registry-backed capability inventory
- TUI/overview/doctor/status readback surfaces
- Documentation that already separates alpha readiness, work intake, GitHub tracker mutations, verification model, and brownfield seams

## Gaps

Actual-use completion is blocked by these remaining gaps:

1. Live readiness is not currently asserted; `odin healthcheck` fails closed.
2. Installed `odin` and repo-local `./bin/odin` can diverge, so command truth must be explicitly scoped.
3. The primary checkout is dirty; implementation must use isolated clean worktrees.
4. Duplicate or shallow seams still need retirement or promotion decisions, especially `cmd/odin-os`, `internal/runner`, `internal/orchestrator`, `internal/dashboard`, `internal/db`, `internal/config`, `internal/logging`, `internal/review`, `internal/security`, and `configs`.
5. GitHub issue intake exists, but the full issue-to-work-to-PR actual-use path is not one proven operator flow.
6. PR handoff storage exists, but branch push, draft PR creation/update, review handoff, and closeout are not yet a fully proven live path.
7. Real Codex subprocess execution remains security-sensitive and must not bypass `internal/executors`.
8. Scheduler/trigger behavior is partially present, but actual-use scheduling needs proof through existing trigger/materialization/readback surfaces.
9. Approval gates exist, but every mutating class must fail closed through the same review/approval surfaces.
10. Dashboard/admin surfaces must remain projections and bounded controls over SQLite state, not a second control plane.

## Reuse Plan

Keep these centers of gravity:

- `cmd/odin` for operator entry.
- `internal/app/lifecycle` for command/service wiring.
- `internal/store/sqlite` for runtime authority.
- `internal/runtime/jobs` for work dispatch and execution admission.
- `internal/runtime/approvals` plus `odin review` for human decisions.
- `internal/runtime/triggers` and `odin trigger` for scheduled/event-driven work creation.
- `internal/tracker` for GitHub issue intake and approved tracker mutations.
- `internal/executors` for any worker or Codex execution.
- `internal/vcs` for branch/worktree isolation.
- `internal/api/http` for operational HTTP and dashboard projections.
- `registry` for authored agents, skills, workflows, and commands.

Do not add a second queue, scheduler, approval store, executor runner family, dashboard authority, GitHub authority, or readiness authority.

## Completion Phases

### Phase 0: Authority And Readiness Baseline

Goal: make the operator's starting state unambiguous and safely repeatable.

PR-sized tickets:

1. Document and test installed-vs-repo-local binary proof.
   - Files: `docs/operations/`, `scripts/tests/`, nearest CLI tests if needed.
   - Acceptance criteria: `which odin`, `realpath "$(which odin)"`, `odin help`, and `./bin/odin help` are reported together in every operator proof.
   - Command proofs: `which odin`, `realpath "$(which odin)"`, `odin help`, `./bin/odin help`.
   - Stop condition: stop if installed `odin` does not resolve to the intended release/current path or if repo-local `./bin/odin` is stale after `make build`.

2. Restore and prove live readiness.
   - Files: `internal/app/lifecycle`, `internal/runtime/health`, `internal/runtime/state`, `docs/operations/alpha-readiness.md`.
   - Acceptance criteria: a controlled runtime root fails closed before `serve`, becomes ready while `odin serve` is running, and fails closed after shutdown; the live root's readiness state is explained by `odin doctor`.
   - Command proofs: `ODIN_ROOT="$(mktemp -d)" ./bin/odin healthcheck`, `ODIN_ROOT="$tmp" ODIN_HTTP_ADDR=127.0.0.1:0 ./bin/odin serve`, `ODIN_ROOT="$tmp" ./bin/odin healthcheck`, `ODIN_ROOT="$tmp" ./bin/odin doctor --json`.
   - Stop condition: stop if readiness can be marked ready without a live `serve` process or if `doctor` cannot explain a not-ready state.

3. Lock clean-worktree operating rule.
   - Files: `docs/operations/`, optional `scripts/tests/`.
   - Acceptance criteria: operator docs require isolated worktrees for dirty checkouts and include the exact `git status --short --branch` proof.
   - Command proofs: `git status --short --branch`, `git worktree list`.
   - Stop condition: stop if work would edit the dirty primary checkout without explicit operator approval.

### Phase 1: Review Queue And Approval Completeness

Goal: all governed decisions flow through one review/approval operator surface.

PR-sized tickets:

1. Prove review source coverage on fresh runtime fixtures.
   - Files: `internal/app/lifecycle/review_sources.go`, `internal/app/lifecycle/review_sources_test.go`, `docs/contracts/tui-overview.md`.
   - Acceptance criteria: `odin review list --json` includes intake review, intake approval, intake goal conversion, goal, task approval, skill artifact, context pack, failed work, recovery, and memory proposal sources when fixtures exist.
   - Command proofs: `ODIN_ROOT="$tmp" ./bin/odin review list --json`, plus focused lifecycle tests.
   - Stop condition: stop if any source requires a second queue or a source-specific command to be visible.

2. Fail closed on unsupported approvals.
   - Files: `internal/runtime/approvals`, `internal/app/lifecycle/review.go`, tests.
   - Acceptance criteria: pending approvals without resolver support are inspectable but cannot be approved into action; supported approvals continue only through their workflow-owned resolver.
   - Command proofs: `./bin/odin approvals all --json`, `./bin/odin review show approval:<id> --json`, `./bin/odin review act approval:<id> approve --json`.
   - Stop condition: stop if approval action can bypass resolver support or mutate unrelated work.

3. Add approval parity fixtures for high-risk classes.
   - Files: `internal/runtime/jobs`, `internal/runtime/triggers`, `internal/tools`, `internal/app/lifecycle`, policy docs.
   - Acceptance criteria: destructive filesystem, GitHub write, public publish, finance action, deployment, permission change, data deletion, and browser live-action classes block behind approval.
   - Command proofs: controlled `ODIN_ROOT` fixture commands for each class, ending in `odin review list --json`.
   - Stop condition: stop if a high-risk class can dispatch or execute without a pending approval record.

### Phase 2: Intake To Work Without Execution

Goal: make external signals become reviewable or queued work without launching workers.

PR-sized tickets:

1. Prove raw intake lifecycle.
   - Files: `internal/app/lifecycle`, `internal/store/sqlite`, `internal/runtime/projections`, `docs/operations/raw-intake.md`.
   - Acceptance criteria: raw intake create/list/show/process/review paths persist evidence, route deterministically, and read back through overview and review.
   - Command proofs: `./bin/odin intake raw create --text "..." --json`, `./bin/odin intake process --id intake-1 --json`, `./bin/odin review list --json`, `./bin/odin overview --json`.
   - Stop condition: stop if intake processing creates executable work without review or explicit routing evidence.

2. Prove GitHub issue intake dry-run and persistence.
   - Files: `internal/tracker`, `internal/cli/commands/work.go`, `docs/operations/work-intake.md`.
   - Acceptance criteria: `odin work intake --project <key> --dry-run` fetches eligible issues without persistence; non-dry-run persists `external_issues` and still reports `dispatch=not_started prs=not_created`.
   - Command proofs: fixture tests plus opt-in disposable-repo live smoke from `docs/operations/work-intake-live-smoke.md`.
   - Stop condition: stop if intake creates runs, branches, PRs, comments, labels, or worker dispatch.

3. Prove reconcile from persisted issue to Work Item.
   - Files: `internal/tracker/intake`, `internal/cli/commands/work.go`, `internal/store/sqlite`.
   - Acceptance criteria: `odin work reconcile --project <key>` creates or reuses deterministic Work Items with task intake evidence and no dispatch.
   - Command proofs: `./bin/odin work reconcile --project <key>`, `./bin/odin work status`, `./bin/odin overview --json`.
   - Stop condition: stop if GitHub labels become runtime truth or reconciliation starts workers.

### Phase 3: Work Dispatch With Existing Safe Executor

Goal: prove one queued Work Item can run through current runtime admission, leases, executor routing, events, and readback.

PR-sized tickets:

1. Prove dispatch admission matrix.
   - Files: `internal/runtime/jobs`, `internal/core/projects`, `internal/store/sqlite`, tests.
   - Acceptance criteria: read-only work may dispatch when policy permits; mutation/governance/destructive work blocks unless transition and approval requirements are satisfied.
   - Command proofs: `./bin/odin work dispatch --task <id> --json`, `./bin/odin review list --json`, `./bin/odin runs --json`.
   - Stop condition: stop if a mutable run can start without a leased task-owned worktree.

2. Prove `codex_headless` actual-use run.
   - Files: `internal/executors/codex`, `internal/runtime/jobs`, `prompts/workers`, registry workflow docs.
   - Acceptance criteria: one fixture Work Item runs through `codex_headless`, emits run output/artifact metadata, records terminal state, and appears in overview/runs.
   - Command proofs: `ODIN_CODEX_DRIVER_ACTION=run ./bin/odin work execute --task <id> --json`, `./bin/odin runs --json`, `./bin/odin logs --json` where available.
   - Stop condition: stop if output exists only in process stdout and not in runtime state or event evidence.

3. Prove retry and failed-work recovery loop.
   - Files: `internal/runtime/recovery`, `internal/cli/commands/work.go`, `internal/app/lifecycle/review.go`.
   - Acceptance criteria: a failed retryable Work Item appears in review and can be retried through `odin work retry`, with max-attempt and next-eligible guards.
   - Command proofs: `./bin/odin review list --json`, `./bin/odin work retry --task <id> --json`.
   - Stop condition: stop if retry bypasses failure analysis or max-attempt guardrails.

### Phase 4: Scheduler And Automation Trigger Actual Use

Goal: scheduled/event triggers create or update governed Work Items, never direct worker execution.

PR-sized tickets:

1. Promote source-local scheduler command to live truth or remove it from actual-use claims.
   - Files: `cmd/odin`, `internal/app/lifecycle`, `internal/runtime/triggers`, docs.
   - Acceptance criteria: installed `odin` and repo-local `./bin/odin` agree on the scheduler/trigger operator surface, or docs explicitly state scheduler is source-local only.
   - Command proofs: `odin help`, `./bin/odin help`, `./bin/odin scheduler help`, `odin trigger list --json`.
   - Stop condition: stop if docs claim live scheduler support while installed `odin` lacks the command.

2. Prove trigger upsert/evaluate/materialize path.
   - Files: `internal/runtime/triggers`, `internal/store/sqlite`, `internal/app/lifecycle`, `internal/cli/commands/trigger.go`.
   - Acceptance criteria: a trigger can be upserted, evaluated, and materialized into a Work Item with `execution_intent_source=trigger`, then surfaced through overview/review/work status.
   - Command proofs: `./bin/odin trigger upsert ... --json`, `./bin/odin trigger evaluate --json`, `./bin/odin overview --json`, `./bin/odin work status --json`.
   - Stop condition: stop if trigger evaluation dispatches directly or creates work without provenance.

3. Prove quiet-hours/defer behavior where configured.
   - Files: `internal/runtime/triggers`, config/policy docs, tests.
   - Acceptance criteria: humanized timing and quiet-hour rules defer materialization with explicit next-eligible evidence.
   - Command proofs: deterministic `now=<RFC3339>` trigger/scheduler commands and overview readback.
   - Stop condition: stop if deferred work disappears without durable evidence.

### Phase 5: PR Handoff Without Autonomous Merge

Goal: create a review-ready handoff from a completed run without granting autonomous merge or deploy authority.

PR-sized tickets:

1. Prove PR handoff storage and readback.
   - Files: `internal/store/sqlite`, `internal/review`, `internal/app/lifecycle`, `docs/contracts/github-tracker-mutations.md`.
   - Acceptance criteria: a completed run can record PR handoff evidence, review results, branch, commit, and target repo metadata in SQLite.
   - Command proofs: fixture command or focused E2E that ends in `odin review list --json` and a PR handoff readback command.
   - Stop condition: stop if GitHub is treated as runtime authority instead of projection/handoff.

2. Add draft PR creation behind approval.
   - Files: `internal/tracker`, `internal/runtime/approvals`, `internal/app/lifecycle`, docs.
   - Acceptance criteria: proposed PR creation is shown as a dry-run approval payload first; only approved resolver execution may call GitHub to create/update a draft PR.
   - Command proofs: dry-run proposal readback, approval detail, resolver execution against disposable repo.
   - Stop condition: stop if PR creation can run without an approval that names exact repo, branch, title, body, and draft status.

3. Add human-review closeout surface.
   - Files: `internal/app/lifecycle`, `internal/runtime/projections`, `internal/tracker`, docs.
   - Acceptance criteria: Odin can mark work as human-review-ready with PR link and evidence, but cannot merge or deploy.
   - Command proofs: `./bin/odin review list --json`, `./bin/odin work status --json`, GitHub disposable PR readback.
   - Stop condition: stop if a command merges, deploys, deletes branches, or closes issues without a separate explicit approval.

### Phase 6: Real Codex Execution Behind Canonical Executor

Goal: replace or supplement `codex_headless` with a real Codex execution lane without bypassing policy.

PR-sized tickets:

1. Write the Codex execution security contract.
   - Files: `docs/contracts/executor-contract.md`, `docs/security/`, `internal/executors`.
   - Acceptance criteria: contract forbids `danger-full-access`, production secret exposure, default-branch mutation, autonomous merge/deploy, and execution outside leased worktrees.
   - Command proofs: contract tests and security tests.
   - Stop condition: stop if the contract requires a second runner authority outside `internal/executors`.

2. Move useful `internal/runner/codexexec` behavior into `internal/executors`.
   - Files: `internal/executors/codexexec` or equivalent canonical adapter, tests.
   - Acceptance criteria: real Codex subprocess adapter implements the existing executor contract and fails closed without explicit sandbox/worktree configuration.
   - Command proofs: focused adapter tests, `./bin/odin doctor --json` executor health, controlled dry-run execution.
   - Stop condition: stop if subprocess launch receives unrestricted environment, production secrets, or root repo mutation access.

3. Promote one actual-use project through real Codex lane.
   - Files: executor config, runtime jobs, docs.
   - Acceptance criteria: one low-risk read/write fixture project runs through real Codex in a leased worktree and produces reviewable evidence, not an auto-merged change.
   - Command proofs: `./bin/odin work execute --task <id> --json`, `git -C <leased-worktree> status --short`, `./bin/odin review list --json`.
   - Stop condition: stop if Codex changes the primary checkout or can access unapproved secrets.

### Phase 7: Dashboard As Read-Only Projection Plus Explicit Controls

Goal: make the web/dashboard surface useful without creating a parallel control plane.

PR-sized tickets:

1. Prove dashboard read models mirror CLI projections.
   - Files: `internal/api/http`, `internal/runtime/projections`, docs.
   - Acceptance criteria: dashboard endpoints for overview, work, runs, approvals, triggers, readiness, and PR handoffs return the same derived truth as CLI readbacks.
   - Command proofs: `./bin/odin serve`, `curl /healthz`, `curl /readyz`, `curl /metrics`, selected read-only JSON endpoints, matching CLI commands.
   - Stop condition: stop if dashboard computes independent status or bypasses SQLite projections.

2. Gate dashboard admin controls behind the same approval semantics.
   - Files: `internal/api/http`, `internal/runtime/approvals`, docs.
   - Acceptance criteria: pause/resume/kill-switch/admin actions either create reviewable approvals or execute only bounded local readiness controls with audit events.
   - Command proofs: HTTP request against controlled runtime root, `odin review list --json`, `odin doctor --json`.
   - Stop condition: stop if dashboard mutates work state without operator-visible audit evidence.

### Phase 8: Final Actual-Use E2E

Goal: prove the end-to-end actual-use path on a disposable target with no autonomous merge/deploy.

PR-sized tickets:

1. Add an actual-use E2E runbook.
   - Files: `docs/operations/actual-use-e2e.md`, optional script under `scripts/` if it only orchestrates existing commands.
   - Acceptance criteria: the runbook creates a controlled runtime root, proves readiness, ingests/reconciles one disposable issue or raw intake item, dispatches work, records run evidence, creates review/approval evidence, prepares a draft PR handoff if enabled, and stops at human review.
   - Command proofs: exact final command set below.
   - Stop condition: stop if any step requires production repo, production secrets, autonomous merge, or direct runtime DB mutation.

2. Add CI-safe fixture for the same command shape.
   - Files: `internal/e2e`, `scripts/tests`, `.github/workflows` only if necessary.
   - Acceptance criteria: fixture-backed E2E proves command wiring without live GitHub mutation.
   - Command proofs: `make odin-e2e-local` or a narrower repo-owned target.
   - Stop condition: stop if fixture proves only service helpers and not the real `odin` command path.

## Risk Register

| Risk | Severity | Current signal | Mitigation | Stop condition |
| --- | --- | --- | --- | --- |
| Readiness | High | Live `odin healthcheck` fails closed. | Make readiness state explainable through `doctor`, `/readyz`, and `healthcheck`; prove with controlled `ODIN_ROOT`. | Stop if ready can be asserted without `serve` evidence or if not-ready lacks actionable diagnostics. |
| Binary divergence | High | Installed `odin` and repo-local `./bin/odin` can expose different commands. | Always report `which odin`, `realpath`, installed help, and repo-local help. | Stop if a live claim depends on repo-local-only command output. |
| Dirty worktree | High | Primary checkout has unrelated dirty changes and gone upstream branch. | Use isolated clean worktrees from `origin/main` for all edits. | Stop if implementation would touch the dirty primary checkout. |
| Duplicate seams | High | `cmd/odin-os`, `internal/runner`, `internal/orchestrator`, `internal/dashboard`, and related scaffolds can split authority. | Promote useful code into canonical seams or retire through explicit cleanup PRs. | Stop if a new package becomes a second runtime authority. |
| Codex execution | Critical | Real subprocess execution is security-sensitive. | Keep all execution behind `internal/executors`; enforce sandbox, leased worktree, no secrets, no autonomous merge/deploy. | Stop if runner needs broad shell authority or bypasses executor contract. |
| GitHub writes | High | Read-only intake exists; mutations require stricter approval. | Dry-run first; approval payload must name exact repo/object/payload; use disposable live proof. | Stop if labels/comments/PRs can be written without approval or if GitHub becomes runtime truth. |
| Scheduler | Medium | Trigger substrate exists; installed scheduler command may diverge from repo-local command. | Use `odin trigger` as live surface; promote scheduler only after installed and repo-local agree. | Stop if scheduled trigger directly dispatches workers or lacks materialization evidence. |
| Approval bypasses | Critical | High-risk paths must fail closed across all action classes. | Centralize through review/approvals and workflow-owned resolvers. | Stop if any destructive, financial, deploy, publish, permission, or data-delete action can execute without approval. |
| Dashboard authority drift | High | Dashboard/admin code can be tempting as a second control plane. | Read-only projections first; admin controls use the same approval/audit state. | Stop if dashboard owns status, readiness, work state, or approval state separately from SQLite/CLI. |

## Exact Final E2E Command Set For Actual Use

The final actual-use proof should run from a clean worktree after `make build` and should use a controlled runtime root. Live GitHub steps must use a disposable target.

```bash
cd /home/orchestrator/odin-os
git status --short --branch
which odin
realpath "$(which odin)"
odin help
make build
./bin/odin help

tmp="$(mktemp -d)"
export ODIN_ROOT="$tmp"
export ODIN_HTTP_ADDR="127.0.0.1:0"

./bin/odin doctor --json
./bin/odin healthcheck || true

./bin/odin serve >"$tmp/serve.log" 2>&1 &
serve_pid="$!"
sleep 2
./bin/odin healthcheck
./bin/odin overview --json
./bin/odin work status --json
./bin/odin review list --json
./bin/odin trigger list --json
./bin/odin intake raw list --json

./bin/odin intake raw create --text "Create one governed fixture work item for actual-use proof" --json
./bin/odin intake process --id intake-1 --json
./bin/odin review list --json
./bin/odin overview --json

work_start="$(./bin/odin work start --project odin-core --title "Actual-use fixture work" --intent read_only)"
printf '%s\n' "$work_start"
task_key="$(printf '%s\n' "$work_start" | sed -n 's/.*key=\([^ ]*\).*/\1/p')"
test -n "$task_key"
./bin/odin work status --json
./bin/odin work dispatch --task "$task_key" --json
./bin/odin work execute --task "$task_key" --json
./bin/odin runs --json
./bin/odin review list --json
./bin/odin overview --json

./bin/odin backup "$tmp/backup.tar.gz"
./bin/odin verify-backup "$tmp/backup.tar.gz"

kill "$serve_pid"
wait "$serve_pid" || true
./bin/odin healthcheck || true
```

Add the GitHub-backed variant only after disposable project setup:

```bash
GITHUB_TOKEN="<disposable-token>" ./bin/odin work intake --project <disposable-project> --dry-run
GITHUB_TOKEN="<disposable-token>" ./bin/odin work intake --project <disposable-project>
./bin/odin work reconcile --project <disposable-project>
./bin/odin work status --json
./bin/odin review list --json
```

The final E2E is complete only when every command's output is captured in a proof note and the terminal state stops at human review, not merge or deployment.

## Best Operating Rule Going Forward

Actual use means Odin can accept a real or fixture-backed signal, persist governed state, expose review/approval/readback, run only through policy-approved execution, preserve branch/worktree isolation, and stop at human-controlled review boundaries. Anything less is a partial capability, even if docs, prompts, configs, or package tests exist.

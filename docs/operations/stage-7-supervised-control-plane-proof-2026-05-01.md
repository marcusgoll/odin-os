---
title: Stage 7 Supervised Control Plane Proof
status: control-plane-passed
date: 2026-05-01
---

# Stage 7 Supervised Control Plane Proof

This proof covers the Stage 7 **Supervised Agency Mode** control-plane slice on
branch `codex/serve-lifecycle-cancel-fix`.

It proves the operator-visible `odin work supervise ...` controls through the
repo-owned `./bin/odin` binary. It does not prove overnight operation, worker
dispatch, PR creation, merge, deploy, or live GitHub issue success.

## Current State

The control plane exists under the canonical `odin work supervise ...` surface.
Mutable control state, queue decisions, duplicate-dispatch claims, and recovery
observations are persisted in SQLite under the configured `ODIN_ROOT`.

The proof used a temporary runtime root and a local read-only fake GitHub API
endpoint so the tracker-backed queue command could complete without touching
live GitHub.

## What Already Exists

- `internal/store/sqlite` migrations and repository methods for supervision
  control state, queue decisions, dispatch claims, and recovery observations.
- `internal/runtime/supervision` for conservative Stage 7 config validation,
  eligibility evaluation, duplicate-claim creation, and recovery reporting.
- `internal/tracker/intake` and `internal/tracker/github` as the existing Issue
  Intake Source seam.
- `internal/cli/commands/work.go` for the canonical operator command surface.

## Gaps

- Live GitHub tracker success was not proven. A live attempt in review reached
  GitHub but returned `404` for the configured repo endpoint in this
  environment.
- Overnight operation is not proven.
- Codex worker launch, branch work, PR creation, CI watching, human review
  handoff, merge, and deploy are intentionally out of scope for this
  control-plane proof.

## Reuse Plan

The implementation extends existing Odin structures only:

- `odin work supervise ...` extends the existing `odin work` command family.
- Queue intake reuses the existing tracker intake seam.
- Stage 7 decisions reuse SQLite as mutable runtime authority.
- Eligibility reuses local scope preflight rather than trusting GitHub labels
  alone.

## New Additions

- Supervision SQLite tables for control state, queue decisions, dispatch claims,
  recovery observations, and the global active/reserved claim limit.
- `internal/runtime/supervision` service and tests.
- `odin work supervise status|start|stop|queue|recover --json`.
- Tracker-backed `queue --project <key> --json` evaluation.
- Fixture-only non-durable queue proof with `--fixture-issue`.
- Durable queue decision evidence that stores issue body hashes, not raw issue
  bodies.

## Why New Additions Are Necessary

Stage 7 needs a narrow control plane before any 24/7 execution loop can be
trusted. The added pieces prove operator control, kill-switch state,
read-only intake evaluation, duplicate-dispatch claim persistence, restart
recovery visibility, and no worker/PR/merge/deploy side effects without adding
a parallel scheduler authority.

## Commands Run

Focused tests:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands -run 'Supervision|Supervise|Eligibility'
```

Result: passed.

Broader relevant tests:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/cli/commands ./internal/tracker/...
```

Result: passed.

Build:

```bash
make build
```

Result: passed. `./bin/odin` and `./bin/odin-os` were built.

Real command proof with controlled runtime root:

```bash
ODIN_ROOT="$(mktemp -d)/runtime" \
ODIN_GITHUB_API_BASE_URL="http://127.0.0.1:<local-fake-github-port>" \
GITHUB_TOKEN="<synthetic-token>" \
./bin/odin work supervise status --json

./bin/odin work supervise start --json
./bin/odin work supervise queue --project odin-core --json
./bin/odin work supervise queue --project odin-core --json
./bin/odin work supervise stop --json
./bin/odin work supervise recover --json
```

Result: all commands exited `0` and returned valid JSON.

## Real Odin E2E Verification

Config hash:

```text
sha256:9d643a206e186ddb5e4ee8a9dcccfbdada5b4ff010101f175cebb88be8a5033d
```

Command results:

| Command | Result |
|---|---|
| `status --json` | `enabled=false`, `kill_switch=true`, side effects `not_started` / `not_created` |
| `start --json` | `enabled=true`, `kill_switch=false`, side effects unchanged |
| `queue --project odin-core --json` | `source=issue_intake_source`, issue `701`, decision `eligible`, one reserved claim |
| second `queue --project odin-core --json` | same claim key reused; no duplicate claim row |
| `stop --json` | `enabled=false`, `kill_switch=true`, side effects unchanged |
| `recover --json` | `recovery.status=clean`, `reason=no_stale_claims`, `active_claims=1` |

SQLite side-effect counts after the command sequence:

```text
projects=1
queue_decisions=1
dispatch_claims=1
tasks=0
runs=0
approvals=0
worktree_leases=0
```

Durable queue evidence:

```text
issue_number=701
decision=eligible
reason=eligible
claim_key=stage7_supervised_agency:odin-core:701
claim_status=reserved
claimed_by=supervision-service
changed_paths=["docs/stage-7-supervised-agency.md"]
issue_body_hash=sha256:ee8d59af11929331c9f684a71713b54400c5c457fbaee2c2edd9b6223e5a41a0
```

The durable decision JSON did not include a raw `issue_body` field.

## Redaction Proof

The real command proof used a synthetic GitHub token value. The proof artifact
directory was scanned for GitHub-token-shaped strings after command execution.

Result: no token-shaped value appeared in command output, stderr, SQLite
decision evidence, or the fake server logs.

## Remaining Risks

- A successful live GitHub read through `odin work supervise queue --project
  odin-core --json` is still unproven.
- Stage 7 full 24/7 mode is still unproven because no overnight supervised run
  was performed.
- This proof does not authorize Codex worker execution, PR creation, merge,
  deploy, higher concurrency, protected-path mutation, or autonomous operation.

## Best Operating Rule Going Forward

Keep Stage 7 as supervised control-plane proof only until an accessible,
low-risk live issue proves the tracker-backed queue path through the same
`odin work supervise queue --project odin-core --json` command, and until a
separate overnight run proves restart, duplicate-dispatch, kill-switch, CI,
review, and zero-merge/deploy behavior.

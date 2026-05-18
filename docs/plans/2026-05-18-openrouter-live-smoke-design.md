# OpenRouter Live Smoke Design

Date: 2026-05-18
Status: design
Scope: Odin-OS executor/provider routing

## Objective

Design the first live OpenRouter smoke path without implementing the live call.
The design must define the operator approval record, credential source, logging
redaction rules, fixture/live split, and the exact real `odin` command that may
run the live smoke after approval.

## Audit Summary

- Existing state:
  - `openrouter_api` is now an executor lane that can construct a fixture-backed
    OpenRouter-compatible request, return deterministic fixture output, and
    record redacted `executor_evidence`.
  - `config/models.yaml` carries fixture OpenRouter model ids such as
    `fixture/openrouter-kimi-k2-6`.
  - `internal/runtime/jobs` already records `model_routing` and
    `executor_evidence` on the existing Run Attempt.
  - The SQLite Approval Request model already links an approval to a Work Item
    and optionally a Run Attempt, records snapshot hashes, and emits approval
    events.
  - `odin approvals` is the canonical operator approval read/resolve surface.
- Partial or contradictory state:
  - The fixture executor proves request shape but deliberately has no live HTTP
    transport, no credential source, and no live smoke command.
  - `Approval` rows do not have provider-specific columns. Provider-specific
    context must live in task/run evidence, approval events, and snapshot hashes
    rather than a new approval authority.
- Missing pieces:
  - A dedicated operator command that prepares and then runs a live smoke.
  - A resolver rule that only permits live execution after an approved matching
    Approval Request.
  - A credential boundary for `OPENROUTER_API_KEY`.
  - Redaction tests that cover OpenRouter-specific request, response, error, and
    log paths.
- Reusable pieces:
  - `internal/executors/openrouter_api` request construction and redaction.
  - `internal/runtime/jobs` Work Item, Run Attempt, and artifact recording.
  - `internal/runtime/approvals` detail/resolve lifecycle.
  - `internal/telemetry/logs` token and sensitive-key redaction.
  - Existing `odin work`, `odin approvals`, `odin runs`, and `odin logs`
    operator readback patterns.
- Relevant docs:
  - `docs/contracts/executor-contract.md`
  - `docs/contracts/executor-routing.md`
  - `docs/contracts/runtime-events.md`
  - `docs/contracts/approval-policy-parity.md`
  - `CONTEXT.md` approval and operator-surface glossary entries
- Blockers found:
  - None for a design artifact.
  - Live execution remains blocked until an implementation slice adds the
    command, resolver, credential handling, and tests described here.

## Approach Options

### Approach 1 - Dedicated Provider Smoke Command

- Reuses: existing Work Item, Run Attempt, Approval Request, OpenRouter executor
  request builder, and run artifacts.
- Adds: a thin top-level `odin provider openrouter smoke ...` command family.
- Tradeoffs: Adds one new command group, but keeps provider smoke explicit and
  avoids overloading general `odin work` with live-provider credential flags.
- Risks: Command naming must stay narrow so it does not become a general provider
  management surface.
- Test/verification shape: fixture prepare/run tests, approval-required tests,
  credential-redaction tests, and one explicitly gated live smoke test command.
- Rollout shape: implement fixture prepare first; add live run behind approval
  after redaction and secrets tests pass.
- Recommendation strength: Strong.

### Approach 2 - Work-Only Profile

- Reuses: `odin work start`, `dispatch`, `execute`, and task metadata.
- Adds: a special Work Item profile such as `openrouter_live_smoke`.
- Tradeoffs: Fewer top-level commands, but the actual live-provider call becomes
  harder to distinguish from ordinary work execution.
- Risks: Operators may accidentally treat a live smoke as normal low-risk work.
- Test/verification shape: work-profile tests plus approval and artifact tests.
- Rollout shape: add a profile and route, then document exact work commands.
- Recommendation strength: Medium.

### Approach 3 - Executor-Internal Live Flag

- Reuses: `openrouter_api.RunTask`.
- Adds: a live flag or environment switch inside the executor.
- Tradeoffs: Smallest implementation, but it hides the live side effect behind
  executor configuration and weakens operator visibility.
- Risks: Accidental live calls, unclear approval linkage, and harder redaction
  auditing.
- Test/verification shape: executor tests only, which is too narrow for a live
  operator path.
- Rollout shape: not recommended.
- Recommendation strength: Weak.

## Selected Design

Use Approach 1: a dedicated, thin operator command that prepares an approval
request and later runs the live smoke only when the exact approval has been
approved.

The command family is intentionally narrow:

```bash
./bin/odin provider openrouter smoke prepare --model openrouter-kimi-k2-6 --json
./bin/odin provider openrouter smoke run --approval <approval-id> --model openrouter-kimi-k2-6 --live --confirm-live-provider-call --json
```

The only command that may make a live OpenRouter call is:

```bash
OPENROUTER_API_KEY=<local-secret> ./bin/odin provider openrouter smoke run --approval <approval-id> --model openrouter-kimi-k2-6 --live --confirm-live-provider-call --json
```

No other `odin work`, executor, fixture, health, status, or routing command may
infer live OpenRouter permission from environment variables.

## Operator Approval Record

`prepare` creates one Work Item and one prepare Run Attempt, then blocks the Work
Item with an Approval Request.

The Approval Request uses the existing approvals table:

- `task_id`: the OpenRouter live smoke Work Item
- `run_id`: the prepare Run Attempt that recorded the exact proposed request
  shape
- `status`: `pending`
- `requested_by`: `operator`
- `policy_snapshot_hash`: current approval policy snapshot
- `runtime_snapshot_hash`: snapshot covering task, run, route, model, fixture
  request hash, and live-smoke parameters

The prepare Run Attempt records a non-secret `openrouter_live_smoke_request`
artifact with:

- `provider_key=openrouter`
- `model_key`
- `provider_model_id`
- `request_sha256`
- `redacted_request_json`
- `fixture_transport=true`
- `network_access=false`
- `approval_required=true`
- `live_smoke_status=approval_required`

The approval detail/readback must show enough operator context to decide:

- provider and model
- that this is a live external network call
- the redacted request shape
- the expected maximum output tokens
- the exact run command to execute after approval

Approval resolution stays on the canonical surface:

```bash
./bin/odin approvals resolve <approval-id> approve because "OpenRouter live smoke approved for one request"
./bin/odin approvals resolve <approval-id> deny because "Do not call live provider"
```

Approving the request does not run the live smoke by itself. It only permits the
subsequent `provider openrouter smoke run` command to proceed.

## Credential Source

The only accepted credential source for the first live smoke is the local process
environment variable `OPENROUTER_API_KEY` supplied to the approved `run` command.

Rules:

- The key is read only after approval validation succeeds.
- The key is not stored in SQLite, config YAML, run artifacts, runtime events,
  memory, logs, or shell output.
- The key is not added to `internal/executors/drivers.AllowlistedEnvironment`;
  OpenRouter live smoke is an in-process provider call, not a worker subprocess
  credential.
- Missing `OPENROUTER_API_KEY` after approval fails closed with
  `credential_missing` and records only that non-secret error code.
- The command must reject credential sources from CLI flags, config files,
  project manifests, memory, or prompt text.

## Logging And Redaction Rules

All live-smoke code paths must apply the same redaction boundary before anything
is emitted to logs, events, artifacts, or operator output.

Must redact:

- `Authorization` headers
- bearer tokens
- `OPENROUTER_API_KEY`
- values matching `sk-*`
- keys containing token, secret, password, api key, access key, private key,
  credential, or authorization markers
- raw prompt/message content
- provider error bodies before they are persisted

May record:

- provider key
- model key and provider model id
- request hash
- response id when non-secret
- HTTP status code
- latency milliseconds
- token usage counts
- redacted request JSON
- redacted response/error summary
- `network_access=true` only on the live run artifact

Tests must prove that a sentinel key such as `sk-live-secret-for-test` does not
appear in:

- `run_artifacts.details_json`
- runtime events payloads
- structured log output
- `odin provider openrouter smoke ... --json`
- `odin runs show <run-id>`
- `odin logs --json`

## Fixture/Live Split

Fixture mode remains the default and does not require approval:

- no network access
- fixture provider ids remain under `fixture/...`
- emits `network_access=false`
- proves request construction and redaction only

Live mode is opt-in and approval-gated:

- requires `--live`
- requires `--confirm-live-provider-call`
- requires `--approval <approval-id>`
- requires an approved matching Approval Request
- requires `OPENROUTER_API_KEY`
- emits `network_access=true`
- may call exactly one OpenRouter chat-completions request
- must use a low-cost smoke prompt and fixed max output token limit

The live smoke must not become a general model execution path. It is a provider
connectivity and redaction proof only.

## Validation Rules For `run`

Before any credential read or HTTP request, the live run command must verify:

1. `--live` and `--confirm-live-provider-call` are both present.
2. The approval exists and is `approved`.
3. The approval is linked to an OpenRouter live-smoke Work Item.
4. The approval's prepare Run Attempt recorded `live_smoke_status=approval_required`.
5. The requested `--model` matches the prepared model.
6. The prepared redacted request hash still matches the command's rebuilt request.
7. The model is still enabled in `config/models.yaml`.
8. The model is not high-risk-enabled by accident and remains allowed only for
   low-risk provider smoke.
9. `OPENROUTER_API_KEY` is present only after checks 1-8 pass.

Any mismatch fails closed before credential read or network access.

## Exact Future Operator Flow

Prepare:

```bash
./bin/odin provider openrouter smoke prepare --model openrouter-kimi-k2-6 --json
```

Review:

```bash
./bin/odin approvals show <approval-id>
./bin/odin runs show <prepare-run-id>
```

Approve or deny:

```bash
./bin/odin approvals resolve <approval-id> approve because "OpenRouter live smoke approved for one request"
```

Run the only approved live smoke command:

```bash
OPENROUTER_API_KEY=<local-secret> ./bin/odin provider openrouter smoke run --approval <approval-id> --model openrouter-kimi-k2-6 --live --confirm-live-provider-call --json
```

Verify:

```bash
./bin/odin runs show <live-run-id>
./bin/odin logs --json
```

## Failure Handling

- Missing approval: fail `approval_required`, no credential read, no network.
- Pending approval: fail `approval_pending`, no credential read, no network.
- Denied approval: fail `approval_denied`, no credential read, no network.
- Stale approval snapshot: fail `stale_approval`, no credential read, no
  network.
- Missing credential after approval: fail `credential_missing`, no network.
- Provider HTTP failure: record `provider_live_smoke_failed`, status code when
  available, redacted provider error summary, and no raw response body.
- Ambiguous success: do not retry automatically. Require a fresh prepare and
  approval if another live request is needed.

## Tests Required For Implementation

Focused tests:

- prepare command creates a Work Item, prepare Run Attempt, request artifact, and
  pending Approval Request.
- run command rejects pending, denied, missing, mismatched, and stale approvals
  before credential read.
- run command rejects missing `OPENROUTER_API_KEY` after approval validation.
- run command redacts sentinel secrets from artifacts, events, logs, and JSON
  output.
- fixture mode remains no-network and does not require approval.
- live mode uses an injected test transport for unit/integration tests.

Real operator proof:

- `make build`
- `./bin/odin provider openrouter smoke prepare --model openrouter-kimi-k2-6 --json`
- `./bin/odin approvals show <approval-id>`
- `./bin/odin approvals resolve <approval-id> approve because "..."`
- `OPENROUTER_API_KEY=sk-test-sentinel ./bin/odin provider openrouter smoke run --approval <approval-id> --model openrouter-kimi-k2-6 --live --confirm-live-provider-call --json` using injected non-network transport in tests
- `./bin/odin runs show <live-run-id>`
- `./bin/odin logs --json`

The first real network smoke is allowed only after those tests pass and an
operator supplies a real local `OPENROUTER_API_KEY` for one attended run.

## Implementation Handoff

Create a new implementation goal:

```text
Implement the approval-gated OpenRouter live smoke operator path from the design in docs/plans/2026-05-18-openrouter-live-smoke-design.md. Keep fixture mode default and no-network. Add `odin provider openrouter smoke prepare` and `odin provider openrouter smoke run` as thin operator adapters over existing Work Item, Run Attempt, Approval Request, and OpenRouter executor seams. The live run must require `--approval`, `--live`, `--confirm-live-provider-call`, an approved matching Approval Request, and `OPENROUTER_API_KEY` read only after approval validation. Add tests proving pending/denied/stale/mismatched approvals fail before credential read, secrets are redacted from artifacts/events/logs/JSON output, fixture mode remains no-network, and the real repo-local `./bin/odin` proof path works with injected non-network transport. Do not run a real external OpenRouter call in CI or unattended verification.
```

---
title: Odin Doctor Operator Report
status: approved
date: 2026-04-17
---

# Odin Doctor Operator Report

## Existing State

`odin-os` already has the native pieces needed for runtime health reporting:

- `cmd/odin` dispatches operational commands through `internal/app/lifecycle/run.go`
- `odin doctor` already exists and returns either a minimal text summary or `--json`
- REPL `/doctor` and `/doctor json` already reuse the same runtime health service
- `internal/runtime/health/service.go` already produces structured checks for database, registry, executor freshness, queue pressure, projection freshness, and source freshness
- `/healthz` and `/readyz` already expose machine-facing health JSON over HTTP

What is partial today is the operator experience. The underlying health signals exist, but the human-facing surfaces compress them into terse summaries. Odin can tell whether it is healthy, degraded, or failed, but it does not yet explain operational impact, rank findings, call out blind spots, or produce an action-oriented report.

What is missing is an operator-grade report surface built on the existing health contract rather than alongside it.

## Recommendation

Phase 1 should extend `odin doctor` as the canonical Odin self-health command.

This design intentionally does **not** add a parallel `odin ops` command family in `odin-os`, and it does **not** attempt direct homelab or Hetzner probing in the first pass. The repo already has a native health/report path. Reusing it is the lowest-risk, most idiomatic change. Host-level infrastructure checks can be added later once `odin-os` has an explicit remote telemetry contract rather than inferred shell behavior.

## Scope

Phase 1 covers **Odin OS self-health only**.

Included:

- richer structured doctor snapshots derived from the existing health service
- operator-grade markdown rendering from the same report object
- CLI support for machine and human output
- REPL support for structured and human report views
- conversational summaries sourced from the same report model
- explicit handling of missing telemetry as degraded or unknown operational state

Excluded:

- direct homelab host health checks
- direct Hetzner VPS health checks
- SSH probe orchestration
- cross-host rollup reports
- replacing `/healthz` or `/readyz` with heavy operator output

## Product Goal

`odin doctor` should answer a real operator question, not just produce a status bit:

> Is Odin healthy enough for its current duties, what is degraded, what is unknown, and what should be fixed first?

The command should remain safe and read-only. It is a diagnostic and reporting surface, not a remediation surface.

## Proposed Surfaces

### CLI

Keep the current machine contract:

- `odin doctor --json`

Add an operator-facing report format:

- `odin doctor --format markdown`

Optional compatibility shortcut if useful during implementation:

- `odin doctor --report`

The JSON form remains the canonical structured output. Markdown is a rendering of that same report, not a separate data source.

### REPL

Keep the current structured view:

- `/doctor json`

Add a report view:

- `/doctor report`

Plain `/doctor` can remain the compact one-line summary so the shell stays fast and readable, while `/doctor report` becomes the operator-grade detailed view.

### Conversation

Replace the current one-line health response with a concise summary generated from the same report model used by CLI and REPL. The conversation layer should not invent a separate health interpretation.

### HTTP

Keep `/healthz` and `/readyz` lightweight and machine-oriented. They should continue to return compact JSON status suited to probes and orchestration.

Do not expose the full operator markdown report on these endpoints in phase 1.

## Report Model

The existing `health.Report` should evolve into a richer operator snapshot while staying cleanly serializable as JSON.

The report should add, at minimum:

- top-level status
- generated timestamp
- normalized coverage metadata describing which subsystems were evaluated and which are unknown
- ordered findings with severity, confidence, observation, impact, and evidence references
- inferred root causes separated from confirmed causes
- grouped recommendations: immediate, near-term, strategic
- missing telemetry list
- final verdict fields

The existing check list remains useful as the raw evidence layer. The richer report should be derived from the checks, not replace them with free-form prose.

## Findings and Recommendation Logic

The report should convert raw checks into operator findings using stable rules:

- `failed` checks create high-severity findings
- `degraded` checks create medium- or high-severity findings depending on impact
- absent or stale telemetry creates explicit findings about reduced confidence
- healthy checks should still contribute to the snapshot, but should not create noise

Recommendations should be deterministic and grouped by urgency:

- `Immediate`: active breakage, major degradation, or missing telemetry that blocks safe judgment
- `Near-Term`: tuning, cleanup, alerting, or health-check improvements
- `Strategic`: larger architecture or observability improvements deferred until operationally justified

Each recommendation should include:

- recommendation
- reason
- expected benefit
- effort
- risk
- approval requirement

## Missing Telemetry Semantics

Missing telemetry is an operational problem, not a neutral state.

When Odin lacks enough evidence to judge a subsystem confidently, the report should:

- mark that area as unknown or degraded in coverage
- include a finding that health confidence is reduced
- recommend the smallest telemetry addition needed to restore confidence

This prevents false healthy reports caused by silence.

## Odin Self-Health Focus

Phase 1 should treat Odin itself as the monitored system:

- database reachability
- registry health
- executor freshness
- queue pressure
- projection freshness
- source freshness

The report should also be designed so new Odin runtime signals can slot in cleanly later, such as:

- worker saturation
- retry storms
- stuck runs
- memory growth
- task latency
- supervision backlog

Those signals should be added only where there is an existing or near-term native source in `odin-os`, not by fabricating a second monitoring framework.

## Data Flow

1. CLI, REPL, or conversation entrypoint requests doctor output.
2. `internal/runtime/health.Service` gathers raw checks from SQLite-backed runtime state.
3. A doctor report builder converts checks into normalized findings, coverage, recommendations, missing telemetry, and verdict fields.
4. A formatter renders either JSON or markdown from that single report object.
5. CLI and REPL return the selected output form; conversation returns a concise summary derived from the same report.

This keeps one evidence path and multiple renderers, instead of multiple loosely synchronized health implementations.

## Error Handling

If the health service itself cannot execute, the command should fail clearly rather than synthesize a healthy-looking report.

If one subsystem cannot be evaluated but the overall report can still be built:

- record the problem as failed or unknown evidence
- lower confidence for that area
- keep the rest of the report available

Formatting errors should not mutate health judgment. Data collection and rendering must remain separate.

## Verification Strategy

Implementation is not complete unless the real `odin` command path is exercised.

Required verification targets:

- unit tests for report building and recommendation ordering
- existing health service tests updated only where contracts intentionally change
- CLI tests for `odin doctor --json` and `odin doctor --format markdown`
- REPL tests for `/doctor json` and `/doctor report`
- conversation tests for doctor-answer summaries
- real command checks through the repo-owned entrypoint, such as:
  - `go run ./cmd/odin doctor --json`
  - `go run ./cmd/odin doctor --format markdown`

If repo baseline allows it, broader module and integration tests should also run. If baseline failures are unrelated, they must be reported explicitly rather than hidden.

## Risks and Constraints

The main risks are:

- breaking the existing `doctor --json` machine contract
- overloading `/healthz` or `/readyz` with human-oriented payloads
- mixing evidence collection with prose generation in a way that becomes brittle
- claiming infrastructure visibility that `odin-os` does not yet have

This design avoids those risks by keeping JSON canonical, HTTP health lightweight, and phase 1 scoped to Odin self-health only.

## Deferred Work

After phase 1 is stable, the next logical expansion is host-level observability with explicit contracts for:

- homelab inventory and probe targets
- Hetzner VPS inventory and probe targets
- remote telemetry transport
- cross-host aggregation into the same operator report model

That should be a separate design and implementation phase, not folded into this one.

## Result

`odin-os` gains a native, operator-grade `odin doctor` surface that explains Odin's own runtime health in a practical, prioritized, and testable way, without duplicating the command model already present in the repo and without pretending to observe infrastructure it does not yet instrument.

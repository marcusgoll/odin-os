---
title: Odin OS Production Readiness Test
date: 2026-05-14
status: tested-with-gaps
scope: live operator surface plus isolated repo-local workflow proofs
---

# Odin OS Production Readiness Test

## Verdict

Odin OS is ready for bounded alpha dogfooding on this homelab with the current
live runtime and repo-local binary. It is not yet broadly production-ready as an
unattended real-project controller.

The current live runtime is healthy and has live worker dispatch enabled. The
largest acute readiness risk, the failed-work review queue, has been reduced
from 44 failed-work recovery items to zero failed-work review items without
blind retry. Those failures are now explicit active follow-up obligations, and
their original tasks are blocked with `failed_work_follow_up_created`.

The enabled production-readiness trigger has now materialized a live work item.
That first materialized work item failed on the installed release because
automation-trigger tasks had no acceptance criteria. The repo-local code now
adds acceptance criteria to future materialized trigger tasks, but this fix has
not been installed into `/home/orchestrator/odin-os/releases/current/bin/odin`.

Browser Control can execute a live public read-only browser task through the
explicit live adapter, and command output labels stub evidence versus real
browser evidence. Authenticated browser session attachment now fails closed
until the profile attach contract is implemented. Provider readiness is still
narrow: one executor is healthy and seven tracked executors are unhealthy, but
doctor output now names the healthy and unhealthy executor keys.

## Current State

- Target repo: `/home/orchestrator/odin-os`
- Installed operator command: `odin` resolves through
  `/home/orchestrator/.local/bin/odin` to
  `/home/orchestrator/odin-os/releases/current/bin/odin`
- Repo-local command after `make build`: `/home/orchestrator/odin-os/bin/odin`
- Installed release was not repointed or restarted during this test.
- Live trigger state, through installed `odin`: `production-readiness-daily-check`
  is `enabled`, last materialized at `2026-05-14T16:12:29Z`, last work item
  `automation-production-readiness-daily-check-34b4861c1e141bbe`, next eligible
  at `2026-05-16T15:00:00Z`.
- Repo-local `./bin/odin overview --json`: `actual_use.status=action_required`,
  `action_required_count=45`, `review_queue_count=0`,
  `blocked_work_item_count=45`, `failed_work_item_count=0`,
  `follow_up_obligation_count=45`, `due_follow_up_obligation_count=45`,
  `automation_trigger_count=1`, `enabled_automation_trigger_count=1`,
  `materialized_count=1`.
- Repo-local `./bin/odin followup list --json`: 45 active follow-up
  obligations, grouped by `target_project_key` as `pbs=41`, `family-ops=3`,
  and `odin-core=1`.
- Repo-local `./bin/odin jobs --json`: 45 original tasks blocked with
  `failed_work_follow_up_created`, zero queued jobs, zero failed jobs.
- Repo-local `./bin/odin doctor --json`: overall `healthy`; executor check
  reports `tracked_executors=8`, `healthy_executors=1`,
  `unhealthy_executors=7`, `healthy_executor_keys=codex_headless`, and
  `unhealthy_executor_keys=anthropic_api,claude_code_headless,gemini_cli_headless,google_api,openai_api,openrouter_api,xai_api`.

## What Already Exists

- Canonical operator surfaces exist for `project`, `workspace`, `work`, `task`,
  `agenda`, `review`, `trigger`, `scheduler`, `browser`, `goal`, `overview`,
  `doctor`, `healthcheck`, and PWA/mobile approval UI.
- Five live initiatives/projects are visible: `pbs`, `family-ops`, `odin-core`,
  `cfipros`, and `marcusgoll`.
- The repo has a production-readiness contract in
  `docs/contracts/real-world-readiness.md` and an alpha operating envelope in
  `docs/operations/alpha-readiness.md`.
- The actual-use E2E path exercises project, task, scheduler/routine-like,
  review, recovery, and observability workflows against an isolated runtime
  root and a temporary demo project.
- Browser Control supports a stub adapter by default and an explicit live
  adapter through `ODIN_BROWSER_ADAPTER=live` plus an allowlisted
  `ODIN_HUGINN_BROWSER_COMMAND`.
- Browser session metadata, manual verification metadata, encrypted fixture
  artifact foundations, and materialization foundations exist, but authenticated
  attach is not wired to browser execution.

## Gaps

- Live actual-use readback is still not clean: `action_required_count=45`
  remains because the former failed-work items are now active follow-up
  obligations. They have been made explicit, not resolved.
- The generic `routine` command does not exist; routine coverage is through
  `trigger`, `scheduler`, `followup`, and `agenda` surfaces.
- The enabled trigger materialized a live work item, but the installed release
  created that task without acceptance criteria, so dispatch failed with
  `template "go-orchestrator" requires acceptance criteria before dispatch`.
  Future materializations are fixed in repo-local code only until a reviewed
  release is installed.
- Authenticated browser session reuse remains future work by contract.
  `odin browser run --session-id <id>` now fails closed with an explicit
  unsupported attach error.
- The default `odin browser run --worker-mode browser` path still chooses
  `adapter_kind=stub_local` unless the live adapter env is set explicitly.
- Provider breadth is limited. Current readiness depends on one live executor
  lane, not all configured executors.

## Follow-Up Obligation Classification

The 45 active follow-up obligations are intentionally classified, not resolved.
This classification was produced from `./bin/odin followup list --json` on the
live runtime state.

| Project | Count | Failure family | Obligation IDs | Disposition |
| --- | ---: | --- | --- | --- |
| `pbs` | 29 | GitHub event review failures for push, pull request opened, synchronize, edited, closed, ready-for-review, and review-submitted events | `5`, `12`-`39` | Batch triage in the PBS repo; likely consolidate duplicate event-review failures before executing individual retries. |
| `pbs` | 7 | Python dependency/update CI failures from `pip` update tasks | `6`-`9`, `40`-`42` | Treat as one PBS dependency/CI lane before clearing duplicates. |
| `pbs` | 4 | Docker update CI failures | `10`, `11`, `43`, `44` | Treat as one PBS Docker build lane before clearing duplicates. |
| `pbs` | 1 | Pilot SSH intake smoke failure | `1` | Inspect separately because it is an older smoke/intake proof, not part of the May CI cluster. |
| `family-ops` | 1 | Family Ops shadow smoke failure | `2` | Verify whether the shadow smoke is still a valid production-readiness gate before retry. |
| `family-ops` | 1 | Plaid transactions unknown `account_id` field triage | `3` | Treat as a schema/ingest compatibility follow-up. |
| `family-ops` | 1 | Plaid zero transactions after transport fix and Robinhood account correlation | `4` | Treat as a finance-data correctness follow-up requiring read-only evidence first. |
| `odin-core` | 1 | Production readiness trigger materialized work without acceptance criteria on the installed release | `45` | Should be re-proven only after the merged trigger acceptance-criteria fix is installed. |

## Reuse Plan

- Keep installed `odin` as the live operator proof path and repo-local
  `./bin/odin` as the current-checkout proof path after `make build`.
- Keep project/task/routine proof on existing `project`, `work`, `task`,
  `trigger`, `scheduler`, `review`, `followup`, `agenda`, and `overview`
  surfaces.
- Keep Browser Control proof on `odin browser run` and the existing
  `huginn-browser-worker` adapter boundary.
- Keep PWA/browser UI proof on the existing `make odin-pwa-e2e` target.
- Keep provider readiness on the existing `doctor` executor health projection.

## New Additions

- Added `browser_proof_kind` and `real_browser_evidence` to browser executor
  results, persisted goal evidence payloads, task work-evidence artifacts, and
  `odin browser run --json` output.
- Added fail-closed behavior for authenticated browser session attach attempts
  until the attach contract is implemented.
- Added acceptance criteria to automation-trigger-created tasks so future
  materialized trigger work is dispatchable.
- Added failed-work follow-up behavior that blocks the original failed task
  after a follow-up obligation is created, removing it from the failed-work
  review queue without retrying it blindly.
- Added executor health detail fields that list healthy, stale, and unhealthy
  executor keys.
- Added follow-up triage detail to `overview.actual_use` and
  `followup list --json`: actual-use now reports follow-up and due follow-up
  counts, and follow-up JSON exposes `target_project_id` and
  `target_project_key`.
- Converted 45 live failed-work review items to active follow-up obligations
  using the repo-local `./bin/odin review act <queue_id> follow-up --json`
  surface after building the current checkout.

## Why New Additions Are Necessary

The previous operator surface exposed enough information to see risk but not to
close it safely. Failed work stayed in the review queue after follow-up
creation, trigger materialization created non-dispatchable tasks, browser
session IDs could be over-credited as authenticated attach, and executor
readiness only reported counts. After the failed-work conversion, the main
actual-use surface still did not say that the remaining action-required work was
follow-up work or which project owned it. The changes deepen the existing Odin
surfaces instead of adding parallel tools: failed work becomes explicit
follow-up work, follow-ups are project-triageable, triggers create dispatchable
work, browser attach fails closed, browser proof is classified, and provider
readiness names the lanes that need attention.

## Real Odin E2E Verification

Commands run from `/home/orchestrator/odin-os` on 2026-05-14:

| Check | Command | Result |
| --- | --- | --- |
| Binary identity | `which odin`; `realpath "$(which odin)"`; `ls -l "$(which odin)" bin/odin releases/current/bin/odin` | Installed command points to `releases/current/bin/odin`; repo-local `bin/odin` was rebuilt separately. |
| Build | `make build` | Passed. |
| Focused regression tests | `go test ./internal/runtime/triggers ./internal/app/lifecycle ./internal/executors/browser ./internal/runtime/health -count=1` | Passed. |
| Alpha gate | `make test-alpha` | Passed `TestAlphaAcceptance` with all subtests. |
| Full Go test gate | `make test` | Passed `go test ./...`. |
| Local E2E gate | `make odin-e2e-local` | Passed fixture-backed failure analysis, GitHub intake, delivery dry-run, workspace-safe creation, and tracker lifecycle scenarios. |
| Actual-use proof | `make odin-actual-use-e2e` | Passed. Latest summary: `.odin/actual-use-e2e/latest.json`, status `passed`, project `actual-use-demo`, scenarios `Binary proof`, `Readiness smoke`, `Raw intake`, `Dedupe`, `Approval gate`, `Work dispatch`, `Scheduler`, `Review queue`, `Observability`. |
| PWA/browser UI | `make odin-pwa-e2e` | Passed two Playwright mobile-chrome tests for installable shell sections and authenticated approval decisions. |
| Live enabled trigger materialization | `odin scheduler tick now=2026-05-15T15:00:01Z recovery=false --json`; `odin trigger show production-readiness-daily-check --json`; `odin trigger audit production-readiness-daily-check --json` | Scheduler tick materialized one work item. Trigger audit has six events including `automation_trigger.evaluated` and `automation_trigger.materialized` for task 107. |
| Trigger dispatch failure diagnosis | `./bin/odin review show failed-work:107 --json`; `./bin/odin runs --json`; `./bin/odin logs --json` | Materialized task failed because the installed release created it without acceptance criteria: `template "go-orchestrator" requires acceptance criteria before dispatch`. |
| Live review queue after follow-up conversion | `./bin/odin review list --json` | `count=0`, `failed=0`. |
| Live follow-ups after conversion | `./bin/odin followup list --json` | 45 obligations, all `active`. |
| Live jobs after conversion | `./bin/odin jobs --json` | 45 tasks blocked with `failed_work_follow_up_created`; zero failed jobs and zero queued jobs. |
| Live overview after conversion | `./bin/odin overview --json` | `actual_use.status=action_required`, `action_required_count=45`, `review_queue_count=0`, `blocked_work_item_count=45`, `failed_work_item_count=0`, `follow_up_obligation_count=45`, `due_follow_up_obligation_count=45`, trigger `materialized_count=1`. |
| Live follow-up project triage | `./bin/odin followup list --json | jq '{count:(.obligations|length), by_project:([.obligations[].target_project_key] | group_by(.) | map({project:.[0], count:length}))}'` | 45 obligations grouped by project: `pbs=41`, `family-ops=3`, `odin-core=1`. |
| Provider readiness detail | `./bin/odin doctor --json` | Overall `healthy`; executor check names `codex_headless` as healthy and seven unhealthy executor keys. |
| Public PWA ingress allowed routes | `curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type} %{redirect_url}\n' https://odin.marcusgoll.com/ https://odin.marcusgoll.com/app/ https://odin.marcusgoll.com/app/app.js https://odin.marcusgoll.com/app/manifest.webmanifest` | `/` returned `302` to `/app/`; `/app/` returned `200 text/html`; `/app/app.js` returned `200 text/javascript`; `/app/manifest.webmanifest` returned `200 application/manifest+json`. |
| Public mobile API auth gate | `curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type}\n' https://odin.marcusgoll.com/mobile/status https://odin.marcusgoll.com/mobile/overview https://odin.marcusgoll.com/mobile/review-queue https://odin.marcusgoll.com/mobile/browser/status https://odin.marcusgoll.com/mobile/devices/register https://odin.marcusgoll.com/mobile/` | Concrete mobile endpoints returned `401 application/json` with `admin_auth_required`; `GET /mobile/devices/register` returned `405 Method Not Allowed`; `/mobile/` returned `404` because there is no registered root endpoint. |
| Public health/readiness ingress | `curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type}\n' https://odin.marcusgoll.com/healthz https://odin.marcusgoll.com/readyz` | `/healthz` returned `200 application/json`; `/readyz` returned `503 application/json`, which is fail-closed readiness. Source tests intentionally allow a healthy doctor report body with HTTP 503 when runtime readiness is not established. |
| Public denied route gate | `curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type}\n' https://odin.marcusgoll.com/metrics https://odin.marcusgoll.com/api/v1/status https://odin.marcusgoll.com/api/v1/overview https://odin.marcusgoll.com/api/health https://odin.marcusgoll.com/admin` | All denied operator/API/metrics paths returned `404 text/html` at the nginx public path gate. |
| Homelab release dry-run | `make homelab-release-dry-run` | Passed. It built the repo-local binaries, checked backup/restore/verify/serve help, ran installer dry-run in a temp config root, printed release update commands, and proved fail-closed readiness against an isolated runtime without repointing or restarting production. |
| Stub browser proof | isolated `./bin/odin browser run --worker-mode browser --url https://example.com ...` | Recorded `adapter_kind=stub_local`, `browser_proof_kind=stub_contract_only`, `real_browser_evidence=false`, and action log containing `no_live_browser_launched`. |
| Live browser proof | isolated `ODIN_BROWSER_ADAPTER=live ODIN_HUGINN_BROWSER_COMMAND=/home/orchestrator/odin-os/bin/huginn-browser-worker ODIN_HUGINN_BROWSER_ALLOWED_COMMANDS=/home/orchestrator/odin-os/bin/huginn-browser-worker ./bin/odin browser run --goal-id <id> --url https://example.com --allowed-domain example.com --worker-mode browser --evidence-required --json` | Passed with `adapter_kind=huginn_live`, `browser_proof_kind=live_browser_readonly`, `real_browser_evidence=true`, one screenshot, page title `Example Domain`, and action log containing `browser_mode_selected`, `opened_start_url`, `captured_read_only_evidence`, and `screenshot_captured`. |
| Authenticated attach boundary | isolated verified browser session followed by `./bin/odin browser run --session-id <id> ... --json` | Failed closed with exit code 1 and message `authenticated browser session attachment is not implemented; run without browser_session_id for public read-only evidence or complete the profile attach contract first`. |

## Prompt-To-Artifact Checklist

| Objective requirement | Evidence | Status |
| --- | --- | --- |
| Audit current repo and live operator surfaces | `AGENTS.md`, readiness contracts, command helps, `odin` and `./bin/odin` readbacks | Done |
| Exercise real project workflows | Live project list plus `make odin-actual-use-e2e` temp project `actual-use-demo` with project selection and transition | Done, isolated proof |
| Exercise task workflows | `make odin-actual-use-e2e` `task run`, `work start`, `work dispatch`, `work execute`, `work retry`, `runs --json` | Done, isolated proof |
| Exercise routine workflows | `make odin-actual-use-e2e` `trigger upsert`, `scheduler tick`, duplicate tick, `trigger audit`, plus live `production-readiness-daily-check` materialization | Done through trigger/scheduler/agenda, not a `routine` command |
| Run a real browser task end to end | Live adapter `odin browser run` against `https://example.com` with `huginn-browser-worker`, real PNG screenshot, persisted `browser_readonly` evidence, `browser_proof_kind=live_browser_readonly`, `real_browser_evidence=true` | Done for public read-only browser task |
| Verify authenticated browser boundary | Verified session plus `odin browser run --session-id` proof | Done, fails closed by design |
| Verify persistence | SQLite-backed evidence IDs returned for goal/browser run; actual-use workflow persisted runtime state in isolated `ODIN_ROOT`; live review/status/overview readbacks query persisted runtime | Done |
| Verify state transitions | Actual-use E2E covered intake processing, duplicate handling, approval blocking, work dispatch/execute/retry, scheduler materialization, recovery tick, review queue | Done, isolated proof |
| Verify failure handling | `make odin-e2e-local` failure-analysis scenario and actual-use induced deterministic failure/retry/recovery scenario passed | Done |
| Document exact commands/results and gaps | This briefing | Done |
| Declare readiness only if critical workflows are proven | Verdict is bounded: alpha dogfood ready; not broad unattended production-ready | Done |

## Remaining Risks

- The former failed-work backlog is no longer a failed-work review queue, but it
  is still real work: 45 active follow-up obligations remain and must be
  resolved or intentionally closed. The backlog is now triageable by project:
  `pbs=41`, `family-ops=3`, `odin-core=1`.
- The trigger acceptance-criteria fix is repo-local only until a reviewed
  release is installed. The installed release may still create non-dispatchable
  automation-trigger work before deployment.
- Public ingress is path-gated correctly for the tested route matrix, but live
  `/readyz` still returns HTTP `503` until the public runtime proves readiness
  after a reviewed release cutover.
- Authenticated browser sessions, reusable profile attach, and real login
  handoff remain partial/future by contract. The current behavior is correctly
  fail-closed, not ready.
- Provider breadth is limited. The current readiness claim depends on
  `codex_headless`; seven tracked executor lanes are unhealthy.
- `odin browser run` defaults to the stub adapter unless live adapter env vars
  are configured. Operators must check `real_browser_evidence` before crediting
  a run as real browser proof.

## Best Operating Rule Going Forward

Treat Odin OS as a bounded alpha control plane on this homelab. For real
project work, require live `odin` health/readiness, no failed-work review items
for the relevant project, explicit project transition state, and
command-specific evidence from installed `odin` or rebuilt `./bin/odin` before
widening claims. For browser work, do not count a run as real browser proof
unless `real_browser_evidence=true`,
`browser_proof_kind=live_browser_readonly`, `adapter_kind=huginn_live`, and the
action log plus artifacts show an actual read-only browser capture. For
automation triggers, do not rely on the installed release for dispatchable
materialized work until the acceptance-criteria fix is installed and proven.

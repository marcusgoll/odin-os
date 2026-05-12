# Capability Truth And Risk Gates Design

Date: 2026-05-11
Status: implemented in `codex/risk-hardening-design`
Scope: Odin-OS capability truth, policy drift, completion state, scheduler noise, and high-risk integration gates

## Audit Summary

Audited a clean worktree at `/home/orchestrator/odin-os/.worktrees/risk-hardening-design`, based on current `origin/main`, because the parent checkout had unrelated dirty state.

Relevant artifacts inspected:

- `AGENTS.md`
- `CONTEXT.md`
- `docs/contracts/capability-gateway.md`
- `docs/contracts/capability-catalog.md`
- `docs/contracts/work-execution-state.md`
- `docs/contracts/odin-operating-model.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/operational-autonomy.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/tui-overview.md`
- `docs/brownfield/PROMPT_INVENTORY.md`
- `docs/brownfield/AGENTS_INVENTORY.md`
- `docs/brownfield/SKILL_AGENT_REGISTRY.md`
- `docs/superpowers/specs/2026-05-10-policy-capability-gateway-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-policy-parity-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-review-queue-design.md`
- `docs/superpowers/specs/2026-05-10-scheduler-trigger-system-design.md`
- `docs/superpowers/specs/2026-05-10-audit-logs-events-runtime-evidence-design.md`
- `docs/superpowers/specs/2026-05-10-agent-registry-subagent-delegation-design.md`
- `internal/registry`
- `internal/core/capabilities`
- `internal/core/policy`
- `internal/runtime/jobs`
- `internal/runtime/approvals`
- `internal/runtime/triggers`
- `internal/skills`
- `internal/cli/overview`
- `internal/cli/commands`
- `internal/app/lifecycle`
- `internal/store/sqlite`
- `config/policies.yaml`
- `registry/`

Commands run during design audit:

```bash
go test ./internal/registry ./internal/registry/loader ./internal/runtime/jobs ./internal/runtime/approvals ./internal/runtime/triggers ./internal/core/policy ./internal/skills -count=1
find registry/agents -type f -name '*.md' | wc -l
find registry/skills -type f -name '*.md' | wc -l
find registry/workflows -type f -name '*.md' | wc -l
find registry/commands -type f -name '*.md' | wc -l
go run ./cmd/odin-os help
tmp=$(mktemp -d); ODIN_ROOT="$tmp" go run ./cmd/odin-os overview --json; rm -rf "$tmp"
go run ./cmd/odin-os trigger --help
go run ./cmd/odin-os review --help
go run ./cmd/odin-os skills
```

Observed current facts:

- Registry assets currently include 60 agent files, 19 skill files, 4 workflow files, and 2 command files.
- Fresh `odin overview --json` reports `capability_catalog` as `agent_definition_count=60`, `skill_count=19`, `workflow_count=4`, `command_count=2`, and `tool_count=14`.
- `odin help` exposes real operator surfaces for `work`, `jobs`, `runs`, `approvals`, `review`, `logs`, `trigger`, `scheduler`, `skills`, `companion`, and other commands.
- `odin trigger --help` now exposes `test`, `audit`, quiet hours, batching fields, event ingest, event evaluation, and scheduler-related proof paths.
- `odin review --help` exposes list/show/approve/reject/act surfaces, including `--dry-run` for review actions.
- `odin skills` exposes list/get/create/update/delete/invoke/run/artifacts/artifact, but `skills --help` is not currently accepted.

## Existing State

Odin already has many of the required safety seams:

- Registry prompts and authored definitions are loaded through `internal/registry`.
- Capability Gateway is documented as the canonical runtime discovery and invocation surface.
- Work Item and Run Attempt states are documented in `docs/contracts/work-execution-state.md`.
- Job admission carries execution intent and blocks governance, destructive, and unsafe mutation paths through approval or worktree policy.
- `approvals.Service` owns Approval Request resolution.
- `skills.Service` owns skill invocation policy and reviewable artifact handling.
- `triggers.Service` owns Automation Trigger evaluation, quiet-hours deferral, batching fields, event ingest, materialization, and audit events.
- SQLite events are the canonical audit authority.
- `odin logs --json`, `odin overview --json`, `odin jobs --json`, `odin runs --json`, `odin approvals all --json`, `odin review`, `odin trigger`, and `odin skills` are existing operator surfaces for proof.

The current cross-cutting gap is that these seams are not yet expressed as one product rule. Individual docs say the right thing in their own area, but the operator-facing capability story can still over-credit authored prompts or future-facing registry assets.

## Implementation Evidence

The first implementation slice adds `capability_truth` to `odin overview`
readback and text rendering.

Implemented artifacts:

- `internal/cli/overview/service.go` adds the `CapabilityTruthLane` read model, authored inventory mirror, conservative truth summaries, runtime-proven counts, partial/advisory counts, and high-risk risk labels.
- `internal/cli/render/overview.go` renders a `Capability Truth` section after `Capability Catalog`.
- `internal/cli/overview/service_test.go` proves an authored registry agent is not counted as runtime-proven capability.
- `internal/cli/render/overview_test.go` proves the text overview separates authored asset counts from runtime-proven rows.
- `docs/contracts/capability-catalog.md` and `docs/contracts/tui-overview.md` now document the authored-inventory/runtime-truth split.

## Prompt Risk Coverage

| Prompt risk | Current risk in Odin | Selected fix in this design | First-slice evidence target |
| --- | --- | --- | --- |
| Prompt-Library Sprawl | Many registry agents exist as prompts; raw catalog counts can make Odin look more capable than it is. | Separate authored inventory from runtime-proven capability counts. Count a capability only when real Odin invocation, durable output/state, policy enforcement, and audit evidence exist. | `overview --json` shows authored counts separately from runtime-proven counts; registry agent count is not labeled as implemented automation. |
| Policy Drift | Docs may require approval while an ungoverned runtime path bypasses it. | Use the Capability Truth Gate over existing policy seams: jobs, capabilities, skills, triggers, approvals, and review. | Tests and operator proof show high-risk paths are labeled approval-required, unsupported, or runtime-proven only after the owning policy seam is visible. |
| False Completion | Partial work can look done if states collapse into generic success language. | Keep state words owned by their runtime objects and render missing evidence as `Unknown`, never inferred completion. | State ownership map stays aligned with `docs/contracts/work-execution-state.md`; `Done` is projection-only. |
| Scheduler Noise | Recurring automation can create spam if due work is always materialized without batching, quiet hours, waiting states, or stale suppression. | Require trigger/scheduler capability claims to show quiet-hours, batching, waiting/deferred/not-ready, materialization, or stale/suppression evidence. | Trigger-related truth rows reference `odin trigger test`, `odin trigger audit`, `odin trigger evaluate`, or `scheduler tick` evidence instead of prompt/config existence. |
| Integration Overreach | Email, GitHub, calendar, finance, production, and public posting actions can cause external damage. | High-risk integrations require read-only first, approval before mutation, least-privilege policy, audit evidence, and fail-closed unsupported mutation. | Overview risk notes label integration families as read-only, approval-required, unsupported, or runtime-proven based on actual evidence. |

## Reused Components

The selected design reuses:

- `internal/registry` for authored assets.
- `internal/core/capabilities` for runtime descriptors and invocation.
- `internal/core/policy` for gateway authorization.
- `internal/runtime/jobs` for executable Work Item admission, execution intent, and approval/worktree blocking.
- `internal/runtime/approvals` for approval lifecycle and supported resolver handling.
- `internal/runtime/triggers` for scheduling, quiet hours, batching, waiting/deferred behavior, materialization, and trigger audit events.
- `internal/skills` for skill policy, reviewable artifacts, and durable skill activity.
- `internal/store/sqlite` for Work Items, Run Attempts, approvals, events, artifacts, triggers, and review state.
- `internal/cli/overview` for operator readback.
- `docs/contracts/work-execution-state.md` for state ownership.
- `docs/superpowers/specs/2026-05-10-audit-logs-events-runtime-evidence-design.md` for provenance trail direction.
- `docs/superpowers/specs/2026-05-10-policy-capability-gateway-design.md` for gateway consolidation direction.

## New Components

Add one cross-cutting concept:

- **Capability Truth Gate**: a read-model and contract rule that classifies each authored or discoverable item by the strongest proof Odin has for it.

Add only narrow implementation components in the first slice:

- `CapabilityTruthLevel` or equivalent internal enum for operator readback.
- A `capability_truth` section in `overview` JSON and text rendering.
- A separation between authored asset counts and runtime-proven capability counts.
- Tests proving raw registry prompt counts are not reported as implemented runtime capability counts.
- Contract documentation describing the proof bar.

Do not add a new policy engine, new registry, new scheduler, new queue, new audit store, new integration runtime, new approval system, or new status table.

## Why New Components Are Necessary

The user-visible risk is not that Odin lacks prompts. The risk is that prompts can look like capabilities.

Current `overview` counts 60 agent definitions beside skills, workflows, commands, and tools. That is useful inventory, but it can be misread as implemented automation. A registry prompt should not count as an implemented capability unless a real Odin path invokes it, persists output, enforces policy, and emits audit evidence.

The Capability Truth Gate is necessary because it gives every operator-facing capability claim the same proof standard:

1. A real `odin` command or gateway invocation path exists.
2. The invocation creates or reads durable runtime state when the capability claims runtime effect.
3. Policy is enforced before mutation or external side effects.
4. Audit evidence is emitted and readable through Odin-owned surfaces.

Anything below that bar may still be useful, but it must be labeled as authored, planned, discoverable, advisory, or review-required rather than counted as implemented runtime capability.

## Locked Domain Decisions

- **Authored Asset** means a registry file, prompt, design doc, config entry, or descriptor that describes possible behavior.
- **Runtime-Proven Capability** means an Odin capability with a real invocation path, durable output or state, policy enforcement, and audit evidence.
- **Capability Truth Gate** is the canonical proof rule for counting an Odin capability as implemented.
- Capability inventory and capability implementation counts must be separate.
- Registry agents are authored assets unless they have a proven runtime surface such as `odin companion delegate` with persisted Work Items, Run Attempts, delegation records, artifacts, policy, and audit evidence.
- A skill is not counted as runtime-proven merely because `registry/skills/*.md` exists; it must be invokable through `odin skills` or gateway-equivalent policy and artifact handling.
- A trigger is not counted as safe automation merely because it exists; it must show quiet-hours/deferred/batched/materialized state and audit evidence through `odin trigger` or scheduler surfaces.
- A high-risk integration action is not runtime-proven until it is read-only by default, approval-gated before mutation, least-privilege scoped, and auditable.
- `done` remains an operator projection, not a primary stored Work Item status.
- The user-facing lifecycle words `Drafted`, `Needs clarification`, `Needs review`, `Needs approval`, `Approved`, `Running`, `Blocked`, `Failed`, `Done`, and `Unknown` must map to owning runtime objects rather than becoming one generic status enum.
- `Unknown` must be rendered when evidence is missing or correlation is not available. Odin must not infer completion, approval, or safety from absence of evidence.

No ADR is required for this slice. The design tightens already-recorded contracts instead of changing the authority model.

## Selected Design

Implement Capability Truth Gate V1 as an operator-facing read model over existing registry, command, runtime, policy, and event evidence.

### Truth Levels

Use these levels for readback and tests:

| Level | Meaning | Counts as implemented capability |
| --- | --- | --- |
| `authored_asset` | Registry prompt, config, design, or descriptor exists. | No |
| `discoverable` | Listed through a catalog/gateway/operator surface. | No |
| `invokable` | A real `odin` command or gateway invocation path exists. | No, unless no runtime effect is claimed |
| `persisted_output` | Invocation produces durable Work Item, Run Attempt, artifact, approval, review, trigger, memory, event, or equivalent store state. | Partial |
| `policy_enforced` | The invocation passes through the appropriate policy or approval gate before mutation. | Partial |
| `audit_evidenced` | The result is visible through Odin-owned logs, runs, jobs, review, approvals, trigger, overview, or provenance readback. | Partial |
| `runtime_proven` | Invokable, persisted where relevant, policy-enforced, and audit-evidenced. | Yes |

The first implementation can compute conservative levels from known surfaces rather than attempting perfect dynamic proof for every future capability. Conservative under-counting is acceptable. Over-counting authored prompts as runtime-proven is not.

### Operator Readback

Extend `overview` from a raw `capability_catalog` count into a split view:

- authored inventory counts: agents, skills, workflows, commands, tools
- runtime-proven counts: only items meeting the Capability Truth Gate
- advisory/future counts: authored items with no runtime proof
- partial counts: discoverable, invokable, persisted, policy-enforced, or audit-evidenced but not all gates satisfied
- risk notes for high-risk integration families that remain read-only, approval-required, or unsupported

Existing fields may remain for compatibility, but new fields must make the distinction explicit. Text rendering should avoid implying that `agent_definition_count=60` means 60 working agent automations.

### State Ownership Mapping

The user-facing lifecycle words map as follows:

| User-facing word | Canonical owner | Current proof path |
| --- | --- | --- |
| `Drafted` | Intake, review, proposal, skill artifact, or source-specific draft object | `odin review`, source command |
| `Needs clarification` | Intake/review/source-specific request | `odin review` or source command |
| `Needs review` | Review queue source | `odin review list/show` |
| `Needs approval` | Approval Request or work blocked by policy | `odin approvals`, `odin jobs`, `odin work status` |
| `Approved` | Approval Request or source decision | `odin approvals`, `odin review`, source command |
| `Running` | Work Item plus active Run Attempt | `odin jobs`, `odin runs` |
| `Blocked` | Work Item or source object with explicit reason | `odin jobs`, `odin work status`, relevant queue |
| `Failed` | Work Item / Run Attempt / recovery source | `odin jobs`, `odin runs`, `odin review` failed-work |
| `Done` | Operator projection from terminal states | derived only; never primary persisted status |
| `Unknown` | Evidence missing, weak correlation, or unsupported surface | explicit readback, not inferred |

Implementation should reuse `docs/contracts/work-execution-state.md` and not create a parallel status model.

### Policy Drift Control

Policy enforcement remains distributed by owning runtime seam, but the proof gate is centralized:

- jobs own executable work admission
- capabilities gateway owns generic invocation authorization
- skills own skill permissions and reviewable artifact policy
- triggers own materialization, quiet-hours, batching, and intent propagation
- approvals own human authorization and resolver support
- review owns source-specific decisions and unsupported-action refusal

The Capability Truth Gate checks whether a capability has passed the correct owning seam. It does not move all policy into one mega-service.

### Scheduler Noise Control

Scheduler and trigger capability claims must show at least one of:

- quiet-hours deferral evidence
- batch key or batch window evidence
- waiting/deferred/not-ready proof through `odin trigger test` or `audit`
- materialization evidence through `odin trigger evaluate` or `scheduler tick`
- stale/suppressed routine evidence when that capability is later implemented

Recurring automation without one of those proof paths remains authored or partial, not runtime-proven safe automation.

### Integration Overreach Control

High-risk integrations include email, GitHub mutation, calendar mutation, finance, production/deploy, permissions, public posting, legal/medical records, deletion, purchase, and external account actions.

For these, runtime-proven means:

- read-only first path exists
- mutation path has explicit approval or supported resolver
- least-privilege permission is visible in policy/config/descriptors
- audit evidence is emitted and readable
- unsupported or unapproved mutation fails closed without external side effects

## Rejected Alternatives

### Count every active registry item as a capability

Rejected. This is the prompt-library sprawl failure mode. Registry inventory is useful, but it is not proof of runtime capability.

### Hide authored prompt counts entirely

Rejected. Operators still need to know the inventory exists. The fix is labeling and proof separation, not deleting visibility.

### Build a new central policy engine now

Rejected. Jobs, capabilities, skills, triggers, approvals, and review already own policy in their bounded contexts. The first step is a common proof rule and readback, not a replacement engine.

### Add one universal status enum

Rejected. Status words are owned by different objects. Collapsing them would recreate false completion risk.

### Disable scheduler or integration surfaces until perfect

Rejected. Odin already has useful read-only, review, approval, trigger, and audit paths. The correct behavior is fail-closed classification and proof-gated counting.

## Test And Verification Plan

Focused tests for the first implementation slice:

```bash
go test ./internal/cli/overview -run 'Capability|Truth|Catalog' -count=1
go test ./internal/registry ./internal/registry/loader -run 'Agent|Skill|Workflow|Command|Capability' -count=1
go test ./internal/app/lifecycle -run 'Overview|Review|Approval|Skill|Trigger|Companion' -count=1
go test ./internal/runtime/jobs ./internal/runtime/approvals ./internal/runtime/triggers ./internal/core/policy ./internal/skills -run 'Policy|Approval|Trigger|Skill|Intent|Audit' -count=1
```

Broader local verification:

```bash
go test ./internal/cli/overview ./internal/app/lifecycle ./internal/runtime/jobs ./internal/runtime/approvals ./internal/runtime/triggers ./internal/core/policy ./internal/skills ./internal/registry ./internal/registry/loader -count=1
make build
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin overview --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin trigger --help
ODIN_ROOT="$tmp" ./bin/odin trigger audit <trigger-key> --json
ODIN_ROOT="$tmp" ./bin/odin approvals all --json
ODIN_ROOT="$tmp" ./bin/odin logs --json
rm -rf "$tmp"
```

Required proof conditions:

- `overview --json` separates authored inventory from runtime-proven capability counts.
- Raw registry agent count remains visible but is not labeled as implemented automation.
- High-risk integration families are labeled read-only, approval-required, unsupported, or runtime-proven based on actual evidence.
- Missing proof renders as partial or unknown, never as done.
- Existing work execution status contract remains unchanged.

## Documentation Changes

First slice should update:

- `docs/contracts/capability-catalog.md` with the authored-vs-runtime-proven distinction.
- `docs/contracts/capability-gateway.md` only if gateway readback fields change.
- `docs/contracts/tui-overview.md` if overview rendering changes.
- This design spec with implementation results.

Do not update `CONTEXT.md` until the term **Capability Truth Gate** is accepted as canonical domain language.

## Open Blockers

- User approval is needed before treating **Capability Truth Gate** as a locked canonical term.
- Implementation should verify whether the recently merged capability gateway and scheduler slices already cover part of the first slice before editing code.
- The parent checkout has unrelated dirty files; implementation must remain in a clean worktree.

## Planning Handoff

Recommended first implementation slice:

1. Add a conservative capability truth read model for overview.
2. Keep existing raw capability inventory fields for compatibility.
3. Add explicit runtime-proof fields and risk labels.
4. Prove registry prompts are not counted as runtime-proven unless a known Odin invocation path, persistence, policy, and audit evidence exist.
5. Update capability catalog and overview docs.
6. Prove with focused tests and real `./bin/odin overview --json`.

## Implementation Goal Prompt

```text
/goal Implement Capability Truth Gate V1 in /home/orchestrator/odin-os.

Use the proposed design at docs/superpowers/specs/2026-05-11-capability-truth-and-risk-gates-design.md after confirming the term and first slice are accepted. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse internal/cli/overview, internal/registry, internal/core/capabilities, internal/runtime/jobs, internal/runtime/approvals, internal/runtime/triggers, internal/core/policy, internal/skills, existing runtime events, and existing operator surfaces. Do not add a new policy engine, registry, queue, scheduler, audit store, integration runtime, approval system, or generic status table.

Required proof:
- go test ./internal/cli/overview -run 'Capability|Truth|Catalog' -count=1
- go test ./internal/registry ./internal/registry/loader -run 'Agent|Skill|Workflow|Command|Capability' -count=1
- go test ./internal/app/lifecycle -run 'Overview|Review|Approval|Skill|Trigger|Companion' -count=1
- go test ./internal/runtime/jobs ./internal/runtime/approvals ./internal/runtime/triggers ./internal/core/policy ./internal/skills -run 'Policy|Approval|Trigger|Skill|Intent|Audit' -count=1
- go test ./internal/cli/overview ./internal/app/lifecycle ./internal/runtime/jobs ./internal/runtime/approvals ./internal/runtime/triggers ./internal/core/policy ./internal/skills ./internal/registry ./internal/registry/loader -count=1
- make build
- which odin && realpath "$(which odin)"
- tmp="$(mktemp -d)"; ODIN_ROOT="$tmp" ./bin/odin help; ODIN_ROOT="$tmp" ./bin/odin overview --json; ODIN_ROOT="$tmp" ./bin/odin review list --json; ODIN_ROOT="$tmp" ./bin/odin approvals all --json; ODIN_ROOT="$tmp" ./bin/odin logs --json; rm -rf "$tmp"

Delivery:
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```

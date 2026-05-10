# Agent Registry And Subagent Delegation Design

Date: 2026-05-10
Status: approved for implementation planning
Scope: Odin-OS agent registry and bounded subagent delegation v1

## Audit Summary

Inspected the current `odin-os` repo rules, domain contracts, registry assets, delegation runtime, supervision runtime, job admission, storage schema, tests, recent commits, dirty worktree state, and live `odin` operator surface.

Relevant artifacts inspected:

- `AGENTS.md`
- `README.md`
- `CONTEXT.md`
- `docs/contracts/ubiquitous-language.md`
- `docs/contracts/workspace-context-map.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/companion-swarm-orchestration.md`
- `docs/contracts/executor-routing.md`
- `docs/brownfield/SKILL_AGENT_REGISTRY.md`
- `docs/brownfield/ARCHITECTURE_GAP_ANALYSIS.md`
- `registry/agents/subagent-delegation-planner-agent.md`
- `registry/agents/portal-delivery-agent.md`
- `registry/agents/review-agent.md`
- `internal/registry`
- `internal/runtime/delegations`
- `internal/runtime/supervision`
- `internal/runtime/jobs/service.go`
- `internal/store/sqlite/migrations/0009_delegations.sql`
- `internal/runtime/events/events.go`
- `internal/cli/commands/companion.go`
- `internal/app/lifecycle/run.go`
- `internal/runtime/delegations/service_test.go`
- `tests/integration/companion_swarm_acceptance_test.go`

Live operator checks:

```bash
which odin
realpath "$(which odin)"
odin companion
odin overview --json
```

Observed results:

- `which odin` resolved to `/home/orchestrator/.local/bin/odin`.
- `realpath "$(which odin)"` resolved to `/home/orchestrator/odin-os/releases/current/bin/odin`.
- `odin companion` showed `odin companion delegate` as the real delegation operator surface.
- `odin overview --json` showed live `delegation_truth` with `operator_surface: "companion delegate"`, `companion_work_path: "governed_work_items"`, `companion_swarm_count: 11`, and `capability_catalog.agent_definition_count: 60`.

## Existing State

Odin already has the core delegation substrate:

- `registry/agents/*.md` is the authored source for Odin agent role definitions.
- `internal/registry` loads, validates, and compiles registry agents, skills, workflows, and commands.
- `odin companion delegate` is the live operator surface for bounded delegation.
- `internal/runtime/delegations.Service.RunAgent` creates a parent Work Item, parent Run Attempt, child delegation records, child Work Items, child Run Attempts, checkpoints, artifacts, memory summaries, and learning proposals for the hardcoded `portal-delivery-agent` path.
- `internal/runtime/supervision.Service.PlanSwarm` and `MaterializeSwarm` can plan and materialize generic child delegations from explicit `DelegationPlan` inputs.
- `internal/runtime/jobs.Service.NarrowDelegationAdmission` narrows tools, memory scopes, and executor selection for child delegation admission.
- `internal/runtime/jobs` already carries `execution_intent` and blocks governance, destructive, and unsafe mutation paths through approval or worktree policy.
- SQLite already persists `delegations` and `delegation_artifacts`.
- Runtime events already include `delegation.created`, `delegation.status_changed`, `delegation.child_attached`, `delegation.artifact_recorded`, `delegation.retry_requested`, and `delegation.retry_skipped`.
- `odin overview --json` already exposes a delegation-truth lane.

The registry already contains many specialist agent definitions, including research, planning, review, writing, admin, classifier, deduper, router, and delivery-related agents. The registry also has `subagent-delegation-planner-agent`, but that agent is intentionally advisory: it plans bounded subagent use and does not launch subagents.

## Reused Components

Implementation should reuse:

- `registry/agents/*.md` as the authored agent registry.
- `docs/contracts/registry-format.md` for registry format and validation rules.
- `internal/registry` parser, validator, compiler, snapshot, and loader tests.
- `docs/contracts/companion-swarm-orchestration.md` for delegation authority, child bounds, memory views, convergence modes, result envelope, and operator visibility.
- `odin companion delegate` as the canonical operator surface.
- `internal/cli/commands/companion.go` for parser and output shape.
- `internal/app/lifecycle/run.go` for command execution wiring.
- `internal/runtime/delegations` for the companion-delegate operator path.
- `internal/runtime/supervision` for generic swarm planning, child task materialization, and aggregation.
- `internal/runtime/jobs` for execution intent, approval gating, executor admission, and delegated tool/memory narrowing.
- `internal/store/sqlite` as the durable authority for Work Items, Run Attempts, delegations, artifacts, approvals, events, and projections.
- `internal/runtime/events` for audit events.
- existing tests in `internal/runtime/delegations`, `internal/runtime/supervision`, `internal/registry`, `internal/cli/commands`, `internal/app/lifecycle`, and `tests/integration`.

## New Components

Add one small registry-backed concept:

- **Delegatable Agent Profile**: optional metadata on a `registry/agents/*.md` item declaring that the agent may be used by `odin companion delegate`.

The v1 profile should describe:

- whether delegation is enabled for the agent.
- supported operator surface, initially `companion_delegate`.
- required inputs and optional inputs.
- child delegation specs or a reference to an existing template.
- child roles, action class, action key template, mutation mode source, executor preference, optional skill key, artifact target, and wave.
- supported convergence mode.
- required child result envelope fields.
- requested tool names and requested memory scopes for admission narrowing.
- audit/readback surfaces that must prove the run.

Add small runtime adapters only where necessary:

- registry frontmatter/types/compiler support for the optional profile.
- profile validation diagnostics.
- a profile-to-child-spec compiler in `internal/runtime/delegations`.
- command/runtime tests proving that unsupported agents are rejected and supported profiles produce the same durable delegation records as the hardcoded path.

Do not add a new agent runtime, second registry, second queue, second policy engine, second operator console, provider-native swarm path, or sidecar delegation service.

## Why New Components Are Necessary

The current system has two truths that need to converge:

- `registry/agents` names many specialist agents.
- `internal/runtime/delegations/templates.go` decides which agents are actually runtime-delegatable.

That mismatch is now the main hardening gap. Without an enforceable registry profile, an active registry agent can look available to operators while the runtime only supports one hardcoded template. Conversely, adding more hardcoded Go templates would deepen the split between authored registry intent and execution authority.

The Delegatable Agent Profile is necessary because Odin needs a reviewable, compiled contract for when an agent can be delegated, what child work it may create, what outputs it must produce, and what policy/readback proof is required.

## Locked Domain Decisions

- Product language remains **Work Item**, **Run Attempt**, **Companion**, **Control Scope**, and **Execution Lane**.
- `agent` remains a narrowed term for registry/runtime role definitions, not the primary product noun.
- `worker` remains runtime implementation, not the same thing as an agent definition.
- `executor` remains the tool/model lane used by a Run Attempt.
- `skill` remains a reusable procedure.
- `companion` remains the durable AI role visible to operators.
- A registry agent is not runtime-delegatable unless it declares a valid Delegatable Agent Profile.
- The Delegatable Agent Profile is optional. Most registry agents may stay descriptive or advisory.
- `subagent-delegation-planner-agent` remains advisory in v1. It may recommend a delegation plan, but it does not launch child work.
- `portal-delivery-agent` is the first compatibility target for a registry-backed profile because it is already proven through the real `odin companion delegate` path.
- Child delegation records remain the durable child-assignment contract.
- Child Work Items and child Run Attempts remain normal Odin work execution records.
- `delegation_artifacts` remain the structured child-output contract.
- Child delegations may only narrow parent authority: tools, memory visibility, mutation mode, and side-effect authority.
- Child delegations may not expand beyond the parent Workspace, Initiative, Companion, project governance, or approval policy.
- `odin companion delegate` remains the canonical operator surface for this slice.
- Runtime execution intent remains explicit and must continue to flow through tasks, delegations, jobs, approvals, runs, logs, overview, and retry behavior.
- No background child work is allowed without durable Work Item, Run Attempt, delegation, approval, event, or projection evidence.

No ADR is needed for this slice. The design makes existing registry and delegation contracts enforceable without introducing a hard-to-reverse architecture change.

## Selected Design

Implement registry-backed delegatable agent profiles in one compatibility-first slice.

The first implementation should promote the existing `portal-delivery-agent` hardcoded child spec into registry-backed metadata while preserving current operator behavior and proof paths. The runtime may keep a temporary compatibility fallback for `portal-delivery-agent`, but tests should prove that the profile path is used when present and that unsupported agents fail closed.

Suggested authored profile shape:

```yaml
delegation:
  enabled: true
  operator_surface: companion_delegate
  inputs:
    required:
      - portal_track
      - surface
    optional:
      - goal
      - intent
  convergence_mode: merge
  children:
    - delegation_key: ia-audit
      role: ia_audit
      wave: 1
      action_class: portal_delivery
      action_key_template: "{{portal_track}}:{{surface}}"
      mutation_mode_source: intent
      artifact_target: run_detail
      executor: codex_headless
      requested_tools:
        - repo_read
      requested_memory_scopes:
        - workspace
        - initiative
        - companion
    - delegation_key: design-direction
      role: design_direction
      wave: 1
      action_class: portal_delivery
      action_key_template: "{{portal_track}}:{{surface}}"
      mutation_mode_source: intent
      artifact_target: run_detail
      executor: codex_headless
      skill_key: pixel-perfect-ui-ux-designer
      requested_tools:
        - repo_read
      requested_memory_scopes:
        - workspace
        - initiative
        - companion
```

The exact YAML field names may be adjusted during implementation to match the registry compiler style, but the semantics should stay fixed: the profile compiles into child delegation specs, not into a second runtime object.

Runtime behavior:

1. `odin companion delegate <companion> --agent <agent-key> ...` parses the requested agent key and input fields.
2. The lifecycle loads the registry snapshot as it does today.
3. `internal/runtime/delegations` looks up the agent in the registry snapshot.
4. If the agent has no valid Delegatable Agent Profile, the command fails with a stable unsupported-delegation error and creates no Work Item, Run Attempt, delegation, or artifact.
5. If the profile exists, the runtime validates required inputs, renders child action keys from the profile, normalizes execution intent, and compiles child specs.
6. For each child, existing job admission narrows tool and memory access.
7. The existing delegation runtime creates parent and child Work Items, Run Attempts, delegations, checkpoints, artifacts, events, memory summaries, and readback records.
8. Existing retry and approval-aware behavior remains unchanged.
9. Operator readback stays in `odin companion delegate list/show/retry --json`, `odin jobs --json`, `odin runs --json`, `odin approvals all --json`, `odin logs --json`, `odin overview --json`, and `odin work status`.

Convergence mode note:

`docs/contracts/companion-swarm-orchestration.md` supports `merge`, `review_gate`, `rank`, and `quorum`. The current hardcoded `portal-delivery-agent` child specs use `parent_summary`, which is implementation drift. The v1 profile should use a contract-supported convergence mode, preferably `merge` for the portal-delivery compatibility path, while preserving parent summary output behavior in the existing parent-run completion code.

## Rejected Alternatives

### Add a new agent registry table

Rejected. `registry/agents/*.md` is already the authored authority, and `internal/registry` already compiles a snapshot. A table would add a second registry authority before runtime behavior requires one.

### Keep adding Go hardcoded templates

Rejected. Hardcoded templates are acceptable as compatibility scaffolding, but using them for every specialist agent would keep the registry descriptive and make operator availability hard to audit.

### Treat every active registry agent as delegatable

Rejected. Many registry agents are advisory, prompt inventory, or future-facing. Runtime delegation requires stricter input, child-work, output, policy, and readback contracts.

### Let the subagent planner launch children directly

Rejected. The planner is advisory by design. Launching child work must remain in Odin-owned runtime services behind `odin companion delegate`, Work Items, approvals, and delegation records.

### Add provider-native swarm execution

Rejected. The companion swarm contract explicitly forbids a second policy engine, second runtime authority, or provider-specific swarm path.

### Make worker output free-form Markdown only

Rejected. Free-form summaries can remain human-readable, but aggregation and audit trails require structured `delegation_artifacts` with stable result envelope fields.

## Test And Verification Plan

Focused local tests:

```bash
go test ./internal/registry -run 'Agent|Delegation|Compile|Validate' -count=1
go test ./internal/runtime/delegations -run 'PortalDelivery|DelegationProfile|UnsupportedAgent|Intent|Retry' -count=1
go test ./internal/runtime/supervision -run 'Swarm|Delegation|Aggregate' -count=1
go test ./internal/cli/commands -run 'Companion|Delegate' -count=1
go test ./internal/app/lifecycle -run 'TestRunCompanion.*Delegate|TestRunCompanion.*Delegation' -count=1
go test ./tests/integration -run 'CompanionSwarm|AlphaAcceptance' -count=1
```

Broader local verification:

```bash
go test ./...
make build
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
tmpdir="$(mktemp -d)"
ODIN_ROOT="$tmpdir" ./bin/odin help
ODIN_ROOT="$tmpdir" ./bin/odin companion delegate primary --agent portal-delivery-agent --portal-track student --surface dashboard --goal "prove registry-backed delegation" --intent read_only --json
ODIN_ROOT="$tmpdir" ./bin/odin companion delegate list --json
ODIN_ROOT="$tmpdir" ./bin/odin companion delegate show <delegation-id> --json
ODIN_ROOT="$tmpdir" ./bin/odin companion delegate primary --agent subagent-delegation-planner-agent --portal-track student --surface dashboard --goal "should fail closed" --intent read_only --json
ODIN_ROOT="$tmpdir" ./bin/odin overview --json
ODIN_ROOT="$tmpdir" ./bin/odin jobs --json
ODIN_ROOT="$tmpdir" ./bin/odin runs --json
ODIN_ROOT="$tmpdir" ./bin/odin approvals all --json
rm -rf "$tmpdir"
```

Required proof conditions:

- `portal-delivery-agent` delegates successfully through registry-backed profile metadata.
- child delegations preserve role, wave, action class, action key, mutation mode, executor, skill key, details JSON, child Work Item links, and child Run Attempt links.
- child tasks carry explicit `execution_intent` and `execution_intent_source`.
- unsupported agents fail closed before creating parent or child runtime records.
- governance/destructive/mutation intent behavior remains approval-aware and retry-safe.
- `delegation_artifacts` include structured result/readback evidence.
- `odin overview --json` still shows delegation truth through the existing operator surface.

E2E proof when available:

```bash
make odin-e2e-local
```

## Documentation Changes

Implementation should update:

- `docs/contracts/registry-format.md` with the optional Delegatable Agent Profile field contract.
- `docs/contracts/companion-swarm-orchestration.md` to state that runtime-delegatable registry agents must declare a valid profile and compile into normal delegation records.
- `docs/brownfield/SKILL_AGENT_REGISTRY.md` to mark runtime-delegatable agents as a stricter subset of registry agents.

Implementation may update:

- `CONTEXT.md` only if the implementer needs to lock a product-facing term. The likely term, **Delegatable Agent Profile**, can stay contract-local unless it appears in operator UI or cross-contract domain language.
- `docs/contracts/runtime-events.md` only if new event payload fields or error keys are introduced.

## Open Blockers

None for implementation planning.

Operational caveat: the current checkout is dirty with unrelated changes. Implementation should start in an isolated worktree or explicitly preserve existing local edits.

Proof caveat: the installed `odin` path resolved to `/home/orchestrator/odin-os/releases/current/bin/odin` during design audit. Implementation proof should rebuild and verify the intended `./bin/odin` or installed binary path before claiming operator behavior.

## Planning Handoff

Implement one PR-sized hardening slice:

- add optional Delegatable Agent Profile parsing/validation/compilation to the existing registry path.
- add a profile for the already-proven `portal-delivery-agent`.
- have `odin companion delegate` compile child specs from the registry profile when present.
- reject agents without valid profiles before persistence.
- preserve current `portal-delivery-agent` operator behavior, output fields, approval-aware intent behavior, retry behavior, and overview delegation truth.
- do not add broad worker implementations, provider-native swarms, PR creation, review/merge automation, or external mutations.

## Implementation Goal Prompt

```text
/goal Implement registry-backed Delegatable Agent Profiles for Odin companion delegation in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-agent-registry-subagent-delegation-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse registry/agents, internal/registry, odin companion delegate, internal/app/lifecycle, internal/runtime/delegations, internal/runtime/supervision, internal/runtime/jobs, internal/store/sqlite, delegation_artifacts, and runtime events. Do not introduce a second agent runtime, registry, queue, policy engine, operator console, provider-native swarm path, or broad worker implementation.

Required proof:
- go test ./internal/registry -run 'Agent|Delegation|Compile|Validate' -count=1
- go test ./internal/runtime/delegations -run 'PortalDelivery|DelegationProfile|UnsupportedAgent|Intent|Retry' -count=1
- go test ./internal/runtime/supervision -run 'Swarm|Delegation|Aggregate' -count=1
- go test ./internal/cli/commands -run 'Companion|Delegate' -count=1
- go test ./internal/app/lifecycle -run 'TestRunCompanion.*Delegate|TestRunCompanion.*Delegation' -count=1
- go test ./...
- make build
- tmpdir="$(mktemp -d)"; ODIN_ROOT="$tmpdir" ./bin/odin help; ODIN_ROOT="$tmpdir" ./bin/odin companion delegate primary --agent portal-delivery-agent --portal-track student --surface dashboard --goal "prove registry-backed delegation" --intent read_only --json; ODIN_ROOT="$tmpdir" ./bin/odin companion delegate list --json; ODIN_ROOT="$tmpdir" ./bin/odin companion delegate show <delegation-id> --json; ODIN_ROOT="$tmpdir" ./bin/odin companion delegate primary --agent subagent-delegation-planner-agent --portal-track student --surface dashboard --goal "should fail closed" --intent read_only --json; ODIN_ROOT="$tmpdir" ./bin/odin overview --json; ODIN_ROOT="$tmpdir" ./bin/odin jobs --json; ODIN_ROOT="$tmpdir" ./bin/odin runs --json; ODIN_ROOT="$tmpdir" ./bin/odin approvals all --json; rm -rf "$tmpdir"

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```

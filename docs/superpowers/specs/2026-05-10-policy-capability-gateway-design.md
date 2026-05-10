---
title: Policy And Capability Gateway Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os policy and capability gateway v1
---

# Policy And Capability Gateway Design

## Purpose

Odin uses policy to decide what tools, skills, commands, workflows, agents, and runtime actions are allowed. This design locks the v1 boundary: **Capability Gateway** is the canonical runtime concept. `plugin` is not a first-class Odin runtime kind in v1.

Plugin-like packaging may exist later as an import/distribution format, but once content enters Odin it must publish ordinary capabilities into the existing gateway, broker, registry, executor, and policy surfaces. It must not create a second runtime, second approval path, second executor catalog, or second policy system.

## Audit Summary

Inspected the active `/home/orchestrator/odin-os` checkout and treated the existing dirty worktree as pre-existing work.

Relevant artifacts:

- `/home/orchestrator/odin-os/AGENTS.md`
- `README.md`
- `CONTEXT.md`
- `docs/contracts/capability-gateway.md`
- `docs/contracts/capability-catalog.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/skill-lifecycle.md`
- `docs/contracts/executor-routing.md`
- `docs/operations/capability-reload.md`
- `docs/plans/2026-04-09-phase-07-tool-broker-design.md`
- `docs/plans/2026-04-16-odin-os-capability-platform-design.md`
- `docs/plans/2026-04-16-skill-permission-enforcement-design.md`
- `docs/plans/2026-04-16-skill-execution-wrapper-design.md`
- `docs/plans/2026-05-09-odin-os-governed-operating-system.md`
- `internal/core/capabilities`
- `internal/core/policy`
- `internal/tools/broker`
- `internal/tools/catalog`
- `internal/tools/invocation`
- `internal/integrations/tools`
- `internal/skills`
- `internal/core/commands`
- `internal/core/workflows`
- `internal/api/http/capabilities.go`
- `internal/transports/mcp/server.go`
- `internal/cli/repl/shell.go`
- `config/policies.yaml`
- `registry/`

Observed command surface:

- `./bin/odin help` exposes `skills`, `knowledge`, `browser`, `review`, `trigger`, `scheduler`, `transition`, `e2e`, and other current commands.
- No `plugin` command or registry kind is present.

## Existing State

Odin already has the pieces of a policy-governed capability system:

- `internal/core/capabilities.Gateway` lists descriptors, resolves versions, validates input shape, authorizes through `internal/core/policy`, invokes through a configured dispatcher, and exposes run lookup.
- HTTP exposes `GET /capabilities`, `GET /capabilities/{id}`, `POST /capabilities/{id}:invoke`, and `GET /runs/{run_id}`.
- MCP lists invokable `skill`, `workflow`, and `command` descriptors from the capability source.
- `registry/` authoring supports `agent`, `skill`, `workflow`, and `command`.
- Builtin tools live in `internal/tools/catalog` and are exposed by `internal/tools/broker`.
- Skill invocation is already more hardened than the generic gateway: it resolves permissions, checks project scope and transition policy for mutation, runs command-backed handlers through a restricted wrapper, and emits lifecycle events.
- Executor routing is already declarative through `config/executors.yaml` and `internal/executors/router`.
- Capability reload is designed as immutable snapshot publication with published/rejected events.

The current system is partial:

- Tools are real catalog entries, but they are not yet first-class descriptors in the active capability snapshot.
- REPL `/tool` builds a broker over `catalog.BuiltinDefinitions()` with an empty registry snapshot, so it is still a bootstrap path beside the gateway.
- `internal/core/capabilities.Gateway.ResumeRun` and `CancelRun` are stubs.
- Generic gateway policy uses a caller-kind permission allowlist such as `filesystem` and `web`, while skills use the stricter `repo.*` permission vocabulary. This is a known parity gap.
- Some high-risk tool behavior is guarded near the REPL path, such as requiring an approved social outcome before `browser_x_post_publish`, instead of being expressed as a common gateway policy result.
- There is no first-class plugin manager, plugin manifest, plugin executor, or plugin approval model.

## Reused Components

The selected design reuses:

- `internal/core/capabilities.Service` as the active immutable snapshot holder.
- `internal/core/capabilities.Gateway` as the canonical list/get/invoke/run surface.
- `internal/registry` as the authored capability ingestion layer for `agent`, `skill`, `workflow`, and `command`.
- `internal/tools/catalog` for builtin tool definitions.
- `internal/tools/broker` for thin-card planning and context-budget behavior.
- `internal/skills.Service` for skill CRUD, invocation, permission enforcement, wrapper execution, and lifecycle events.
- `internal/core/commands` and `internal/core/workflows` for command/workflow execution over the gateway.
- `internal/core/policy` and existing project approval/transition policy for authorization.
- `internal/executors/router` and executor contracts for bounded worker execution.
- Existing HTTP and MCP transports as thin gateway consumers.
- Existing operator proof rule: no capability counts as implemented without a real `odin` command path, policy enforcement where relevant, persistence where relevant, and readable audit evidence.

## New Components

Add only narrow adapter components. Do not add a plugin runtime.

New or changed components for the first implementation slice:

- a generated `tool` capability descriptor adapter that projects `catalog.ToolDefinition` entries into capability cards/descriptors without making tools authored registry files
- a gateway dispatcher branch for builtin tool descriptors that delegates to the existing catalog invocation path
- shared tests proving builtin tools are visible through gateway discovery while the broker still exposes thin cards
- contract/docs updates that state `plugin` is packaging language only, not a runtime kind

Possible later components, not part of the first slice:

- policy vocabulary consolidation between gateway descriptors and skill `repo.*` permissions
- durable run envelope persistence for gateway-invoked tools
- gateway-backed replacement for REPL `/tool` invocation once policy parity is proven
- resume/cancel implementation for capability runs

## Why New Components Are Necessary

The gap is not absence of a plugin manager. The gap is that existing capability surfaces are not fully consolidated.

A generated tool descriptor adapter is necessary because builtin tools already exist outside `registry/`, and forcing them into a new plugin or registry kind would create churn before the tool contract is stable. Projecting them into the active capability model keeps one gateway without pretending every tool is an authored Markdown asset.

A narrow dispatcher branch is necessary because listing a tool as a capability is incomplete unless the same gateway can invoke it through the existing invocation code and policy boundary.

No plugin manager is necessary now because Odin already has registry assets, tool catalog entries, executor routing, policy checks, and operator surfaces. A plugin manager would duplicate those concerns before the current capability plane is fully hardened.

## Locked Domain Decisions

- **Capability Gateway** is the canonical runtime surface for dynamic discovery and controlled invocation.
- **Capability** is the canonical runtime term for invokable or discoverable Odin units.
- V1 capability kinds are `tool`, `skill`, `agent`, `command`, and `workflow`.
- `plugin` is not a v1 runtime kind, registry kind, executor lane, approval object, or policy object.
- Future plugin-like packages may only be import/distribution containers that publish normal capabilities into existing Odin surfaces.
- `registry/` remains the authored source for `agent`, `skill`, `workflow`, and `command`.
- Builtin tools remain code-owned catalog entries for now, but should be projected into gateway discovery as generated `tool` descriptors.
- The tool broker remains a thin-card planning/catalog adapter, not a second runtime authority.
- Skill execution remains owned by `internal/skills.Service`; the gateway may dispatch to it but must not bypass its permission and wrapper rules.
- All mutation-capable invocation must pass policy before dispatch and produce audit evidence through existing Odin operator surfaces.
- High-risk real-world actions remain blocked or approval-gated until a source-specific resolver contract, policy rule, and real `odin` proof exist.

No ADR is required for this slice. The decision strengthens an already documented direction and is reversible at the packaging layer if a future marketplace/import format is added.

## Selected Design

Use a three-layer model.

### 1. Authored And Generated Capability Sources

Authored sources:

- `registry/agents/*.md`
- `registry/skills/*.md`
- `registry/workflows/*.md`
- `registry/commands/*.md`

Generated sources:

- builtin tool descriptors projected from `internal/tools/catalog.BuiltinDefinitions()`

The generated tool descriptors use kind `tool`, stable version `1.0.0` until tool-specific versioning exists, existing scopes/tags/schemas from `ToolDefinition`, and implementation metadata that makes clear they dispatch through the builtin catalog.

Generated descriptors are runtime descriptors, not registry files. They must not introduce `registry/tools/*.md` in this slice.

### 2. One Gateway, Thin Adapters

Discovery:

- HTTP, MCP, REPL capability listing, and future external agent adapters should consume `capabilities.Gateway`.
- Broker thin-card behavior may continue, but broker should be fed from capability descriptors or an explicit generated source rather than becoming runtime authority.

Invocation:

- `skill` dispatch calls `internal/skills.Service` and preserves its policy/wrapper behavior.
- `command` dispatch stays through `internal/core/commands`.
- `workflow` dispatch stays through `internal/core/workflows`.
- `tool` dispatch calls the existing builtin tool invocation path.
- Unsupported capability kinds fail closed with a structured error.

### 3. Policy Before Dispatch

Gateway authorization remains the top-level pre-dispatch gate. Capability-specific services may enforce stricter nested policy.

The first slice does not solve every policy vocabulary gap. It must make the gap explicit:

- generic gateway permissions still use descriptor permissions such as `filesystem` and `web`
- skill permissions still use `repo.read`, `runtime.read`, and `repo.mutate.*`
- high-risk tool gates must remain in place until moved into a common policy result

Implementation must not weaken existing skill policy or REPL social-publish checks while consolidating discovery.

### 4. Operator Language

Operator docs should say:

- "capability gateway" for the runtime surface
- "tool catalog" or "broker" for thin planning cards and builtin tool definitions
- "skill lifecycle" for authored skills and command-backed skill execution
- "plugin package" only for a future import/distribution container, never for an Odin runtime authority

## Rejected Alternatives

### Add A First-Class Plugin Runtime

Rejected.

This would require plugin manifests, plugin install state, plugin permissions, plugin execution, plugin events, and plugin operator commands. Those overlap directly with existing registry, catalog, gateway, executor, policy, and skill lifecycle surfaces.

### Rename Everything To Plugin

Rejected.

The repo already has precise domain terms. Renaming `tool`, `skill`, `workflow`, `command`, and `agent` into generic plugins would erase useful boundaries and make policy harder to reason about.

### Leave Tools Outside The Gateway

Rejected as the long-term target.

It is acceptable as bootstrap reality today, but it keeps `/tool`, HTTP/MCP capability discovery, and policy readback from converging on one operator story.

### Move Builtin Tools Into Registry Files Now

Rejected for the first slice.

Builtin tools include Go-backed drivers and live browser/calendar/social behavior. Projecting them into generated descriptors is a smaller step than inventing an authored `registry/tools` contract before the policy shape is fully settled.

## Test And Verification Plan

Local tests for the implementation slice:

- `go test ./internal/core/capabilities ./internal/tools/broker ./internal/api/http ./internal/transports/mcp`
- focused tests proving generated builtin tool descriptors appear in capability gateway listing
- focused tests proving registry-backed capabilities still list and resolve by version
- focused tests proving gateway rejects unsupported or policy-denied invocation before dispatch
- focused tests proving broker thin cards still omit full schemas/bodies until expansion

Repo-owned/operator proof:

- `which odin`
- `go build -o ./bin/odin ./cmd/odin`
- `./bin/odin help`
- a real capability discovery proof through the available operator surface, such as `odin serve` plus `GET /capabilities` if the implementation touches HTTP wiring
- any existing `odin e2e` or fixture-backed proof required by the touched command path

Security review requirements:

- confirm no plugin package can execute outside existing tool, skill, command, workflow, agent, executor, and policy surfaces
- confirm skill invocation still uses `internal/skills.Service` and restricted command wrapper
- confirm high-risk browser/social publish gates are preserved
- confirm generated tool descriptors do not expose hidden tools unless explicitly allowed
- confirm policy denials happen before dispatcher invocation

## Documentation Changes

Update:

- `CONTEXT.md` with the locked language decision
- this design spec as the implementation handoff

No ADR in this slice.

Later, if implementation lands, update:

- `docs/contracts/capability-gateway.md`
- `docs/contracts/capability-catalog.md`
- `docs/contracts/registry-format.md` only if generated `tool` descriptors require clarification
- `docs/operations/capability-reload.md` if generated tool descriptor publication affects reload/readiness behavior

## Open Blockers

- Active worktree already has unrelated dirty changes. Implementation should start in an isolated worktree or explicitly preserve those changes.
- Gateway `ResumeRun` and `CancelRun` remain unimplemented.
- Gateway policy and skill permission vocabulary are not unified.
- Builtin tool invocation has one-off high-risk guards that must not be removed until replaced by common policy.
- Durable gateway run persistence for tool invocation is not fully designed in this slice.

## Planning Handoff

Implement this in the smallest useful slice:

1. Keep the domain decision fixed: no v1 plugin runtime.
2. Add generated builtin tool descriptors to capability gateway discovery.
3. Add only the dispatcher logic needed for builtin tool invocation if the existing gateway path is exercised by the proof.
4. Preserve broker thin-card behavior and skill execution hardening.
5. Add tests that prevent a parallel plugin registry or plugin kind from appearing accidentally.
6. Prove the real operator-visible discovery path.

Do not implement external plugin installation, marketplace behavior, dynamic code loading, new approval tables, new executor catalogs, or a new policy engine.

## Implementation Goal Prompt

```text
/goal Implement Policy and Capability Gateway v1 consolidation in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-policy-capability-gateway-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse internal/core/capabilities, internal/tools/catalog, internal/tools/broker, internal/skills.Service, internal/core/policy, existing HTTP/MCP capability surfaces, and existing Odin operator proof paths. Do not introduce a plugin runtime, plugin registry kind, plugin executor lane, plugin approval model, plugin command group, or parallel policy engine.

Required proof:
- which odin
- go test ./internal/core/capabilities ./internal/tools/broker ./internal/api/http ./internal/transports/mcp
- go build -o ./bin/odin ./cmd/odin
- ./bin/odin help
- a real operator-visible capability discovery proof through the touched gateway surface

Delivery:
- include a security review section covering policy, tools, skill invocation, and absence of a plugin runtime
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```

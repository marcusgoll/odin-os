---
title: Odin OS Agents Inventory
status: draft
date: 2026-04-30
---

# Odin OS Agents Inventory

## Scope

This inventory covers active agent definitions and agent-like role assets in the current `odin-os` checkout, excluding `.git/`, `.worktrees/`, and `node_modules/`. The repo has no root `.agents/`, `.codex/`, or `agents/` directory. The canonical authored agent location is `registry/agents/`.

## Canonical Registry Agents

| Path | Name | Role | Instructions summary | Model / sandbox / approval settings | Maps to target role model | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `registry/agents/triage-agent.md` | Triage Agent | `intake-triager` | Review incoming requests, identify scope, classify work, and return a routing recommendation with missing constraints. Must not mutate project state or invent authority. | No model, sandbox, or approval settings in the registry asset. Tools listed: `filesystem`, `web`. | Yes. Maps to the target intake/triage role. It is role/persona authority while `triage-skill` is procedure authority. | Keep. Preserve as the canonical intake agent and extend only through the registry contract. |

## Worker Role Implementations

| Path | Name | Role | Instructions summary | Model / sandbox / approval settings | Maps to target role model | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `internal/workers/planner/service.go` | Planner worker service | Planner / capability selector | Prepares thin tool/skill/agent catalog cards and materializes selected capabilities. Rejects sub-agent expansion without explicit opt-in. | No model or sandbox settings. Uses broker/budget limits from `internal/tools`. | Yes. Maps to planner role and enforces useful explicit sub-agent opt-in. | Keep/refactor. Keep this as the canonical planner substrate and connect future planning prompts through it. |
| `internal/workers/builder/.gitkeep` | Builder worker placeholder | Builder | Empty placeholder directory only. | None. | Intended target role, but no behavior. | Refactor later. Implement only after the executor/worktree security path is real. |
| `internal/workers/qa/.gitkeep` | QA worker placeholder | QA | Empty placeholder directory only. | None. | Intended target role, but no behavior. | Refactor later. Tie to existing verification model and command proof requirements. |
| `internal/workers/reviewer/.gitkeep` | Reviewer worker placeholder | Reviewer | Empty placeholder directory only. | None. | Intended target role, but no behavior. | Refactor later. Use review findings and human handoff, not autonomous approval. |
| `internal/workers/research/.gitkeep` | Research worker placeholder | Research | Empty placeholder directory only. | None. | Intended target role, but no behavior. | Refactor later. Keep read-only unless a ticket gives specific mutation authority. |

## Scaffold Role Definitions

| Path | Name | Role | Instructions summary | Model / sandbox / approval settings | Maps to target role model | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `internal/agents/roles.go` | Scaffold role constants | `triage`, `planner`, `builder`, `qa`, `reviewer`, `security` | Uncommitted role enum from agency scaffold. | None. | Partially. It overlaps executor task kinds and worker directories. | Replace into existing role/task-kind model or remove. Do not keep as a second role authority. |
| `src/agents/index.ts` | TypeScript agency roles | `triage`, `planner`, `builder`, `qa`, `reviewer`, `maintainer` | Uncommitted TypeScript scaffold role list. | None. | No. Conflicts with Go-native direction and includes `maintainer` instead of `security`. | Remove with the accidental TypeScript scaffold after explicit cleanup approval. |

## Agent-Like Prompt Assets

| Path | Name | Role | Instructions summary | Model / sandbox / approval settings | Maps to target role model | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `prompts/templates/agency-builder.md` | Agency builder template | Builder | One work item, one worktree, no merge, no production deploy, no root, no `danger-full-access`, return structured handoff. | Encodes sandbox prohibitions in prose; no typed enforcement. | Yes conceptually, but duplicated with worker prompts. | Refactor into canonical prompt location or remove duplicate after prompt model decision. |
| `prompts/workers/agency-builder.md` | Agency builder worker prompt | Builder | Work on exactly one Work Item in assigned worktree and branch; no merge/deploy/secrets; return changed files, verification, risks, handoff. | Encodes safety rules in prose; no typed enforcement. | Yes conceptually. | Refactor. Preserve useful boundaries but integrate with `internal/executors` and prompt renderer. |
| `prompts/workers/agency-qa.md` | Agency QA worker prompt | QA | Run requested checks, record failures, return handoff summary; QA evidence does not approve merge/deploy. | No model/sandbox settings. | Yes conceptually. | Refactor. Tie to verification model and review outputs. |
| `prompts/workers/agency-reviewer.md` | Agency review worker prompt | Reviewer | Prioritize bugs, regressions, missing tests, policy violations, and unclear handoff evidence; human review remains required. | No model/sandbox settings. | Yes conceptually. | Refactor. Useful instructions, but should not bypass human approval. |

## Agent Gaps And Conflicts

- No active registry agents exist for builder, QA, reviewer, security, or GitHub intake.
- `internal/agents/roles.go`, `internal/workers/*`, executor `TaskKind`, and prompt `role` frontmatter are separate role vocabularies today.
- The TypeScript scaffold role list conflicts with the Go-native brownfield target.
- No active agent definition declares model, sandbox, or approval policy. Those concerns currently belong in executor config, security policy, and workflow/operator gates.

## Consolidation Recommendations

1. Keep `registry/agents/triage-agent.md` and `internal/workers/planner` as proven/current.
2. Define one Go role vocabulary by reconciling `internal/executors/contract.TaskKind`, `internal/workers`, and registry `role` values.
3. Remove or merge `internal/agents/roles.go` after the canonical role vocabulary is documented.
4. Treat prompt files as draft role instructions until a prompt renderer loads them through a typed interface.
5. Do not add runtime model/sandbox/approval fields to ad hoc prompts; place those controls in executor config, worker policy, and approval gates.

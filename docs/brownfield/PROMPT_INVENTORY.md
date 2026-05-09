---
title: Odin OS Prompt Inventory
status: draft
date: 2026-04-30
---

# Odin OS Prompt Inventory

## Scope

This inventory covers authored prompt assets, prompt-like registry instructions, prompt renderers, and instruction files in the active checkout. `.worktrees/` snapshots and `node_modules/` are excluded.

## Active Prompt Files

| Path | Name | Role / use case | Inputs | Outputs | Valid | Overlap | Recommendation |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `prompts/templates/agency-builder.md` | Agency builder template | Builder prompt for one Work Item in one worktree. | Work Item context and assigned worktree, once renderer exists. | Structured summary, verification evidence, risks, human handoff state. | Draft-valid Markdown with simple frontmatter; not validated by current registry compiler. | Overlaps `prompts/workers/agency-builder.md`. | Refactor. Keep safety language but choose one canonical builder prompt. |
| `prompts/workers/agency-builder.md` | Agency builder worker prompt | Worker prompt for build role. | Assigned Work Item, task branch, worktree, constraints. | Changed files, verification run, risks, handoff notes. | Draft-valid Markdown with simple frontmatter; not validated by current registry compiler. | Overlaps `prompts/templates/agency-builder.md`. | Refactor as canonical builder worker prompt if prompt loader is built. |
| `prompts/workers/agency-qa.md` | Agency QA worker prompt | QA checks and failure reporting. | Requested checks and worker output/artifacts. | Concise handoff summary with failures. | Draft-valid Markdown with simple frontmatter; not validated by current registry compiler. | Overlaps verification model and future QA worker. | Refactor. Tie to `docs/contracts/verification-model.md`. |
| `prompts/workers/agency-reviewer.md` | Agency review worker prompt | Review worker prompt. | Worker diff/output and verification evidence. | Bugs, regressions, missing tests, policy violations, unclear handoff evidence. | Draft-valid Markdown with simple frontmatter; not validated by current registry compiler. | Overlaps future reviewer agent and code review skills. | Refactor. Preserve human-approval boundary. |
| `prompts/system/.gitkeep` | Placeholder | Reserves system prompt directory. | None. | None. | Valid placeholder. | None. | Keep until prompt layout decision. |
| `prompts/templates/.gitkeep` | Placeholder | Reserves template prompt directory. | None. | None. | Valid placeholder. | None. | Keep until prompt layout decision. |
| `prompts/workers/.gitkeep` | Placeholder | Reserves worker prompt directory. | None. | None. | Valid placeholder. | None. | Keep until prompt layout decision. |

## Prompt Renderers And Prompt-Like Scaffolds

| Path | Purpose | Inputs | Outputs | Valid | Overlap | Recommendation |
| --- | --- | --- | --- | --- | --- | --- |
| `internal/prompts/renderer.go` | Go interface for rendering Odin-owned prompt templates into worker prompts. | Template name and `TemplateData` with WorkItemID and Role. | Rendered prompt string. | Compiles and is covered by `internal/prompts` tests; wiring remains limited to current runtime callers. | Overlaps current file-based prompts. | Refactor. Useful seam if connected to canonical prompt directory and executor contract. |
| `src/prompts/index.ts` | Removed TypeScript prompt renderer scaffold. | Historical inputs were `WorkItem` and `RunAttempt` from TS orchestrator types. | Prior inventory preserved its useful summary as a joined role/work item/boundary string. | Absent from the current tree; no active TypeScript prompt source remains to migrate. | No active runtime overlap remains. | Keep removed; do not recreate a TypeScript prompt renderer. |

## Registry Instructions With Prompt Content

Registry assets are not prompt templates, but they include durable instructions that prompt renderers and planners can project into execution context.

| Path | Kind | Prompt-like content | Valid | Recommendation |
| --- | --- | --- | --- | --- |
| `registry/skills/triage-skill.md` | Skill | Purpose, trigger, inputs, procedure, outputs, constraints, success criteria for intake. | Valid registry skill. | Keep and use as canonical intake instruction. |
| `registry/agents/triage-agent.md` | Agent | Role/persona instructions for deterministic triage. | Valid registry agent. | Keep and use as canonical intake role. |
| `registry/workflows/project-intake.md` | Workflow | Composes `triage-skill` and `triage-agent`. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-schedule.md` | Workflow | Operator-invoked schedule preflight instructions. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-seniority-bid.md` | Workflow | Operator-approved seniority bid instructions. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-fcfs-bid.md` | Workflow | Operator-approved FCFS bid instructions. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-tradeboard.md` | Workflow | Operator-invoked TradeBoard workflow instructions. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-tradeboard-split-post.md` | Workflow | Operator-attended split-post workflow instructions. | Valid registry workflow. | Keep. |
| `registry/workflows/flica-annual-vacation.md` | Workflow | Draft annual vacation workflow instructions. | Valid draft registry workflow. | Refactor only after an operator surface exists. |
| `registry/commands/status.md` | Command | Status command behavior and constraints. | Valid registry command. | Keep. |

## Instruction Files

| Path | Purpose | Precedence / role | Recommendation |
| --- | --- | --- | --- |
| `AGENTS.md` | Repo-local Codex worker instructions for brownfield Odin work. | Applies before `WORKFLOW.md`, after session/system instructions. | Keep. Update when repo-wide operating rules change. |
| `WORKFLOW.md` | Brownfield workflow for audit, classify, characterize, refactor, verify, document. | Supports `AGENTS.md`. | Keep. |
| `/home/orchestrator/AGENTS.md` | Parent machine-level Odin instructions. | Inherited when working on this machine; repo-local `AGENTS.md` documents precedence. | Reference only from this repo. |
| `CONTEXT.md` | Domain model and project context. | Canonical context for Odin domain decisions. | Keep. |
| `docs/adr/*.md` | Historical accepted ADRs. | Existing architecture authority. | Keep. |
| `docs/architecture/ADR-*.md` | Brownfield architecture decisions. | New ADR location for migration decisions. | Keep. |
| `docs/contracts/*.md` | Durable contracts for registry, executor, runtime, verification, layout, etc. | Contract authority. | Keep and prefer over prompt-only behavior. |
| `docs/plans/*.md` | Historical and design plans. | Reference only until implemented/proven. | Keep/reference; do not treat as runtime proof. |

## Legacy Prompt Inventory

`state/migration/inventory.json` accounts for 20 legacy prompt candidates from `odin-orchestrator`: rewrite candidates include prompt-caching, prompt templates, prompt tests, and Odin prompt scripts; archive candidates are duplicate backup copies. These are not active prompt files in Odin-OS. Reuse only by rewriting into current prompt or contract locations.

## Prompt Consolidation Recommendations

1. Choose one canonical builder prompt: keep either `prompts/templates/agency-builder.md` or `prompts/workers/agency-builder.md`, not both.
2. Define a prompt frontmatter contract before adding more worker prompts.
3. Wire prompts through a Go renderer only after the canonical executor role vocabulary is settled.
4. Keep safety boundaries in typed policy and executor launch checks; prompts can repeat them but must not be the only enforcement.
5. Treat registry assets as authored instructions, not free-form prompt blobs.

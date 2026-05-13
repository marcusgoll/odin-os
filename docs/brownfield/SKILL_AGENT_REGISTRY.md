---
title: Odin OS Skill And Agent Registry
status: active
date: 2026-04-30
---

# Odin OS Skill And Agent Registry

## Authority

Active Odin-authored skills and agents live under `registry/`.

- `registry/skills/*.md`: reusable procedures.
- `registry/agents/*.md`: role/persona definitions.
- `state/migration/drafts/skills/*.md`: review-only migration drafts.
- global Codex skills under `/home/orchestrator/.codex` or `/home/orchestrator/.agents` are operator tooling, not Odin runtime authority.
- `prompts/workers/<template>.md` contains canonical rendered worker prompts for the current Go file renderer.
- other `prompts/` paths are placeholders or deprecated provenance unless a later contract promotes them.

Do not create repo-local Codex `SKILL.md` files until Odin has a documented bridge between external Codex skills and Odin registry skills.

## What To Use

| Need | Use | Status | Notes |
| --- | --- | --- | --- |
| Intake classification | `registry/skills/triage-skill.md` with `registry/agents/triage-agent.md` | Active | Current canonical intake pair. |
| Audit before refactor | `registry/skills/brownfield-audit.md` | Active | Start here for brownfield work. |
| Feature scoping | `registry/skills/feature-spec.md` | Active | Use before implementation. |
| Architecture planning | `registry/skills/architecture-plan.md` | Active | Use for cross-package or contract changes. |
| Go runtime implementation | `registry/skills/go-orchestration-feature.md` | Active | Use for lifecycle/runtime/store/executor/VCS slices. |
| Runner consolidation | `registry/skills/runner-refactor.md` | Active | Use for Codex, app-server, executor, timeout, dry-run, and redaction changes. |
| Shim migration | `registry/skills/shim-normalization.md` | Active | Use before deleting or moving shims. |
| QA evidence | `registry/skills/qa-review.md` | Active | Produces verification evidence, not merge approval. |
| Security review | `registry/skills/security-review.md` | Active | Required for secrets, runners, shims, process execution, GitHub tokens, deployment, and worker policy. |
| PR readiness | `registry/skills/pr-review.md` | Active | Review findings and PR template proof. |
| Release readiness | `registry/skills/release-checklist.md` | Active | Human approval remains required. |
| Failure diagnosis | `registry/skills/failure-analysis.md` | Active | Reproduce and isolate before fixing. |

## Target Roles

| Target role | Current authority | Boundary | Status |
| --- | --- | --- | --- |
| `architect` | `architecture-plan`, `docs/architecture/*`, ADRs | Architecture mapping and migration sequencing; no direct runtime mutation without implementation ticket. | Documented target. |
| `go-orchestrator` | `go-orchestration-feature`, `internal/app`, `internal/runtime`, `internal/store`, `internal/executors`, `internal/vcs` | Go daemon and orchestration features through existing runtime seams. | Documented target. |
| `backend` | Existing Go services and API packages | Backend/service changes outside runner/security specializations. | Documented target. |
| `frontend` | No active Odin frontend worker | UI/dashboard work only after dashboard surface is chosen. | Gap. |
| `ios` | No active Odin iOS worker | Not an Odin core role today; use only for managed-project work that explicitly needs iOS. | Gap. |
| `qa` | `qa-review`, `internal/workers/qa` placeholder | Verification evidence and failure reports; no merge approval. | Skill active, worker pending. |
| `security` | `security-review`, `internal/security` scaffold | Secrets, sandbox, approvals, subprocesses, GitHub tokens, deployment policy. | Skill active, enforcement still pending. |
| `reviewer` | `pr-review`, `prompts/workers/agency-reviewer.md` draft | Findings and handoff clarity; human review remains required. | Skill active, worker pending. |
| `devops` | `release-checklist`, `scripts/dev/*`, `deploy/systemd/*` | Service install, release readiness, deployment handoff; no autonomous production deploy. | Skill active, hardening pending. |
| `docs` | `feature-spec`, `architecture-plan`, `pr-review` | Docs and contracts that reflect proven behavior separately from roadmap. | Documented target. |

## Role Boundaries

- A skill is procedure.
- An agent is a role/persona.
- A delegatable agent is the stricter subset of registry agents with an enabled `delegation` profile that compiles into Odin-owned child work records.
- A worker is runtime implementation.
- A prompt is text loaded by a future renderer.
- An executor is the tool/model lane used by a run.
- A companion/operator surface is not the same thing as a worker.

Do not collapse these into a single "agent" bucket. When in doubt, keep the asset in docs or draft status until a runtime owner exists.

## Duplicate Warnings

| Duplicate group | Keep now | Deprecated / review-only item | Migration note |
| --- | --- | --- | --- |
| Builder prompts | `prompts/workers/agency-builder.md` | `prompts/templates/agency-builder.md` | Canonical layout is `prompts/workers/<template>.md`; the deprecated template is provenance-only and its useful safety wording is preserved in the worker prompt. |
| Role vocabulary | `registry/agents/triage-agent.md`, `internal/workers/*`, executor `TaskKind` | `internal/agents/roles.go` | Reconcile before adding worker implementations. |
| TypeScript scaffold roles | Go registry and Go runtime | Historical `src/agents/index.ts`, `src/runner/*`, `src/prompts/*` | Keep absent from the current tree; do not recreate or wire a TypeScript runtime. |
| Migration skill drafts | New active Odin registry skills | `state/migration/drafts/skills/*.md` | Review-only provenance. Promote by rewriting, not copying wholesale. |
| App-server runner | `internal/runner/codexexec` compatibility facade and `internal/executors` target | `internal/runner/appserver` | Keep experimental and unimplemented until Codex exec is proven. |

## Promotion Rules

Promote a draft or duplicate only when:

1. It maps to one target role or target skill.
2. It follows `docs/contracts/registry-format.md`.
3. It has concise, trigger-specific summary text.
4. It does not create a second runtime authority.
5. It preserves human approval boundaries.
6. It has tests or real `odin` proof when runtime behavior changes.

Runtime delegation requires the stricter delegatable-agent profile contract. Active registry status alone does not mean `odin companion delegate` may launch the agent.

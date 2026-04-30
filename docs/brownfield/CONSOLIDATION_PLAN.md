---
title: Odin OS Skills And Agents Consolidation Plan
status: draft
date: 2026-04-30
---

# Odin OS Skills And Agents Consolidation Plan

## Current State

Odin-OS has one active registry skill, one active registry agent, several active workflow/command registry assets, a working planner service, thin shell scripts, and a large legacy migration inventory. It also has uncommitted agency scaffold assets that duplicate existing Go seams:

- `internal/runner` duplicates `internal/executors`.
- `internal/workspace` duplicates `internal/vcs`.
- `internal/agents/roles.go` duplicates executor task kinds and worker directories.
- `src/*`, `package.json`, and prompt scaffolds duplicate the Go-native target.
- `config/agency.example.yaml` and `configs/*.yaml` duplicate the active `config/` root.

## Consolidation Goals

1. Preserve working registry, planner, executor, VCS, and verification behavior.
2. Consolidate role vocabulary before adding new workers.
3. Keep migration drafts as review evidence until explicitly promoted.
4. Remove accidental scaffolds only after inventory and approval.
5. Implement real worker launch only through `internal/executors/contract` with security review.

## Keep / Refactor / Replace / Remove Decisions

### Keep

- `registry/skills/triage-skill.md`
- `registry/agents/triage-agent.md`
- `registry/workflows/*`
- `registry/commands/status.md`
- `internal/workers/planner`
- `internal/executors/contract`, `internal/executors/router`, and current adapter catalog
- `internal/vcs` worktree and lease packages
- thin dev/CI scripts under `scripts/`
- `AGENTS.md`, `WORKFLOW.md`, `CONTEXT.md`, `docs/adr/`, `docs/architecture/`, and `docs/contracts/`

### Refactor

- `prompts/workers/*.md` and `prompts/templates/agency-builder.md`: choose one prompt layout and add a prompt contract.
- `internal/prompts/renderer.go`: keep only if wired to canonical prompt assets.
- `internal/security/policy.go`: move enforcement into the canonical executor launch path before real subprocess execution.
- `scripts/dev/install-systemd-service.sh`: add hardening review before production use.
- high-value migration drafts such as `odin-control-plane-contract-checks`, `odin-github-auth-boundaries`, and `incident-commander`: rewrite into current contracts or registry entries.

### Replace

- `internal/runner/*`: replace with `internal/executors` implementations.
- `internal/tracker/github`: replace/refactor into one canonical GitHub intake adapter after package root decision.
- `internal/workspace/manager.go`: replace with `internal/vcs` leases/worktrees.
- `configs/*.yaml` and `config/agency.example.yaml`: merge useful examples into the active `config/` convention or remove.

### Remove

- TypeScript scaffold under `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, `eslint.config.js`, and TS tests after explicit cleanup approval.
- Migration drafts that are not relevant to Odin's agency/runtime mission, after approval. The first cleanup slice archived `blog-writer`, `brand-ad-generator`, and `slack-gif-creator` under `state/migration/archive/skills/`.
- Duplicate legacy backup candidates already marked `archive` in `state/migration/inventory.json`.

## Target Role Model

Use one vocabulary across registry, prompts, workers, and executors:

| Target role | Current assets | Action |
| --- | --- | --- |
| Intake triage | `registry/agents/triage-agent.md`, `registry/skills/triage-skill.md`, `registry/workflows/project-intake.md` | Keep as current canonical intake path. |
| Planner | `internal/workers/planner`, executor `TaskKindPlan` | Keep and expose through existing runtime services. |
| Builder | prompt drafts, executor `TaskKindBuild`, empty worker dir | Refactor after executor/worktree security is real. |
| QA | prompt draft, executor `TaskKindQA`, empty worker dir | Refactor around verification model. |
| Reviewer | prompt draft, executor `TaskKindReview`, empty worker dir | Refactor around code-review findings and human handoff. |
| Research | executor `TaskKindResearch`, empty worker dir | Keep read-only by default. |
| Security reviewer | `internal/agents/roles.go`, security policy scaffold, legacy security skills | Add only after security contract exists. |

## Ordered Refactor Tickets

1. **Lock active asset authority**
   - Document that `registry/` is active authority, `state/migration/drafts/skills/` is review-only, and `.worktrees/` is non-canonical.
   - Proof: registry loader tests pass.

2. **Choose the canonical role vocabulary**
   - Reconcile registry `role`, executor `TaskKind`, worker directories, and `internal/agents/roles.go`.
   - Output: ADR or contract update.

3. **Collapse runner seam**
   - Remove or merge `internal/runner/*` into `internal/executors`.
   - Keep real `codex exec` future work behind `internal/executors/contract`.

4. **Collapse workspace seam**
   - Replace `internal/workspace/manager.go` concept with `internal/vcs` worktree leases.
   - Add characterization tests before changing lease behavior.

5. **Decide prompt layout**
   - Choose `prompts/workers/` or `prompts/templates/` as canonical, add a prompt contract, and remove duplicate builder prompt after approval.

6. **Promote only one migration skill**
   - Start with `odin-control-plane-contract-checks`.
   - Rewrite into current `docs/contracts/` or `registry/skills/`; do not copy legacy source directly.

7. **Define GitHub intake adapter root**
   - Pick one package root and remove placeholder duplication.
   - Keep GitHub read-only until approval gates and tokens are reviewed.

8. **Add executor security review contract**
   - Move root/danger-full-access checks into the canonical executor launch path before real subprocess execution.

9. **Clean accidental TypeScript scaffold**
   - Remove TS files after confirming no unique useful prompt or role language remains.

10. **Add builder/QA/reviewer registry entries only after seams are settled**
   - Create active agents one at a time with tests and real `odin` proof for any operator-visible behavior.

## Stop Conditions

Stop and re-audit before proceeding if a proposed change:

- adds a second active skill registry
- adds a second runner interface
- launches Codex outside `internal/executors`
- mutates worktrees outside `internal/vcs`
- treats migration drafts as active runtime authority
- exposes GitHub tokens or production secrets to workers
- makes prompts the only security boundary
- removes an existing skill, agent, shim, or script without inventory and approval

## Preservation Rules

- Preserve useful behavior from legacy assets by rewriting into current contracts, registry assets, or tests.
- Preserve provenance when promoting migration drafts.
- Preserve human approval boundaries: no autonomous merge and no autonomous production deploy.
- Preserve backward compatibility until a removal ticket explicitly says otherwise.

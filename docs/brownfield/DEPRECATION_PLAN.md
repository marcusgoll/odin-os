---
title: Odin OS Skill And Agent Deprecation Plan
status: active
date: 2026-04-30
---

# Odin OS Skill And Agent Deprecation Plan

## Policy

Deprecation means "do not use for new work by default." It does not mean deletion.

No skill, agent, prompt, shim, or script should be removed until a separate cleanup ticket confirms:

1. the item is inventoried,
2. useful behavior has been preserved elsewhere,
3. no active callers depend on it,
4. tests and relevant real `odin` proof pass, and
5. the operator approves the removal.

## Deprecated Or Review-Only Items

| Item | Status | Replacement / preferred path | Migration note |
| --- | --- | --- | --- |
| `state/migration/drafts/skills/agent-designer.md` | Review-only | `architecture-plan`, future agent contract | Extract registry-authoring rules only; do not promote a parallel agent framework. |
| `state/migration/drafts/skills/agent-workflow-designer.md` | Review-only | `feature-spec`, `architecture-plan`, existing workflow registry | Preserve workflow authoring guidance only if it matches `docs/contracts/registry-format.md`. |
| `state/migration/drafts/skills/api-design-reviewer.md` | Review-only | `pr-review`, `security-review` | Fold API findings into reviewer/security behavior if needed. |
| `state/migration/drafts/skills/api-test-suite-builder.md` | Review-only | `qa-review` | Extract test strategy only when tied to Odin verification proof. |
| `state/migration/drafts/skills/blog-writer.md` | Deprecated | None for Odin core | Archive unless a governed content workflow is explicitly added. |
| `state/migration/drafts/skills/brand-ad-generator.md` | Deprecated | None for Odin core | Archive; not part of the orchestration mission. |
| `state/migration/drafts/skills/changelog-generator.md` | Review-only | `release-checklist`, `pr-review` | Preserve release-note guidance only as release handoff support. |
| `state/migration/drafts/skills/ci-cd-pipeline-builder.md` | Review-only | `release-checklist`, `security-review` | Keep CI/CD advice advisory and approval-gated. |
| `state/migration/drafts/skills/claude-api.md` | Deprecated | Executor/provider docs if needed | Replace with provider-neutral executor documentation. |
| `state/migration/drafts/skills/cloudflare.md` | Review-only | `release-checklist`, managed-project ops docs | Promote only for a project that actually uses Cloudflare. |
| `state/migration/drafts/skills/odin-control-plane-contract-checks.md` | Review-only, high value | `brownfield-audit`, `architecture-plan`, `pr-review` | Rewrite selected contract checks into current docs/contracts or tests. |
| `state/migration/drafts/skills/slack-gif-creator.md` | Deprecated | None for Odin core | Archive; not part of Odin-OS orchestration. |
| `prompts/templates/agency-builder.md` | Deprecated duplicate | `prompts/workers/agency-builder.md` until prompt contract exists | Keep for provenance; choose one prompt layout in a future ticket. |
| `internal/agents/roles.go` | Deprecated duplicate role authority | Registry role docs, worker directories, executor `TaskKind` | Reconcile in role-vocabulary ticket before removing. |
| `src/agents/index.ts` | Deprecated scaffold | Go registry and Go runtime | Remove with TypeScript scaffold cleanup after approval. |
| `src/runner/*` | Deprecated scaffold | `internal/runner/codexexec`, then `internal/executors` | Do not wire TS runner into Odin runtime. |
| `src/prompts/index.ts` | Deprecated scaffold | `registry/skills/*` and future Go prompt renderer | Preserve any unique wording before cleanup. |

## Migration Notes By Target Skill

| Target skill | Existing source to preserve | Notes |
| --- | --- | --- |
| `brownfield-audit` | Brownfield audit docs and AGENTS/WORKFLOW rules | Keep audit-first behavior and explicit current/partial/missing separation. |
| `feature-spec` | Roadmap and architecture planning docs | Keep scoped acceptance criteria and non-goals. |
| `architecture-plan` | Gap analysis and migration plan docs | Keep incremental migration and ADR triggers. |
| `go-orchestration-feature` | Existing Go runtime packages and verification model | Keep real `odin` proof requirement. |
| `runner-refactor` | Runner consolidation and shim retirement docs | Keep explicit args, timeout, dry-run, redaction, no `danger-full-access`. |
| `shim-normalization` | Shims inventory and retirement plan | Keep compatibility-first migration. |
| `qa-review` | Verification model and test stabilization work | Keep evidence-focused QA without merge authority. |
| `security-review` | Security docs and policy scaffolds | Move enforcement into runtime code before real worker launch. |
| `pr-review` | PR template validator and review rules | Keep findings-first review and required headings. |
| `release-checklist` | systemd/deployment docs and release proof rules | Keep human approval before deploy. |
| `failure-analysis` | Debugging lessons from prior incidents | Reproduce before fixing; avoid guess stacking. |

## Retirement Sequence

1. Keep all existing assets available.
2. Use the new registry skills for new brownfield work.
3. Decide prompt layout before editing prompt duplicates.
4. Reconcile role vocabulary before adding new worker implementations.
5. Clean TypeScript scaffold only after explicit cleanup approval.
6. Promote migration drafts one at a time by rewrite, not copy.

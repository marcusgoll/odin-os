---
title: Odin OS Skills Inventory
status: draft
date: 2026-04-30
---

# Odin OS Skills Inventory

## Scope

This inventory covers the active `odin-os` checkout with `.git/`, `.worktrees/`, and `node_modules/` excluded. Nested `.worktrees/` directories are branch snapshots, not canonical active assets. No repo-local `.agents/`, `.codex/`, root `skills/`, root `agents/`, or root `shims/` directories exist in the active checkout.

Active Odin skills live under `registry/skills/`. Migration-review skill drafts live under `state/migration/drafts/skills/`. Legacy source skills from `odin-orchestrator` are represented by `state/migration/inventory.json`, `docs/migration/legacy-inventory.md`, and `docs/migration/duplicate-report.md`; they are evidence, not runtime authority.

## Active Registry Skills

| Path | Name | Description | Trigger / use case | Inputs | Outputs | Scripts / references / assets | Valid | Overlap | Recommendation |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `registry/skills/triage-skill.md` | Triage Skill | Guides intake classification before deeper work starts. | Use when a request arrives and Odin must decide answer, plan, research, or execute. | User request, current scope, relevant repo context, known constraints. | Classification, blockers or assumptions, next runtime action. | Referenced by `registry/workflows/project-intake.md`; loaded by `internal/registry`; consumed by planner tests through the tool broker. | Yes. It follows `docs/contracts/registry-format.md`, and `go test ./internal/registry/...` passed. | Overlaps with `registry/agents/triage-agent.md` at intake classification, but this is acceptable: the skill is procedure, the agent is role/persona. | Keep. Preserve as the canonical intake skill and deepen through registry tooling rather than adding a second triage skill. |

## Migration Draft Skills

All files in this table are generated draft assets. They share the same common structure: `kind: skill`, `status: draft`, `tags: migration-draft`, `strictness: review`, and `applies_to: migration`. Their inputs are migration review of the legacy source and surrounding references. Their outputs are normalized assets ready for maintainers to promote or reject. None are active runtime authority until deliberately promoted into `registry/skills/`.

| Path | Name | Description | Trigger / use case | Scripts / references / assets | Valid | Overlap | Recommendation |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `state/migration/drafts/skills/agent-designer.md` | Agent Designer - Multi-Agent System Architecture | Draft migrated from legacy agent-design skill. | Use only when reviewing whether agent-design guidance belongs in Odin. | Legacy source `.claude/skills/agent-designer/SKILL.md`. | Draft-valid for migration review; not a registry asset because it lives under `state/`. | Overlaps `agent-workflow-designer`, `registry/agents/triage-agent.md`, and role constants in `internal/agents/roles.go`. | Refactor before promotion. Extract only durable agent-definition rules into registry/docs; do not create a parallel agent framework. |
| `state/migration/drafts/skills/agent-workflow-designer.md` | Agent Workflow Designer | Draft migrated from legacy workflow-design skill. | Use only when reviewing workflow design guidance. | Legacy source `.claude/skills/agent-workflow-designer/SKILL.md`. | Draft-valid for migration review only. | Overlaps `docs/contracts/registry-format.md`, `registry/workflows/*`, and `agent-designer`. | Refactor. Promote only workflow-authoring guidance that fits the registry contract. |
| `state/migration/drafts/skills/api-design-reviewer.md` | API Design Reviewer | Draft migrated from legacy API review skill. | Use only when reviewing API design review guidance. | Legacy source `.claude/skills/api-design-reviewer/SKILL.md`. | Draft-valid for migration review only. | Overlaps future review/security roles and generic PR review. | Refactor. Fold into a broader reviewer/security capability if API review becomes a first-class role. |
| `state/migration/drafts/skills/api-test-suite-builder.md` | API Test Suite Builder | Draft migrated from legacy API test skill. | Use only when reviewing API test-generation guidance. | Legacy source `.claude/skills/api-test-suite-builder/SKILL.md`. | Draft-valid for migration review only. | Overlaps QA role and verification model. | Refactor. Extract test strategy only if tied to Odin's `Verification Model`; avoid standalone test-suite skill. |
| `state/migration/archive/skills/blog-writer.md` | Blog Writer | Archived legacy writing skill draft. | Do not use for new Odin work; restore only if a governed content workflow is approved. | Legacy source `.claude/skills/blog-writer/SKILL.md`. | Archived; preserved for provenance only. | Overlaps marketing/content legacy skills. | Archived in the non-core content/media cleanup slice. |
| `state/migration/archive/skills/brand-ad-generator.md` | Brand Ad Generator | Archived legacy brand/ad skill draft. | Do not use for new Odin work. | Legacy source `.claude/skills/brand-ad-generator/SKILL.md`. | Archived; preserved for provenance only. | Overlaps marketing/content legacy skills. | Archived in the non-core content/media cleanup slice. |
| `state/migration/drafts/skills/changelog-generator.md` | Changelog Generator | Draft migrated from legacy changelog skill. | Use only when deciding whether release notes generation belongs in Odin. | Legacy source `.claude/skills/changelog-generator/SKILL.md`. | Draft-valid for migration review only. | Overlaps release-manager and PR/review handoff behavior. | Refactor. Potentially useful as release-handoff guidance, not as an autonomous skill. |
| `state/migration/drafts/skills/ci-cd-pipeline-builder.md` | CI/CD Pipeline Builder | Draft migrated from legacy CI/CD skill. | Use only when reviewing CI/CD automation guidance. | Legacy source `.claude/skills/ci-cd-pipeline-builder/SKILL.md`. | Draft-valid for migration review only. | Overlaps deployment/security rules and GitHub Actions CI. | Refactor. Keep only advisory CI review behavior; do not let workers alter deploy paths without approval. |
| `state/migration/archive/skills/claude-api.md` | Building LLM-Powered Applications with Claude | Archived legacy Claude API skill draft. | Do not use for new Odin work; restore only if provider-specific Claude guidance is explicitly approved. | Legacy source `.claude/skills/claude-api/SKILL.md`. | Archived; preserved for provenance only. | Overlaps executor/provider docs; not current runtime. | Archived as a provider-specific non-core migration draft; prefer provider-neutral executor docs. |
| `state/migration/drafts/skills/cloudflare.md` | Cloudflare Platform Skill | Draft migrated from legacy Cloudflare skill. | Use only when reviewing Cloudflare operational knowledge. | Legacy source `.claude/skills/cloudflare/SKILL.md`. | Draft-valid for migration review only. | Overlaps deployment/ops docs, not current Odin agency runtime. | Refactor into operations docs only if a managed project needs it. |
| `state/migration/drafts/skills/odin-control-plane-contract-checks.md` | Odin Control Plane Contract Checks | Draft migrated from legacy Odin contract checks. | Use when reviewing what legacy checks should become current contracts/tests. | Legacy source `.agents/skills/odin-control-plane-contract-checks/SKILL.md`. | Draft-valid for migration review only. | Overlaps current `docs/contracts/*`, `AGENTS.md`, `WORKFLOW.md`, and PR validator. | Refactor and promote selectively. This is the highest-value migration draft. |
| `state/migration/archive/skills/slack-gif-creator.md` | Slack GIF Creator | Archived legacy Slack media skill draft. | Do not use for new Odin work. | Legacy source `.agents/skills/slack-gif-creator/SKILL.md`. | Archived; preserved for provenance only. | Overlaps none in current Odin core. | Archived in the non-core content/media cleanup slice. |

## Legacy Source Skill Inventory

`state/migration/inventory.json` accounts for 130 legacy skill candidates from `/home/orchestrator/odin-orchestrator`: 74 classified `rewrite` and 56 classified `archive`. These files do not exist as active Odin-OS skills. They should not be copied wholesale.

Primary rewrite candidates worth revisiting first:

- `odin-control-plane-contract-checks`
- `odin-github-auth-boundaries`
- `odin-pr-state-check`
- `odin-backlog-triage`
- `odin-dispatch-efficiency`
- `odin-dispatch-modes`
- `odin-telegram-safety`
- `odin-webhook-dedup`
- `incident-commander`
- `observability-designer`
- `dependency-auditor`
- `code-reviewer`
- `pr-review-expert`
- `env-secrets-manager`
- `git-worktree-manager`

Known duplicate groups are already recorded in `docs/migration/duplicate-report.md`. Archive-classified backup paths must stay reference-only unless a future ticket explicitly promotes a specific asset.

## Skill-Level Consolidation Recommendations

1. Keep `registry/skills/triage-skill.md` as the only active skill.
2. Promote no migration draft directly. Rewrite each candidate into the current registry contract or into docs/contracts.
3. Prefer a small set of Odin-native skills: intake triage, brownfield refactor guard, verification proof, security review, GitHub intake review, and release handoff.
4. Do not create Codex `SKILL.md` files inside this repo until Odin has a clear bridge between external Codex skills and Odin registry skills.
5. Keep global Codex skills outside Odin-OS as operator tooling, not runtime authority.

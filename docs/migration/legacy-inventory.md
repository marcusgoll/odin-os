# Legacy Migration Inventory

Source root: `/home/orchestrator/odin-orchestrator`

## Summary

- `rewrite`: 107
- `reference_only`: 90
- `archive`: 61

## Candidates

- `skill` `brand-ad-generator` `.agents/skills/brand-ad-generator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `claude-api` `.agents/skills/claude-api/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt-caching` `.agents/skills/claude-api/shared/prompt-caching.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `mcp-builder` `.agents/skills/mcp-builder/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-control-plane-contract-checks` `.agents/skills/odin-control-plane-contract-checks/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `skill-creator` `.agents/skills/skill-creator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `slack-gif-creator` `.agents/skills/slack-gif-creator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `upgrade-stripe` `.agents/skills/upgrade-stripe/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `webapp-testing` `.agents/skills/webapp-testing/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `agent-designer` `.claude/skills/agent-designer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `agent-workflow-designer` `.claude/skills/agent-workflow-designer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `agent-designer` `.claude/skills/agents-skills-backup/agent-designer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `agent-workflow-designer` `.claude/skills/agents-skills-backup/agent-workflow-designer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `api-design-reviewer` `.claude/skills/agents-skills-backup/api-design-reviewer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `api-test-suite-builder` `.claude/skills/agents-skills-backup/api-test-suite-builder/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `blog-writer` `.claude/skills/agents-skills-backup/blog-writer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `changelog-generator` `.claude/skills/agents-skills-backup/changelog-generator/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `ci-cd-pipeline-builder` `.claude/skills/agents-skills-backup/ci-cd-pipeline-builder/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `code-reviewer` `.claude/skills/agents-skills-backup/code-reviewer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `codebase-onboarding` `.claude/skills/agents-skills-backup/codebase-onboarding/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `dependency-auditor` `.claude/skills/agents-skills-backup/dependency-auditor/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `docx` `.claude/skills/agents-skills-backup/docx/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `email-marketer` `.claude/skills/agents-skills-backup/email-marketer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `env-secrets-manager` `.claude/skills/agents-skills-backup/env-secrets-manager/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `frontend-design` `.claude/skills/agents-skills-backup/frontend-design/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `gdpr-dsgvo-expert` `.claude/skills/agents-skills-backup/gdpr-dsgvo-expert/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `git-worktree-manager` `.claude/skills/agents-skills-backup/git-worktree-manager/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `humanized-writer` `.claude/skills/agents-skills-backup/humanized-writer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `incident-commander` `.claude/skills/agents-skills-backup/incident-commander/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `marketing-copywriter` `.claude/skills/agents-skills-backup/marketing-copywriter/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `mcp-builder` `.claude/skills/agents-skills-backup/mcp-builder/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `migration-architect` `.claude/skills/agents-skills-backup/migration-architect/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `observability-designer` `.claude/skills/agents-skills-backup/observability-designer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-agent-browser` `.claude/skills/agents-skills-backup/odin-agent-browser/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-ai-agent-frameworks` `.claude/skills/agents-skills-backup/odin-ai-agent-frameworks/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-backlog-triage` `.claude/skills/agents-skills-backup/odin-backlog-triage/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-dispatch-efficiency` `.claude/skills/agents-skills-backup/odin-dispatch-efficiency/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-dispatch-modes` `.claude/skills/agents-skills-backup/odin-dispatch-modes/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-github-auth-boundaries` `.claude/skills/agents-skills-backup/odin-github-auth-boundaries/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-learnings-pollution` `.claude/skills/agents-skills-backup/odin-learnings-pollution/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-n8n-workflow-ops` `.claude/skills/agents-skills-backup/odin-n8n-workflow-ops/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-pr-state-check` `.claude/skills/agents-skills-backup/odin-pr-state-check/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-research` `.claude/skills/agents-skills-backup/odin-research/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-sentry` `.claude/skills/agents-skills-backup/odin-sentry/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-skill-scout` `.claude/skills/agents-skills-backup/odin-skill-scout/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-task-timing` `.claude/skills/agents-skills-backup/odin-task-timing/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-task-type-reference` `.claude/skills/agents-skills-backup/odin-task-type-reference/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-telegram-safety` `.claude/skills/agents-skills-backup/odin-telegram-safety/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `odin-webhook-dedup` `.claude/skills/agents-skills-backup/odin-webhook-dedup/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `pdf` `.claude/skills/agents-skills-backup/pdf/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `pptx` `.claude/skills/agents-skills-backup/pptx/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `pr-review-expert` `.claude/skills/agents-skills-backup/pr-review-expert/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `prompt-engineer-toolkit` `.claude/skills/agents-skills-backup/prompt-engineer-toolkit/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `prompt` `prompt-templates` `.claude/skills/agents-skills-backup/prompt-engineer-toolkit/references/prompt-templates.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `prompt` `prompt_tester` `.claude/skills/agents-skills-backup/prompt-engineer-toolkit/scripts/prompt_tester.py` -> `archive`
  rationale: backup or worktree path is not canonical
- `prompt` `prompt_versioner` `.claude/skills/agents-skills-backup/prompt-engineer-toolkit/scripts/prompt_versioner.py` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `release-manager` `.claude/skills/agents-skills-backup/release-manager/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `runbook-generator` `.claude/skills/agents-skills-backup/runbook-generator/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `senior-prompt-engineer` `.claude/skills/agents-skills-backup/senior-prompt-engineer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `prompt` `prompt_engineering_patterns` `.claude/skills/agents-skills-backup/senior-prompt-engineer/references/prompt_engineering_patterns.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `prompt` `prompt_optimizer` `.claude/skills/agents-skills-backup/senior-prompt-engineer/scripts/prompt_optimizer.py` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `senior-secops` `.claude/skills/agents-skills-backup/senior-secops/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `simplify` `.claude/skills/agents-skills-backup/simplify/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `skill-creator` `.claude/skills/agents-skills-backup/skill-creator/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `skill-security-auditor` `.claude/skills/agents-skills-backup/skill-security-auditor/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `skill-tester` `.claude/skills/agents-skills-backup/skill-tester/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `sample-skill` `.claude/skills/agents-skills-backup/skill-tester/assets/sample-skill/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `social-media-writer` `.claude/skills/agents-skills-backup/social-media-writer/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `tech-debt-tracker` `.claude/skills/agents-skills-backup/tech-debt-tracker/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `webapp-testing` `.claude/skills/agents-skills-backup/webapp-testing/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `xlsx` `.claude/skills/agents-skills-backup/xlsx/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `api-design-reviewer` `.claude/skills/api-design-reviewer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `api-test-suite-builder` `.claude/skills/api-test-suite-builder/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `blog-writer` `.claude/skills/blog-writer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `brand-ad-generator` `.claude/skills/brand-ad-generator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `changelog-generator` `.claude/skills/changelog-generator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `ci-cd-pipeline-builder` `.claude/skills/ci-cd-pipeline-builder/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `claude-api` `.claude/skills/claude-api/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `cloudflare` `.claude/skills/cloudflare/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `code-reviewer` `.claude/skills/code-reviewer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `codebase-onboarding` `.claude/skills/codebase-onboarding/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `dependency-auditor` `.claude/skills/dependency-auditor/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `doc-coauthoring` `.claude/skills/doc-coauthoring/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `docker-development` `.claude/skills/docker-development/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `docx` `.claude/skills/docx/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `email-marketer` `.claude/skills/email-marketer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `env-secrets-manager` `.claude/skills/env-secrets-manager/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `find-skills` `.claude/skills/find-skills/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `frontend-design` `.claude/skills/frontend-design/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `gdpr-dsgvo-expert` `.claude/skills/gdpr-dsgvo-expert/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `git-worktree-manager` `.claude/skills/git-worktree-manager/SKILL.md` -> `archive`
  rationale: backup or worktree path is not canonical
- `skill` `google-workspace-cli` `.claude/skills/google-workspace-cli/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `humanized-writer` `.claude/skills/humanized-writer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `incident-commander` `.claude/skills/incident-commander/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `internal-comms` `.claude/skills/internal-comms/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `marketing-copywriter` `.claude/skills/marketing-copywriter/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `mcp-builder` `.claude/skills/mcp-builder/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `migration-architect` `.claude/skills/migration-architect/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `observability-designer` `.claude/skills/observability-designer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-agent-browser` `.claude/skills/odin-agent-browser/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-backlog-triage` `.claude/skills/odin-backlog-triage/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-circuit-breaker` `.claude/skills/odin-circuit-breaker/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-content-insight` `.claude/skills/odin-content-insight/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-dispatch-efficiency` `.claude/skills/odin-dispatch-efficiency/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-dispatch-modes` `.claude/skills/odin-dispatch-modes/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-github-auth-boundaries` `.claude/skills/odin-github-auth-boundaries/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-learnings-pollution` `.claude/skills/odin-learnings-pollution/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-n8n-workflow-ops` `.claude/skills/odin-n8n-workflow-ops/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-pr-state-check` `.claude/skills/odin-pr-state-check/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-research` `.claude/skills/odin-research/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-sentry` `.claude/skills/odin-sentry/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-skill-scout` `.claude/skills/odin-skill-scout/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-task-timing` `.claude/skills/odin-task-timing/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-task-type-reference` `.claude/skills/odin-task-type-reference/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-telegram-safety` `.claude/skills/odin-telegram-safety/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `odin-webhook-dedup` `.claude/skills/odin-webhook-dedup/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `pdf` `.claude/skills/pdf/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `pptx` `.claude/skills/pptx/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `pr-review-expert` `.claude/skills/pr-review-expert/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `prompt-engineer-toolkit` `.claude/skills/prompt-engineer-toolkit/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt-templates` `.claude/skills/prompt-engineer-toolkit/references/prompt-templates.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt_tester` `.claude/skills/prompt-engineer-toolkit/scripts/prompt_tester.py` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt_versioner` `.claude/skills/prompt-engineer-toolkit/scripts/prompt_versioner.py` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `release-manager` `.claude/skills/release-manager/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `runbook-generator` `.claude/skills/runbook-generator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `senior-prompt-engineer` `.claude/skills/senior-prompt-engineer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt_engineering_patterns` `.claude/skills/senior-prompt-engineer/references/prompt_engineering_patterns.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `prompt_optimizer` `.claude/skills/senior-prompt-engineer/scripts/prompt_optimizer.py` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `senior-secops` `.claude/skills/senior-secops/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `sentry-fix-issues` `.claude/skills/sentry-fix-issues/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `simplify` `.claude/skills/simplify/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `skill-creator` `.claude/skills/skill-creator/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `skill-security-auditor` `.claude/skills/skill-security-auditor/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `skill-tester` `.claude/skills/skill-tester/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `sample-skill` `.claude/skills/skill-tester/assets/sample-skill/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `social-media-writer` `.claude/skills/social-media-writer/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `tech-debt-tracker` `.claude/skills/tech-debt-tracker/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `upgrade-stripe` `.claude/skills/upgrade-stripe/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `web-artifacts-builder` `.claude/skills/web-artifacts-builder/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `webapp-testing` `.claude/skills/webapp-testing/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `skill` `xlsx` `.claude/skills/xlsx/SKILL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `architecture_doc` `2026-03-20-odin-engine-v2` `docs/adr/2026-03-20-odin-engine-v2.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-cognitive-control-plane` `docs/adr/2026-03-27-odin-cognitive-control-plane.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `adr-001-codex-nested-delegation-guard` `docs/adrs/ADR-001-codex-nested-delegation-guard.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `adr-002-openapi-contract-check-branch-protection` `docs/adrs/ADR-002-openapi-contract-check-branch-protection.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `adr-003-agent-model-routing` `docs/adrs/ADR-003-agent-model-routing.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `adr-004-managed-mcp-browser-architecture` `docs/adrs/ADR-004-managed-mcp-browser-architecture.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `adr-005-claude-github-app-permissions` `docs/adrs/ADR-005-claude-github-app-permissions.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `prompt` `2026-03-27-odin-shims-prompts-workflow-audit` `docs/audits/2026-03-27-odin-shims-prompts-workflow-audit.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `finance-readout-delivery-review` `docs/checklists/finance-readout-delivery-review.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `finance-readout-draft-review` `docs/checklists/finance-readout-draft-review.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `finance-readout-review` `docs/checklists/finance-readout-review.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `finance-summary-review` `docs/checklists/finance-summary-review.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `staging-and-promotion` `docs/deployments/staging-and-promotion.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `architecture_doc` `2026-03-22-content-visual-pipeline-design` `docs/plans/2026-03-22-content-visual-pipeline-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-content-visual-pipeline` `docs/plans/2026-03-22-content-visual-pipeline.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-github-intake-consolidation-design` `docs/plans/2026-03-22-github-intake-consolidation-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-huginn-extraction-quality` `docs/plans/2026-03-22-huginn-extraction-quality.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-jarvis-mode-design` `docs/plans/2026-03-22-jarvis-mode-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-jarvis-mode-implementation` `docs/plans/2026-03-22-jarvis-mode-implementation.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-sentry-pipeline-fix-design` `docs/plans/2026-03-22-sentry-pipeline-fix-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-22-sentry-pipeline-fix-plan` `docs/plans/2026-03-22-sentry-pipeline-fix-plan.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-homelab-design` `docs/plans/2026-03-23-whisparr-homelab-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-homelab` `docs/plans/2026-03-23-whisparr-homelab.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-integrations-design` `docs/plans/2026-03-23-whisparr-integrations-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-integrations` `docs/plans/2026-03-23-whisparr-integrations.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-public-indexer-expansion-design` `docs/plans/2026-03-23-whisparr-public-indexer-expansion-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-23-whisparr-public-indexer-expansion` `docs/plans/2026-03-23-whisparr-public-indexer-expansion.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-autonomous-perception-layer-design` `docs/plans/2026-03-24-autonomous-perception-layer-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-autonomous-perception-layer-plan` `docs/plans/2026-03-24-autonomous-perception-layer-plan.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-dynamic-model-router-design` `docs/plans/2026-03-24-dynamic-model-router-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-dynamic-model-router-impl` `docs/plans/2026-03-24-dynamic-model-router-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-error-handling-observability` `docs/plans/2026-03-24-error-handling-observability.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-huginn-captcha-solving-design` `docs/plans/2026-03-24-huginn-captcha-solving-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-huginn-captcha-solving-impl` `docs/plans/2026-03-24-huginn-captcha-solving-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-qa-pr-pipeline-design` `docs/plans/2026-03-24-qa-pr-pipeline-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-qa-pr-pipeline-impl` `docs/plans/2026-03-24-qa-pr-pipeline-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-simulation-engine-design` `docs/plans/2026-03-24-simulation-engine-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-simulation-engine-impl` `docs/plans/2026-03-24-simulation-engine-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-stall-kill-per-role-scheduling-design` `docs/plans/2026-03-24-stall-kill-per-role-scheduling-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-24-stall-kill-per-role-scheduling` `docs/plans/2026-03-24-stall-kill-per-role-scheduling.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-25-dynamic-model-router-audit-remediation` `docs/plans/2026-03-25-dynamic-model-router-audit-remediation.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-25-odin-pipeline-hardening-design` `docs/plans/2026-03-25-odin-pipeline-hardening-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-25-odin-pipeline-hardening` `docs/plans/2026-03-25-odin-pipeline-hardening.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-backend-health-automation` `docs/plans/2026-03-26-backend-health-automation.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-ceo-direct-line-status-e2e` `docs/plans/2026-03-26-ceo-direct-line-status-e2e.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-ceo-shim-contract-design` `docs/plans/2026-03-26-ceo-shim-contract-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-ceo-shim-contract` `docs/plans/2026-03-26-ceo-shim-contract.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-infra-maintenance-hardening` `docs/plans/2026-03-26-infra-maintenance-hardening.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-llm-backend-failover` `docs/plans/2026-03-26-llm-backend-failover.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-main-push-ci-workflow-design` `docs/plans/2026-03-26-main-push-ci-workflow-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-main-push-ci-workflow` `docs/plans/2026-03-26-main-push-ci-workflow.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-self-hosted-runner-stack-design` `docs/plans/2026-03-26-odin-self-hosted-runner-stack-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-self-hosted-runner-stack` `docs/plans/2026-03-26-odin-self-hosted-runner-stack.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-token-burn-reduction-design` `docs/plans/2026-03-26-odin-token-burn-reduction-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-token-burn-reduction` `docs/plans/2026-03-26-odin-token-burn-reduction.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-tui-task-labels-design` `docs/plans/2026-03-26-odin-tui-task-labels-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-26-odin-tui-task-labels` `docs/plans/2026-03-26-odin-tui-task-labels.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-gmail-watch-renew-fail-closed` `docs/plans/2026-03-27-gmail-watch-renew-fail-closed.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-agentic-roadmap-design` `docs/plans/2026-03-27-odin-agentic-roadmap-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-agentic-roadmap` `docs/plans/2026-03-27-odin-agentic-roadmap.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-overseer-tui-design` `docs/plans/2026-03-27-odin-overseer-tui-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-overseer-tui` `docs/plans/2026-03-27-odin-overseer-tui.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-pr-workflow-design` `docs/plans/2026-03-27-odin-pr-workflow-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-pr-workflow-implementation` `docs/plans/2026-03-27-odin-pr-workflow-implementation.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-shims-prompts-audit-design` `docs/plans/2026-03-27-odin-shims-prompts-audit-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-shims-prompts-audit` `docs/plans/2026-03-27-odin-shims-prompts-audit.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-trust-audit-design` `docs/plans/2026-03-27-odin-trust-audit-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-trust-audit` `docs/plans/2026-03-27-odin-trust-audit.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-web-standards-hardening-design` `docs/plans/2026-03-27-odin-web-standards-hardening-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-odin-web-standards-hardening` `docs/plans/2026-03-27-odin-web-standards-hardening.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-pr-merge-orchestration-design` `docs/plans/2026-03-27-pr-merge-orchestration-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-pr-merge-orchestration-impl` `docs/plans/2026-03-27-pr-merge-orchestration-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-token-efficiency-audit-design` `docs/plans/2026-03-27-token-efficiency-audit-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-27-token-efficiency-audit-plan` `docs/plans/2026-03-27-token-efficiency-audit-plan.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-28-merge-first-pipeline-design` `docs/plans/2026-03-28-merge-first-pipeline-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-28-merge-first-pipeline` `docs/plans/2026-03-28-merge-first-pipeline.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-30-automated-promotion-design` `docs/plans/2026-03-30-automated-promotion-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-30-automated-promotion-impl` `docs/plans/2026-03-30-automated-promotion-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-30-server-management-design` `docs/plans/2026-03-30-server-management-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-03-30-server-management-impl` `docs/plans/2026-03-30-server-management-impl.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-finance-week3-reconciliation-gates-design` `docs/plans/2026-04-01-finance-week3-reconciliation-gates-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-finance-week3-reconciliation-gates` `docs/plans/2026-04-01-finance-week3-reconciliation-gates.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-finance-week4-summary-surfaces-design` `docs/plans/2026-04-01-finance-week4-summary-surfaces-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-finance-week4-summary-surfaces` `docs/plans/2026-04-01-finance-week4-summary-surfaces.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-odin-finance-audit-design` `docs/plans/2026-04-01-odin-finance-audit-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-01-odin-finance-audit` `docs/plans/2026-04-01-odin-finance-audit.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week5-readout-artifacts-design` `docs/plans/2026-04-03-finance-week5-readout-artifacts-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week5-readout-artifacts` `docs/plans/2026-04-03-finance-week5-readout-artifacts.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week6-draft-handoff-design` `docs/plans/2026-04-03-finance-week6-draft-handoff-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week6-draft-handoff` `docs/plans/2026-04-03-finance-week6-draft-handoff.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week7-gmail-draft-design` `docs/plans/2026-04-03-finance-week7-gmail-draft-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-03-finance-week7-gmail-draft` `docs/plans/2026-04-03-finance-week7-gmail-draft.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-04-memory-pruning-and-scope-fix-design` `docs/plans/2026-04-04-memory-pruning-and-scope-fix-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-05-ci-gates-design` `docs/plans/2026-04-05-ci-gates-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-05-ci-gates-implementation` `docs/plans/2026-04-05-ci-gates-implementation.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `architecture_doc` `2026-04-05-odin-autonomy-loop-design` `docs/plans/2026-04-05-odin-autonomy-loop-design.md` -> `reference_only`
  rationale: architecture material should inform design but not be promoted directly
- `operational_playbook` `codex_session_protocol` `docs/process/CODEX_SESSION_PROTOCOL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `odin_workflow_event_model` `docs/process/ODIN_WORKFLOW_EVENT_MODEL.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `pr_policy_onboarding` `docs/process/PR_POLICY_ONBOARDING.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `workflow_stage_gates` `docs/process/WORKFLOW_STAGE_GATES.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `.env` `ops/github-runner/.env.example` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `` `ops/github-runner/.gitignore` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `readme` `ops/github-runner/README.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `docker-compose` `ops/github-runner/docker-compose.yml` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `readme` `ops/homelab/watchtower/README.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `allowed-services` `ops/homelab/watchtower/allowed-services.txt` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `operational_playbook` `docker-compose` `ops/homelab/watchtower/docker-compose.yml` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `odin-prompt` `scripts/odin/odin-prompt.md` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `compliance-prompt-part61-141-test` `scripts/odin/tests/compliance-prompt-part61-141-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `odin-prompt-direct-line-routing-test` `scripts/odin/tests/odin-prompt-direct-line-routing-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `perf-prompt-repo-awareness-test` `scripts/odin/tests/perf-prompt-repo-awareness-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `po-repo-awareness-prompt-test` `scripts/odin/tests/po-repo-awareness-prompt-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `qa-lead-web-review-prompt-test` `scripts/odin/tests/qa-lead-web-review-prompt-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `sass-prompt-policy-test` `scripts/odin/tests/sass-prompt-policy-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `prompt` `sentry-fix-prompt-contract-test` `scripts/odin/tests/sentry-fix-prompt-contract-test.sh` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `workflow` `odin-self-learning-self-healing` `specs/ultrathink/odin-self-learning-self-healing.yaml` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract
- `workflow` `skill-observability-lifecycle` `specs/ultrathink/skill-observability-lifecycle.yaml` -> `rewrite`
  rationale: legacy asset needs normalization into the new contract

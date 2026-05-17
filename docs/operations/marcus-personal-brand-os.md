# Marcus Personal Brand OS Operations

Use this runbook to operate the Odin-side Marcus Personal Brand Operating System.

## Current State

Supported runtime path: `odin trigger seed marcus-brand-os`, `odin scheduler tick`, `odin jobs`, `odin skills run <task>`, the Marcus registry workflow, and existing approval-gated social memory commands.

Brand authority path: `marcusgoll` owns the public voice, site, resources, and brand rules. `odin-os` owns the recurring runtime and approval-safe work materialization.

The morning editorial strategist lane is command-backed and produces a reviewable strategy artifact. The writing, resource, newsletter, and growth lanes still need the same treatment before they can produce useful downstream work.

## Install Routine Triggers

From `/home/orchestrator/odin-os` after building or installing the current `odin` binary:

```bash
./bin/odin trigger seed marcus-brand-os start=2026-05-18 --json
```

Default behavior:

- project/initiative: `marcusgoll`
- workspace: `default`
- local schedule timezone: `America/New_York`
- quiet-hours timezone: `UTC` because the current trigger evaluator only supports UTC quiet windows
- status: `enabled`
- execution intent: `read_only`

The seed creates recurring skill-invocation triggers for:

- morning editorial scan
- engagement opportunity checks during the day
- midday writing pass
- resource production pass
- weekly newsletter editorial pass
- evening growth review

## Prove Before Running Live

Use dry-run and scheduler proof before relying on the loop:

```bash
./bin/odin trigger test marcus-brand-morning-editorial-scan now=2026-05-18T12:30:00Z --json
./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --dry-run --json
```

When ready to materialize due work:

```bash
./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --json
./bin/odin jobs --json
```

Scheduled brand work appears as `work_kind=skill_invocation`. Run an accepted item through:

```bash
./bin/odin skills run <task-key> --json
```

## Approval Boundaries

The brand OS may draft, plan, review, package, and analyze. It must not publish, send email, reply, like, repost, follow, DM, schedule public content, or bypass Marcus approval.

Public social work stays on the existing Social Copilot approval path documented in [marcus-social-copilot-loop.md](marcus-social-copilot-loop.md).

## Operating Check

Daily operator check:

```bash
./bin/odin trigger list --json
./bin/odin scheduler tick --dry-run --json
./bin/odin jobs --json
./bin/odin review list --json
```

Stop if duplicate Marcus brand trigger keys exist, if public actions appear without approval, or if `jobs` shows overlapping social polling jobs for `marcus-social-growth-workflow`.

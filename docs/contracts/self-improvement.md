---
title: Self-Improvement Contract
status: active
date: 2026-04-09
phase: "14"
---

# Self-Improvement Contract

Phase 14 defines Odin's proposal-driven self-improvement path.

Self-improvement is distinct from self-heal:

- self-heal addresses immediate bounded operational faults
- self-improvement addresses recurring weak prompts, weak routing, weak retries, weak playbooks, or weak policy suggestions

## Rules

- No live self-editing of canonical policy without promotion.
- No hidden improvement path outside the recorded proposal lifecycle.
- Evaluation must be reproducible from replay or sandbox fixture inputs.
- Promotion in Phase 14 activates runtime records only.
- Promotion in Phase 14 does not rewrite canonical `config/*.yaml` files or Markdown assets.
- Rollback must be explicit and auditable.

## Proposal types

- `prompt_refinement`
- `routing_rule_refinement`
- `retry_policy_refinement`
- `playbook_proposal`
- `policy_suggestion`

## Lifecycle statuses

- `draft`
- `submitted`
- `evaluating`
- `approved`
- `promoted`
- `rejected`
- `rolled_back`

## Evaluation modes

- `replay`
- `sandbox`

Evaluation uses recorded fixture inputs and deterministic scoring. Identical fixtures must produce identical results.

## Promotion model

- only one active promoted runtime record may exist per proposal type, scope, and target key
- promoting a newer proposal supersedes the current active promotion for that target
- rollback deactivates the current promotion and restores the superseded promotion when one exists

## Audit expectations

Phase 14 adds explicit audit detail for:

- proposal creation
- proposal submission
- evaluation recording
- proposal rejection
- promotion application
- promotion rollback

These actions must appear in runtime events and SQLite-backed learning records.

# Phase 14 Self-Improvement Design

## Goal

Implement a proposal-driven self-improvement pipeline that allows Odin OS to evaluate, promote, reject, and roll back bounded runtime improvements without uncontrolled self-modification.

## Current Context

Odin OS already distinguishes operational self-heal from broader system change:

- self-heal is deterministic and bounded
- runtime authority lives in SQLite
- important runtime changes append audit events
- replay and evaluation package roots already exist, but they are still placeholders

What the system does not yet have is a controlled way to capture recurring weaknesses, evaluate candidate improvements reproducibly, and activate only promoted runtime changes.

Phase 14 should add that path without rewriting canonical authored files or introducing hidden live-edit behavior.

## Approaches Considered

### 1. SQLite-backed proposals with replay-driven evaluation and runtime-only promotion

This keeps improvement state in the runtime authority, makes promotion reversible, and avoids uncontrolled editing of canonical files. This is the recommended approach.

### 2. File-editing improvement proposals that rewrite config and prompts directly

This is attractive later, but it is too risky for the first self-improvement phase because promotion would immediately mutate authored truth and complicate rollback.

### 3. Generic plugin-based learning engine

This would maximize flexibility, but the learning substrate is not mature enough yet to justify a plugin architecture.

## Recommendation

Store proposals, evaluations, promotions, and rollbacks in SQLite. Evaluate proposals deterministically through replay or sandbox fixture inputs. Promotion should activate runtime records only; it must not edit canonical authored policy, prompts, or routing files in this phase.

Rollback should deactivate the active promotion and restore the previous promoted version when one exists.

## Proposal Types

Phase 14 should support these proposal types:

- `prompt_refinement`
- `routing_rule_refinement`
- `retry_policy_refinement`
- `playbook_proposal`
- `policy_suggestion`

These types are intentionally scoped to the improvement areas already implied by runtime failures and routing quality.

## Proposal Lifecycle

Recommended statuses:

- `draft`
- `submitted`
- `evaluating`
- `approved`
- `promoted`
- `rejected`
- `rolled_back`

Rules:

- proposals begin in `draft`
- only submitted proposals can move into evaluation
- promotion can happen only after a successful evaluation and explicit approval
- rollback changes active runtime state but does not delete proposal history
- `policy_suggestion` remains advisory in Phase 14 even if promoted

## Proposal Record Shape

Each proposal should capture:

- proposal type
- scope
- target key
- summary
- hypothesis
- change payload JSON
- created by
- current status
- created at
- updated at

The change payload remains type-specific and opaque to storage. Evaluation and promotion logic interpret it through typed services, not through uncontrolled dynamic execution.

## Evaluation Model

Phase 14 should support two evaluation modes:

- `replay`
- `sandbox`

The common requirement is reproducibility. Evaluation should use a fixture record that captures:

- fixture key
- mode
- baseline score inputs
- candidate score inputs
- metric weights

The evaluator should score proposals deterministically from those recorded inputs. This keeps Phase 14 honest: it provides real evaluation scaffolding without pretending the system can autonomously derive high-quality improvement metrics from live traffic yet.

## Scoring Model

The evaluator should compute a weighted score from recorded metrics such as:

- success rate
- operator intervention count
- latency
- cost
- policy violations

The exact scoring formula can stay simple as long as it is:

- deterministic
- inspectable
- stable across reruns with the same fixture

Evaluation records should store:

- proposal id
- fixture key
- mode
- score
- baseline summary JSON
- candidate summary JSON
- result summary
- recorded at

## Promotion Model

Promotion should create runtime activation records keyed by proposal type and target.

Rules:

- only one active promoted record may exist for a given proposal type and target
- promoting a new proposal supersedes the currently active promotion for that target
- superseded promotions remain in history and may be restored by rollback
- promotion does not rewrite canonical config or Markdown assets in Phase 14

This makes runtime improvement visible and reversible without introducing live self-editing of authored truth.

## Rollback Model

Rollback should be explicit and auditable.

Recommended behavior:

- mark the active promotion as rolled back
- record rollback reason and actor
- if the promotion superseded an earlier active promotion, restore that earlier promotion to active state

This gives the system a clean reversible path instead of forcing operators to reconstruct prior state manually.

## Storage Model

Phase 14 should add runtime storage for:

- `learning_proposals`
- `learning_evaluations`
- `learning_promotions`

Suggested promotion record fields:

- proposal id
- proposal type
- target key
- status as `active`, `superseded`, or `rolled_back`
- supersedes promotion id
- promoted at
- promoted by
- rolled back at
- rollback reason

The key point is that active runtime improvement state lives in SQLite, not in untracked memory or implicit package globals.

## Event Model

Phase 14 should extend the audit stream with explicit learning events:

- `learning.proposal_created`
- `learning.proposal_submitted`
- `learning.evaluation_recorded`
- `learning.proposal_rejected`
- `learning.promotion_applied`
- `learning.promotion_rolled_back`

These events must be written in the same transaction as their corresponding row changes.

## Package Shape

Recommended package layout:

- `internal/learning/proposals`
- `internal/learning/evaluator`
- `internal/learning/replay`
- `internal/learning/promotion`

The store layer remains under `internal/store/sqlite`.

The learning packages should provide typed orchestration over SQLite-backed records rather than bypassing the store with ad hoc SQL from multiple call sites.

## Operator Visibility

Phase 14 should expose at least a read-only projection or listing surface for:

- proposals and their statuses
- latest evaluation results
- currently active promotions

This satisfies the requirement that promotion decisions are visible and reversible.

## Testing Strategy

Tests should cover the full proposal lifecycle:

1. create proposal
2. submit proposal
3. evaluate proposal with reproducible replay or sandbox input
4. approve or reject based on score
5. promote proposal into active runtime state
6. supersede an active promotion with a new promotion
7. roll back and restore the previous active promotion
8. confirm all lifecycle transitions append events
9. confirm no canonical file mutation happens as part of promotion

The tests should stay focused on deterministic lifecycle behavior, not speculative autonomous improvement quality.

## Non-Goals

Phase 14 does not include:

- automatic editing of `config/*.yaml`
- automatic rewriting of registry Markdown or prompt files
- hidden improvement paths outside the recorded proposal lifecycle
- live policy mutation without promotion
- autonomous generation of high-quality scoring fixtures from production traffic

## Success Criteria

Phase 14 succeeds when Odin can capture candidate improvements as proposals, evaluate them reproducibly, promote only approved improvements into runtime-active state, roll them back cleanly, and show the entire lifecycle through auditable records and events.

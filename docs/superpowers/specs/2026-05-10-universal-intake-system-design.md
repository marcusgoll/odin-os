---
title: Universal Intake System Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os universal intake v1
---

# Universal Intake System Design

## Purpose

Universal Intake v1 hardens Odin's canonical raw-to-reviewed-draft lane. It captures raw signals as durable Intake Items, preserves evidence, classifies and deduplicates them, routes them into a Reviewable Intake Proposal, and stops before executable Work Item creation.

The approved v1 scope is intentionally narrower than the full product vision. Notes, email, voice, logs, GitHub, calendar, reminders, manual CLI commands, and future integrations should all enter the same normalized intake contract, but this design proves the core authority before adding broad adapter work.

## Current State Found

The live operator surface already exposes:

- `odin intake raw create/list/show`
- `odin intake process`
- `odin intake review list/show/accept/reject/clarify/archive`
- `odin intake approval list/show/approve/deny`
- `/overview` and `odin work status` intake readback

The current repository already includes:

- `intake_items` SQLite authority with `source_facts_json`, `dedupe_key`, `dedupe_recipe_version`, `canonical_intake_item_id`, `suppression_reason`, `routing_notes`, and `goal_id`
- deterministic classification, typed routing outcomes, semantic duplicate linking, review-required states, clarification states, duplicate-linked states, and approval-required states
- runtime events for intake creation, processing, routing, review, and approval decisions
- registry agents for triage, review, and universal ticket generation
- `/overview` Intake Inbox counts for raw, processed, review queue, duplicate, approval, accepted, rejected, and archived states

## Non-Goals

- Add email, voice, GitHub, calendar, reminders, notes, or log adapters in this slice
- Dispatch workers, start Run Attempts, mutate external systems, or create executable Work Items during raw intake processing
- Replace the existing review and approval command families before implementation planning decides the smallest compatibility path
- Create a second inbox, duplicate registry, or adapter-specific intake authority

## Architecture

The v1 pipeline is:

`Source Adapter -> Intake Item -> Process Intake -> Reviewable Intake Proposal -> Intake Review Queue -> Operator Decision`

`process` prepares reviewable truth. It may classify, dedupe, route, prepare a draft artifact, mark an item as needing clarification, mark a duplicate, or identify an archive candidate. It must not dispatch work, launch execution, mutate external systems, or create executable Work Items by default.

Promotion remains a separate operator-controlled boundary. Existing broader capabilities may remain available as compatibility behavior, but the Universal Intake v1 success path ends at a reviewable draft unless an explicit promotion command is invoked.

## Source Envelope

All adapters and CLI entrypoints should normalize source input into one envelope before Odin core creates or processes an Intake Item.

Required or canonical fields:

- `source_family`: source class such as `cli`, `email`, `github`, `calendar`, `voice`, `note`, `log`, or `reminder`
- `external_object_id`: source-owned identity when one exists
- `event_kind`: normalized intake kind such as `request`, `bug`, `research`, `writing`, `admin`, `routine`, or `destructive`
- `observed_at`: when the source event was observed
- `subject`: concise human-readable subject
- `body` or `summary`: source content or normalized summary
- `actor`: person, system, or adapter responsible for the source event
- `source_uri`: source link or local evidence reference when available
- `attachments` or `evidence_refs`: optional evidence pointers
- `adapter_facts`: namespaced adapter-specific facts that do not own core intake identity

Adapters normalize facts. Odin core owns final dedupe identity and lifecycle decisions.

## Dedupe Ownership

Odin core derives or verifies the final `dedupe_key` from Workspace, source family, and normalized signal fingerprint. Adapters may supply normalized source facts, but they must not own final identity.

Rules:

- always store a durable Intake Item for each valid arrival, including duplicates
- link duplicates to a canonical Intake Item instead of dropping them before ingress
- keep duplicate grouping as a derived operator projection, not a separate runtime aggregate
- preserve the `dedupe_recipe_version` used for the decision
- keep dedupe Workspace-local, with optional narrowing after Initiative or Scope resolution
- support cooldown-bounded canonical linking in the product model so old canonical items do not absorb equivalent arrivals forever

## Reviewable Intake Proposal

The approved artifact model is one shared Reviewable Intake Proposal envelope with typed draft artifacts inside it.

Common proposal fields:

- `source_intake_key`
- `title`
- `category`
- `route`
- `summary`
- `draft_artifact.kind`
- `acceptance_criteria` or `clarification_prompts`
- `risk_level`
- `approval_posture`
- `missing_constraints`
- `dedupe_result`
- `recommended_next_action`
- `operator_next_action`

Typed artifact kinds preserve routing semantics. Examples include `draft_task`, `draft_research`, `draft_document`, `draft_incident_review`, `draft_routine`, `draft_follow_up`, `draft_policy_change`, `draft_destructive_action`, and `archive_candidate`.

The proposal envelope is the operator-facing review shape. The typed artifact is the routing-specific payload inside it.

## Lifecycle Model

Universal Intake v1 uses a small product lifecycle and allows compatibility aliases where current code already uses broader status names.

Canonical product states:

- `received`: raw evidence is stored
- `processing`: transient operation while classification, dedupe, and routing run
- `review_required`: a Reviewable Intake Proposal is ready for operator review
- `needs_clarification`: a safe proposal cannot be drafted from the available evidence
- `duplicate_linked`: this arrival is linked to a canonical Intake Item
- `archived`: operator or policy says no work is needed
- `accepted_for_promotion`: operator accepted the proposal, but executable Work Item creation belongs to the next boundary
- `errored`: processing failed visibly and the raw item remains inspectable

Compatibility aliases may map current implementation states such as `duplicate_linked_or_suppressed`, `accepted`, and `approval_required` to the product lifecycle plus proposal fields.

Branching detail should live in proposal fields and outcome references, not in an ever-growing status enum.

## Operator Surface

The v1 operator path is:

1. `odin intake raw create/list/show`: raw evidence authority
2. `odin intake process --id`: deterministic classification, dedupe, routing, and proposal creation
3. `odin intake review list/show`: operator review of Reviewable Intake Proposals
4. `odin intake review accept/reject/clarify/archive`: operator decision boundary
5. `/overview` and `odin work status`: readback only

Output wording should prefer `proposal_created`, `review_required`, or equivalent language until an explicit promotion boundary is crossed. It should not imply `task_created` during raw creation, processing, or review inspection.

Every intake proof must include negative proof that no Work Item, Run Attempt, dispatch, or external mutation happened during raw creation, processing, and review inspection.

## Error Handling

Failure behavior:

- invalid source envelope: reject before creating an Intake Item
- valid but vague input: create the Intake Item, then route to `needs_clarification`
- duplicate signal: store the new arrival, then link it to the canonical item
- risky proposal: keep as reviewable with explicit approval posture, no promotion
- adapter failure before evidence is received: fail without inventing an Intake Item
- adapter failure after raw evidence is received: preserve the raw evidence and record adapter error context
- processing failure: leave the raw item visible with `errored` or a compatibility equivalent and an event trail

The system should fail visible, preserve evidence, and never convert ambiguity into executable work.

## Testing And Proof

Implementation planning should require:

- parser and unit tests for source envelope validation and compatibility aliases
- store tests for durable raw items, source facts, dedupe recipe, canonical duplicate links, and routing notes
- lifecycle tests for `received -> review_required`, `received -> needs_clarification`, `received -> duplicate_linked`, and processing failure visibility
- fixture-backed universal source envelope tests without adding new source adapters yet
- CLI E2E proof with real `odin`: raw create, raw show, process, review list/show, overview, and work status
- negative proof that raw/process/review-show creates no Work Item, Run Attempt, dispatch, or external mutation

## Implementation Planning Notes

The implementation plan should decide whether the current `review accept` behavior remains the explicit promotion boundary or whether a separate non-promoting `mark reviewed` style action is needed. The design does not require that decision before planning, but it does require the default Universal Intake v1 lane to stop at reviewable proposal creation and inspection.

The implementation should reuse existing intake storage, runtime events, review commands, overview projection, and registry agents. New components are justified only where they centralize the source envelope, dedupe derivation, proposal envelope, compatibility mapping, or proof fixtures.

## Approved Decisions

- Scope is raw intake to reviewed draft artifact, not full Work Item creation.
- Use one Reviewable Intake Proposal envelope with typed draft artifacts inside it.
- `process` stops at proposal creation; promotion is a separate operator boundary.
- Adapters normalize source facts; Odin core owns dedupe identity.
- Keep intake lifecycle small and place branching detail in proposal fields and outcome references.
- The operator surface must include negative proof that no execution happened.
- Contract tests come before connector breadth.

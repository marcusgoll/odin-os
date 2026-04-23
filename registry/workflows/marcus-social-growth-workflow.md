---
kind: workflow
key: marcus-social-growth-workflow
title: Marcus Social Growth Workflow
summary: Coordinates compliant throughout-the-day monitoring, drafting, review, approval, publishing, and retrospective work for Marcus's aviation authority growth on X and LinkedIn.
status: active
tags:
  - social
  - aviation
  - workflow
owners:
  - odin-core
entrypoint: skill:marcus-social-content-strategist
composes:
  - marcus-social-content-strategist
  - marcus-x-drafting-assistant
  - marcus-linkedin-drafting-assistant
  - marcus-engagement-research-assistant
  - marcus-social-analytics-advisor
  - marcus-social-content-strategist-companion
  - marcus-x-drafting-assistant-companion
  - marcus-linkedin-drafting-assistant-companion
  - marcus-engagement-research-assistant-companion
  - marcus-social-analytics-advisor-companion
---

# Marcus Social Growth Workflow

## Purpose

Define the shared workflow Odin should follow to help Marcus grow authority on X and LinkedIn through compliant, approval-gated, text-first social operations, including a throughout-the-day operating loop for monitoring and queue-building.

## When to Use

Use this workflow when Marcus wants to plan content, draft posts, prepare reply suggestions, monitor explicit social work throughout the day, review outcomes, or update next-week strategy.

## Inputs

The workflow takes topic ideas, platform goals, approved voice preferences, content history, explicit operator-seeded watch inputs, bounded polling or manual wake triggers, performance notes, and any approval-sensitive context, then may turn observed opportunities into refreshed evidence, updated research, approval-ready drafts, and normal jobs/runs state without creating a separate social queue object or overlapping peer polling jobs. In v1, those operator-seeded watch inputs should live on the single workflow-owned polling job as one authoritative structured scope set in configuration or runtime metadata rather than as loosely managed independent watch entries or standalone social memory records. That set should use a fixed structured shape with separate explicit sections for Marcus-owned surfaces, explicit target URLs, and operator-maintained watchlist entries rather than remaining an untyped heterogeneous collection, and that section taxonomy should stay closed in v1 unless a later explicit domain decision adds another section kind. Within that v1 shape, the Marcus-owned-surfaces section should itself stay closed to Marcus's own timeline and mentions only unless a later explicit domain decision adds another Marcus-owned surface. That Marcus-owned-surfaces section should use fixed literal canonical stable target keys for Marcus's own timeline and Marcus's own mentions rather than keys derived from the current account handle, with any URLs for those built-in watched surfaces retained only as reference forms. The explicit-target-URLs section should itself stay closed to explicit post URLs only; treat specific threads as operator-maintained watchlist entries, and treat thread-root URLs or explicit reply-target URLs as valid stable target identities rather than as direct members of that watch-scope section unless a later explicit domain decision broadens it. The operator-maintained-watchlist-entries section should itself stay closed to specific accounts and specific threads only unless a later explicit domain decision adds another watchlist entry kind. Each stable target key should appear at most once across the whole authoritative watch-scope set; if operators need multiple reasons, labels, or notes for the same watched target, that context should live on one canonical entry for that key rather than duplicating the target across sections or entries. Raw operator-entered target strings should be normalized into canonical stable target keys before uniqueness checks, checkpoint ownership, cooldown lookup, or persistence. Accepted X status URLs should normalize to one canonical URL form, `https://x.com/<screen_name>/status/<status_id>`, with host aliases collapsed to `x.com`, query strings removed, fragments removed, and trailing slash differences ignored. For watched X posts, the true stable target identity should be the numeric `status_id`; the canonical X status URL should remain the display or reference form derived from that identity rather than the deeper authority. For specific X threads in the operator-maintained watchlist, the true stable target identity should also be the root post's numeric `status_id`; the root canonical X status URL should remain the display or reference form for that watched thread. For specific watched X accounts in the operator-maintained watchlist, the true stable target identity should be the canonical lowercase handle without `@`; the canonical X profile URL should remain the display or reference form derived from that identity rather than the deeper authority. Canonical X profile URLs for watched accounts should normalize to `https://x.com/<lowercase_handle>`, with host aliases collapsed to `x.com`, query strings removed, fragments removed, and trailing slash differences ignored. Updates to that set should apply as whole-set replacements rather than piecemeal patches to independent watch entries or subfields. When a whole-set watch-scope replacement changes the target list, the polling job should preserve checkpoint and cooldown state only for stable target keys that remain in the new set, drop that state for removed targets, and initialize clean state for newly added targets. Watched-target identity must come from explicit stable target keys such as post URLs, thread root URLs, explicit mention or reply target URLs, or operator-seeded watchlist entry keys, while any per-target checkpoint state stays minimal and runtime-local on jobs/runs metadata with only compact resume and backoff fields plus optional hint-level row pointers that must be revalidated on wake, and post-resolution duplicate suppression stays on cooldown markers keyed by target identity plus observation fingerprint.

## Procedure

Plan the week, run one workflow-owned bounded polling loop per environment over explicit operator-seeded work and engagement opportunities, allow ordinary manual wakes against that same loop when needed while still honoring existing target-level cooldowns, collect observations, refresh evidence, update reusable research, revise the existing pending research or draft when the same watched target resurfaces with a materially same recommendation, classify ideas by output type, create a fresh draft or research item only when the earlier item is resolved or materially different, route drafting to the correct skill, review for tone and accuracy, require Marcus approval before account actions through the existing `social_draft` plus `/memory resolve` lane, record outcomes, and feed durable learnings back into the next cycle. In v1, no separate force-recheck or cooldown-bypass override exists.

## Outputs

The output is a weekly plan, refreshed evidence, updated research, draft set, approval-ready notes, reply suggestions, throughout-the-day approval queues, and a retrospective summary for the next planning loop.

## Constraints

Do not automate publishing or engagement through deceptive means, do not bypass approval gates, do not let social growth goals override factuality or platform compliance, do not let the throughout-the-day loop take unapproved account actions, do not let it expand beyond explicit operator-seeded watch scopes, do not let v1 depend on reactive event-driven wakes, do not let the pre-approval loop do more than observe, refresh evidence, update research, and queue approval-ready recommendations, do not introduce a separate first-class social queue object when existing social memory plus normal jobs/runs metadata is enough, do not allow multiple overlapping Marcus social polling jobs in the same environment, do not route the same social candidate through both `/memory resolve` and `/approvals`, do not create duplicate pending items for the same watched target when the recommendation is materially the same, do not use fuzzy topic or text similarity as the identity key for watched-target dedupe in v1, do not promote polling checkpoints into a new social memory type when minimal jobs/runs metadata is enough, do not store larger rendered observations, snapshots, or draft text in polling checkpoints, do not treat cached row pointers in polling checkpoints as authoritative without revalidating them against social memory, do not immediately recreate a resolved social candidate when the same unchanged observation is still inside its target-level cooldown window, do not let an ordinary manual wake bypass that cooldown implicitly, and do not add a separate cooldown-bypass or force-recheck override in v1.

## Success Criteria

Marcus can use Odin as a reliable social copilot that improves consistency and quality without crossing compliance boundaries.

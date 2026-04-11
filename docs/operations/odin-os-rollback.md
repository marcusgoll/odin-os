---
title: Odin OS Rollback Playbook
status: active
date: 2026-04-11
---

# Odin OS Rollback Playbook

Rollback is the safety valve for a cutover pilot. The goal is to pause or roll back quickly enough that the operator can still trust the rollout evidence.

## Rollback triggers

Cutover must pause or roll back when any of the following occur:

- successful completions stop being recent
- stalled or dead-letter work grows without bounded recovery
- approval flows stop protecting high-risk mutations
- operator surfaces cannot explain current control ownership
- a pilot project requires the legacy stack for routine completion
- pilot requires the legacy stack for routine completion

These triggers are not advisory. If one is present, stop advancing the rollout.

## Rollback sequence

1. Stop admitting new cutover work for the affected project.
2. Move the project back to shadow or limited-action, whichever is safer for the current evidence.
3. Preserve the runtime reports, run history, and transition events.
4. Restore legacy primary handling for routine completion if the pilot still depends on it.
5. Re-run shadow or compare evidence before any new cutover attempt.

## What rollback is not

- It is not a deletion of the pilot record.
- It is not a reason to lose the compare or approval evidence.
- It is not a shortcut to skip the graduation criteria the next time.

Rollback exists so the rollout can fail safely without hiding why it failed.

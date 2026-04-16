---
title: Odin OS Cutover Playbook
status: active
date: 2026-04-11
---

# Odin OS Cutover Playbook

This playbook documents how a managed project graduates from legacy-controlled observation into Odin OS primary ownership. It is authored evidence, not a runtime switch.

## Pilot project selection rules

Select pilot projects with the smallest surface area that still exercises the full queue and mutation path.

- The project must already be registered in `config/projects.yaml`.
- The project must have a clear runtime owner expectation of `odin_os` once cutover begins.
- The project must not require the legacy primary for routine completion.
- The project should already have shadow and compare evidence from the existing transition ladder.
- Pick one pilot cohort at a time and keep it narrow until the graduation criteria are met.
- Prefer the first project that can prove the whole path, not the first project that looks easy.

For the current repo, the first cutover pilot is `pbs`, with `odin-orchestrator` as comparison context.

## Shadow graduation criteria

Shadow is the read-only proving ground. Graduation from shadow requires:

- legacy and Odin readouts agree on project scope and ownership
- no mutation attempt requires an allowed action
- operator review confirms the project can stay read-only

If these are not true, the project stays in shadow.

## Limited-action graduation criteria

Limited-action is the narrow mutation proving ground. Graduation from limited-action requires:

- allowlisted isolated mutations complete successfully under Odin ownership
- limited-action work never depends on legacy primary completion
- operator review shows no unbounded approval or recovery drift

If these are not true, the project stays in limited-action.

## Cutover graduation criteria

Cutover is the point where Odin OS becomes the normal controller for the project. Graduation from cutover requires:

- routine queued work completes under Odin OS ownership
- normal project mutations no longer need legacy-primary intervention
- rollback remains available and rehearsed

At this point the project can be treated as runnable without a legacy primary in the normal path.

## Legacy duties to retire in order

Retire the legacy controller in this order:

1. read-only observation and compare reporting
2. limited-action handling for allowlisted low-risk mutations
3. routine queue intake and run selection
4. normal project mutation and merge authority
5. legacy-primary fallback for routine completion

Do not retire the next duty until the current one has proven stable for the pilot cohort.

## Evidence to keep beside the config

- `config/projects.yaml` lists the pilot cohort and runtime ownership expectations.
- `docs/operations/odin-os-rollback.md` records the rollback triggers and recovery sequence.
- `docs/operations/cutover-readiness.md` stays as the broader operational readiness checklist.

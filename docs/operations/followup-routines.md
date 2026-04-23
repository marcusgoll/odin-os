---
title: Follow-Up Routines
status: active
updated: 2026-04-17
---

# Follow-Up Routines

Odin OS can now manage recurring life-admin follow-through as governed workspace state.

This flow is meant for Marcus's routine operating model:

- create a non-project routine initiative
- create a named companion or advisor
- persist workspace profile defaults
- create a recurring follow-up obligation
- inspect due work through `agenda`
- let `odin serve` materialize governed work items when obligations become due

## Core commands

Create a routine initiative:

```bash
odin initiative create --kind routine --key life-admin --title "Life Admin"
```

Create a companion:

```bash
odin companion create --kind advisor --key finance --title "Finance Advisor"
```

Set workspace profile defaults:

```bash
odin profile set --quiet-hours 22:00-07:00
```

Create a recurring follow-up:

```bash
odin followup add --initiative life-admin --title "Review mail" --cadence daily
```

Inspect obligations:

```bash
odin followup list --json
odin agenda
```

Complete or snooze obligations:

```bash
odin followup complete <obligation-id>
odin followup snooze <obligation-id> --until 2026-04-20T09:00:00Z
```

## Runtime behavior

`odin serve` does not perform follow-up side effects directly.

Instead it:

- evaluates due obligations
- materializes governed work items
- leaves those work items visible through `jobs`
- only produces `runs` once normal execution policy allows work to start

This keeps routines bounded and auditable.

## What to expect

For a due recurring obligation:

- `odin agenda` shows the obligation in `due_work`
- `odin serve` materializes a work item for the current occurrence
- `odin jobs --json` shows that work item
- `odin runs --json` may still be empty if the work item is blocked or waiting

## Durability

The following state is durable across command invocations:

- routine initiatives
- companion definitions
- workspace operating-profile updates
- follow-up obligations
- follow-up completion memory

Profile updates are recorded as workspace-owned memory summaries.
Follow-up completion history is recorded as initiative- or companion-scoped memory, depending on ownership.

# Managed Project Delivery Workflow Design

**Date:** 2026-04-24

**Status:** proposed

**Domain Source of Truth:** `CONTEXT.md`, `docs/contracts/workspace-context-map.md`, `docs/contracts/registry-format.md`, `docs/contracts/project-manifest.md`, `docs/contracts/verification-model.md`

## Goal

Reuse useful Spec-Flow delivery ideas inside Odin without embedding Spec-Flow as a second runtime, planner, or operator surface.

## Current State

Odin already owns managed-project enrollment, workflow selection, execution routing, runtime evidence, approvals, and verification through the `odin` operator surface. Authored workflows, skills, agents, and commands live in Odin's capability catalog.

Spec-Flow is useful source material because it has a mature software-delivery shape and recognizable repo-local artifacts such as `.spec-flow/`, `.claude/commands/`, `CLAUDE.md`, `specs/*`, and `epics/*`.

## Design Choice

Use an Odin-native managed-project delivery workflow plus an optional Spec-Flow compatibility profile.

The workflow belongs to Odin's registry. Current `main` does not expose a real `/workflow` operator command, so v1 proves the workflow as authored catalog content and leaves first-class workflow selection to the capability-catalog operator-surface roadmap. Planning remains Odin-native and should use Odin plan docs plus planning skills such as `writing-plans`.

The compatibility profile is a project profile or capability hint layered onto existing `local_git_project` or `github_backed_project` enrollment. It is not a project class.

## Components

### Managed Project Delivery Workflow

Add `registry/workflows/managed-project-delivery-workflow.md`.

Recommended phase spine:

- `intake`
- `spec`
- `plan`
- `tasks`
- `implement`
- `validate`
- `ship`
- `close`

The phase spine guides prompt assembly and operator expectations. It is not a new runtime state machine in v1.

### Spec-Flow Compatibility Profile

Detect compatibility conservatively from multiple strong project-local signals:

- `.spec-flow/`
- `.claude/commands/`
- `CLAUDE.md`
- `specs/`
- `epics/`

One signal alone must not mark a project as Spec-Flow-compatible. The detector returns evidence so operators can inspect the decision.

### Artifact Compatibility Adapter

Read a narrow set of compatible artifacts as project evidence:

- `specs/<feature>/spec.md`
- `specs/<feature>/plan.md`
- `specs/<feature>/tasks.md`
- `epics/<slug>/state.yaml`

These files are project-local compatibility artifacts. Odin runtime truth remains in work items, run attempts, approvals, and observability.

## Current Operator Constraint

Current `main` has registry-backed workflow assets, but no real `/workflow` command in the REPL or top-level CLI. This design must not reintroduce a stale command surface just to select one workflow. A later operator-surface slice should decide whether workflow selection belongs under a restored `/workflow`, a generic capability command, or another existing Odin surface.

## Non-Goals

- running `npx spec-flow`
- invoking `spec-cli.py`
- importing slash-command files as live Odin commands
- bidirectional state sync
- a new Spec-Flow project class
- replacing `writing-plans` with Spec-Flow planning
- restoring `/workflow` as part of this slice

## Best Operating Rule

Use Spec-Flow as source material and compatibility input. Keep planning, execution, evidence, and authority in Odin.

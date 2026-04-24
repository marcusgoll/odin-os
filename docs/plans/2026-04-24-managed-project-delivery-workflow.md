# Managed Project Delivery Workflow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an Odin-native managed-project delivery workflow and a conservative Spec-Flow compatibility profile without embedding Spec-Flow as a runtime, planner, or operator surface.

**Domain Source of Truth:** `CONTEXT.md`, `docs/contracts/workspace-context-map.md`, `docs/contracts/registry-format.md`, `docs/contracts/project-manifest.md`, `docs/contracts/verification-model.md`, `docs/plans/2026-04-24-managed-project-delivery-workflow-design.md`

**Context:** Managed Project / Capability Catalog

**Owns / Does Not Own:** This slice owns an authored managed-project delivery workflow, conservative project-profile detection, project view evidence, and a narrow compatible-artifact reader. It does not own a new project class, Spec-Flow runtime execution, slash-command import, bidirectional sync, or replacement of Odin's `writing-plans` planning workflow.

**Invariants:**
- Odin remains the control plane and runtime authority.
- Spec-Flow compatibility is a project profile or capability hint, not a project class.
- Spec-Flow compatibility never runs `npx spec-flow` or `spec-cli.py`.
- Spec-Flow-style files are project-local evidence and compatibility artifacts, not Odin runtime authority.
- Planning remains Odin-native and uses Odin plan docs plus planning skills such as `writing-plans`.
- User-visible behavior is proven through real `odin` commands when applicable.

**Architecture:** Add a registry workflow asset for managed-project delivery and a small detector in `internal/core/projects` that returns conservative Spec-Flow profile evidence from an enrolled project's Git root. Surface that evidence through existing `odin project show/list` views and JSON output. Add a narrow artifact reader for known Spec-Flow-compatible files. Current `main` does not expose a real `/workflow` command, so this slice proves the workflow as registry-authored content and leaves operator workflow selection to a later capability-catalog surface.

**Tech Stack:** Go, Odin CLI, Markdown-frontmatter registry, YAML project manifests, SQLite runtime store, Go unit tests, real `./bin/odin` smoke checks

---

## Context Mapping

- `Context:` Managed Project / Capability Catalog
- `Owns:` managed-project delivery workflow asset, project profile detection, project view fields, compatibility evidence rendering
- `Depends on:` `internal/core/projects`, `internal/cli/commands/project.go`, registry loader/compiler, real `odin` binary
- `Does not own:` Work Item schema changes, Run Attempt lifecycle, approvals, Spec-Flow package publishing, Spec-Flow installer CLI, Spec-Flow shared execution engine
- `Boundary crossings:` managed project manifest -> profile detector -> project command view; registry workflow asset -> registry compiler; project files -> compatible artifact reader -> project evidence

## Implementation Tasks

### Task 1: Add Conservative Spec-Flow Profile Detection

Add `internal/core/projects/profile.go` and tests proving multiple strong signals are required and `.spec-flow/` is required.

### Task 2: Surface Project Profile Evidence In Project Views

Add profile fields to `projectView`, populate them from `coreprojects.DetectProjectProfile`, and render text and JSON evidence through existing `odin project show/list` paths.

### Task 3: Add Odin-Native Managed Project Delivery Workflow Asset

Add `registry/workflows/managed-project-delivery-workflow.md` using the existing registry workflow format and validate it through registry health and registry tests. Do not add a workflow-selection command in this slice.

### Task 4: Add Artifact Compatibility Adapter Skeleton

Add `internal/core/projects/specflow_artifacts.go` and tests for the known compatible files: `spec.md`, `plan.md`, `tasks.md`, and `state.yaml`.

### Task 5: Prove Real Odin Command Behavior

Build `./bin/odin`, enroll or inspect a controlled Spec-Flow-shaped project, verify `project show --json` surfaces profile evidence, and verify `odin status --json` still reports a healthy registry with the workflow asset present.

## Review Checklist

- Domain naming matches `CONTEXT.md`.
- `SpecFlowCompatible` is a profile/hint, not a project class.
- No code path invokes `npx spec-flow` or `spec-cli.py`.
- Compatible artifacts are exposed as evidence, not runtime authority.
- Workflow asset validates through the registry.
- Real `odin` command paths prove project profile visibility and registry health.
- Workflow operator selection remains a separate follow-up because current `main` has no real `/workflow` command.

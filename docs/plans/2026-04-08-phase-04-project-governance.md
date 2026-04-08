# Phase 04 Project Governance Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add authored project manifest contracts, validation, and deterministic CLI scope resolution for local Git projects, GitHub-backed projects, and the `odin-core` system project.

**Architecture:** Keep `config/projects.yaml` as the authored source of truth, add typed project loading and validation in `internal/core/projects`, and add explicit scope resolution logic in `internal/cli/scope`. Leave runtime import, branch execution, and cutover workflows for later phases.

**Tech Stack:** Go, YAML frontmatter-style config parsing with `gopkg.in/yaml.v3`, table-driven tests, existing Makefile verification

---

### Task 1: Define the authored manifest contract

**Files:**
- Create: `docs/contracts/project-manifest.md`
- Modify: `config/projects.yaml`
- Test: `internal/core/projects/manifest_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- a valid `local_git_project`
- a valid `github_backed_project`
- a valid `system_project` keyed as `odin-core`
- rejection of duplicate keys and missing required fields

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/projects`
Expected: FAIL because manifest types and loader behavior do not exist yet.

**Step 3: Write minimal implementation**

Create typed manifest structs and a YAML loader that can parse `config/projects.yaml` into normalized project manifests.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/projects`
Expected: PASS for parsing and baseline contract tests.

**Step 5: Commit**

```bash
git add docs/contracts/project-manifest.md config/projects.yaml internal/core/projects/manifest.go internal/core/projects/manifest_test.go
git commit -m "feat: add project manifest contract"
```

### Task 2: Add governance validation rules

**Files:**
- Modify: `internal/core/projects/manifest.go`
- Create: `internal/core/projects/validate.go`
- Create: `internal/core/projects/validate_test.go`

**Step 1: Write the failing test**

Add tests that reject:
- non-Git roots
- `github_backed_project` without `github.repo`
- `system_project` not keyed as `odin-core`
- system-project direct default-branch mutation
- incomplete destructive-operation and approval-gate policy blocks

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/projects`
Expected: FAIL with missing validator behavior.

**Step 3: Write minimal implementation**

Implement explicit validation helpers that return structured diagnostics without panicking.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/projects`
Expected: PASS for valid and invalid governance cases.

**Step 5: Commit**

```bash
git add internal/core/projects/manifest.go internal/core/projects/validate.go internal/core/projects/validate_test.go
git commit -m "feat: validate project governance policies"
```

### Task 3: Add deterministic CLI scope resolution

**Files:**
- Create: `docs/contracts/cli-scope.md`
- Create: `internal/cli/scope/scope.go`
- Create: `internal/cli/scope/scope_test.go`

**Step 1: Write the failing test**

Add tests that expect deterministic resolution of:
- `global`
- `odin-core`
- `project`
- `new-project`
- explicit target precedence over CWD hints

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/scope`
Expected: FAIL because scope types and resolver do not exist yet.

**Step 3: Write minimal implementation**

Implement scope kinds, resolution input, and resolution output with explicit precedence rules.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/scope`
Expected: PASS for all scope behaviors.

**Step 5: Commit**

```bash
git add docs/contracts/cli-scope.md internal/cli/scope/scope.go internal/cli/scope/scope_test.go
git commit -m "feat: add explicit cli scope resolution"
```

### Task 4: Integrate registration entrypoints and verify the phase

**Files:**
- Create: `internal/core/projects/register.go`
- Create: `internal/core/projects/register_test.go`
- Modify: `README.md`

**Step 1: Write the failing test**

Add tests for project registration that prove Odin can distinguish `odin-core` from normal projects and surface validated manifests for later consumers.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/core/projects`
Expected: FAIL because the registration entrypoint does not exist yet.

**Step 3: Write minimal implementation**

Add a registration service that loads manifests, validates them, and exposes lookup helpers for project class and system-project detection.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/core/projects`
Expected: PASS, then update `README.md` to reflect Phase 04.

**Step 5: Commit**

```bash
git add internal/core/projects/register.go internal/core/projects/register_test.go README.md docs/contracts/project-manifest.md docs/contracts/cli-scope.md config/projects.yaml
git commit -m "feat: add project governance and scope model for phase 04"
```

### Task 5: Full verification

**Files:**
- Modify: none
- Test: `internal/core/projects/manifest_test.go`
- Test: `internal/core/projects/validate_test.go`
- Test: `internal/core/projects/register_test.go`
- Test: `internal/cli/scope/scope_test.go`

**Step 1: Run focused package tests**

Run: `go test ./internal/core/projects ./internal/cli/scope`
Expected: PASS

**Step 2: Run repo verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: all commands exit `0`

**Step 3: Review git diff**

Run: `git status --short`
Expected: only intended Prompt 04 files are changed.

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add project governance and scope model for phase 04"
```

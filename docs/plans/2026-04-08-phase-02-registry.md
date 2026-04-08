# Phase 02 Markdown Registry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the Markdown-frontmatter registry system for agents, skills, workflows, and commands, including parsing, validation, compilation, example assets, and tests.

**Architecture:** The implementation uses a small staged pipeline: scanner, parser, validator, compiler, and loader. Authoring truth stays in `registry/*.md`, while compiled results are normalized in memory with diagnostics for invalid files so future hot reload can reuse the same snapshot build path.

**Tech Stack:** Go 1.22, `gopkg.in/yaml.v3`, GNU Make

---

### Task 1: Define the registry authoring contract

**Files:**
- Create: `docs/contracts/registry-format.md`

**Step 1: Write the contract document**

Document the shared and kind-specific frontmatter fields, required Markdown sections, path rules, and validation behavior.

**Step 2: Verify doc placement**

Run:

```bash
test -f docs/contracts/registry-format.md
```

Expected: exit 0

### Task 2: Add the failing parser and loader tests

**Files:**
- Create: `internal/registry/loader/load_test.go`
- Create: `internal/registry/parser/parse_test.go`
- Create: `internal/registry/validator/validate_test.go`

**Step 1: Write the failing tests**

Cover:

- valid file parses and loads
- missing frontmatter fails
- missing required section fails
- path kind mismatch fails
- duplicate key fails

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/registry/...
```

Expected: FAIL with undefined types and functions

### Task 3: Implement shared types, parser, validator, compiler, loader, and watcher contract

**Files:**
- Create: `internal/registry/types.go`
- Create: `internal/registry/parser/parse.go`
- Create: `internal/registry/validator/validate.go`
- Create: `internal/registry/compiler/compile.go`
- Create: `internal/registry/loader/load.go`
- Create: `internal/registry/watcher/watcher.go`
- Modify: `go.mod`

**Step 1: Add shared registry types**

Define kinds, diagnostics, normalized items, and snapshot containers.

**Step 2: Add parser**

Implement frontmatter splitting, YAML decoding, and heading-based required section extraction.

**Step 3: Add validator**

Implement common, kind-specific, section, and duplicate-key checks.

**Step 4: Add compiler and loader**

Compile validated documents into a normalized snapshot and scan `registry/` deterministically.

**Step 5: Add future-facing watcher contract**

Expose a no-op watcher interface and event model without implementing live watching.

### Task 4: Add example registry assets

**Files:**
- Create: `registry/agents/triage-agent.md`
- Create: `registry/skills/triage-skill.md`
- Create: `registry/workflows/project-intake.md`
- Create: `registry/commands/status.md`

**Step 1: Write valid examples**

Each example must satisfy the contract and cover the kind-specific frontmatter fields.

**Step 2: Verify examples load**

Run:

```bash
go test ./internal/registry/... -run TestLoaderLoadDir
```

Expected: PASS

### Task 5: Run full verification

**Files:**
- Modify: `Makefile` only if verification commands need adjustment

**Step 1: Format**

Run:

```bash
make format
```

Expected: exit 0

**Step 2: Verify registry packages**

Run:

```bash
go test ./internal/registry/...
```

Expected: PASS

**Step 3: Verify full repo**

Run:

```bash
make lint
make test
make build
```

Expected: all commands exit 0

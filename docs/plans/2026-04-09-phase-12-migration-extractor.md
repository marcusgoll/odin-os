# Phase 12 Migration Extractor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a deterministic extractor that scans `odin-orchestrator`, classifies candidate assets, detects duplicates and backup trees, emits migration reports, and optionally generates review-only draft registry files.

**Architecture:** Add a focused `internal/migration/extractor` package with typed scan, classification, duplicate, draft, and reporting layers. Keep runtime outputs explicit: machine-readable inventory under `state/migration/`, human review docs under `docs/migration/`, and opt-in draft registry files under `state/migration/drafts/`.

**Tech Stack:** Go, filesystem walking, JSON encoding, Markdown generation, existing registry validator/loader packages

---

### Task 1: Add extractor types and failing candidate-scan tests

**Files:**
- Create: `internal/migration/extractor/types.go`
- Create: `internal/migration/extractor/scan.go`
- Test: `internal/migration/extractor/scan_test.go`

**Step 1: Write the failing scan tests**

Cover:

- skills under `.claude/skills/<name>/SKILL.md`
- mirrored skills under `.agents/skills`
- prompts and docs under likely legacy paths
- junk trees such as `.git`, `.cache`, and `.worktrees` are ignored

**Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/migration/extractor -run 'TestScan'`
Expected: FAIL because the extractor package does not exist yet.

**Step 3: Implement the minimal scan layer**

Add:

- candidate kind enums
- source record types
- directory ignore rules
- path-based candidate detection

**Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/migration/extractor -run 'TestScan'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/migration/extractor/types.go internal/migration/extractor/scan.go internal/migration/extractor/scan_test.go
git commit -m "feat: add migration extractor scanning"
```

### Task 2: Add classification and duplicate detection with failing tests

**Files:**
- Create: `internal/migration/extractor/classify.go`
- Create: `internal/migration/extractor/duplicates.go`
- Test: `internal/migration/extractor/classify_test.go`
- Test: `internal/migration/extractor/duplicates_test.go`

**Step 1: Write the failing classification and duplicate tests**

Cover:

- mirrored skills across `.claude/skills` and `.agents/skills` group as duplicates
- backup/worktree paths classify as `archive` or `delete`
- architecture docs classify as `reference_only`
- legacy runtime assets that need reshaping classify as `rewrite`

**Step 2: Run the focused tests to verify they fail**

Run: `go test ./internal/migration/extractor -run 'TestClassif|TestDuplicate'`
Expected: FAIL because classification and duplicate logic do not exist yet.

**Step 3: Implement the minimal classification and duplicate logic**

Add:

- deterministic path signal extraction
- content-hash duplicate grouping
- normalized key detection
- conservative default classification rules

**Step 4: Run the focused tests to verify they pass**

Run: `go test ./internal/migration/extractor -run 'TestClassif|TestDuplicate'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/migration/extractor/classify.go internal/migration/extractor/duplicates.go internal/migration/extractor/classify_test.go internal/migration/extractor/duplicates_test.go
git commit -m "feat: add migration classification and duplicate detection"
```

### Task 3: Add draft generation with failing contract tests

**Files:**
- Create: `internal/migration/extractor/drafts.go`
- Test: `internal/migration/extractor/drafts_test.go`

**Step 1: Write the failing draft generation tests**

Cover:

- emitted draft file has `status: draft`
- generated draft maps to the Phase 02 registry contract
- unsupported candidate kinds do not emit registry drafts

**Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/migration/extractor -run 'TestDraft'`
Expected: FAIL because draft generation does not exist yet.

**Step 3: Implement the minimal draft generator**

Generate review-only drafts under `state/migration/drafts/` with:

- draft frontmatter
- generated sections that satisfy the registry contract
- provenance back to the legacy source path

Use the existing registry loader or validator in the tests to prove conformance.

**Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/migration/extractor -run 'TestDraft'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/migration/extractor/drafts.go internal/migration/extractor/drafts_test.go
git commit -m "feat: add migration draft registry generation"
```

### Task 4: Add report generation, service wiring, and a runnable script with failing tests

**Files:**
- Create: `internal/migration/extractor/reports.go`
- Create: `internal/migration/extractor/service.go`
- Create: `internal/migration/extractor/service_test.go`
- Create: `scripts/migrate/extract-odin-orchestrator.go`

**Step 1: Write the failing service tests**

Cover:

- service emits `inventory.json`
- service emits Markdown inventory and duplicate reports
- opt-in draft generation writes files only when enabled

**Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/migration/extractor -run 'TestService'`
Expected: FAIL because the integrated service and reports do not exist yet.

**Step 3: Implement the minimal service and report layer**

Add:

- end-to-end extraction orchestration
- JSON inventory rendering
- Markdown review report rendering
- a small Go script entry point under `scripts/migrate/`

**Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/migration/extractor -run 'TestService'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/migration/extractor/reports.go internal/migration/extractor/service.go internal/migration/extractor/service_test.go scripts/migrate/extract-odin-orchestrator.go
git commit -m "feat: add migration extractor service and reports"
```

### Task 5: Generate the real migration outputs and run final verification

**Files:**
- Create: `docs/migration/legacy-inventory.md`
- Create: `docs/migration/duplicate-report.md`
- Create: `state/migration/inventory.json`
- Create: `state/migration/drafts/...` when draft generation is enabled for this phase
- Modify: `README.md` only if Phase 12 status needs to be recorded now

**Step 1: Run the real extractor against `odin-orchestrator`**

Run the script against `/home/orchestrator/odin-orchestrator` and write outputs into the repo.

**Step 2: Review and adjust deterministic output shape if needed**

Keep the outputs reviewable and stable. Do not hand-edit generated JSON beyond what the generator should own.

**Step 3: Run focused verification**

Run: `go test ./internal/migration/extractor`
Expected: PASS

**Step 4: Run full verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: exit 0

**Step 5: Commit**

```bash
git add docs/migration state/migration internal/migration/extractor scripts/migrate/extract-odin-orchestrator.go README.md
git commit -m "feat: add odin-orchestrator migration extractor for phase 12"
```

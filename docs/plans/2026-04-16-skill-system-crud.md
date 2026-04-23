# Skill System CRUD And Dynamic Invocation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make skills in `odin-os` first-class runtime capabilities with real CRUD, authoritative discovery, and standardized dynamic invocation that both Odin and the Codex maintenance workflow can use cleanly.

**Architecture:** Keep `registry/skills/*.md` as the canonical authored source of truth and extend the skill frontmatter with runtime contract fields. Add a single `internal/skills` service that owns rendering, validation, CRUD, reference checks, fresh registry loading, and command-backed invocation. Rework broker and CLI integration so discovery and invocation both source from the same fresh registry path instead of static snapshots or direct file edits.

**Tech Stack:** Go 1.25, Markdown with YAML frontmatter, standard library JSON/process execution, existing registry loader/compiler, existing planner/broker packages, Cobra-free internal CLI command routing, Go unit and integration tests.

---

### Task 1: Extend the canonical registry skill contract for executable skills

**Files:**
- Modify: `docs/contracts/registry-format.md`
- Modify: `internal/registry/types.go`
- Modify: `internal/registry/validator/validate.go`
- Modify: `internal/registry/validator/validate_test.go`
- Modify: `internal/registry/parser/parse_test.go`
- Modify: `internal/registry/loader/load_test.go`
- Modify: `registry/skills/triage-skill.md`

**Step 1: Write the failing tests**

Add validator and loader coverage for:

- required skill fields: `version`, `enabled`, `scopes`, `permissions`, `handler_type`, `handler_ref`, `timeout_seconds`, `input_schema`, `output_schema`
- invalid `handler_type`
- missing `handler_ref`
- invalid timeout bounds
- malformed schema values

Example test shape:

```go
func TestValidateSkillRequiresRuntimeContractFields(t *testing.T) {
	document := makeValidDocument("skills/triage.md", "triage")
	document.Frontmatter.Version = ""
	document.Frontmatter.HandlerType = ""

	diagnostics := ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticCode(t, diagnostics, "missing_field")
}

func TestLoadDirIncludesExecutableSkillFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "skills", "triage.md"), validExecutableSkillMarkdown("triage-skill"))

	snapshot, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	item := snapshot.ByKey["triage-skill"]
	if item.HandlerType != "command" {
		t.Fatalf("HandlerType = %q, want command", item.HandlerType)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/parser ./internal/registry/validator ./internal/registry/loader -count=1`

Expected: FAIL because the registry types and validator do not yet know about executable skill contract fields.

**Step 3: Write the minimal implementation**

- Add the new fields to `registry.Frontmatter` and `registry.Item`
- validate the required executable-skill contract in `validate.go`
- keep the contract narrow:
  - `handler_type` initially supports only `command`
  - `timeout_seconds` must be positive and bounded
  - `input_schema` and `output_schema` must decode as map-like structured values
- update the sample registry skill to the new contract

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/registry/parser ./internal/registry/validator ./internal/registry/loader -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/registry-format.md internal/registry/types.go internal/registry/validator/validate.go internal/registry/validator/validate_test.go internal/registry/parser/parse_test.go internal/registry/loader/load_test.go registry/skills/triage-skill.md
git commit -m "feat: extend registry contract for executable skills"
```

### Task 2: Add a shared skill service for render, read, create, update, and delete

**Files:**
- Create: `internal/skills/types.go`
- Create: `internal/skills/render.go`
- Create: `internal/skills/service.go`
- Create: `internal/skills/service_test.go`
- Create: `internal/skills/testdata/valid-command-skill.sh`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Write the failing tests**

Add service tests for:

- list and get read skills from the canonical registry
- create writes `registry/skills/<key>.md` and returns the normalized view
- update changes the same canonical file and preserves atomic behavior on validation failure
- delete removes the canonical file
- duplicate create is rejected
- invalid update leaves the prior file unchanged

Example test shape:

```go
func TestCreateSkillWritesCanonicalRegistryFile(t *testing.T) {
	service := newTestService(t)

	skill, err := service.Create(context.Background(), CreateRequest{
		Key:         "echo-skill",
		Title:       "Echo Skill",
		Version:     "1.0.0",
		HandlerType: "command",
		HandlerRef:  "scripts/skills/echo-skill.sh",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if skill.Key != "echo-skill" {
		t.Fatalf("Key = %q, want echo-skill", skill.Key)
	}
}

func TestUpdateSkillIsAtomicOnValidationFailure(t *testing.T) {
	service := newTestService(t)
	seedSkill(t, service, "echo-skill")
	original := mustReadFile(t, service.repoRoot, "registry/skills/echo-skill.md")

	_, err := service.Update(context.Background(), "echo-skill", UpdateRequest{
		HandlerType: ptr(""),
	})
	if err == nil {
		t.Fatalf("Update() error = nil, want validation failure")
	}

	current := mustReadFile(t, service.repoRoot, "registry/skills/echo-skill.md")
	if current != original {
		t.Fatalf("canonical file changed on failed update")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills -count=1`

Expected: FAIL because `internal/skills` does not exist.

**Step 3: Write the minimal implementation**

- define normalized skill types and request structs
- render canonical markdown from a typed spec instead of raw string concatenation in CLI code
- implement CRUD against `registry/skills/*.md`
- reload the registry after every mutation and surface post-write validation diagnostics
- use temp-file plus rename for update atomicity

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/types.go internal/skills/render.go internal/skills/service.go internal/skills/service_test.go internal/skills/testdata/valid-command-skill.sh docs/contracts/repo-layout.md
git commit -m "feat: add shared skill CRUD service"
```

### Task 3: Add reference checks and standardized command-backed skill invocation

**Files:**
- Modify: `internal/skills/service.go`
- Modify: `internal/skills/service_test.go`
- Create: `internal/skills/invoke.go`
- Create: `internal/skills/invoke_test.go`
- Create: `scripts/skills/triage-skill.sh`
- Modify: `registry/skills/triage-skill.md`

**Step 1: Write the failing tests**

Add tests for:

- invoking a skill by key using JSON stdin/stdout
- timeout handling
- non-zero exit handling
- malformed JSON response handling
- rejecting invalid handler paths
- delete rejection when an agent or workflow still references the skill

Example test shape:

```go
func TestInvokeExecutesCommandSkillAndReturnsStructuredResponse(t *testing.T) {
	service := newTestService(t)
	seedExecutableSkill(t, service, "echo-skill", "scripts/skills/echo-skill.sh")

	response, err := service.Invoke(context.Background(), InvokeRequest{
		Key:   "echo-skill",
		Input: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("Status = %q, want ok", response.Status)
	}
}

func TestDeleteRejectsReferencedSkill(t *testing.T) {
	service := newTestService(t)
	seedExecutableSkill(t, service, "triage-skill", "scripts/skills/triage-skill.sh")
	seedAgentReferencingSkill(t, service.repoRoot, "triage-skill")

	err := service.Delete(context.Background(), "triage-skill")
	if err == nil {
		t.Fatalf("Delete() error = nil, want reference rejection")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills -run 'Test(Invoke|DeleteRejectsReferencedSkill)' -count=1`

Expected: FAIL because invocation and reference checks are not implemented.

**Step 3: Write the minimal implementation**

- add a standard invoke request/response envelope
- implement `command` handler execution with:
  - repo-relative handler resolution
  - no shell interpolation
  - JSON stdin/stdout
  - context timeout
- reject delete when the compiled registry contains references to the skill key
- add one real repo-local skill script for the sample skill

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/service.go internal/skills/service_test.go internal/skills/invoke.go internal/skills/invoke_test.go scripts/skills/triage-skill.sh registry/skills/triage-skill.md
git commit -m "feat: add standard skill invocation and delete safety"
```

### Task 4: Make discovery and execution use fresh registry state instead of stale snapshots

**Files:**
- Modify: `internal/tools/broker/broker.go`
- Modify: `internal/tools/broker/broker_test.go`
- Modify: `internal/tools/catalog/types.go`
- Modify: `internal/workers/planner/service.go`
- Modify: `internal/workers/planner/service_test.go`
- Create: `internal/tools/broker/source.go`

**Step 1: Write the failing tests**

Add tests for:

- broker catalog sees a skill created after broker construction
- broker expand sees updated skill metadata after update
- broker invoke routes a registry-backed skill through the skill service
- deleted skills disappear from catalog and fail on expansion/invocation

Example test shape:

```go
func TestCatalogReloadsRegistryStateOnEachCall(t *testing.T) {
	source := newMutableTestSource(t)
	broker := New(source, catalog.BuiltinDefinitions(), nil, testLimits())

	source.UpsertSkill("echo-skill")
	cards := broker.Catalog("project")

	assertHasCard(t, cards, "echo-skill")
}

func TestInvokeSkillUsesRegistryBackedInvoker(t *testing.T) {
	source := newMutableTestSource(t)
	source.UpsertExecutableSkill("echo-skill")
	broker := New(source, catalog.BuiltinDefinitions(), stubSkillInvoker, testLimits())

	result, err := broker.InvokeSkill("echo-skill", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("InvokeSkill() error = %v", err)
	}
	if result.CapabilityKey != "echo-skill" {
		t.Fatalf("CapabilityKey = %q, want echo-skill", result.CapabilityKey)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tools/broker ./internal/workers/planner -count=1`

Expected: FAIL because broker uses a static registry snapshot and has no registry-backed invocation path.

**Step 3: Write the minimal implementation**

- replace the static snapshot dependency with a fresh-loading source/provider
- add a standard registry-backed skill invocation path in the broker
- keep built-in tool support unchanged
- update planner materialization to allow registry-backed skill execution through the same broker contract

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/broker ./internal/workers/planner -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/broker/source.go internal/tools/broker/broker.go internal/tools/broker/broker_test.go internal/tools/catalog/types.go internal/workers/planner/service.go internal/workers/planner/service_test.go
git commit -m "feat: reload skills dynamically for broker discovery and execution"
```

### Task 5: Add repo-owned CLI commands for skill lifecycle and invocation

**Files:**
- Create: `internal/cli/commands/skills.go`
- Create: `internal/cli/commands/skills_test.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/cli/commands/root.go`

**Step 1: Write the failing tests**

Add tests for:

- `odin skills list`
- `odin skills get <key>`
- `odin skills create ...`
- `odin skills update ...`
- `odin skills delete <key>`
- `odin skills invoke <key> --input <json>`

Example test shape:

```go
func TestRunSkillsCreateListAndInvoke(t *testing.T) {
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	mustRun(t, root, runtimeRoot, "skills", "create",
		"--key", "echo-skill",
		"--title", "Echo Skill",
		"--version", "1.0.0",
		"--handler-type", "command",
		"--handler-ref", "scripts/skills/echo-skill.sh",
	)

	stdout := mustRun(t, root, runtimeRoot, "skills", "invoke", "echo-skill", "--input", `{"message":"hello"}`)
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("stdout = %q, want hello", stdout)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'TestRunSkills' -count=1`

Expected: FAIL because there is no skills command surface.

**Step 3: Write the minimal implementation**

- wire `internal/skills.Service` into bootstrap/app construction
- add `skills` subcommands with text plus JSON output support
- ensure the CLI path uses the shared service instead of direct file mutation

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'TestRunSkills' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/skills.go internal/cli/commands/skills_test.go internal/app/bootstrap/bootstrap.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/cli/commands/root.go
git commit -m "feat: add odin skill lifecycle and invocation commands"
```

### Task 6: Add integration and end-to-end coverage for Odin and Codex-facing workflows

**Files:**
- Create: `tests/integration/skill_lifecycle_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `Makefile`

**Step 1: Write the failing tests**

Add integration coverage for:

- create skill through the CLI
- discover it through fresh runtime catalog use
- invoke it through the CLI
- update it and observe changed runtime behavior
- delete it and confirm discovery plus invocation fail
- verify the same commands are suitable for Codex maintenance usage

Example test shape:

```go
func TestSkillLifecycleCrudAndInvocation(t *testing.T) {
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	runOdin(t, root, runtimeRoot, "skills", "create", ...)
	runOdin(t, root, runtimeRoot, "skills", "list")
	runOdin(t, root, runtimeRoot, "skills", "invoke", "echo-skill", "--input", `{"message":"hello"}`)
	runOdin(t, root, runtimeRoot, "skills", "update", "echo-skill", "--summary", "Updated")
	runOdin(t, root, runtimeRoot, "skills", "delete", "echo-skill")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration -run 'Test(SkillLifecycleCrudAndInvocation|AlphaAcceptance)' -count=1 -v`

Expected: FAIL because the CLI lifecycle and dynamic invocation path are not fully wired.

**Step 3: Write the minimal implementation**

- finish any missing runtime wiring discovered by the new integration coverage
- update the alpha acceptance suite to assert that skill CRUD and dynamic invocation are real
- add a stable make target if needed for rerunning the skill e2e slice

**Step 4: Run the tests to verify they pass**

Run: `go test ./tests/integration -run 'Test(SkillLifecycleCrudAndInvocation|AlphaAcceptance)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/skill_lifecycle_test.go tests/integration/alpha_acceptance_test.go Makefile
git commit -m "test: cover skill lifecycle and runtime invocation end to end"
```

### Task 7: Document the supported skill lifecycle and Codex maintenance workflow

**Files:**
- Create: `docs/contracts/skill-lifecycle.md`
- Modify: `README.md`
- Modify: `docs/contracts/capability-catalog.md`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Write the failing documentation checklist**

Create a checklist in your working notes and verify the docs answer:

- where skills live
- which fields are required
- how create/update/delete works
- how runtime discovery stays current
- how invocation works
- how Codex should manage skills without bypassing the source of truth

**Step 2: Run the verification to show the docs are incomplete**

Run: `rg -n "skill lifecycle|odin skills|handler_type|handler_ref" README.md docs/contracts`

Expected: Missing or incomplete coverage for the full lifecycle and Codex workflow.

**Step 3: Write the minimal implementation**

- add a dedicated lifecycle contract doc
- update existing docs to point to the canonical workflow
- document the recommended Codex maintenance path through `odin skills ...`

**Step 4: Run the verification to show the docs are complete**

Run: `rg -n "skill lifecycle|odin skills|handler_type|handler_ref" README.md docs/contracts`

Expected: Matches the new contract and workflow docs.

**Step 5: Commit**

```bash
git add docs/contracts/skill-lifecycle.md README.md docs/contracts/capability-catalog.md docs/contracts/repo-layout.md
git commit -m "docs: document skill lifecycle and codex workflow"
```

### Task 8: Run full verification on the merged behavior

**Files:**
- Modify: any files required to address failures uncovered here

**Step 1: Run targeted package tests**

Run: `go test ./internal/registry/... ./internal/skills ./internal/tools/broker ./internal/workers/planner ./internal/cli/commands ./internal/app/lifecycle -count=1`

Expected: PASS

**Step 2: Run integration tests**

Run: `go test ./tests/integration -count=1 -v`

Expected: PASS

**Step 3: Run full repository verification**

Run: `go test ./... -count=1`

Expected: PASS

**Step 4: Run build verification**

Run: `make build`

Expected: PASS

**Step 5: Commit any final fixups**

```bash
git add <files>
git commit -m "fix: stabilize skill lifecycle verification"
```

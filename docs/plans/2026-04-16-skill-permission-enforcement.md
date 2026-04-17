# Skill Permission Enforcement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enforce skill `permissions` during invocation so command-backed skills are gated by Odin's real scope, project policy, transition state, and approval rules instead of running on metadata alone.

**Architecture:** Keep `registry/skills/*.md` as the canonical source of truth and keep the existing `permissions` field, but make invocation resolve those permissions into an explicit governance policy before the handler starts. Reuse `internal/core/projects` transition and approval logic for mutating skill permissions, keep read-only permissions available in global scope, and record allow/deny outcomes through the existing skill lifecycle observer and SQLite runtime event stream.

**Tech Stack:** Go 1.25, existing `internal/skills` service, existing `internal/core/projects` policy and transition service, bootstrap app state, CLI scope/session state, SQLite runtime events, Go unit and integration tests.

---

### Task 1: Define the enforced skill permission vocabulary and parser

**Files:**
- Modify: `internal/registry/validator/validate.go`
- Modify: `internal/registry/validator/validate_test.go`
- Create: `internal/skills/permissions.go`
- Create: `internal/skills/permissions_test.go`
- Modify: `docs/contracts/registry-format.md`
- Modify: `docs/contracts/skill-lifecycle.md`

**Step 1: Write the failing tests**

Add tests for:

- valid permission strings:
  - `repo.read`
  - `runtime.read`
  - `repo.mutate.isolated:docs_audit_note`
  - `repo.mutate.full`
  - `repo.mutate.governance`
  - `repo.mutate.destructive`
- invalid permission strings:
  - unknown prefixes
  - missing isolated action key
  - malformed isolated permission
- validator rejection when a skill manifest uses an unknown permission

Example test shape:

```go
func TestParsePermissionRejectsUnknownValue(t *testing.T) {
	_, err := ParsePermission("repo.write")
	if err == nil {
		t.Fatal("ParsePermission() error = nil, want rejection")
	}
}

func TestValidateSkillRejectsUnknownPermission(t *testing.T) {
	document := validSkillDocument()
	document.Frontmatter.Permissions = []string{"repo.write"}

	diagnostics := ValidateDocuments([]registry.ParsedDocument{document})

	assertDiagnosticMessage(t, diagnostics, "invalid permission")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/validator ./internal/skills -run 'Test(ParsePermission|ValidateSkillRejectsUnknownPermission)' -count=1`

Expected: FAIL because the permission vocabulary and parser do not exist yet.

**Step 3: Write the minimal implementation**

- add a centralized permission parser in `internal/skills/permissions.go`
- support the explicit first-pass vocabulary only
- expose a normalized representation that later tasks can consume
- update registry validation to reject unknown permission strings
- document the enforced vocabulary in the contracts

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/registry/validator ./internal/skills -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/registry/validator/validate.go internal/registry/validator/validate_test.go internal/skills/permissions.go internal/skills/permissions_test.go docs/contracts/registry-format.md docs/contracts/skill-lifecycle.md
git commit -m "feat: define enforced skill permission vocabulary"
```

### Task 2: Resolve invocation policy from permissions and scope

**Files:**
- Modify: `internal/skills/types.go`
- Modify: `internal/skills/service.go`
- Modify: `internal/skills/invoke.go`
- Modify: `internal/skills/invoke_test.go`
- Create: `internal/skills/policy.go`
- Create: `internal/skills/policy_test.go`

**Step 1: Write the failing tests**

Add tests for:

- read-only permissions allowed in global scope
- mutating permissions denied in global scope
- isolated mutation extracts the limited-action key correctly
- multiple permissions collapse to the most restrictive effective policy
- deny unknown or empty permission sets at invocation time even if bypassed earlier

Example test shape:

```go
func TestResolveInvocationPolicyDeniesMutationInGlobalScope(t *testing.T) {
	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ScopeKind:    "global",
		Permissions:  []string{"repo.mutate.full"},
	})
	if err == nil {
		t.Fatal("ResolveInvocationPolicy() error = nil, want denial")
	}
	if policy.Allowed {
		t.Fatal("policy.Allowed = true, want false")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills -run 'TestResolveInvocationPolicy' -count=1`

Expected: FAIL because invocation policy resolution does not exist.

**Step 3: Write the minimal implementation**

- extend invocation input/context types to include resolved scope and optional project metadata
- add a central policy resolver that turns permissions into:
  - read-only vs mutating
  - action class
  - isolated action key when present
  - approval-needed signal
- deny mutation in global scope
- keep the enforcement decision separate from command execution

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/types.go internal/skills/service.go internal/skills/invoke.go internal/skills/invoke_test.go internal/skills/policy.go internal/skills/policy_test.go
git commit -m "feat: resolve invocation policy from skill permissions"
```

### Task 3: Integrate skill permission checks with project transition and approval policy

**Files:**
- Modify: `internal/app/lifecycle/skills.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `internal/core/projects/service_test.go`
- Modify: `internal/skills/invoke.go`
- Modify: `internal/skills/invoke_test.go`
- Modify: `docs/contracts/project-transition.md`

**Step 1: Write the failing tests**

Add tests for:

- project-scoped isolated mutation denied when the project is not in `limited_action`
- project-scoped isolated mutation denied when the action key is not allowlisted
- project-scoped isolated mutation allowed when transition state and allowlist both match
- governance or destructive mutation denied when project policy requires approval
- odin-core mutations inherit system-project approval requirements

Example test shape:

```go
func TestInvokeDeniesIsolatedMutationWhenLimitedActionIsNotAllowlisted(t *testing.T) {
	env := newInvocationHarness(t, withProjectScope("alpha"))
	env.setTransitionState(projects.TransitionStateLimitedAction, []string{"docs_audit_note"})
	env.seedSkill("skill-note", []string{"repo.mutate.isolated:repo_hygiene_note"})

	_, err := env.service.Invoke(context.Background(), env.request("skill-note"))
	if err == nil {
		t.Fatal("Invoke() error = nil, want allowlist denial")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills ./internal/app/lifecycle -run 'TestInvoke' -count=1`

Expected: FAIL because skill invocation is not yet consulting project transition or approval policy.

**Step 3: Write the minimal implementation**

- load current CLI state inside `runSkills`
- for project and odin-core scopes, resolve the selected manifest
- construct invocation context with:
  - scope kind
  - project key
  - manifest
  - transition service/store access
- call the existing project transition authorization path for mutating permissions
- reject approval-required invocations with a clear error before running the handler
- preserve audit logging and SQLite event recording for denials

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills ./internal/app/lifecycle -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/app/lifecycle/skills.go internal/app/lifecycle/run_test.go internal/core/projects/service_test.go internal/skills/invoke.go internal/skills/invoke_test.go docs/contracts/project-transition.md
git commit -m "feat: gate skill invocation through project policy"
```

### Task 4: Extend lifecycle audit classification for permission denials

**Files:**
- Modify: `internal/skills/service.go`
- Modify: `internal/skills/hardening_test.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `tests/integration/skill_lifecycle_test.go`
- Modify: `docs/contracts/runtime-events.md`

**Step 1: Write the failing tests**

Add tests for:

- denial error codes:
  - `unknown_permission`
  - `mutation_requires_project_scope`
  - `transition_denied`
  - `approval_required`
- lifecycle event payload carries the denial code and text
- integration coverage confirms denied invokes are recorded in SQLite

Example test shape:

```go
func TestInvokeEmitsPermissionDeniedLifecycleEvent(t *testing.T) {
	observer := &recordingObserver{}
	service := newScopedService(t, observer, globalScope())
	seedSkill(t, service, "mutating-skill", []string{"repo.mutate.full"})

	_, err := service.Invoke(context.Background(), InvokeRequest{Key: "mutating-skill"})
	if err == nil {
		t.Fatal("Invoke() error = nil, want permission denial")
	}
	if observer.events[len(observer.events)-1].ErrorCode != "mutation_requires_project_scope" {
		t.Fatalf("ErrorCode = %q", observer.events[len(observer.events)-1].ErrorCode)
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/skills ./tests/integration -run 'Test(InvokeEmitsPermissionDeniedLifecycleEvent|SkillLifecycleCrudAndInvocation)' -count=1`

Expected: FAIL because permission denial classification is not yet wired through the lifecycle event path.

**Step 3: Write the minimal implementation**

- extend skill error classification for permission denials
- preserve existing lifecycle event emission behavior
- ensure SQLite runtime event payloads include the stable denial code
- update runtime event docs to include permission-denied cases explicitly

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/skills ./tests/integration -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/service.go internal/skills/hardening_test.go internal/runtime/events/events.go tests/integration/skill_lifecycle_test.go docs/contracts/runtime-events.md
git commit -m "test: audit permission enforcement outcomes"
```

### Task 5: Prove the full binary-driven permission gate end to end

**Files:**
- Modify: `tests/integration/skill_lifecycle_test.go`
- Modify: `tests/integration/helpers_test.go`
- Modify: `README.md`
- Modify: `docs/contracts/skill-lifecycle.md`

**Step 1: Write the failing test**

Extend the existing compiled-binary skill lifecycle coverage so it also proves:

- `odin skills invoke` succeeds for a read-only skill in allowed scope
- `odin skills invoke` fails for a mutating skill in global scope
- `odin project select <project>` plus transition setup allows a properly allowlisted isolated-mutation skill
- denied and allowed invocation events are both present in the runtime SQLite event stream

Example test shape:

```go
func TestSkillInvocationPermissionGateE2E(t *testing.T) {
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	binaryPath := buildOdinBinary(t, projectRoot(t))
	runtimeRoot := t.TempDir()

	// seed project scope, transition state, and both read-only and mutating skills
	// run compiled binary commands
	// inspect sqlite events
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./tests/integration -run 'TestSkillInvocationPermissionGateE2E' -count=1 -v`

Expected: FAIL because the permission gate is not fully enforced or observed yet.

**Step 3: Write the minimal implementation and docs**

- finish any missing harness support for transition seeding
- update README and the skill lifecycle contract with the enforced permission model
- keep examples aligned with the new permission vocabulary

**Step 4: Run the test to verify it passes**

Run: `go test ./tests/integration -run 'TestSkillInvocationPermissionGateE2E' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/skill_lifecycle_test.go tests/integration/helpers_test.go README.md docs/contracts/skill-lifecycle.md
git commit -m "test: prove skill permission enforcement end to end"
```

### Task 6: Run full verification

**Files:**
- Modify: none

**Step 1: Run targeted package verification**

Run:

```bash
go test ./internal/registry/validator ./internal/skills ./internal/app/lifecycle ./internal/core/projects ./internal/store/sqlite -count=1
```

Expected: PASS

**Step 2: Run integration verification**

Run:

```bash
go test ./tests/integration -count=1 -v
```

Expected: PASS

**Step 3: Run full repo verification**

Run:

```bash
go test ./... -count=1
make build
```

Expected: PASS

**Step 4: Commit verification-only changes if needed**

If the implementation touched docs/tests during verification cleanup:

```bash
git add README.md docs/contracts/skill-lifecycle.md docs/contracts/runtime-events.md tests/integration/skill_lifecycle_test.go
git commit -m "docs: finalize skill permission enforcement contract"
```

Otherwise skip this step.

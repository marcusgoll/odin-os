# Social Outcome Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Normalize social approval history by validating `social_outcome` entries inside the existing `/memory remember` flow.

**Architecture:** Reuse the current shell memory command, current `memoryRememberRequest` parsing, current `details_json.fields` storage, and the current recall commands. Add narrow type-specific validation for `social_outcome`, update tests to require valid outcome fields, and prove the behavior through the real `odin` shell.

**Tech Stack:** Go 1.25, REPL shell under `internal/cli/repl`, help/docs under `docs/contracts` and `docs/plans`, existing knowledge-memory store and recall logic.

---

## Preconditions

- Use [2026-04-18-social-outcome-validation-design.md](/home/orchestrator/odin-os/docs/plans/2026-04-18-social-outcome-validation-design.md) as the design authority.
- Do not add a new `/memory outcome` command.
- Do not add a migration or new memory tables.
- Keep the first slice limited to `social_outcome` validation at write time.
- Prove the finished behavior through the real `odin` binary in `odin-os`.

### Task 1: Lock the expected outcome-history behavior with failing shell tests

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

Add tests like:

```go
func TestShellMemoryRememberValidatesSocialOutcomeRequiredFields(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, _ := New(env)

	var output bytes.Buffer
	_ = shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output)
	output.Reset()

	if err := shell.HandleLine(ctx, "/memory remember social_outcome channel=linkedin -- Missing result", &output); err != nil {
		t.Fatalf("HandleLine(...) error = %v", err)
	}
	if !strings.Contains(output.String(), "social_outcome requires") {
		t.Fatalf("output = %q, want validation error", output.String())
	}
}

func TestShellMemoryListSeparatesApprovedAndRejectedSocialOutcomes(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, _ := New(env)

	var output bytes.Buffer
	_ = shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output)
	_ = shell.HandleLine(ctx, "/memory remember social_outcome result=approved channel=linkedin content_kind=post -- Approved outcome", &output)
	_ = shell.HandleLine(ctx, "/memory remember social_outcome result=rejected channel=x content_kind=reply -- Rejected outcome", &output)
	output.Reset()

	_ = shell.HandleLine(ctx, "/memory list type=social_outcome field.result=approved", &output)
	if !strings.Contains(output.String(), "Approved outcome") {
		t.Fatalf("output = %q, want approved outcome", output.String())
	}
	if strings.Contains(output.String(), "Rejected outcome") {
		t.Fatalf("output = %q, want rejected outcome filtered out", output.String())
	}
}
```

Also update any existing `social_outcome` happy-path test inputs so they include `content_kind=...`.

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShellMemory(Remember|List).*SocialOutcome|TestShellMemoryListShowsWorkflowEntries' -count=1`

Expected: FAIL because `social_outcome` is not validated yet.

**Step 3: Write the minimal implementation**

Do not change production code yet beyond test-driven contract updates.

**Step 4: Run the tests again to verify the failure stays on runtime behavior**

Run: `go test ./internal/cli/repl -run 'TestShellMemory' -count=1`

Expected: FAIL only because validation logic is missing.

### Task 2: Implement narrow `social_outcome` validation in the shell

**Files:**
- Modify: `internal/cli/repl/shell.go`

**Step 1: Write the failing tests**

Add focused cases for:

```go
func TestShellMemoryRememberRejectsSocialOutcomeInvalidResult(t *testing.T) {}
func TestShellMemoryRememberRejectsSocialOutcomeInvalidChannel(t *testing.T) {}
func TestShellMemoryRememberRejectsSocialOutcomeInvalidContentKind(t *testing.T) {}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryRememberRejectsSocialOutcome' -count=1`

Expected: FAIL because the shell still accepts invalid values.

**Step 3: Write the minimal implementation**

Add a validation helper in `internal/cli/repl/shell.go`, for example:

```go
func validateMemoryRememberRequest(request memoryRememberRequest) error
func validateSocialOutcomeFields(fields map[string]string) error
```

Rules:

- only apply special validation when `request.MemoryType == "social_outcome"`
- require:
  - `result` in `{approved,rejected}`
  - `channel` in `{x,linkedin}`
  - `content_kind` in `{post,reply,thread,article_seed}`
- leave other memory types unchanged
- return clear errors such as:
  - `social_outcome requires result=approved|rejected`
  - `social_outcome requires channel=x|linkedin`
  - `social_outcome requires content_kind=post|reply|thread|article_seed`

Call the validator before writing to the store in `handleMemoryRemember(...)`.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/repl -run 'TestShellMemory' -count=1`

Expected: PASS.

### Task 3: Update social contract examples to match the validated format

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`

**Step 1: Write the failing expectation**

Treat doc drift as a contract failure for this phase and manually verify the examples still omit `content_kind`.

**Step 2: Update the examples**

Change CLI examples so they show:

```text
/memory remember social_outcome result=approved channel=linkedin content_kind=post -- LinkedIn post about CFI decision-making approved for next review batch.
/memory remember social_outcome result=rejected channel=x content_kind=reply reason=too-defensive -- X reply draft rejected because it sounded too defensive.
/memory list type=social_outcome field.result=approved
```

If the roadmap needs it, mark explicit approved-versus-rejected content history logging as live once the implementation is verified.

### Task 4: Prove the feature through the real `odin` command

**Files:**
- Verify only

**Step 1: Run targeted tests**

Run:

```bash
go test ./internal/cli/repl ./internal/cli/commands ./internal/memory/knowledge -count=1
```

Expected: PASS.

**Step 2: Build the real binary**

Run:

```bash
make build
```

Expected: `go build -o bin/odin ./cmd/odin`

**Step 3: Run real `odin` verification for valid approved and rejected logging**

Run:

```bash
tmp_input=$(mktemp -p /tmp odin-social-outcome-input-XXXXXXXX)
cat > "$tmp_input" <<'EOF'
/workflow use marcus-social-growth-workflow
/memory remember social_outcome result=approved channel=linkedin content_kind=post -- LinkedIn post approved for queue
/memory remember social_outcome result=rejected channel=x content_kind=reply reason=too-defensive -- X reply rejected for tone
/memory list type=social_outcome field.result=approved
/memory list type=social_outcome field.result=rejected
/exit
EOF
ODIN_ROOT=$(mktemp -d -p /tmp odin-social-outcome-root-XXXXXXXX)
env ODIN_ROOT="$ODIN_ROOT" ./bin/odin < "$tmp_input"
```

Expected:

- both valid entries are recorded
- approved filter returns only the approved entry
- rejected filter returns only the rejected entry

**Step 4: Run real `odin` verification for invalid logging**

Run:

```bash
tmp_input=$(mktemp -p /tmp odin-social-outcome-invalid-input-XXXXXXXX)
cat > "$tmp_input" <<'EOF'
/workflow use marcus-social-growth-workflow
/memory remember social_outcome channel=linkedin -- Missing required fields
/exit
EOF
ODIN_ROOT=$(mktemp -d -p /tmp odin-social-outcome-invalid-root-XXXXXXXX)
env ODIN_ROOT="$ODIN_ROOT" ./bin/odin < "$tmp_input"
```

Expected:

- the shell prints a validation error
- no invalid outcome entry is recorded

**Step 5: Final verification review**

Before claiming completion, confirm:

- tests are green
- build succeeds
- real `odin` accepted valid approved/rejected outcomes
- real `odin` rejected invalid outcomes
- no new social-only command was introduced

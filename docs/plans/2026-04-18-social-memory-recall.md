# Social Memory Recall Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the existing `/memory` command so Odin operators can inspect one saved memory by id and filter workflow-scoped social history by text, fields, order, and limit.

**Architecture:** Reuse the current shell command surface, workflow-aware memory scope resolution, knowledge-memory service, and existing SQLite memory summaries. Add narrow recall helpers for `show` and filtered `list`, parse the already-stored `details_json.fields`, and keep verification grounded in the real `odin` CLI rather than introducing a social-only history command.

**Tech Stack:** Go 1.25, existing REPL shell under `internal/cli/repl`, existing help text under `internal/cli/commands`, existing knowledge-memory service, existing SQLite store/models, markdown docs under `docs/contracts` and `docs/plans`.

---

## Preconditions

- Use [2026-04-18-social-memory-recall-design.md](/home/orchestrator/odin-os/docs/plans/2026-04-18-social-memory-recall-design.md) as the design authority.
- Do not create a new `/social` command.
- Do not add a migration or new memory table.
- Keep the first slice limited to `show` plus filtered `list`.
- Prove the finished behavior through the real `odin` binary in `odin-os`.

### Task 1: Lock the CLI contract with failing shell tests

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/cli/commands/help.go`

**Step 1: Write the failing tests**

Add tests like:

```go
func TestShellMemoryShowDisplaysSingleWorkflowEntry(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, _ := New(env)

	var output bytes.Buffer
	_ = shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output)
	output.Reset()
	_ = shell.HandleLine(ctx, "/memory remember social_draft channel=x approval=pending -- Draft A", &output)
	output.Reset()

	if err := shell.HandleLine(ctx, "/memory show 1", &output); err != nil {
		t.Fatalf("HandleLine(/memory show) error = %v", err)
	}
	if !strings.Contains(output.String(), "memory=1") {
		t.Fatalf("output = %q, want memory id", output.String())
	}
	if !strings.Contains(output.String(), "fields=approval=pending,channel=x") {
		t.Fatalf("output = %q, want structured fields", output.String())
	}
}

func TestShellMemoryListFiltersByFieldContainsAndLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, _ := New(env)

	var output bytes.Buffer
	_ = shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output)
	_ = shell.HandleLine(ctx, "/memory remember social_draft channel=x approval=pending -- Crosswind draft", &output)
	_ = shell.HandleLine(ctx, "/memory remember social_draft channel=x approval=approved -- Crosswind published", &output)
	output.Reset()

	if err := shell.HandleLine(ctx, "/memory list type=social_draft field.approval=pending contains=Crosswind limit=1 order=desc", &output); err != nil {
		t.Fatalf("HandleLine(/memory list filtered) error = %v", err)
	}
	if strings.Contains(output.String(), "approval=approved") {
		t.Fatalf("output = %q, want only pending entry", output.String())
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShellMemory(ShowDisplaysSingleWorkflowEntry|ListFiltersByFieldContainsAndLimit)' -count=1`

Expected: FAIL because `/memory show` and filtered recall do not exist yet.

**Step 3: Write the minimal implementation**

Update `internal/cli/commands/help.go` so `MemoryUsage` documents:

```go
MemoryUsage = "/memory [list [type=<memory_type>] [contains=<text>] [field.<name>=<value> ...] [limit=<n>] [order=asc|desc]|show <id>|remember <memory_type> [field=value ...] -- <summary...>]"
```

Do not change any runtime behavior yet beyond the help text needed by the tests.

**Step 4: Run the tests again to verify they still fail only on runtime behavior**

Run: `go test ./internal/cli/repl ./internal/cli/commands -run 'TestShellMemory(ShowDisplaysSingleWorkflowEntry|ListFiltersByFieldContainsAndLimit)|TestInteractiveHelp' -count=1`

Expected: FAIL because the shell behavior is still missing, not because the contract text is absent.

### Task 2: Add minimal memory recall helpers in the shell

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

Add tests for:

```go
func TestShellMemoryShowRejectsMissingEntry(t *testing.T) {}
func TestShellMemoryListOrdersDescendingWhenRequested(t *testing.T) {}
func TestShellMemoryListSupportsContainsWithoutType(t *testing.T) {}
```

Assertions to cover:

- `show` reports `unknown memory` or equivalent when the id is not in scope
- `order=desc` returns the newest matching entry first
- `contains=` works against summary text

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShellMemory' -count=1`

Expected: FAIL on the new recall cases.

**Step 3: Write the minimal implementation**

In `internal/cli/repl/shell.go`:

- extend `handleMemory(...)` with `show`
- add a parsed request type for list filters
- add helpers like:

```go
type memoryListRequest struct {
	MemoryType   string
	Contains     string
	FieldFilters map[string]string
	Limit        int
	OrderDesc    bool
}

func parseMemoryListArgs(args []string) (memoryListRequest, error)
func parseMemoryDetails(detailsJSON string) (memoryDetailsPayload, error)
func filterMemorySummaries(summaries []sqlite.MemorySummary, request memoryListRequest) []sqlite.MemorySummary
func renderMemorySummary(output io.Writer, summary sqlite.MemorySummary) error
```

- preserve current scope resolution through `shell.memoryScope(ctx)`
- fetch entries through the existing `knowledgememory.Service{Store: shell.env.Store}.List(...)`
- apply `contains`, `field.*`, `limit`, and `order` in shell code
- implement `show` by locating the visible entry by id within the scoped list
- render a `fields=` line when parsed `details_json.fields` is present

Keep the implementation intentionally narrow:

- only parse the existing payload shape
- ignore unknown `details_json` fields beyond `fields`
- no free-form JSON query language

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/repl -run 'TestShellMemory' -count=1`

Expected: PASS.

### Task 3: Add service/store support only if shell recall needs it

**Files:**
- Modify: `internal/memory/knowledge/service.go`
- Modify: `internal/memory/knowledge/service_test.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/models.go`

**Step 1: Write the failing tests**

Only add these if the shell implementation shows a real need for a direct-by-id fetch. The preferred test shape is:

```go
func TestServiceGetVisibleSummaryByIDWithinScope(t *testing.T) {}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/memory/knowledge ./internal/store/sqlite -run 'TestServiceGetVisibleSummaryByIDWithinScope' -count=1`

Expected: FAIL because no direct recall helper exists.

**Step 3: Write the minimal implementation**

Only if needed, add:

```go
func (service Service) GetVisibleByID(ctx context.Context, scope Scope, memoryID int64) (sqlite.MemorySummary, bool, error)
```

Reuse existing `ListMemorySummaries` logic or add the narrowest store helper needed. Avoid expanding into a general search API.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/memory/knowledge ./internal/store/sqlite -run 'TestServiceGetVisibleSummaryByIDWithinScope' -count=1`

Expected: PASS.

### Task 4: Update operator docs and the Marcus social contract

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `internal/cli/commands/help.go`

**Step 1: Write the failing expectations**

Add or update assertions in existing help tests if needed so the new `/memory` usage string is covered.

**Step 2: Run the tests to verify they fail if docs/help drift**

Run: `go test ./internal/cli/commands -count=1`

Expected: FAIL only if the help contract is stale.

**Step 3: Write the minimal implementation**

Update the social contract examples to show recall usage such as:

```text
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory show 12
```

Keep the roadmap unchanged unless implementation scope changes.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/commands -count=1`

Expected: PASS.

### Task 5: Prove the feature through the real `odin` command

**Files:**
- Verify only: no new files required

**Step 1: Run the targeted test suites**

Run:

```bash
go test ./internal/cli/commands ./internal/cli/repl ./internal/memory/knowledge
```

Expected: PASS.

**Step 2: Build the real binary**

Run:

```bash
make build
```

Expected: `go build -o bin/odin ./cmd/odin`

**Step 3: Run real `odin` E2E verification**

Run:

```bash
tmp_input=$(mktemp -p /tmp odin-memory-recall-input-XXXXXXXX)
cat > "$tmp_input" <<'EOF'
/workflow use marcus-social-growth-workflow
/memory remember social_draft channel=x approval=pending -- Crosswind coaching draft
/memory remember social_outcome channel=linkedin result=approved -- LinkedIn post approved for queue
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory show 1
/exit
EOF
ODIN_ROOT=$(mktemp -d -p /tmp odin-memory-recall-root-XXXXXXXX)
env ODIN_ROOT="$ODIN_ROOT" ./bin/odin < "$tmp_input"
```

Expected:

- workflow selection succeeds
- remembered items are recorded in workflow scope
- filtered list returns only the pending social draft
- `show` returns the selected memory with summary and parsed fields

**Step 4: Re-run any targeted command if the returned memory id is not deterministic**

If ids vary, capture the id from the list output and rerun `/memory show <id>` in a second real session.

**Step 5: Final verification review**

Before claiming completion, confirm:

- tests are green
- build succeeds
- real `odin` recall path works
- no social-only parallel command was introduced

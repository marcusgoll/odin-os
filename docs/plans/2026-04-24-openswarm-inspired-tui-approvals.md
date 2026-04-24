# OpenSwarm-Inspired TUI Approvals Implementation Plan

**Status:** Partially executed in `codex/openswarm-tui-approvals-main`.

**Execution note:** This plan was written before the main-based implementation audit. Execution reused existing `internal/runtime/approvals`, `internal/cli/overview`, `internal/cli/render`, and `internal/cli/repl` surfaces rather than creating parallel packages. The task steps below remain the planning record and should be read with the execution commits as the current source of truth.

**Goal:** Deliver a canonical `/overview` operator board plus approval list/detail/scoped-resolve surfaces inspired by OpenSwarm's workbench ergonomics without creating a parallel runtime authority.

**Domain Source of Truth:** `CONTEXT.md`, `docs/contracts/tui-overview.md`, `docs/contracts/workspace-context-map.md`, `docs/contracts/repo-layout.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`, `docs/plans/2026-04-24-openswarm-inspired-tui-approvals-design.md`

**Context:** `internal/cli` operator surface over existing Odin runtime, projection, registry, memory, and approval services.

**Owns / Does Not Own:** This slice owns `/overview` presentation, approval lane presentation, `/approvals` list/detail routing, and scoped resolve command routing. It does not own SQLite authority, workflow-specific continuation semantics, social draft approvals, browser evidence, transfer submit behavior, or physical table renames.

**Invariants:**
- `/overview` is read-only and must not mutate runtime state.
- `Workspace -> Initiative -> Work Item -> Run Attempts` remains the default operator hierarchy.
- `Approval Request` remains a governance lane, not the primary landing object.
- `Work Item`, `Run Attempt`, and `Approval Request` statuses remain separate.
- `/approvals resolve` must refuse unsupported approvals without calling storage-level `ResolveApproval`.
- Non-pending approvals must not be resolved again.
- Deny receipts must not print a fake continuation `run=`.
- Shell receipts use `approval=<id>`, not `approval_id=<id>`.
- Social Copilot approval-ready drafts remain on `/memory resolve`, not `/approvals`.

**Architecture:** Add a small runtime approval service that reads approval detail and delegates mutation only to registered workflow resolvers. Add a CLI overview read model and renderer that compose existing projection, registry, memory, health, and approval state into canonical lanes. Wire `/overview`, `/approvals show`, and `/approvals resolve` through the existing REPL and command help, with real binary proof in an isolated `ODIN_ROOT`.

**Tech Stack:** Go, existing REPL shell, SQLite store, runtime projections, registry snapshots, existing render package, integration tests with `./bin/odin`.

---

### Task 1: Add Approval Resolver Contract and Detail Read Model

**Domain Goal:** Give approval review and resolution one runtime service boundary so the CLI can inspect every `Approval Request` but only mutate approvals with a workflow-owned resolver.

**Domain Rules Enforced:**
- Storage-level `ResolveApproval` is not the operator authorization boundary.
- Unsupported approvals stay visible and immutable through `/approvals resolve`.
- Resolver support is derived from registered workflow contracts, not guessed from shell command text.

**Why this matters:**
- OpenSwarm-style approval buttons are useful only if Odin keeps workflow-specific continuation, denial, and wake-packet semantics intact.

**Files:**
- Create: `internal/runtime/approvals/service.go`
- Create: `internal/runtime/approvals/service_test.go`

**Step 1: Write the failing tests**

```go
func TestServiceDetailsMarkUnsupportedApproval(t *testing.T) {
	ctx := context.Background()
	env := newApprovalTestEnvironment(t)
	approval := env.requestPendingApproval(t, ctx)

	detail, err := approvals.Service{Store: env.Store}.Detail(ctx, approval.ID)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if detail.Approval.ID != approval.ID {
		t.Fatalf("Approval ID = %d, want %d", detail.Approval.ID, approval.ID)
	}
	if detail.ResolverSupport != approvals.ResolverUnsupported {
		t.Fatalf("ResolverSupport = %q, want unsupported", detail.ResolverSupport)
	}
}

func TestResolveUnsupportedApprovalDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	env := newApprovalTestEnvironment(t)
	approval := env.requestPendingApproval(t, ctx)

	result, err := approvals.Service{Store: env.Store}.Resolve(ctx, approvals.ResolveRequest{
		ApprovalID: approval.ID,
		Decision:   approvals.DecisionApprove,
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err == nil {
		t.Fatalf("Resolve() error = nil, want unsupported error")
	}
	if result.Status != approvals.ResolveStatusUnsupported {
		t.Fatalf("Status = %q, want unsupported", result.Status)
	}

	persisted, getErr := env.Store.GetApproval(ctx, approval.ID)
	if getErr != nil {
		t.Fatalf("GetApproval() error = %v", getErr)
	}
	if persisted.Status != "pending" {
		t.Fatalf("persisted status = %q, want pending", persisted.Status)
	}
}

func TestResolveSupportedApprovalDelegatesToRegisteredResolver(t *testing.T) {
	ctx := context.Background()
	env := newApprovalTestEnvironment(t)
	approval := env.requestPendingApproval(t, ctx)
	runID := int64(42)
	resolver := fakeResolver{result: approvals.ResolveResult{
		Status:    approvals.ResolveStatusResolved,
		Result:    approvals.DecisionApprove,
		RunID:     &runID,
		Summary:   "approval granted; continuation started",
		Mutated:   true,
	}}

	result, err := approvals.Service{
		Store:     env.Store,
		Resolvers: []approvals.Resolver{resolver},
	}.Resolve(ctx, approvals.ResolveRequest{
		ApprovalID: approval.ID,
		Decision:   approvals.DecisionApprove,
		DecisionBy: "operator",
		Reason:     "final confirmation",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.RunID == nil || *result.RunID != runID {
		t.Fatalf("RunID = %v, want %d", result.RunID, runID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/approvals -run 'Test(ServiceDetailsMarkUnsupportedApproval|ResolveUnsupportedApprovalDoesNotMutate|ResolveSupportedApprovalDelegatesToRegisteredResolver)' -v`

Expected: FAIL because `internal/runtime/approvals` does not exist.

**Step 3: Write minimal implementation**

Create:

```go
package approvals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type ResolverSupport string

const (
	ResolverSupported   ResolverSupport = "supported"
	ResolverUnsupported ResolverSupport = "unsupported"
)

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionDeny    Decision = "deny"
)

type ResolveStatus string

const (
	ResolveStatusResolved    ResolveStatus = "resolved"
	ResolveStatusUnsupported ResolveStatus = "unsupported"
	ResolveStatusRejected    ResolveStatus = "rejected"
)

var ErrUnsupported = errors.New("approval has no registered resolver")

type Service struct {
	Store     *sqlite.Store
	Resolvers []Resolver
}

type Detail struct {
	Approval        sqlite.Approval
	Task            sqlite.Task
	Run             *sqlite.Run
	ResolverSupport ResolverSupport
}

type ResolveRequest struct {
	ApprovalID int64
	Decision   Decision
	DecisionBy string
	Reason     string
}

type ResolveResult struct {
	Status  ResolveStatus
	Result  Decision
	RunID   *int64
	Summary string
	Mutated bool
}

type Resolver interface {
	Supports(ctx context.Context, detail Detail) bool
	Resolve(ctx context.Context, detail Detail, request ResolveRequest) (ResolveResult, error)
}

func (service Service) Detail(ctx context.Context, approvalID int64) (Detail, error) {
	approval, err := service.Store.GetApproval(ctx, approvalID)
	if err != nil {
		return Detail{}, err
	}
	task, err := service.Store.GetTask(ctx, approval.TaskID)
	if err != nil {
		return Detail{}, err
	}
	var run *sqlite.Run
	if approval.RunID != nil {
		loaded, err := service.Store.GetRun(ctx, *approval.RunID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Detail{}, err
		}
		if err == nil {
			run = &loaded
		}
	}
	detail := Detail{Approval: approval, Task: task, Run: run, ResolverSupport: ResolverUnsupported}
	if service.resolverFor(ctx, detail) != nil {
		detail.ResolverSupport = ResolverSupported
	}
	return detail, nil
}

func (service Service) Resolve(ctx context.Context, request ResolveRequest) (ResolveResult, error) {
	detail, err := service.Detail(ctx, request.ApprovalID)
	if err != nil {
		return ResolveResult{}, err
	}
	if detail.Approval.Status != "pending" {
		return ResolveResult{Status: ResolveStatusRejected, Result: request.Decision, Summary: "approval is not pending"}, fmt.Errorf("approval %d is not pending", request.ApprovalID)
	}
	resolver := service.resolverFor(ctx, detail)
	if resolver == nil {
		return ResolveResult{Status: ResolveStatusUnsupported, Result: request.Decision, Summary: "approval has no registered resolver; inspect only"}, ErrUnsupported
	}
	return resolver.Resolve(ctx, detail, request)
}

func (service Service) resolverFor(ctx context.Context, detail Detail) Resolver {
	for _, resolver := range service.Resolvers {
		if resolver.Supports(ctx, detail) {
			return resolver
		}
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/approvals -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/approvals/service.go internal/runtime/approvals/service_test.go
git commit -m "feat(runtime): add scoped approval resolver contract"
```

### Task 2: Build the Canonical Overview Read Model

**Domain Goal:** Compose a single read-only overview model from current Odin authority while showing approval resolver support honestly.

**Domain Rules Enforced:**
- `/overview` reads existing authority and does not create new state.
- Managed projects render as `Initiatives` for the operator surface while storage-era project tables remain unchanged.
- Approval lane is a side lane and does not replace `Workspace -> Initiative -> Work Item`.

**Why this matters:**
- This is the Odin-native version of OpenSwarm's mission-control screen: dense status, clear handles, and no parallel dashboard authority.

**Files:**
- Create: `internal/cli/overview/service.go`
- Create: `internal/cli/overview/service_test.go`

**Step 1: Write the failing test**

```go
func TestBuildReturnsCanonicalOverviewWithApprovalLane(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)
	approval := env.requestPendingApproval(t, ctx)

	view, err := overview.Service{
		Store:           env.Store,
		Registry:        env.Registry,
		RegistrySnapshot: env.RegistrySnapshot,
		Approvals: approvals.Service{Store: env.Store},
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if view.Workspace.Label == "" {
		t.Fatalf("Workspace label is empty")
	}
	if len(view.Initiatives) == 0 {
		t.Fatalf("Initiatives len = 0, want managed-project initiatives")
	}
	if view.Approvals.PendingCount != 1 {
		t.Fatalf("PendingCount = %d, want 1", view.Approvals.PendingCount)
	}
	if got := view.Approvals.Items[0].ApprovalID; got != approval.ID {
		t.Fatalf("ApprovalID = %d, want %d", got, approval.ID)
	}
	if got := view.Approvals.Items[0].ResolverSupport; got != overview.ResolverUnsupported {
		t.Fatalf("ResolverSupport = %q, want unsupported", got)
	}
	if view.IntakeInbox.Wiring != overview.WiringNotYetWired {
		t.Fatalf("Intake wiring = %q, want not_yet_wired", view.IntakeInbox.Wiring)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/overview -run TestBuildReturnsCanonicalOverviewWithApprovalLane -v`

Expected: FAIL because the overview package does not exist.

**Step 3: Write minimal implementation**

Implementation notes:

- Reuse `projections.ListProjectPortfolioViews`, `ListTaskStatusViews`, `ListPendingApprovalViews`, `ListActiveRunViews`, `ListIncidentViews`, `ListRecoveryViews`, and `ListFreshnessViews`.
- Call `runtime/approvals.Service.Detail` for each pending approval to mark resolver support.
- Use explicit lane wiring values: `live`, `catalog_backed`, `not_yet_wired`.
- Keep `Run Attempts` nested or mirrored in observability; do not make a run-monitor landing page.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/overview -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/overview/service.go internal/cli/overview/service_test.go
git commit -m "feat(cli): compose canonical overview read model"
```

### Task 3: Render `/overview` and Expose the Read-Only Shell Command

**Domain Goal:** Give operators one real Odin command for the canonical workbench board.

**Domain Rules Enforced:**
- The board uses canonical labels: `Workspace`, `Initiatives`, `Work Items`, `Run Attempts`, `Approvals`, `Observability`, `Memory`, `Intake Inbox`, `Companions`, `Capability Catalog`, and `Automation Triggers`.
- No top-level `Processes` lane.
- `/overview` is read-only.

**Why this matters:**
- A contract-only TUI does not help operators. The surface must be reachable through the real shell.

**Files:**
- Create: `internal/cli/render/overview.go`
- Create: `internal/cli/render/overview_test.go`
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

```go
func TestRenderOverviewUsesCanonicalLanes(t *testing.T) {
	rendered := render.RenderOverview(sampleOverviewView())
	for _, want := range []string{
		"Workspace",
		"Initiatives",
		"Work Items",
		"Run Attempts",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Companions",
		"Capability Catalog",
		"Automation Triggers",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderOverview() missing %q in %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "Processes") {
		t.Fatalf("RenderOverview() must not introduce Processes lane: %q", rendered)
	}
}

func TestShellOverviewCommandRendersCanonicalBoard(t *testing.T) {
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/overview", &output); err != nil {
		t.Fatalf("HandleLine(/overview) error = %v", err)
	}
	if !strings.Contains(output.String(), "Workspace") || !strings.Contains(output.String(), "Approvals") {
		t.Fatalf("output = %q, want canonical overview", output.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/render ./internal/cli/commands ./internal/cli/repl -run 'Test(RenderOverviewUsesCanonicalLanes|ShellOverviewCommandRendersCanonicalBoard|.*Overview.*Help|.*Intent.*Overview)' -v`

Expected: FAIL because `/overview` is not wired.

**Step 3: Write minimal implementation**

Implementation notes:

- Add `IntentOverview` to `internal/cli/commands/commands.go`.
- Add `/overview` to `ShellCommandSummary` and `OperatorHelp`.
- Route ask-mode phrases like `show overview` and `show workspace overview` to the same board.
- Add `overview overview.Service` to `repl.Shell`, constructed from existing `Environment`.
- Add `handleOverview(ctx, output)` that builds the view and calls `render.RenderOverview`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/render ./internal/cli/commands ./internal/cli/repl -run 'Test(RenderOverviewUsesCanonicalLanes|ShellOverviewCommandRendersCanonicalBoard|.*Overview.*Help|.*Intent.*Overview)' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/render/overview.go internal/cli/render/overview_test.go internal/cli/commands/commands.go internal/cli/commands/commands_test.go internal/cli/commands/help.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "feat(cli): add canonical overview operator surface"
```

### Task 4: Upgrade `/approvals` List and Detail

**Domain Goal:** Make pending approval review practical without duplicating run evidence or turning approvals into the primary landing surface.

**Domain Rules Enforced:**
- List output uses `approval=<id>` and stable handles.
- Detail output points to existing evidence surfaces such as `/runs show <run-id>`.
- Resolver support is visible before the operator attempts mutation.

**Why this matters:**
- OpenSwarm's strongest transferable feature is unified human-in-the-loop visibility. Odin needs the same visibility while preserving its governance objects.

**Files:**
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `docs/contracts/tui-overview.md`

**Step 1: Write the failing tests**

```go
func TestShellApprovalsListsHandlesAndResolverSupport(t *testing.T) {
	env := newTestEnvironment(t)
	approval := seedPendingApproval(t, context.Background(), env.Store)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/approvals", &output); err != nil {
		t.Fatalf("HandleLine(/approvals) error = %v", err)
	}

	for _, want := range []string{
		fmt.Sprintf("approval=%d", approval.ID),
		"status=pending",
		"resolver=unsupported",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, missing %q", output.String(), want)
		}
	}
}

func TestShellApprovalsShowIncludesEvidencePointer(t *testing.T) {
	env := newTestEnvironment(t)
	approval := seedPendingApproval(t, context.Background(), env.Store)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), fmt.Sprintf("/approvals show %d", approval.ID), &output); err != nil {
		t.Fatalf("HandleLine(/approvals show) error = %v", err)
	}
	if !strings.Contains(output.String(), fmt.Sprintf("approval=%d", approval.ID)) {
		t.Fatalf("output = %q, want approval handle", output.String())
	}
	if !strings.Contains(output.String(), "evidence=/runs show") {
		t.Fatalf("output = %q, want run evidence pointer", output.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl -run 'TestShellApprovals(ListsHandlesAndResolverSupport|ShowIncludesEvidencePointer)' -v`

Expected: FAIL because `/approvals` is list-only and has no `show`.

**Step 3: Write minimal implementation**

Implementation notes:

- Parse `/approvals` with no args as list.
- Parse `/approvals show <id>` as detail.
- Use `runtime/approvals.Service` for detail and resolver support.
- Keep no-approval output as `no approvals waiting`.
- Update `ApprovalsUsage` in help to `/approvals [show <id>|resolve <id> approve|deny because <reason...>]`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/repl ./internal/cli/commands -run 'TestShellApprovals|Test.*Approvals.*Help' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/help.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go docs/contracts/tui-overview.md
git commit -m "feat(cli): improve approval review surface"
```

### Task 5: Wire Scoped `/approvals resolve`

**Domain Goal:** Let operators resolve only approvals backed by a registered safe resolver, with unsupported approvals refusing mutation.

**Domain Rules Enforced:**
- Unsupported approvals remain pending.
- Non-pending approvals cannot be resolved again.
- Approve may return a continuation run from the resolver.
- Deny does not print `run=`.
- CLI routes intent; workflow resolvers own mutation.

**Why this matters:**
- This is the human-in-the-loop action point. It must feel direct without becoming unsafe generic mutation.

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/runtime/approvals/service.go`
- Modify: `internal/runtime/approvals/service_test.go`

**Step 1: Write the failing tests**

```go
func TestShellApprovalsResolveUnsupportedDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	approval := seedPendingApproval(t, ctx, env.Store)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	err = shell.HandleLine(ctx, fmt.Sprintf("/approvals resolve %d approve because final confirmation", approval.ID), &output)
	if err != nil {
		t.Fatalf("HandleLine(/approvals resolve) error = %v", err)
	}
	if !strings.Contains(output.String(), "status=unsupported") {
		t.Fatalf("output = %q, want unsupported receipt", output.String())
	}

	persisted, err := env.Store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if persisted.Status != "pending" {
		t.Fatalf("status = %q, want pending", persisted.Status)
	}
}

func TestShellApprovalsResolveRequiresBecauseReason(t *testing.T) {
	env := newTestEnvironment(t)
	approval := seedPendingApproval(t, context.Background(), env.Store)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), fmt.Sprintf("/approvals resolve %d approve", approval.ID), &output); err != nil {
		t.Fatalf("HandleLine(/approvals resolve) error = %v", err)
	}
	if !strings.Contains(output.String(), "usage=/approvals resolve <id> approve|deny because <reason...>") {
		t.Fatalf("output = %q, want usage", output.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/approvals ./internal/cli/repl -run 'Test.*ApprovalsResolve' -v`

Expected: FAIL because shell resolve is not wired.

**Step 3: Write minimal implementation**

Implementation notes:

- Accept only `approve` and `deny` decision tokens.
- Require `because <reason...>`.
- Map `approve` to resolver decision `approve`, receipt result `approved`.
- Map `deny` to resolver decision `deny`, receipt result `denied`.
- On `approvals.ErrUnsupported`, print:

```text
approval=<id> status=unsupported result=not_resolved
summary=approval has no registered resolver; inspect only
```

- For supported results, format exactly:

```text
approval=<id> status=resolved result=approved run=<run-id>
summary=<resolver summary>
```

or:

```text
approval=<id> status=resolved result=denied
summary=<resolver summary>
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/approvals ./internal/cli/repl -run 'Test.*ApprovalsResolve' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/approvals/service.go internal/runtime/approvals/service_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "feat(cli): add scoped approval resolve command"
```

### Task 6: Prove Through the Real Odin Command Path

**Domain Goal:** Verify the operator workbench through the repo-owned binary, not just package tests.

**Domain Rules Enforced:**
- Real shell help includes `/overview` and expanded `/approvals`.
- Real `/overview` renders canonical lanes.
- Real `/approvals resolve` refuses unsupported approvals without mutating SQLite.
- Real shell proof uses isolated runtime state.

**Why this matters:**
- Odin operator surfaces are incomplete unless the actual `./bin/odin` path works.

**Files:**
- Create: `tests/integration/operator_overview_approvals_test.go`
- Modify: `README.md` only if the command surface is intentionally documented there during execution.

**Step 1: Write the failing integration test**

```go
func TestRealOdinOverviewAndUnsupportedApprovalResolve(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "odin.db")
	seedRuntimeWithPendingApproval(t, dbPath)

	cmd := exec.Command(filepath.Join(repoRoot(t), "bin", "odin"))
	cmd.Env = append(os.Environ(), "ODIN_ROOT="+root)
	cmd.Stdin = strings.NewReader(strings.Join([]string{
		"/overview",
		"/approvals",
		"/approvals show 1",
		"/approvals resolve 1 approve because final confirmation",
		"/approvals show 1",
		"/quit",
	}, "\n") + "\n")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("odin shell error = %v\n%s", err, output)
	}

	for _, want := range []string{
		"Workspace",
		"Approvals",
		"approval=1",
		"status=unsupported",
		"result=not_resolved",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestRealOdinOverviewAndUnsupportedApprovalResolve -v`

Expected: FAIL until the binary is rebuilt and the shell surfaces are wired.

**Step 3: Build and run targeted package tests**

Run:

```bash
go test ./internal/runtime/approvals ./internal/cli/overview ./internal/cli/render ./internal/cli/commands ./internal/cli/repl -run 'Test.*(Approval|Approvals|Overview)' -v
go build -o ./bin/odin ./cmd/odin
go test ./tests/integration -run TestRealOdinOverviewAndUnsupportedApprovalResolve -v
```

Expected: PASS.

**Step 4: Run manual real-shell smoke**

Run:

```bash
tmpdir="$(mktemp -d)"
ODIN_ROOT="$tmpdir" ./bin/odin <<'EOF'
/overview
/approvals
/quit
EOF
```

Expected:

- `/overview` renders canonical lanes.
- `/approvals` prints `no approvals waiting` on a fresh runtime.
- Prompt still shows `health=healthy`.

**Step 5: Commit**

```bash
git add tests/integration/operator_overview_approvals_test.go README.md
git commit -m "test(integration): prove overview approvals through real odin"
```

## Review Checklist

- Domain naming matches `CONTEXT.md`: `Workspace`, `Initiative`, `Work Item`, `Run Attempt`, `Approval Request`.
- `/overview` is proven read-only by tests that build/render without writes.
- Unsupported approval resolution is tested at service, shell, and real binary levels.
- Non-pending approval protection is tested in `internal/runtime/approvals`.
- Deny receipt omits `run=`.
- `approval=<id>` shell receipt style is used.
- OpenSwarm-inspired behavior is presentation and operator ergonomics only.
- ADR 0001 is honored: SQLite remains the only runtime authority.
- ADR 0002 is honored: OpenSwarm is reference input, not copied runtime architecture.
- Social Copilot remains on `/memory resolve` for social drafts.
- New package boundaries are explicit: CLI presents, runtime approval service delegates, workflow resolvers mutate.

## Execution Handoff

Domain artifacts used:

- `CONTEXT.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/workspace-context-map.md`
- `docs/contracts/repo-layout.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/adr/0002-migration-policy.md`
- `docs/plans/2026-04-24-openswarm-inspired-tui-approvals-design.md`

Reused components:

- existing REPL shell and command parser
- existing render package
- existing SQLite approval storage and lifecycle events
- existing projection functions
- existing registry snapshots and managed-project registry
- existing real `./bin/odin` verification path

New components proposed:

- `internal/runtime/approvals` resolver contract and approval detail/read service
- `internal/cli/overview` canonical overview read model
- `internal/cli/render/overview.go` board renderer
- `tests/integration/operator_overview_approvals_test.go`

Why new components are necessary:

- The CLI needs a workflow-safe boundary for approval resolution instead of calling `ResolveApproval` directly.
- The overview needs one composition layer that translates current project/task/run storage into canonical operator language.
- The renderer keeps presentation in `internal/cli/render` instead of mixing UI formatting into runtime services.
- Real binary integration proof is required for operator-surface confidence.

Invariants and boundary checks that must be proven:

- unsupported approvals stay pending after `/approvals resolve`
- non-pending approvals cannot be resolved again
- denial does not create or print a continuation run
- `/overview` does not mutate runtime state
- canonical labels appear and `Processes` does not
- shell help and real binary output match the documented command surface

Open blockers:

- No production workflow resolver is in scope for this plan unless an existing workflow registers one during execution. The first shipped behavior may expose scoped resolve and safely refuse unsupported approvals until transfer or another workflow adds a resolver.

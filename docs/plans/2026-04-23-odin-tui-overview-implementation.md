# Odin TUI Overview Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a canonical-first Odin overview surface that shows `Workspace -> Initiative -> Work Item -> Run Attempt` plus the approved side lanes without inventing new runtime authority.

**Domain Source of Truth:** `docs/contracts/tui-overview.md`, `docs/contracts/workspace-context-map.md`, `docs/contracts/repo-layout.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`

**Context:** Operator interface in `internal/cli`, projecting current runtime and registry truth into the canonical TUI overview.

**Owns / Does Not Own:** This slice owns CLI overview composition, rendering, and operator entrypoints. It does not own new runtime persistence for `Initiative`, `Companion`, `Intake Item`, or `Automation Trigger`, and it must not rename storage-era `projects`, `tasks`, or `runs` tables in this pass.

**Invariants:**
- Primary navigation remains `Workspace -> Initiative -> Work Item`, with `Run Attempts` nested by default.
- `project`, `task`, `run`, and `agent` remain compatibility aliases only; canonical labels must appear in the overview output.
- `Companions` must be clearly distinguished from registry `Agent Definitions` and from runtime worker/delegation execution.
- `process` must not appear as a first-class lane; the overview must speak `Automation Triggers` instead.
- `Intake Inbox` and `Automation Triggers` must be rendered as explicitly unwired or placeholder lanes until runtime authority exists.
- Existing `odin workspace ...` commands continue to mean project Codex workspace lifecycle and must not be overloaded with business-workspace semantics.

**Architecture:** Add one CLI-owned overview composition service that reads from existing runtime projections, memory services, health checks, and registry snapshots, then renders a canonical overview board through the existing render package. Expose the board through a new read-only REPL `/overview` entrypoint instead of overloading `odin workspace`, and keep the current runtime/storage vocabulary behind a translation layer rather than renaming persistence. On the clean base, reuse the existing workspace, initiative, and companion projections directly, and use explicit lane wiring states so the overview can show truthful placeholders only where first-class runtime models are still not live, namely `Intake Inbox` and `Automation Triggers`.

**Tech Stack:** Go, existing REPL shell, `internal/runtime/projections`, `internal/runtime/health`, `internal/memory/knowledge`, registry snapshots, SQLite-backed integration tests

---

### Task 1: Build the Canonical Overview Read Model

**Domain Goal:** Compose a single overview model from current Odin runtime truth without inventing new aggregates or renaming storage-era authority.

**Domain Rules Enforced:**
- Managed projects are rendered as `Initiatives` in this slice, but `projects` stay the runtime authority.
- Existing workspace and companion projections are reused as live overview truth on this base.
- `Intake Inbox` and `Automation Triggers` must report `not_yet_wired` rather than guessed counts.

**Why this matters:**
- The overview is only useful if it tells the truth about what exists now and what is still staged for future runtime work.

**Files:**
- Create: `internal/cli/overview/service.go`
- Create: `internal/cli/overview/service_test.go`

**Step 1: Write the failing test**

```go
func TestBuildReturnsCanonicalOverviewFromCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	view, err := Service{
		Store:            env.Store,
		RegistrySnapshot: env.RegistrySnapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(view.Initiatives) == 0 {
		t.Fatalf("Initiatives len = 0, want managed-project initiatives")
	}
	if view.Companions.Wiring != WiringLive {
		t.Fatalf("Companions wiring = %q, want live", view.Companions.Wiring)
	}
	if view.IntakeInbox.Wiring != WiringNotYetWired {
		t.Fatalf("Intake wiring = %q, want not-yet-wired", view.IntakeInbox.Wiring)
	}
	if view.AutomationTriggers.Wiring != WiringNotYetWired {
		t.Fatalf("Automation wiring = %q, want not-yet-wired", view.AutomationTriggers.Wiring)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/overview -run 'TestBuildReturnsCanonicalOverviewFromCurrentAuthority' -v`
Expected: FAIL because the overview composition service does not exist yet.

**Step 3: Write minimal implementation**

```go
type Service struct {
	Store            *sqlite.Store
	RegistrySnapshot registry.Snapshot
	Health           healthsvc.Service
}

func (service Service) Build(ctx context.Context, resolved scope.Resolution) (View, error) {
	// Translate existing project/task/run/read-model truth into canonical overview lanes.
}
```

Implementation notes:
- Reuse existing runtime truth before adding queries:
  - `projections.GetWorkspaceOverviewView` for workspace summary
  - `projections.ListInitiativePortfolioViews` for managed-project initiative inventory
  - `projections.ListCompanionAssignmentViews` for live companion inventory
  - `projections.ListTaskStatusViews` for work-item summaries
  - `projections.ListPendingApprovalViews`, `ListActiveRunViews`, `ListBlockedItemViews`, `ListIncidentViews`, `ListRecoveryViews`, and `ListFreshnessViews` for governance and observability
  - `knowledgememory.Service` for scoped memory counts or recent summaries
  - `registry.Snapshot` for capability-catalog sections
- Add explicit lane wiring fields such as `live`, `catalog_backed`, and `not_yet_wired` so the UI can be honest without fake persistence.
- Keep `Companions` separated from registry `Agent Definitions` and worker history even though all three surfaces may appear in the same overview.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/overview -run 'TestBuildReturnsCanonicalOverviewFromCurrentAuthority' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/overview/service.go internal/cli/overview/service_test.go
git commit -m "feat(cli): add canonical overview composition service"
```

### Task 2: Render the Overview Board with Canonical Lanes

**Domain Goal:** Render a readable TUI-style board that uses canonical Odin language even while the backing authority still uses storage-era words.

**Domain Rules Enforced:**
- Headings must use `Workspace`, `Initiative`, `Work Item`, `Run Attempts`, `Companions`, `Capability Catalog`, `Approvals`, `Observability`, `Memory`, `Intake Inbox`, and `Automation Triggers`.
- Compatibility aliases may appear only as explanatory secondary text.
- Do not render a top-level `Processes` lane.

**Why this matters:**
- The domain decision only becomes real for operators once the surface uses the locked terms consistently.

**Files:**
- Create: `internal/cli/render/overview.go`
- Create: `internal/cli/render/overview_test.go`

**Step 1: Write the failing test**

```go
func TestRenderOverviewUsesCanonicalLanes(t *testing.T) {
	rendered := RenderOverview(sampleOverview())

	for _, want := range []string{
		"Workspace",
		"Initiatives",
		"Work Items",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Automation Triggers",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderOverview() missing %q in %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "Processes") {
		t.Fatalf("RenderOverview() = %q, must not introduce Processes lane", rendered)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/render -run 'TestRenderOverviewUsesCanonicalLanes' -v`
Expected: FAIL because the overview renderer does not exist yet.

**Step 3: Write minimal implementation**

```go
func RenderOverview(view overview.View) string {
	var b strings.Builder
	// Render canonical lane headings, scoped summaries, and explicit lane wiring notes.
	return b.String()
}
```

Implementation notes:
- Keep the render output compact and scan-friendly. This is a terminal board, not a Markdown report.
- Include explicit compatibility notes such as `project -> initiative`, `run -> run attempt`, and `agent -> agent definition/worker alias` only where they reduce operator confusion.
- For non-live lanes, render a short truth-preserving note like `status=not_yet_wired source=runtime_authority_missing`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/render -run 'TestRenderOverviewUsesCanonicalLanes' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/render/overview.go internal/cli/render/overview_test.go
git commit -m "feat(cli): render canonical overview board"
```

### Task 3: Add the Read-Only `/overview` Operator Surface

**Domain Goal:** Expose the overview through the real interactive Odin shell without colliding with the existing `workspace` command family that already manages project Codex workspaces.

**Domain Rules Enforced:**
- `/overview` is read-only and canonical-first.
- Existing `odin workspace ...` transport semantics stay intact.
- Ask-mode requests like “show overview” should use the same overview surface instead of going through an executor.

**Why this matters:**
- Operators need one actual entrypoint for the overview, not just a render helper hidden inside tests.

**Files:**
- Modify: `docs/contracts/tui-overview.md`
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

```go
func TestShellHelpIncludesTransitionCommands(t *testing.T) {
	if !strings.Contains(output.String(), "/overview") {
		t.Fatalf("shell help missing /overview")
	}
}

func TestRouteAskIntentRecognizesOverviewRequests(t *testing.T) {
	if got := RouteAskIntent("show workspace overview"); got != IntentOverview {
		t.Fatalf("RouteAskIntent() = %q, want %q", got, IntentOverview)
	}
}

func TestShellOverviewRendersCanonicalBoard(t *testing.T) {
	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/overview", &output); err != nil {
		t.Fatalf("HandleLine(/overview) error = %v", err)
	}
	if !strings.Contains(output.String(), "Workspace") {
		t.Fatalf("output = %q, want canonical overview", output.String())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/cli/repl -run 'Test(RouteAskIntent|ShellHelpIncludesTransitionCommands|ShellOverviewRendersCanonicalBoard)' -v`
Expected: FAIL because `/overview` is not a live shell surface yet.

**Step 3: Write minimal implementation**

```go
const ShellCommandSummary = "/help /mode /scope /project /overview ..."

case "overview":
	return shell.handleOverview(ctx, output)
```

Implementation notes:
- Add `IntentOverview` to `commands.go` and route ask-mode overview requests to the same board.
- Keep `/overview` in the REPL transport layer specifically because `odin workspace` already means Codex-workspace lifecycle.
- Update `docs/contracts/tui-overview.md` to record `/overview` as the v1 operator entrypoint for the canonical board, while keeping the compatibility-alias section intact.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/commands ./internal/cli/repl -run 'Test(RouteAskIntent|ShellHelpIncludesTransitionCommands|ShellOverviewRendersCanonicalBoard)' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/tui-overview.md internal/cli/commands/commands.go internal/cli/commands/commands_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go
git commit -m "feat(cli): add overview operator surface"
```

### Task 4: Prove the Overview Through the Real `odin` Shell Path

**Domain Goal:** Verify the overview through the repo-owned binary and interactive shell path, not only through unit tests or direct DB assertions.

**Domain Rules Enforced:**
- Verification must exercise the real `odin` shell path.
- Output must use canonical overview labels even when the workflow still relies on `project`, `task`, `run`, and `agent` under the hood.
- The integration proof must confirm that unwired lanes are surfaced honestly rather than hidden.

**Why this matters:**
- This feature is an operator surface. It is not complete until the real shell path proves it.

**Files:**
- Create: `tests/integration/operator_overview_test.go`

**Step 1: Write the failing integration test**

```go
func TestOperatorOverviewUsesCanonicalBoard(t *testing.T) {
	repoRoot := projectRoot(t)
	binaryPath := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	output, err := runOdinCommandInDir(
		t,
		repoRoot,
		binaryPath,
		runtimeRoot,
		nil,
		"/project pbs\n/overview\n/quit\n",
	)
	if err != nil {
		t.Fatalf("interactive overview error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "Workspace") || strings.Contains(output, "Processes") {
		t.Fatalf("overview output = %q, want canonical board with no Processes lane", output)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard' -v`
Expected: FAIL because the live shell cannot render `/overview` yet.

**Step 3: Write minimal implementation**

```go
// No new implementation file in this task.
// Reuse the Task 1-3 overview service, renderer, and shell entrypoint through the real binary.
```

Implementation notes:
- Reuse `tests/integration/helpers_test.go` helpers; do not add a second binary-run harness.
- Assert at least:
  - canonical headings render
  - the current managed project appears as an `Initiative`
  - companion/capability sections render from registry truth
  - unwired `Intake Inbox` and `Automation Triggers` lanes say they are unwired

**Step 4: Run test to verify it passes**

Run: `go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/operator_overview_test.go
git commit -m "test(integration): prove overview through real odin shell"
```

### Task 5: Final Verification and Worktree Review

**Domain Goal:** Prove the overview is reachable from the real operator surface and that help, docs, and tests all tell the same story.

**Domain Rules Enforced:**
- Real operator-path proof is mandatory.
- Overview language must remain canonical-first even while transport aliases survive.
- No hidden second control plane.

**Why this matters:**
- This is the point where the plan protects against a technically working but operator-confusing surface.

**Files:**
- Modify: `README.md` only if the implementation introduces or documents a canonical user-facing entrypoint that should appear in the repo overview

**Step 1: Run focused package verification**

Run: `go test ./internal/cli/overview ./internal/cli/render ./internal/cli/commands ./internal/cli/repl -run 'Test(.*Overview|.*Canonical|.*Help)' -v`
Expected: PASS

**Step 2: Run the integration proof**

Run: `go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard' -v`
Expected: PASS

**Step 3: Build and verify the live shell surface**

Run: `go build -o ./bin/odin ./cmd/odin`
Expected: PASS

Run: `./bin/odin --help | rg '/overview'`
Expected: one help line that includes `/overview`

**Step 4: Review worktree status**

Run: `git status --short --branch`
Expected: only the overview implementation, matching tests, and aligned docs are present in the implementation branch

**Step 5: Commit**

```bash
git add README.md docs/plans/2026-04-23-odin-tui-overview-implementation.md
git commit -m "chore(cli): finalize overview verification"
```

## Review Checklist

- domain naming matches `docs/contracts/tui-overview.md`
- primary navigation remains `Workspace -> Initiative -> Work Item`
- `Run Attempts` are nested in work detail, not promoted to the primary landing object
- `Companions` are separated from `Agent Definitions` and worker execution
- `Intake Inbox` and `Automation Triggers` are honest about missing runtime authority
- `process` is absent as a first-class lane
- `odin workspace` semantics remain untouched
- reused repo structures are explicit: `internal/cli/render`, `internal/cli/repl`, `internal/runtime/projections`, `internal/runtime/health`, `internal/memory/knowledge`, registry snapshots, existing integration helpers
- boundary crossings are explicit and justified
- real `odin` shell verification is included before completion

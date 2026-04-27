# Odin Swarm Portal Delivery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a truthful operator-visible swarm lane to `odin-os` and use it immediately to deliver the `admin-cfi` and `student` CFIPros portal tracks with auditable sub-agent execution, dynamic skill telemetry, persistent memory, and real learning output.

**Architecture:** Reuse existing tasks, runs, transcripts, memory summaries, learning services, registry agents, broker expansion, and context packets. Add store/runtime plumbing for the existing `delegations` schema, expose agents through a real `/agent` command, extend run detail to show delegation and learning evidence, and drive the first real swarm through `portal-delivery-agent` plus child worker tasks.

**Tech Stack:** Go 1.25, SQLite, existing `internal/runtime/*`, existing registry markdown under `registry/agents`, existing shell REPL, existing `codex_headless` driver, existing learning and memory packages, existing CFIPros Odin project path.

---

## Preconditions

- Use [2026-04-18-odin-swarm-portal-delivery-design.md](/home/orchestrator/odin-os/docs/plans/2026-04-18-odin-swarm-portal-delivery-design.md) as the design authority for this plan.
- Do not introduce a second execution queue, second registry, or swarm-only memory store.
- Keep orchestration to one parent -> child layer for this pass.
- Keep all CFIPros work executed through Odin CLI only.
- Run real `odin` verification commands from `/home/orchestrator/odin-os` until the repo-root discovery defect is fixed.

### Task 1: Lock the swarm operator contract in docs and failing integration tests

**Files:**
- Create: `docs/contracts/agent-swarm-delivery.md`
- Create: `tests/integration/agent_swarm_delivery_test.go`
- Modify: `internal/cli/commands/help.go`

**Step 1: Write the failing tests**

```go
func TestAgentCommandAppearsInInteractiveHelp(t *testing.T) {
	shell := newTestShell(t)
	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/help", &output); err != nil {
		t.Fatalf("HandleLine(/help) error = %v", err)
	}
	if !strings.Contains(output.String(), "/agent") {
		t.Fatalf("help output = %q, want /agent usage", output.String())
	}
}

func TestPortalDeliveryAgentRunCreatesDelegationsAndChildRuns(t *testing.T) {
	driver := writeFixtureCodexDriver(t)
	stdout := runInteractiveOdin(t, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, []string{
		"/project cfipros",
		"/agent run portal-delivery-agent portal_track=admin-cfi surface=dashboard goal=deliver-admin-dashboard",
		"/runs show active",
	})

	for _, want := range []string{
		"run=",
		"delegation=",
		"effective_skill=",
		"memory=",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration ./internal/cli/repl -run 'Test(AgentCommandAppearsInInteractiveHelp|PortalDeliveryAgentRunCreatesDelegationsAndChildRuns)' -count=1`

Expected: FAIL because `/agent` does not exist, delegations are not wired, and run detail cannot show swarm evidence.

**Step 3: Write the minimal implementation**

Create `docs/contracts/agent-swarm-delivery.md` describing:

- `/agent` as the only supported operator entrypoint for agent runs
- parent/child execution authority staying in `tasks` + `runs`
- delegation linkage and artifact recording rules
- dynamic skill telemetry rules
- memory and learning evidence requirements
- the two acceptance portal tracks: `admin-cfi` and `student`

Update `internal/cli/commands/help.go` so the interactive help contract includes `/agent [list|show <key>|run <key> [input=value...]]`.

**Step 4: Run the tests again to verify they still fail only on runtime behavior**

Run: `go test ./tests/integration ./internal/cli/repl -run 'Test(AgentCommandAppearsInInteractiveHelp|PortalDeliveryAgentRunCreatesDelegationsAndChildRuns)' -count=1`

Expected: FAIL because the runtime wiring is still missing, not because the contract or usage text is absent.

**Step 5: Commit**

```bash
git add docs/contracts/agent-swarm-delivery.md tests/integration/agent_swarm_delivery_test.go internal/cli/commands/help.go
git commit -m "test: lock agent swarm delivery contract"
```

### Task 2: Add SQLite delegation CRUD over the existing schema

**Files:**
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/store/sqlite/delegations_test.go`

**Step 1: Write the failing tests**

```go
func TestDelegationCrudAndArtifacts(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project, parentTask, parentRun := seedProjectTaskRun(t, ctx, store)

	delegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		ProjectID:       project.ID,
		Scope:           "project",
		DelegationKey:   "admin-cfi-design",
		Role:            "design_direction",
		ActionClass:     "design_direction",
		ActionKey:       "portal-track",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "parent_summary",
		ArtifactTarget:  "run_detail",
		Executor:        "codex_headless",
		DetailsJSON:     `{"skill_key":"pixel-perfect-ui-ux-designer"}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation() error = %v", err)
	}

	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  parentTask.ID,
		ChildRunID:   &parentRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask() error = %v", err)
	}

	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "learning_proposal",
		Summary:      "proposal created",
		DetailsJSON:  `{"proposal_id":1}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact() error = %v", err)
	}

	artifacts, err := store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{DelegationID: delegation.ID})
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("ListDelegationArtifacts() = %v, %d; want one artifact", err, len(artifacts))
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/sqlite -run 'TestDelegationCrudAndArtifacts' -count=1`

Expected: FAIL because the delegation schema exists but the store methods do not.

**Step 3: Write the minimal implementation**

Add store methods that map directly onto the existing schema:

```go
func (store *Store) CreateDelegation(ctx context.Context, params CreateDelegationParams) (Delegation, error)
func (store *Store) UpdateDelegationStatus(ctx context.Context, params UpdateDelegationStatusParams) (Delegation, error)
func (store *Store) AttachDelegationChildTask(ctx context.Context, params AttachDelegationChildTaskParams) (Delegation, error)
func (store *Store) AttachDelegationWorktree(ctx context.Context, params AttachDelegationWorktreeParams) (Delegation, error)
func (store *Store) ListDelegations(ctx context.Context, params ListDelegationsParams) ([]Delegation, error)
func (store *Store) CreateDelegationArtifact(ctx context.Context, params CreateDelegationArtifactParams) (DelegationArtifact, error)
func (store *Store) ListDelegationArtifacts(ctx context.Context, params ListDelegationArtifactsParams) ([]DelegationArtifact, error)
```

Rules:

- validate parent task/run/project lineage the same way transcript and memory writes do
- append runtime events for delegation creation/status change and artifact creation
- keep `details_json` and `artifact_type` freeform so the runtime can evolve without schema churn

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/sqlite -run 'TestDelegationCrudAndArtifacts|TestContextPacket' -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/sqlite/store.go internal/store/sqlite/delegations_test.go
git commit -m "feat: add delegation store operations"
```

### Task 3: Build the runtime delegation service and child-task orchestration

**Files:**
- Create: `internal/runtime/delegations/service.go`
- Create: `internal/runtime/delegations/service_test.go`
- Create: `internal/runtime/delegations/templates.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing tests**

```go
func TestPortalDeliveryAgentCreatesParentAndChildWork(t *testing.T) {
	ctx := context.Background()
	env := openDelegationEnv(t)

	parentTask, parentRun, result, err := delegations.Service{
		Store: env.Store,
		Jobs:  env.Jobs,
	}.RunAgent(ctx, delegations.RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "portal-delivery-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "admin-cfi",
			"surface":      "dashboard",
			"goal":         "deliver stronger admin portal dashboard",
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if parentTask.Key == "" || parentRun == nil {
		t.Fatalf("RunAgent() parent output incomplete")
	}
	if len(result.ChildDelegations) < 4 {
		t.Fatalf("child delegations = %d, want >= 4", len(result.ChildDelegations))
	}
}

func TestChildDelegationRecordsSkillTelemetryAndMemory(t *testing.T) {
	ctx := context.Background()
	env := openDelegationEnv(t)

	result, err := env.Delegations.RunAgent(ctx, delegations.RunInput{
		ResolvedScope: scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "cfipros"},
		AgentKey:      "portal-delivery-agent",
		RequestedBy:   "operator",
		Inputs: map[string]string{
			"portal_track": "student",
			"surface":      "dashboard",
			"goal":         "deliver student portal",
		},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}

	foundDesignChild := false
	for _, child := range result.ChildDelegations {
		if child.Role == "design_direction" {
			foundDesignChild = true
		}
	}
	if !foundDesignChild {
		t.Fatalf("expected a design_direction child delegation")
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/delegations ./internal/runtime/jobs -run 'Test(PortalDeliveryAgentCreatesParentAndChildWork|ChildDelegationRecordsSkillTelemetryAndMemory)' -count=1`

Expected: FAIL because no runtime delegation service exists and jobs cannot create agent-requested child tasks.

**Step 3: Write the minimal implementation**

Add a thin runtime service:

```go
type RunInput struct {
	ResolvedScope scope.Resolution
	AgentKey      string
	RequestedBy   string
	Inputs        map[string]string
}

type RunResult struct {
	ParentTask        sqlite.Task
	ParentRun         *sqlite.Run
	ChildDelegations  []sqlite.Delegation
	LearningProposalIDs []int64
}
```

Use a fixed first-pass template map in `templates.go`:

```go
func PortalDeliveryTemplate(inputs map[string]string) []ChildSpec {
	return []ChildSpec{
		{DelegationKey: "ia-audit", Role: "ia_audit", SkillKey: "triage-skill"},
		{DelegationKey: "design-direction", Role: "design_direction", SkillKey: "pixel-perfect-ui-ux-designer"},
		{DelegationKey: "implementation-handoff", Role: "implementation_handoff"},
		{DelegationKey: "visual-verification", Role: "visual_verification", SkillKey: "pixel-perfect-ui-ux-designer"},
		{DelegationKey: "learning-capture", Role: "learning_capture"},
	}
}
```

Extend jobs with a reusable creation path:

```go
type CreateTaskParams struct {
	Resolved    scope.Resolution
	Title       string
	RequestedBy string
}

func (service Service) CreateTask(ctx context.Context, params CreateTaskParams) (sqlite.Task, error)
```

Then make `CreateTaskFromAct()` call `CreateTask(... RequestedBy: "operator")`.

For each child delegation:

- create the delegation row
- create the child task with `RequestedBy: "agent:"+agentKey`
- attach the child task id
- execute the child task through `ExecuteTaskWithRequest`
- set `ExecutionRequest.Metadata`:
  - `agent_key`
  - `delegation_id`
  - `portal_track`
  - `requested_skill`
  - `effective_skill`
  - `skill_source=agent_template`
- create a checkpoint packet whose `SelectedCapabilities` includes `agent:<agent-key>` and `skill:<skill-key>` when present
- create delegation artifacts for:
  - `run_summary`
  - `memory_summary`
  - `learning_proposal` when created

Use existing memory/learning packages rather than direct ad hoc SQL.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/delegations ./internal/runtime/jobs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/delegations/service.go internal/runtime/delegations/service_test.go internal/runtime/delegations/templates.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go
git commit -m "feat: add runtime delegation orchestration"
```

### Task 4: Expose agents through the REPL and surface delegation evidence in run detail

**Files:**
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/runtime/runs/service.go`
- Modify: `internal/runtime/runs/service_test.go`

**Step 1: Write the failing tests**

```go
func TestHandleAgentListShowAndRun(t *testing.T) {
	shell := newDelegationShell(t)
	var output bytes.Buffer

	for _, line := range []string{
		"/agent list",
		"/agent show portal-delivery-agent",
		"/project cfipros",
		"/agent run portal-delivery-agent portal_track=admin-cfi surface=dashboard goal=deliver-admin-dashboard",
	} {
		if err := shell.HandleLine(context.Background(), line, &output); err != nil {
			t.Fatalf("HandleLine(%q) error = %v", line, err)
		}
	}

	for _, want := range []string{"portal-delivery-agent", "role:", "created task", "run="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestRunsShowIncludesDelegationsArtifactsAndSkillTelemetry(t *testing.T) {
	shell := newDelegationShell(t)
	runID := seedCompletedPortalAgentRun(t, shell)
	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), fmt.Sprintf("/runs show %d", runID), &output); err != nil {
		t.Fatalf("HandleLine(/runs show) error = %v", err)
	}
	for _, want := range []string{"delegation=", "artifact=", "effective_skill=", "memory="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("run detail = %q, want %q", output.String(), want)
		}
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/repl ./internal/runtime/runs -run 'Test(HandleAgentListShowAndRun|RunsShowIncludesDelegationsArtifactsAndSkillTelemetry)' -count=1`

Expected: FAIL because the REPL has no `/agent` command and run detail does not load delegation evidence.

**Step 3: Write the minimal implementation**

Add `/agent` support in `shell.go`:

```go
case "agent":
	return shell.handleAgent(ctx, command.Args, output)
```

Behavior:

- `/agent list`:
  - use `shell.newBroker().Catalog(...)`
  - filter `card.Kind == catalog.KindSubAgent`
- `/agent show <key>`:
  - `Expand(key)` and render the `SubAgentDefinition`
- `/agent run <key> [input=value...]`:
  - parse input
  - call `delegations.Service.RunAgent(...)`
  - set `ActiveTask` and `ActiveRun`
  - print parent task/run summary

Extend `runs.Detail` to load:

- delegations where `parent_run_id = run.ID` or `child_run_id = run.ID`
- artifacts for those delegations

Extend `renderRunDetail` to print:

```text
delegation=12 key=design-direction role=design_direction status=completed child_task=...
artifact=33 type=learning_proposal summary=...
effective_skill=pixel-perfect-ui-ux-designer source=agent_template
```

Prefer reading `effective_skill` from transcript tool-summary JSON first; fall back to delegation details JSON when needed.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/repl ./internal/runtime/runs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/help.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/runtime/runs/service.go internal/runtime/runs/service_test.go
git commit -m "feat: expose agents and swarm run detail"
```

### Task 5: Add portal-delivery agent definitions and prove the end-to-end Odin lane

**Files:**
- Create: `registry/agents/portal-delivery-agent.md`
- Create: `registry/agents/portal-worker-agent.md`
- Modify: `tests/integration/agent_swarm_delivery_test.go`

**Step 1: Write the failing integration expectations**

```go
func TestRealOdinAgentLaneForPortalDelivery(t *testing.T) {
	driver := writeFixtureCodexDriver(t)
	stdout := runInteractiveOdin(t, map[string]string{
		"ODIN_CODEX_DRIVER": driver,
	}, []string{
		"/agent list",
		"/agent show portal-delivery-agent",
		"/project cfipros",
		"/agent run portal-delivery-agent portal_track=admin-cfi surface=dashboard goal=deliver-admin-dashboard",
		"/runs show active",
		"/agent run portal-delivery-agent portal_track=student surface=dashboard goal=deliver-student-dashboard",
		"/runs show active",
	})

	for _, want := range []string{
		"portal-delivery-agent",
		"portal-worker-agent",
		"delegation=",
		"learning_proposal",
		"memory=",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./tests/integration -run 'TestRealOdinAgentLaneForPortalDelivery' -count=1`

Expected: FAIL because the registry agents and full end-to-end lane are not in place yet.

**Step 3: Write the minimal implementation**

Create `registry/agents/portal-delivery-agent.md` with:

- kind `agent`
- role `portal-delivery-lead`
- scopes `managed-project`
- purpose: decompose portal delivery into IA, design, implementation, verification, and learning streams
- outputs: child-work summary, portal-track handoff, durable learnings

Create `registry/agents/portal-worker-agent.md` with:

- kind `agent`
- role `portal-worker`
- scopes `managed-project` and `global`
- purpose: execute one bounded child stream and return auditable output only

Keep them registry-simple. Do not add a second registry or a generic planner DSL in this pass.

**Step 4: Run the focused tests and real `odin` checks**

Run:

- `go test ./tests/integration -run 'TestRealOdinAgentLaneForPortalDelivery' -count=1`
- `go build -o /home/orchestrator/.local/bin/odin ./cmd/odin`

Then run real CLI checks from repo root:

```bash
printf '/help\n/agent list\n/agent show portal-delivery-agent\n/quit\n' \
  | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin
```

```bash
printf '/project cfipros\n/agent run portal-delivery-agent portal_track=admin-cfi surface=dashboard goal=deliver-admin-dashboard\n/runs show active\n/quit\n' \
  | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin
```

```bash
printf '/project cfipros\n/agent run portal-delivery-agent portal_track=student surface=dashboard goal=deliver-student-dashboard\n/runs show active\n/quit\n' \
  | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin
```

Expected:

- `/help` includes `/agent`
- `/agent list` shows both registry agents
- each portal run produces parent and child execution evidence
- `/runs show active` includes delegation lines, effective skill telemetry, and memory/learning evidence

**Step 5: Commit**

```bash
git add registry/agents/portal-delivery-agent.md registry/agents/portal-worker-agent.md tests/integration/agent_swarm_delivery_test.go
git commit -m "feat: add portal delivery swarm acceptance lane"
```

### Task 6: Validate dynamic skill usage and persistent memory behavior with the real acceptance workload

**Files:**
- Modify: `tests/integration/agent_swarm_delivery_test.go`
- Modify: `internal/runtime/delegations/service_test.go`
- Modify: `internal/runtime/runs/service_test.go`

**Step 1: Write the failing tests**

```go
func TestPortalDesignChildUsesPixelPerfectSkill(t *testing.T) {
	result := runPortalAgentFixture(t, "admin-cfi")
	if !strings.Contains(result.RunDetail, "effective_skill=pixel-perfect-ui-ux-designer") {
		t.Fatalf("run detail = %q, want pixel-perfect skill telemetry", result.RunDetail)
	}
}

func TestPortalWorkflowWritesEpisodeAndProjectMemory(t *testing.T) {
	result := runPortalAgentFixture(t, "student")
	for _, want := range []string{"memory=", "\"memory_type\":\"episode\"", "\"memory_type\":\"project\""} {
		if !strings.Contains(result.RunDetail, want) {
			t.Fatalf("run detail = %q, want %q", result.RunDetail, want)
		}
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./tests/integration ./internal/runtime/delegations ./internal/runtime/runs -run 'Test(PortalDesignChildUsesPixelPerfectSkill|PortalWorkflowWritesEpisodeAndProjectMemory)' -count=1`

Expected: FAIL until the acceptance run detail exposes both the skill telemetry and the durable memory writes.

**Step 3: Write the minimal implementation**

Make sure the runtime records:

- episode memory for each child run
- one project memory summary for durable portal-track output when justified
- learning proposal artifacts when `learning_capture` child work concludes with a concrete improvement worth preserving

Use delegation artifacts so run detail can show:

```text
artifact=41 type=memory_summary summary=project memory recorded
artifact=42 type=learning_proposal summary=proposal 7 created for cfipros/dashboard-tone
```

**Step 4: Run the tests to verify they pass**

Run: `go test ./tests/integration ./internal/runtime/delegations ./internal/runtime/runs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/agent_swarm_delivery_test.go internal/runtime/delegations/service_test.go internal/runtime/runs/service_test.go
git commit -m "test: verify swarm skill telemetry and memory persistence"
```

## Final Verification

Run the full focused suite:

- `go test ./internal/store/sqlite ./internal/runtime/delegations ./internal/runtime/jobs ./internal/runtime/runs ./internal/cli/repl ./tests/integration -count=1`
- `go build -o /home/orchestrator/.local/bin/odin ./cmd/odin`

Run real `odin` command checks from `/home/orchestrator/odin-os`:

- `printf '/help\n/agent list\n/quit\n' | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin`
- `printf '/project cfipros\n/agent run portal-delivery-agent portal_track=admin-cfi surface=dashboard goal=deliver-admin-dashboard\n/runs show active\n/quit\n' | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin`
- `printf '/project cfipros\n/agent run portal-delivery-agent portal_track=student surface=dashboard goal=deliver-student-dashboard\n/runs show active\n/quit\n' | ODIN_CODEX_DRIVER="$PWD/scripts/drivers/codex-headless.sh" /home/orchestrator/.local/bin/odin`

Expected:

- operator help and REPL both expose `/agent`
- registry agents are listed and inspectable
- each portal workflow creates child delegations
- effective skill telemetry is visible for design children
- memory summaries and learning artifacts are visible in run detail
- no direct CFIPros work occurs outside Odin CLI

## Notes for Execution

- Keep the CFIPros acceptance scope read-only until the swarm lane is stable enough to trust implementation tasks.
- If a portal workflow exposes a real Odin gap, route that repair into `odin-core` as a child work item and record it as an `odin_repair` delegation artifact instead of bypassing the workflow.
- Do not add nested delegation recursion in this pass.

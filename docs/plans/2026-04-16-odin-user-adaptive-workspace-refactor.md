# User-Adaptive Workspace Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor `odin-os` into a user-adaptive workspace operating system for Marcus, with first-class workspaces, initiatives, companions, governed work items, scoped memory, and provider-neutral execution.

**Architecture:** Keep `odin-os` as a modular monolith with SQLite as runtime authority and Markdown as authored capability/catalog input. Introduce workspace, initiative, and companion domain modules inside the existing package layout; keep project governance as a specialized context; map current `tasks` and `runs` to `Work Item` and `Run Attempt` in domain code before any physical table rename; isolate providers and tool integrations behind explicit contracts.

**Tech Stack:** Go, SQLite, YAML, Markdown frontmatter registry assets, standard library CLI/HTTP runtime, git worktrees, Go unit and integration tests

---

### Task 1: Freeze the ubiquitous language and context contracts

**Files:**
- Create: `docs/contracts/ubiquitous-language.md`
- Create: `docs/contracts/workspace-context-map.md`
- Modify: `README.md`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Write the language contract**

Define:
- `Workspace`
- `Initiative`
- `Companion`
- `Managed Project`
- `Work Item`
- `Run Attempt`
- `Control Scope`
- `Execution Lane`

Explicitly mark `task`, `agent`, `command`, and `scope` as legacy or narrowed terms.

**Step 2: Write the context map contract**

Document the target bounded contexts and dependency direction:
- workspace
- initiative
- companion
- project governance
- capability catalog
- planning
- work execution
- memory
- integrations

**Step 3: Update the repo architecture summary**

Update `README.md` and `docs/contracts/repo-layout.md` so the new semantic center is visible before code moves begin.

**Step 4: Run docs lint and targeted grep checks**

Run:
- `rg -n "\\b(task|agent|scope|command)\\b" docs/contracts README.md`

Expected: intentional usages only, with the new canonical vocabulary documented.

### Task 2: Introduce the Workspace domain foundation

**Files:**
- Create: `internal/core/workspaces/types.go`
- Create: `internal/core/workspaces/service.go`
- Create: `internal/core/workspaces/service_test.go`
- Create: `internal/store/sqlite/migrations/0011_workspaces.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Add the workspace schema**

Add `workspaces` and `workspace_policies` tables with fields for:
- key
- name
- owner_ref
- default_companion_key
- status
- policy_json

Do not model multi-tenant complexity beyond what the current repo needs.

**Step 2: Add the core domain types**

Define:

```go
type Workspace struct {
    ID int64
    Key string
    Name string
    OwnerRef string
    Status string
    DefaultCompanionKey string
    Policy WorkspacePolicy
}
```

**Step 3: Add the workspace service**

Support:
- bootstrap default workspace
- fetch by key
- update workspace policy
- list active workspaces

**Step 4: Add focused tests**

Run: `go test ./internal/core/workspaces ./internal/store/sqlite -run 'TestWorkspace'`

Expected: PASS

### Task 3: Introduce the Initiative domain and link projects into it

**Files:**
- Create: `internal/core/initiatives/types.go`
- Create: `internal/core/initiatives/service.go`
- Create: `internal/core/initiatives/service_test.go`
- Create: `internal/store/sqlite/migrations/0012_initiatives.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/core/projects/service.go`

**Step 1: Add initiative persistence**

Create `initiatives` with:
- workspace_id
- key
- title
- kind
- status
- summary
- owner_companion_id
- linked_project_id nullable

**Step 2: Add initiative kinds**

Support:
- `managed_project`
- `goal`
- `case`
- `routine`
- `campaign`
- `personal_admin`

**Step 3: Add project-to-initiative registration**

When a managed project is registered, create or reconcile a matching `managed_project` initiative row instead of keeping projects semantically isolated.

**Step 4: Add focused tests**

Run: `go test ./internal/core/initiatives ./internal/core/projects -run 'Test(Initiative|ProjectBackedInitiative)'`

Expected: PASS

### Task 4: Introduce the Companion domain for assistants and advisors

**Files:**
- Create: `internal/core/companions/types.go`
- Create: `internal/core/companions/service.go`
- Create: `internal/core/companions/service_test.go`
- Create: `internal/store/sqlite/migrations/0013_companions.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `docs/contracts/companion-contract.md`

**Step 1: Add companion persistence**

Create `companions` with:
- workspace_id
- key
- title
- kind
- charter
- status
- initiative_scope_json
- tool_policy_json
- memory_policy_json
- planning_policy_json

**Step 2: Add canonical companion kinds**

Support:
- `assistant`
- `advisor`
- `operator`
- `specialist`

**Step 3: Add the companion contract**

Document that companions are durable runtime roles and may reference catalog capabilities, but are not themselves provider-specific prompt bundles.

**Step 4: Add focused tests**

Run: `go test ./internal/core/companions ./internal/store/sqlite -run 'TestCompanion'`

Expected: PASS

### Task 5: Replace flat scope handling with Control Scope v2

**Files:**
- Create: `internal/core/scope/types.go`
- Create: `internal/core/scope/service.go`
- Create: `internal/core/scope/service_test.go`
- Modify: `internal/cli/scope/scope.go`
- Modify: `internal/cli/scope/scope_test.go`
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/jobs/service.go`

**Step 1: Add the new value object**

Define:

```go
type ControlScope struct {
    SubjectType string
    SubjectKey string
    WorkspaceKey string
    InitiativeKey string
    ProjectKey string
    CompanionKey string
}
```

**Step 2: Add translation from current CLI scope**

Keep the current shell working while routing it through `ControlScope` so interface code no longer owns the semantic model.

**Step 3: Update ask and act flows**

Make conversation and work execution read `ControlScope` instead of stringly-typed scope fields.

**Step 4: Run focused scope tests**

Run: `go test ./internal/core/scope ./internal/cli/scope ./internal/runtime/conversation ./internal/runtime/jobs -run 'Test(ControlScope|Resolution)'`

Expected: PASS

### Task 6: Introduce the Work Item aggregate without breaking the current runtime

**Files:**
- Create: `internal/core/workitems/types.go`
- Create: `internal/core/workitems/service.go`
- Create: `internal/core/workitems/service_test.go`
- Create: `internal/store/sqlite/migrations/0014_work_item_links.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/runs/service.go`

**Step 1: Extend the current task schema**

Add nullable columns to `tasks`:
- `workspace_id`
- `initiative_id`
- `companion_id`
- `work_kind`

Do not rename the physical `tasks` table yet.

**Step 2: Add the Work Item domain model**

Define:

```go
type WorkItem struct {
    ID int64
    Key string
    WorkspaceID int64
    InitiativeID *int64
    CompanionID *int64
    WorkKind string
    Status string
}
```

**Step 3: Move lifecycle rules into the work item service**

Own:
- queue
- start
- block
- complete
- fail
- request approval

**Step 4: Make runtime jobs delegate**

Refactor `internal/runtime/jobs/service.go` so it delegates lifecycle decisions to the new work item application service instead of owning every decision itself.

**Step 5: Run focused lifecycle tests**

Run: `go test ./internal/core/workitems ./internal/runtime/jobs ./internal/runtime/runs -run 'Test(WorkItem|Execute|Approval)'`

Expected: PASS

### Task 7: Separate Work Execution from Project Governance

**Files:**
- Create: `internal/core/execution/service.go`
- Create: `internal/core/execution/service_test.go`
- Modify: `internal/core/projects/service.go`
- Modify: `internal/core/projects/transition.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/vcs/leases/manager.go`

**Step 1: Add execution orchestration service**

Create a dedicated service that coordinates:
- work item load
- project governance check
- execution lane selection
- lease preparation
- run attempt start and finish

**Step 2: Keep project governance narrow**

Ensure `internal/core/projects` owns only:
- managed project registration
- transition authorization
- project mutation rules
- system-project rules

**Step 3: Remove governance leakage**

`jobs.Service` should no longer directly mix project lookup, transition semantics, lease logic, executor routing, and memory writes in one method body.

**Step 4: Run focused execution and governance tests**

Run: `go test ./internal/core/execution ./internal/core/projects ./internal/runtime/jobs ./internal/vcs/leases -run 'Test(Execute|Governance|Lease)'`

Expected: PASS

### Task 8: Add scoped memory for workspace, initiative, companion, and run context

**Files:**
- Create: `internal/store/sqlite/migrations/0015_memory_scopes.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/memory/projects/service.go`
- Modify: `internal/memory/runs/service.go`
- Create: `internal/memory/workspaces/service.go`
- Create: `internal/memory/workspaces/service_test.go`
- Create: `internal/memory/companions/service.go`
- Create: `internal/memory/companions/service_test.go`

**Step 1: Extend memory records**

Add nullable foreign keys and explicit scope typing for:
- workspace_id
- initiative_id
- companion_id
- visibility_scope
- retention_class

**Step 2: Add memory services per scope**

Add explicit services for:
- workspace memory
- companion memory
- initiative memory
- run memory

**Step 3: Remove generic summary assumptions**

Callers must declare which scope they are writing to and which scope may read the memory later.

**Step 4: Run focused memory tests**

Run: `go test ./internal/memory/... ./internal/store/sqlite -run 'Test(Memory|Transcript|Episode)'`

Expected: PASS

### Task 9: Recast the catalog and planning contracts around companions and initiatives

**Files:**
- Modify: `internal/registry/types.go`
- Modify: `internal/tools/catalog/types.go`
- Modify: `internal/tools/broker/broker.go`
- Modify: `internal/workers/planner/service.go`
- Create: `docs/contracts/planning-contract.md`
- Create: `docs/contracts/initiative-and-companion-binding.md`

**Step 1: Keep authored capabilities distinct**

Catalog items should remain typed as:
- workflow
- skill
- agent role
- operator command

Do not collapse companions into generic capability cards.

**Step 2: Add planning inputs for companion and initiative**

Planner inputs should accept:
- workspace context
- initiative context
- companion policy
- catalog selections
- memory references

**Step 3: Add binding rules**

Document how companions compose catalog assets without becoming authored assets themselves.

**Step 4: Run focused planning tests**

Run: `go test ./internal/tools/... ./internal/workers/planner -run 'Test(Planner|Broker|Catalog)'`

Expected: PASS

### Task 10: Isolate provider and tool integrations behind explicit ACLs

**Files:**
- Create: `internal/integrations/providers/types.go`
- Create: `internal/integrations/providers/service.go`
- Create: `internal/integrations/providers/service_test.go`
- Create: `internal/integrations/tools/types.go`
- Create: `internal/integrations/tools/service.go`
- Create: `internal/integrations/tools/service_test.go`
- Modify: `internal/executors/contract/types.go`
- Modify: `internal/executors/router/router.go`
- Modify: `internal/adapters/doc.go`

**Step 1: Add provider-facing canonical types**

Define:
- execution request
- capability profile
- provider error
- streaming event envelope

**Step 2: Add tool-facing canonical types**

Define:
- tool request
- tool result
- authorization result
- artifact reference

**Step 3: Make routing and execution consume these ACLs**

The core execution service should reason about execution lanes and tool contracts, not raw provider or app-specific payloads.

**Step 4: Run focused integration tests**

Run: `go test ./internal/integrations/... ./internal/executors/... ./internal/adapters/... -run 'Test(Provider|Tool|Route)'`

Expected: PASS

### Task 11: Expose workspace, initiative, and companion views in CLI and API

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `internal/runtime/projections/projections.go`

**Step 1: Add operator commands**

Add commands and views for:
- workspace status
- initiative list
- companion list
- current control scope

**Step 2: Add operational JSON surfaces**

Expose workspace, initiative, and companion summaries in API read models without introducing a second control plane.

**Step 3: Update projections**

Add projection views for:
- workspace overview
- initiative portfolio
- companion assignments
- blocked work by initiative or companion

**Step 4: Run focused interface tests**

Run: `go test ./internal/cli/... ./internal/api/http ./internal/runtime/projections -run 'Test(Workspace|Initiative|Companion|Operational)'`

Expected: PASS

### Task 12: Add bootstrap defaults, migration helpers, and rollout verification

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/bootstrap/bootstrap_test.go`
- Create: `scripts/migrate/bootstrap_workspace.go`
- Create: `docs/operations/workspace-bootstrap.md`
- Create: `tests/integration/workspace_refactor_acceptance_test.go`
- Modify: `README.md`

**Step 1: Bootstrap the default Marcus workspace**

Fresh runtimes should create one default workspace and one default operator companion without hand-seeding.

**Step 2: Add migration helpers**

Provide one migration helper that:
- creates initiatives for existing managed projects
- binds current runtime tasks into the workspace
- leaves physical compatibility intact

**Step 3: Add end-to-end acceptance tests**

Cover:
- workspace exists after bootstrap
- managed project becomes an initiative
- a companion can own a work item
- memory writes remain scoped
- project governance still blocks unsafe mutation

**Step 4: Run full verification**

Run:
- `go test ./internal/core/...`
- `go test ./internal/runtime/...`
- `go test ./internal/memory/...`
- `go test ./internal/integrations/...`
- `go test ./internal/cli/...`
- `go test ./internal/api/http`
- `go test ./tests/integration -run 'TestWorkspaceRefactorAcceptance|TestAlphaAcceptance'`
- `make test`
- `make build`

**Step 5: Review worktree status**

Run: `git status --short --branch`

Expected: implementation branch contains the refactor plan artifacts and any accepted code changes only.

# Odin Operating Model Rollout Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Align `odin-os` to the approved workspace operating model so Odin clearly owns persistent workspace state while Codex and Claude workers remain bounded execution lanes.

**Architecture:** Keep `odin-os` as a modular monolith with SQLite as runtime authority and Markdown frontmatter as authored catalog input. Introduce workspace objects and control-plane language in the existing `core`, `memory`, `runtime`, and `executors` boundaries rather than creating a second orchestration stack. Treat projects as one initiative kind, companions as role contracts, and workers as temporary run-time labor.

**Tech Stack:** Go, SQLite, Markdown frontmatter registry assets, YAML config, standard library CLI and HTTP surfaces, existing executor router, existing projections and integration tests

---

### Task 1: Freeze the operating-model vocabulary in repo docs

**Files:**
- Create: `docs/contracts/odin-operating-model.md`
- Create: `docs/contracts/ubiquitous-language.md`
- Modify: `README.md`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Write the operating-model contract**

Document the durable product objects:
- `Workspace`
- `Initiative`
- `Companion`
- `Policy`
- `Memory`
- `Work Item`
- `Run Attempt`

Define Odin as the control plane and workers as execution-plane components.

**Step 2: Write the vocabulary contract**

Mark these terms as canonical:
- `workspace`
- `initiative`
- `companion`
- `work item`
- `run attempt`
- `control plane`
- `execution plane`

Mark these terms as narrowed or legacy:
- `task`
- `agent`
- `scope`
- `command`

**Step 3: Update architecture summaries**

Update `README.md` and `docs/contracts/repo-layout.md` so the repo overview reflects the operating-model center before code moves begin.

**Step 4: Run grep checks**

Run: `rg -n "\\b(task|agent|scope|command)\\b" README.md docs/contracts`

Expected: remaining usages are intentional, explained, or legacy-only.

**Step 5: Commit**

```bash
git add docs/contracts/odin-operating-model.md docs/contracts/ubiquitous-language.md README.md docs/contracts/repo-layout.md
git commit -m "docs: freeze Odin operating model vocabulary"
```

### Task 2: Introduce first-class workspace and initiative persistence

**Files:**
- Create: `internal/core/workspaces/types.go`
- Create: `internal/core/workspaces/service.go`
- Create: `internal/core/workspaces/service_test.go`
- Create: `internal/core/initiatives/types.go`
- Create: `internal/core/initiatives/service.go`
- Create: `internal/core/initiatives/service_test.go`
- Create: `internal/store/sqlite/migrations/0011_workspaces.sql`
- Create: `internal/store/sqlite/migrations/0012_initiatives.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/core/projects/service.go`

**Step 1: Write the failing workspace tests**

Add tests for:
- bootstrapping a default workspace
- fetching a workspace by key
- listing active workspaces

**Step 2: Run the workspace tests to verify they fail**

Run: `go test ./internal/core/workspaces ./internal/store/sqlite -run 'TestWorkspace' -v`

Expected: FAIL because the workspace schema and service do not exist yet.

**Step 3: Write the minimal workspace schema and service**

Add `workspaces` with:
- `key`
- `name`
- `owner_ref`
- `status`
- `default_companion_key`
- `policy_json`

Keep the model single-primary-workspace friendly. Do not build full multi-tenant complexity.

**Step 4: Write the failing initiative tests**

Add tests for:
- creating initiatives by kind
- linking a managed project to an initiative
- listing initiatives for a workspace

**Step 5: Run the initiative tests to verify they fail**

Run: `go test ./internal/core/initiatives ./internal/core/projects -run 'Test(Initiative|ProjectBackedInitiative)' -v`

Expected: FAIL because initiative persistence and project reconciliation do not exist yet.

**Step 6: Write the minimal initiative schema and service**

Add `initiatives` with:
- `workspace_id`
- `key`
- `title`
- `kind`
- `status`
- `summary`
- `linked_project_id`
- `owner_companion_id`

Treat `managed_project` as one initiative kind instead of preserving projects as a parallel product model.

**Step 7: Run the workspace and initiative tests again**

Run: `go test ./internal/core/workspaces ./internal/core/initiatives ./internal/core/projects ./internal/store/sqlite -run 'Test(Workspace|Initiative|ProjectBackedInitiative)' -v`

Expected: PASS

**Step 8: Commit**

```bash
git add internal/core/workspaces internal/core/initiatives internal/core/projects/service.go internal/store/sqlite/migrations/0011_workspaces.sql internal/store/sqlite/migrations/0012_initiatives.sql internal/store/sqlite/models.go internal/store/sqlite/store.go
git commit -m "feat(core): add workspace and initiative foundations"
```

### Task 3: Add companions and policy as product-facing control objects

**Files:**
- Create: `internal/core/companions/types.go`
- Create: `internal/core/companions/service.go`
- Create: `internal/core/companions/service_test.go`
- Create: `internal/core/policies/workspace.go`
- Create: `internal/core/policies/workspace_test.go`
- Create: `internal/store/sqlite/migrations/0013_companions.sql`
- Modify: `internal/core/policy/engine.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `docs/contracts/companion-contract.md`

**Step 1: Write the failing companion tests**

Add tests for:
- creating a default operator companion
- listing companions for a workspace
- assigning a companion to an initiative

**Step 2: Run the companion tests to verify they fail**

Run: `go test ./internal/core/companions ./internal/store/sqlite -run 'TestCompanion' -v`

Expected: FAIL because the companion domain does not exist yet.

**Step 3: Write the minimal companion schema and service**

Add `companions` with:
- `workspace_id`
- `key`
- `title`
- `kind`
- `charter`
- `status`
- `initiative_scope_json`
- `tool_policy_json`
- `memory_policy_json`
- `planning_policy_json`

Do not let companions become provider-specific prompt bundles.

**Step 4: Write the failing policy tests**

Add tests for:
- resolving workspace default policy
- overlaying initiative or companion policy without bypassing core approval rules
- rejecting unknown external side effects by default

**Step 5: Run the policy tests to verify they fail**

Run: `go test ./internal/core/policies ./internal/core/policy -run 'TestWorkspacePolicy' -v`

Expected: FAIL because workspace-level control objects are not connected to the policy engine.

**Step 6: Wire policy overlays into the existing engine**

Keep one policy engine. Add workspace and companion inputs to it. Do not create a second policy authority.

**Step 7: Run the companion and policy tests again**

Run: `go test ./internal/core/companions ./internal/core/policies ./internal/core/policy ./internal/store/sqlite -run 'Test(Companion|WorkspacePolicy)' -v`

Expected: PASS

**Step 8: Commit**

```bash
git add internal/core/companions internal/core/policies internal/core/policy/engine.go internal/store/sqlite/migrations/0013_companions.sql internal/store/sqlite/models.go internal/store/sqlite/store.go docs/contracts/companion-contract.md
git commit -m "feat(core): add companions and workspace policy overlays"
```

### Task 4: Move execution semantics to work items, run attempts, and control scope

**Files:**
- Create: `internal/core/controlscope/types.go`
- Create: `internal/core/controlscope/service.go`
- Create: `internal/core/controlscope/service_test.go`
- Create: `internal/core/workitems/types.go`
- Create: `internal/core/workitems/service.go`
- Create: `internal/core/workitems/service_test.go`
- Create: `internal/store/sqlite/migrations/0014_work_item_links.sql`
- Modify: `internal/cli/scope/scope.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/runs/service.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Write the failing control-scope tests**

Add tests for:
- resolving workspace scope
- resolving initiative scope
- resolving managed-project scope with a companion overlay

**Step 2: Run the control-scope tests to verify they fail**

Run: `go test ./internal/core/controlscope ./internal/cli/scope -run 'TestControlScope' -v`

Expected: FAIL because scope is still too flat and CLI-owned.

**Step 3: Add the `ControlScope` value object**

Include:
- `SubjectType`
- `SubjectKey`
- `WorkspaceKey`
- `InitiativeKey`
- `ProjectKey`
- `CompanionKey`

Keep the current CLI shell working by translating existing flags into the new value object.

**Step 4: Write the failing work-item tests**

Add tests for:
- creating work items from workspace or initiative context
- linking work items to companions and projects
- preserving run-attempt history across retries

**Step 5: Run the work-item tests to verify they fail**

Run: `go test ./internal/core/workitems ./internal/runtime/jobs ./internal/runtime/runs -run 'Test(WorkItem|RunAttempt)' -v`

Expected: FAIL because runtime execution still speaks mostly in raw task and scope terms.

**Step 6: Add the minimal work-item domain and schema links**

Do not rename the physical `tasks` table yet. Add linking columns and wrap it with a `WorkItem` domain model first.

**Step 7: Run the control-scope and work-item tests again**

Run: `go test ./internal/core/controlscope ./internal/core/workitems ./internal/cli/scope ./internal/runtime/jobs ./internal/runtime/runs -run 'Test(ControlScope|WorkItem|RunAttempt)' -v`

Expected: PASS

**Step 8: Commit**

```bash
git add internal/core/controlscope internal/core/workitems internal/cli/scope/scope.go internal/runtime/jobs/service.go internal/runtime/runs/service.go internal/store/sqlite/migrations/0014_work_item_links.sql internal/store/sqlite/models.go internal/store/sqlite/store.go
git commit -m "feat(runtime): align execution with control scope and work items"
```

### Task 5: Add scoped memory and explicit follow-up ownership

**Files:**
- Modify: `internal/memory/users/service.go`
- Modify: `internal/memory/projects/service.go`
- Modify: `internal/memory/runs/service.go`
- Create: `internal/memory/initiatives/service.go`
- Create: `internal/memory/initiatives/service_test.go`
- Create: `internal/memory/companions/service.go`
- Create: `internal/memory/companions/service_test.go`
- Modify: `internal/runtime/checkpoints/service.go`
- Modify: `internal/runtime/projections/observability_test.go`
- Modify: `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`

**Step 1: Write the failing memory-scope tests**

Add tests for:
- initiative memory retrieval
- companion memory retrieval
- blocking project memory from leaking into unrelated initiative or workspace views

**Step 2: Run the memory tests to verify they fail**

Run: `go test ./internal/memory/... -run 'Test(MemoryScope|InitiativeMemory|CompanionMemory)' -v`

Expected: FAIL because memory services do not yet cover the full operating-model scope.

**Step 3: Extend the memory services with typed scopes**

Support:
- `workspace_memory`
- `initiative_memory`
- `companion_memory`
- `project_memory`
- `run_memory`
- `user_preference_memory`

Every memory write must declare source scope, visibility scope, retention intent, and source transcript or run when available.

**Step 4: Add follow-up projection behavior**

Use existing checkpoints and projections so Odin can re-surface blocked work, resumed work, and next obligations in workspace-facing views instead of burying them in run history.

**Step 5: Run the memory and projection tests again**

Run: `go test ./internal/memory/... ./internal/runtime/checkpoints ./internal/runtime/projections -run 'Test(MemoryScope|InitiativeMemory|CompanionMemory|Observability)' -v`

Expected: PASS

**Step 6: Commit**

```bash
git add internal/memory internal/runtime/checkpoints/service.go internal/runtime/projections/observability_test.go internal/store/sqlite/migrations/0010_memory_and_conversations.sql
git commit -m "feat(memory): add scoped operating-model memory and follow-up visibility"
```

### Task 6: Expose the workspace operating model in CLI and API without adding a second control plane

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/api/http/server.go`
- Modify: `internal/runtime/projections/portfolio.go`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `tests/integration/helpers_test.go`

**Step 1: Write the failing integration tests**

Add integration tests for:
- showing workspace status with initiatives and companion assignments
- creating or listing work by initiative
- surfacing approvals and blocked follow-ups in workspace views

**Step 2: Run the integration tests to verify they fail**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance' -v`

Expected: FAIL because the operator surfaces still expose mostly project-first runtime views.

**Step 3: Add workspace-facing projections and CLI/API rendering**

Expose:
- workspace home
- initiative portfolio
- companion assignments
- blocked work and pending approvals

These are read models over the same Odin control plane. Do not create a separate dashboard authority.

**Step 4: Run the integration tests again**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance' -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/repl/shell.go internal/api/http/server.go internal/runtime/projections/portfolio.go tests/integration/alpha_acceptance_test.go tests/integration/helpers_test.go
git commit -m "feat(ui): expose workspace operating model in CLI and API"
```

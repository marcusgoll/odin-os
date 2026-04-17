# Odin OS Workspace Memory Port Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add canonical workspace-era memory ownership to `odin-os` by introducing workspace, initiative, and companion owners; extending the SQLite memory model; and wiring runtime, bootstrap, CLI, and API flows to the new scoped memory semantics.

**Architecture:** Extend the current SQLite-backed transcript and memory-summary model instead of transplanting the legacy orchestrator stack. First add the missing owner entities (`Workspace`, `Initiative`, `Companion`), then add scoped memory ownership and policy fields, then migrate current callers to those semantics, and only then expose read-only operator surfaces and migration helpers.

**Tech Stack:** Go, SQLite migrations, standard library HTTP/CLI, Go unit tests, Go integration tests

---

### Task 1: Add workspace, initiative, and companion owner entities

**Files:**
- Create: `internal/store/sqlite/migrations/0011_workspaces.sql`
- Create: `internal/store/sqlite/migrations/0012_initiatives.sql`
- Create: `internal/store/sqlite/migrations/0013_companions.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/core/workspaces/service.go`
- Create: `internal/core/workspaces/service_test.go`
- Create: `internal/core/initiatives/service.go`
- Create: `internal/core/initiatives/service_test.go`
- Create: `internal/core/companions/service.go`
- Create: `internal/core/companions/service_test.go`

**Step 1: Write the failing owner-entity tests**

Add focused tests that expect:
- a default workspace can be created and fetched by key
- an initiative belongs to a workspace and may link a project
- a companion belongs to a workspace and may constrain initiative scope

Example shapes:

```go
func TestWorkspaceBootstrapCreatesDefaultMarcusWorkspace(t *testing.T) {}
func TestCreateInitiativeLinksWorkspaceAndProject(t *testing.T) {}
func TestCreateCompanionPersistsMemoryPolicy(t *testing.T) {}
```

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./internal/core/workspaces ./internal/core/initiatives ./internal/core/companions ./internal/store/sqlite -run 'Test(Workspace|Initiative|Companion)' -v
```

Expected:
- FAIL because the new migration tables, store methods, and services do not exist yet

**Step 3: Add the new SQLite schema and models**

Implement:
- `workspaces`
- `initiatives`
- `companions`

Fields should cover:
- workspace: `key`, `name`, `owner_ref`, `status`, `default_companion_key`, `policy_json`
- initiative: `workspace_id`, `key`, `title`, `kind`, `status`, `summary`, `linked_project_id`, `owner_companion_id`
- companion: `workspace_id`, `key`, `title`, `kind`, `charter`, `status`, `initiative_scope_json`, `memory_policy_json`, `planning_policy_json`, `tool_policy_json`

Add matching model structs and store methods in `models.go` and `store.go`.

**Step 4: Add the core services**

Implement minimal services that support:
- workspace bootstrap and lookup
- initiative create/list for a workspace
- companion create/list for a workspace

Keep these services thin and store-backed. Do not add planning or policy orchestration here yet.

**Step 5: Run the focused tests to confirm they pass**

Run:

```bash
go test ./internal/core/workspaces ./internal/core/initiatives ./internal/core/companions ./internal/store/sqlite -run 'Test(Workspace|Initiative|Companion)' -v
```

Expected:
- PASS

**Step 6: Commit**

```bash
git add internal/store/sqlite/migrations/0011_workspaces.sql internal/store/sqlite/migrations/0012_initiatives.sql internal/store/sqlite/migrations/0013_companions.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/core/workspaces/service.go internal/core/workspaces/service_test.go internal/core/initiatives/service.go internal/core/initiatives/service_test.go internal/core/companions/service.go internal/core/companions/service_test.go
git commit -m "feat(memory): add workspace owner entities"
```

### Task 2: Extend transcripts and memory summaries with scoped ownership

**Files:**
- Create: `internal/store/sqlite/migrations/0014_memory_scopes.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing store tests for scoped memory ownership**

Add tests that expect:
- memory summaries can persist `workspace_id`, `initiative_id`, and `companion_id`
- transcripts can persist `workspace_id`, `initiative_id`, and `companion_id`
- `visibility_scope` and `retention_class` round-trip correctly
- invalid lineage is rejected, for example a transcript tied to an initiative from a different workspace

Example shapes:

```go
func TestRecordMemorySummaryPersistsWorkspaceInitiativeAndCompanionOwnership(t *testing.T) {}
func TestRecordConversationTranscriptPersistsScopedOwnership(t *testing.T) {}
func TestRecordMemorySummaryRejectsCrossWorkspaceLineage(t *testing.T) {}
```

**Step 2: Run the focused store tests to confirm they fail**

Run:

```bash
go test ./internal/store/sqlite -run 'Test(RecordMemorySummary|RecordConversationTranscript).*' -v
```

Expected:
- FAIL because the scoped ownership fields do not exist yet

**Step 3: Add the migration and model fields**

Extend:
- `conversation_transcripts`
- `memory_summaries`

Add fields:
- `workspace_id`
- `initiative_id`
- `companion_id`
- `visibility_scope` on summaries
- `retention_class` on summaries

Keep existing lineage fields:
- `project_id`
- `task_id`
- `run_id`
- `source_transcript_id`

Do not remove `scope` or `scope_key` in this task.

**Step 4: Update store validation and queries**

Update store APIs so scoped writes:
- validate workspace/initiative/companion lineage when provided
- keep current project/task/run lineage validation intact
- allow transitional callers to omit new owner fields until later tasks migrate them

**Step 5: Run the store tests to confirm they pass**

Run:

```bash
go test ./internal/store/sqlite -run 'Test(RecordMemorySummary|RecordConversationTranscript).*' -v
```

Expected:
- PASS

**Step 6: Commit**

```bash
git add internal/store/sqlite/migrations/0014_memory_scopes.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go
git commit -m "feat(memory): add scoped ownership to transcripts and summaries"
```

### Task 3: Add scoped memory services and adapt existing memory APIs

**Files:**
- Create: `internal/memory/workspaces/service.go`
- Create: `internal/memory/workspaces/service_test.go`
- Create: `internal/memory/initiatives/service.go`
- Create: `internal/memory/initiatives/service_test.go`
- Create: `internal/memory/companions/service.go`
- Create: `internal/memory/companions/service_test.go`
- Modify: `internal/memory/projects/service.go`
- Modify: `internal/memory/projects/service_test.go`
- Modify: `internal/memory/runs/service.go`
- Modify: `internal/memory/runs/service_test.go`
- Modify: `internal/memory/users/service.go`
- Modify: `internal/memory/users/service_test.go`
- Modify: `internal/memory/knowledge/service.go`
- Modify: `internal/memory/knowledge/service_test.go`

**Step 1: Write failing scoped-service tests**

Add tests that expect:
- workspace service writes workspace-owned durable memory
- initiative service writes initiative-owned memory and can include project lineage
- companion service writes companion-owned overlay memory
- run service records transcripts and episodes with workspace and initiative ownership
- project and user services delegate into the scoped model instead of remaining the semantic center

**Step 2: Run the focused tests to verify they fail**

Run:

```bash
go test ./internal/memory/... ./internal/store/sqlite -run 'Test(Memory|Transcript|Episode|Workspace|Initiative|Companion)' -v
```

Expected:
- FAIL due to missing scoped services and old service assumptions

**Step 3: Add the new scoped services**

Implement:
- `workspaces.Service`
- `initiatives.Service`
- `companions.Service`

Each service should call the store with explicit ownership:
- workspace memory -> `workspace_id`
- initiative memory -> `workspace_id`, `initiative_id`
- companion memory -> `workspace_id`, `companion_id`

**Step 4: Adapt the existing services**

Change existing services so:
- `users` becomes transitional workspace-facing behavior, not a separate semantic owner
- `projects` records initiative-linked memory while preserving `project_id`
- `runs` records transcripts and episodes with workspace/initiative lineage
- `knowledge` remains a low-level scoped helper

Do not add retrieval ranking or lifecycle jobs in this task.

**Step 5: Run the focused tests to verify they pass**

Run:

```bash
go test ./internal/memory/... ./internal/store/sqlite -run 'Test(Memory|Transcript|Episode|Workspace|Initiative|Companion)' -v
```

Expected:
- PASS

**Step 6: Commit**

```bash
git add internal/memory/workspaces/service.go internal/memory/workspaces/service_test.go internal/memory/initiatives/service.go internal/memory/initiatives/service_test.go internal/memory/companions/service.go internal/memory/companions/service_test.go internal/memory/projects/service.go internal/memory/projects/service_test.go internal/memory/runs/service.go internal/memory/runs/service_test.go internal/memory/users/service.go internal/memory/users/service_test.go internal/memory/knowledge/service.go internal/memory/knowledge/service_test.go
git commit -m "feat(memory): add scoped workspace memory services"
```

### Task 4: Migrate runtime callers and memory events to explicit ownership

**Files:**
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/conversation/service_test.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing runtime tests**

Add tests that expect:
- run transcripts inherit workspace and initiative ownership
- episode summaries inherit workspace and initiative ownership
- runtime memory events include the new owner fields when present
- project-backed work records initiative-linked memory instead of generic project-only memory

**Step 2: Run the focused runtime tests to confirm they fail**

Run:

```bash
go test ./internal/runtime/conversation ./internal/runtime/jobs ./internal/store/sqlite -run 'Test(Conversation|Job|Transcript|Episode|Memory)' -v
```

Expected:
- FAIL because runtime callers still write the old shape

**Step 3: Update runtime memory writes**

Update the runtime flows so:
- conversation ingestion records transcripts with workspace/initiative lineage
- jobs service records episode summaries with the same ownership
- event payloads expose `workspace_id`, `initiative_id`, and `companion_id` when present

Avoid changing unrelated execution, approval, or lease behavior.

**Step 4: Run the focused runtime tests to confirm they pass**

Run:

```bash
go test ./internal/runtime/conversation ./internal/runtime/jobs ./internal/store/sqlite -run 'Test(Conversation|Job|Transcript|Episode|Memory)' -v
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/runtime/conversation/service.go internal/runtime/conversation/service_test.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/runtime/events/events.go internal/store/sqlite/store_test.go
git commit -m "feat(memory): wire runtime transcripts to workspace scopes"
```

### Task 5: Bootstrap the default workspace and migrate existing project memory

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/bootstrap/bootstrap_test.go`
- Create: `scripts/migrate/bootstrap_workspace.go`
- Create: `docs/operations/workspace-memory-bootstrap.md`
- Create: `tests/integration/workspace_memory_bootstrap_test.go`

**Step 1: Write failing bootstrap and migration tests**

Add tests that expect:
- a fresh runtime bootstraps one default Marcus workspace
- existing managed projects can be reconciled into `managed_project` initiatives
- migrated memory preserves project/task/run lineage while gaining workspace/initiative ownership

**Step 2: Run the bootstrap and integration tests to confirm they fail**

Run:

```bash
go test ./internal/app/bootstrap ./tests/integration -run 'Test(Workspace|Bootstrap|MemoryMigration)' -v
```

Expected:
- FAIL because bootstrap and migration helpers do not exist yet

**Step 3: Implement bootstrap and migration**

Add:
- default workspace bootstrap during app load
- initiative creation for registered managed projects
- a migration helper that backfills old global/project memory into workspace/initiative-owned rows

The migration helper should preserve:
- `project_id`
- `task_id`
- `run_id`
- `source_transcript_id`

It should not introduce orchestrator-style projection behavior.

**Step 4: Run the bootstrap and integration tests to confirm they pass**

Run:

```bash
go test ./internal/app/bootstrap ./tests/integration -run 'Test(Workspace|Bootstrap|MemoryMigration)' -v
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/app/bootstrap/bootstrap.go internal/app/bootstrap/bootstrap_test.go scripts/migrate/bootstrap_workspace.go docs/operations/workspace-memory-bootstrap.md tests/integration/workspace_memory_bootstrap_test.go
git commit -m "feat(memory): bootstrap workspace scoped memory"
```

### Task 6: Expose read-only workspace memory surfaces in CLI and HTTP

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/commands/commands_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `internal/runtime/projections/observability_test.go`

**Step 1: Write failing interface tests**

Add tests that expect:
- CLI can show workspace memory status
- CLI can list initiative and companion memory summaries
- HTTP operational handler exposes a workspace memory view without creating a second control plane
- runtime projections include scoped workspace memory summaries

**Step 2: Run the focused interface tests to confirm they fail**

Run:

```bash
go test ./internal/cli/... ./internal/api/http ./internal/runtime/projections -run 'Test(Workspace|Initiative|Companion|Memory|Operational)' -v
```

Expected:
- FAIL because the new commands and operational surfaces do not exist yet

**Step 3: Implement the read-only views**

Add:
- CLI commands for workspace memory visibility
- REPL command handling for those views
- HTTP operational JSON for workspace/initiative/companion memory summaries
- projection updates that surface workspace memory state in the existing projection model

Keep this task read-only. Do not add correction/delete endpoints yet.

**Step 4: Run the focused interface tests to confirm they pass**

Run:

```bash
go test ./internal/cli/... ./internal/api/http ./internal/runtime/projections -run 'Test(Workspace|Initiative|Companion|Memory|Operational)' -v
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/commands/commands_test.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/api/http/operational.go internal/api/http/operational_test.go internal/runtime/projections/projections.go internal/runtime/projections/observability_test.go
git commit -m "feat(memory): expose workspace memory read models"
```

### Task 7: Run full verification and document the staged follow-on work

**Files:**
- Modify: `README.md`
- Modify: `docs/contracts/workspace-context-map.md`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Update repo docs to reflect landed memory ownership**

Document:
- workspace/initiative/companion memory is now first-class runtime state
- `odin-orchestrator` remains migration input only
- richer retrieval, lifecycle rotation, and operator mutation tooling are intentionally deferred

**Step 2: Run full verification**

Run:

```bash
go test ./internal/core/...
go test ./internal/memory/... ./internal/store/sqlite
go test ./internal/runtime/...
go test ./internal/cli/... ./internal/api/http
go test ./tests/integration/... -run 'Test(Workspace|Memory)'
git diff --check
```

Expected:
- PASS
- clean diff formatting

**Step 3: Commit**

```bash
git add README.md docs/contracts/workspace-context-map.md docs/contracts/repo-layout.md
git commit -m "docs(memory): record odin-os workspace memory cutover"
```

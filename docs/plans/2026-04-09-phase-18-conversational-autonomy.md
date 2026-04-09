# Phase 18 Conversational Autonomy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn Odin into a Claude/Codex-like personal assistant with live `ask`-mode conversation, immediate `act`-mode execution feedback, scoped persistent memory, and autonomous self-improvement with canary rollout and self-editing.

**Architecture:** Add a synchronous conversation service for `ask`, keep `act` as durable runtime work with an immediate foreground run attempt, persist transcripts and summaries into scoped SQLite-backed memory, and extend the existing learning subsystem into autonomous proposal generation, canary activation, promotion, rollback, and self-editing through Odin-owned worktrees and task branches. Runtime overlays remain the first activation path, while canonical file rewrites happen through audited Odin-created tasks, verification, and auto-merge rather than direct default-branch mutation.

**Tech Stack:** Go, SQLite, embedded migrations, Markdown registry and memory assets, existing executor/router/learning packages, git branches/worktrees, Go unit and integration tests

---

### Task 1: Add failing shell tests for real ask-mode chat and inline act execution

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Test: `internal/cli/repl/shell_test.go`
- Test: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing ask-mode shell tests**

Add coverage for:
- generic `ask` prompt returns an executor-backed answer instead of the Phase 05 placeholder
- ask-mode question can consult scope state and return a conversational answer
- ask-mode does not create a task for read-only conversation

**Step 2: Write the failing act-mode shell tests**

Add coverage for:
- act-mode prompt creates a task and immediately attempts one run
- act-mode prints a policy denial inline when mutation is blocked
- act-mode prints a completed result inline when execution succeeds

**Step 3: Tighten the integration test**

Extend the interactive acceptance flow to prove:
- generic `ask` input produces a non-placeholder response
- `act` input produces immediate run visibility, not just `created task`

**Step 4: Run the focused tests and verify they fail**

Run: `go test ./internal/cli/repl ./tests/integration -run 'Test(Ask|Act|AlphaAcceptance)' -count=1`

Expected: FAIL because generic ask-mode still falls back to the Phase 05 placeholder and act-mode does not execute inline yet.

**Step 5: Commit**

```bash
git add internal/cli/repl/shell_test.go tests/integration/alpha_acceptance_test.go
git commit -m "test: cover conversational ask and inline act execution"
```

### Task 2: Implement the synchronous conversation service and wire it into the shell

**Files:**
- Create: `internal/runtime/conversation/service.go`
- Create: `internal/runtime/conversation/service_test.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/jobs/service.go`
- Test: `internal/runtime/conversation/service_test.go`

**Step 1: Write the minimal conversation service**

Create a service that accepts:
- scope resolution
- prompt text
- store
- registry
- executor config and catalog
- optional tool broker

Return:
- answer text
- tool usage summary
- transcript metadata

**Step 2: Route generic ask-mode prompts through the conversation service**

Replace the current Phase 05 fallback in `shell.handleAsk` with:
- keep simple intent shortcuts for `/help`-style operational prompts
- otherwise call the conversation service

**Step 3: Add one foreground execution path for new act-mode prompts**

Add a small job-service entrypoint that:
- creates the task
- starts one immediate run attempt
- returns the resulting task/run status and output

**Step 4: Update shell output formatting**

Print:
- conversational answer for `ask`
- created task plus immediate run result or policy denial for `act`

**Step 5: Run focused tests and verify they pass**

Run: `go test ./internal/runtime/conversation ./internal/cli/repl ./tests/integration -count=1`

Expected: PASS

**Step 6: Commit**

```bash
git add internal/runtime/conversation/service.go internal/runtime/conversation/service_test.go internal/cli/repl/shell.go internal/app/bootstrap/bootstrap.go internal/app/lifecycle/run.go internal/runtime/jobs/service.go tests/integration/alpha_acceptance_test.go
git commit -m "feat: add executor-backed ask mode and inline act execution"
```

### Task 3: Add failing store tests for scoped memory records and interaction events

**Files:**
- Create: `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/events/events.go`
- Test: `internal/store/sqlite/store_test.go`

**Step 1: Write failing persistence tests**

Cover:
- recording a conversation transcript in global scope
- recording a transcript in project scope
- storing compacted memory summaries separately from raw transcripts
- listing memory entries by scope
- emitting explicit runtime events for transcript and summary writes

**Step 2: Run the focused store tests and verify they fail**

Run: `go test ./internal/store/sqlite -run 'Test(Conversation|Memory|Summary)' -count=1`

Expected: FAIL because the memory tables and store methods do not exist yet.

**Step 3: Commit**

```bash
git add internal/store/sqlite/store_test.go
git commit -m "test: cover scoped memory persistence"
```

### Task 4: Implement scoped memory storage and services

**Files:**
- Create: `internal/memory/users/service.go`
- Create: `internal/memory/users/service_test.go`
- Create: `internal/memory/projects/service.go`
- Create: `internal/memory/projects/service_test.go`
- Create: `internal/memory/runs/service.go`
- Create: `internal/memory/runs/service_test.go`
- Create: `internal/memory/knowledge/service.go`
- Create: `internal/memory/knowledge/service_test.go`
- Modify: `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`
- Test: `internal/memory/...`

**Step 1: Add the schema and store methods**

Implement tables and store methods for:
- conversation transcripts
- memory summaries
- scope keys and project binding
- source linkage back to task/run/transcript ids

**Step 2: Implement memory read services**

Provide separate read/write services for:
- global user memory
- project memory
- run or episodic memory
- compacted knowledge summaries

**Step 3: Add merge rules for reads**

Project-scope reads should return:
- project memory first
- then global memory

Global reads should not automatically pull project-private memory upward.

**Step 4: Run the memory and store tests and verify they pass**

Run: `go test ./internal/store/sqlite ./internal/memory/... -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/sqlite/migrations/0010_memory_and_conversations.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/runtime/events/events.go internal/memory
git commit -m "feat: add scoped runtime memory services"
```

### Task 5: Ingest ask and act interactions into scoped memory

**Files:**
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/cli/repl/shell.go`
- Create: `internal/runtime/conversation/ingest_test.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Test: `internal/runtime/conversation/ingest_test.go`

**Step 1: Write failing ingestion tests**

Cover:
- `ask` prompt stores transcript and answer
- `act` prompt stores prompt, run output, and policy denial when one occurs
- run completion writes episodic memory suitable for later compaction

**Step 2: Run the focused ingestion tests and verify they fail**

Run: `go test ./internal/runtime/conversation ./internal/runtime/jobs -run 'Test(Ingest|Persist|Foreground)' -count=1`

Expected: FAIL before ingestion wiring exists.

**Step 3: Implement transcript and outcome ingestion**

Persist:
- user prompt
- model answer or run output
- tool summary
- scope and project binding
- task and run linkage where present

**Step 4: Run the focused ingestion tests and verify they pass**

Run: `go test ./internal/runtime/conversation ./internal/runtime/jobs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/conversation/service.go internal/runtime/conversation/ingest_test.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/cli/repl/shell.go
git commit -m "feat: persist ask and act interactions into memory"
```

### Task 6: Add failing tests for autonomous learning ingestion from repeated patterns

**Files:**
- Create: `internal/learning/ingest/service.go`
- Create: `internal/learning/ingest/service_test.go`
- Modify: `internal/learning/proposals/service.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `internal/runtime/projections/learning_test.go`
- Test: `internal/learning/ingest/service_test.go`

**Step 1: Write failing learning-ingestion tests**

Cover:
- repeated transcripts create a draft proposal automatically
- repeated project-local patterns stay scoped to that project
- repeated cross-project patterns generate global proposals
- the proposal payload contains source transcript references

**Step 2: Run the focused learning-ingestion tests and verify they fail**

Run: `go test ./internal/learning/ingest ./internal/runtime/projections -run 'Test(Proposal|Pattern|Scope)' -count=1`

Expected: FAIL because there is no interaction-driven learning service yet.

**Step 3: Commit**

```bash
git add internal/learning/ingest/service_test.go internal/runtime/projections/learning_test.go
git commit -m "test: cover autonomous learning proposal ingestion"
```

### Task 7: Implement autonomous proposal generation, canary rollout, and rollback

**Files:**
- Create: `internal/store/sqlite/migrations/0011_learning_canaries.sql`
- Create: `internal/learning/ingest/service.go`
- Create: `internal/learning/autonomy/service.go`
- Create: `internal/learning/autonomy/service_test.go`
- Modify: `internal/learning/promotion/service.go`
- Modify: `internal/learning/proposals/service.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/projections/projections.go`
- Test: `internal/learning/autonomy/service_test.go`

**Step 1: Extend the learning schema**

Add canary records and status tracking for:
- canary active
- canary promoted
- canary rolled_back
- sample count and scope

**Step 2: Implement autonomous proposal progression**

The service should:
- create proposals from repeated interaction evidence
- auto-submit them
- run replay or sandbox evaluation
- move passing proposals into canary state without human approval

**Step 3: Implement canary decision logic**

Promote to default when:
- evaluation passes
- canary success threshold is met

Rollback when:
- canary failure threshold is hit
- policy or runtime verification fails

**Step 4: Consume active canaries and promotions at runtime**

Apply runtime overlays for:
- prompt refinements
- routing refinements
- read-only skill and tool additions

**Step 5: Run the focused learning tests and verify they pass**

Run: `go test ./internal/learning/... ./internal/runtime/projections ./internal/runtime/conversation ./internal/runtime/jobs -count=1`

Expected: PASS

**Step 6: Commit**

```bash
git add internal/store/sqlite/migrations/0011_learning_canaries.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/runtime/events/events.go internal/learning internal/runtime/projections/projections.go internal/runtime/conversation/service.go internal/runtime/jobs/service.go
git commit -m "feat: add autonomous canary promotion and rollback"
```

### Task 8: Add failing tests for Odin self-editing through worktrees and task branches

**Files:**
- Create: `internal/learning/selfedit/service.go`
- Create: `internal/learning/selfedit/service_test.go`
- Modify: `internal/vcs/leases/manager.go`
- Modify: `internal/vcs/leases/manager_test.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Test: `internal/learning/selfedit/service_test.go`

**Step 1: Write failing self-edit tests**

Cover:
- Odin creates a self-edit task and isolated worktree
- canonical files are edited only inside the Odin-owned branch/worktree
- verification gates run before merge
- a failed verification triggers rollback
- success merges the branch without direct default-branch writes

**Step 2: Run the focused self-edit tests and verify they fail**

Run: `go test ./internal/learning/selfedit ./internal/vcs/leases -run 'Test(SelfEdit|Worktree|Rollback)' -count=1`

Expected: FAIL before the self-edit orchestrator exists.

**Step 3: Commit**

```bash
git add internal/learning/selfedit/service_test.go internal/vcs/leases/manager_test.go
git commit -m "test: cover autonomous self-edit orchestration"
```

### Task 9: Implement self-edit orchestration and canonical-file rewrites

**Files:**
- Create: `internal/learning/selfedit/service.go`
- Create: `internal/learning/selfedit/service_test.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/vcs/leases/manager.go`
- Modify: `internal/vcs/git/adapter.go`
- Modify: `internal/core/projects/service.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/tools/catalog/builtin.go`
- Test: `internal/learning/selfedit/service_test.go`

**Step 1: Implement the self-edit worker**

The service should:
- create Odin-owned tasks for self-edit proposals
- allocate a branch and worktree
- apply edits to `config`, `registry`, `prompts`, or `memory`
- run verification commands
- merge or rollback automatically

**Step 2: Reuse existing isolation mechanisms**

Do not invent a second branch/worktree system. Reuse:
- worktree leases
- git adapter helpers
- runtime task and run records

**Step 3: Emit explicit self-edit audit events**

Record:
- self-edit started
- verification passed or failed
- merge applied
- rollback applied

**Step 4: Run the focused self-edit tests and verify they pass**

Run: `go test ./internal/learning/selfedit ./internal/vcs/... ./internal/runtime/jobs -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/learning/selfedit/service.go internal/runtime/jobs/service.go internal/vcs/leases/manager.go internal/vcs/git/adapter.go internal/core/projects/service.go internal/runtime/events/events.go internal/tools/catalog/builtin.go
git commit -m "feat: add autonomous self-editing through isolated worktrees"
```

### Task 10: Add operator surfaces for memory, promotions, canaries, and rollback history

**Files:**
- Modify: `internal/cli/commands/commands.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/runtime/projections/projections.go`
- Create: `internal/runtime/projections/autonomy_test.go`
- Test: `internal/cli/repl/shell_test.go`

**Step 1: Add failing shell tests for new read surfaces**

Cover:
- `/memory`
- `/learn`
- `/canaries`
- `/rollbacks`

Each should show scoped data, not hidden internal state.

**Step 2: Run the focused shell tests and verify they fail**

Run: `go test ./internal/cli/repl ./internal/runtime/projections -run 'Test(Memory|Learn|Canaries|Rollbacks)' -count=1`

Expected: FAIL before the projections and commands exist.

**Step 3: Implement the projections and shell commands**

Expose:
- recent memory summaries by scope
- active promotions
- active canaries
- recent rollbacks and self-edit outcomes

**Step 4: Run the focused shell and projection tests and verify they pass**

Run: `go test ./internal/cli/repl ./internal/runtime/projections -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/runtime/projections/projections.go internal/runtime/projections/autonomy_test.go
git commit -m "feat: add autonomy inspection surfaces"
```

### Task 11: Update docs and run full verification

**Files:**
- Modify: `README.md`
- Modify: `docs/operations/alpha-readiness.md`
- Modify: `docs/contracts/self-improvement.md`
- Modify: `docs/contracts/repo-layout.md`

**Step 1: Update docs for the new runtime truth**

Document:
- executor-backed `ask`
- inline `act`
- scoped runtime memory
- autonomous canaries
- self-editing through worktrees and branches

**Step 2: Run complete verification**

Run:
- `make fmtcheck`
- `make lint`
- `go test ./internal/cli/repl ./internal/runtime/conversation ./internal/runtime/jobs ./internal/memory/... ./internal/learning/... ./internal/vcs/... ./internal/store/sqlite ./internal/runtime/projections`
- `make test-alpha`
- `make test`
- `make build`

Expected:
- all commands exit 0
- `make build` produces `bin/odin`

**Step 3: Review branch state**

Run: `git status --short --branch`

Expected: only intended implementation changes remain.

**Step 4: Commit**

```bash
git add README.md docs/operations/alpha-readiness.md docs/contracts/self-improvement.md docs/contracts/repo-layout.md
git commit -m "docs: describe conversational autonomy runtime"
```

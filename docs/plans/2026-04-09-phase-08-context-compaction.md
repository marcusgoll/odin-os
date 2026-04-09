# Phase 08 Context Compaction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add structured context compaction, append-only wake packets, and durable resume loading so Odin can pause and resume long-lived work without replaying raw chat history.

**Architecture:** Extend the existing SQLite `context_packets` authority with a richer envelope and typed packet payloads, then build a checkpoint service in `internal/runtime/checkpoints` that assembles project, run, and task wake context on triggers and reloads resume state from the latest wake packet chain. Keep the store responsible for durable reads and writes, and keep packet assembly and resume shaping in runtime code.

**Tech Stack:** Go, SQLite via `database/sql`, embedded SQL migrations, existing runtime event log, Go tests

---

### Task 1: Add packet schema docs and failing storage tests

**Files:**
- Create: `docs/contracts/context-compaction.md`
- Create: `internal/runtime/checkpoints/types.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write the failing store tests**

Add tests that expect:
- append-only packet creation with explicit scope, trigger, checkpoint key, status, and supersedes ID
- latest wake-packet lookup for a task
- packet lookup by ID

**Step 2: Run the focused store test command and verify it fails**

Run: `go test ./internal/store/sqlite -run 'TestContextPacket|TestLatestWakePacket'`
Expected: FAIL because the new packet fields and lookup methods do not exist yet.

**Step 3: Add the packet schema doc and runtime packet types**

Document:
- `project_context`
- `run_context`
- `task_wake_packet`
- trigger enums
- packet status rules

Add Go types that model those packet payloads without wiring the store yet.

**Step 4: Re-run the focused store test command**

Run: `go test ./internal/store/sqlite -run 'TestContextPacket|TestLatestWakePacket'`
Expected: still FAIL, now only on missing storage behavior.

### Task 2: Add the migration and store support

**Files:**
- Create: `internal/store/sqlite/migrations/0002_context_packets_envelope.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write the failing migration-path test**

Add a test that migrates an existing Phase 03 database and verifies the new context packet columns are present and usable.

**Step 2: Run the focused migration and store tests**

Run: `go test ./internal/store/sqlite -run 'TestMigrate|TestContextPacket|TestLatestWakePacket'`
Expected: FAIL because the migration and store methods are missing.

**Step 3: Implement the migration and store behavior**

Add:
- new columns to `context_packets`
- richer `ContextPacket` and `CreateContextPacketParams`
- store methods for:
  - create typed packet
  - get packet by ID
  - list packets for a task or run
  - get latest active task wake packet

Update the context-packet event payload to include packet scope and trigger metadata.

**Step 4: Re-run the focused migration and store tests**

Run: `go test ./internal/store/sqlite -run 'TestMigrate|TestContextPacket|TestLatestWakePacket'`
Expected: PASS

### Task 3: Build the checkpoint service with failing resume tests

**Files:**
- Create: `internal/runtime/checkpoints/service.go`
- Create: `internal/runtime/checkpoints/service_test.go`
- Modify: `internal/runtime/checkpoints/types.go`

**Step 1: Write the failing checkpoint tests**

Add tests that expect:
- approval-wait compaction to create linked project, run, and task wake packets
- restart compaction to create a new wake packet that supersedes the prior one
- resume loading to rebuild next-step state from the latest wake packet without raw transcript history

**Step 2: Run the focused checkpoint test command and verify it fails**

Run: `go test ./internal/runtime/checkpoints`
Expected: FAIL because the checkpoint service does not exist yet.

**Step 3: Implement the minimal checkpoint service**

Add:
- compaction trigger enum
- packet assembly helpers
- append-only packet write flow
- resume loader that reads the latest wake packet and linked context packets

Keep packet summaries structured and inspectable.

**Step 4: Re-run the checkpoint test command**

Run: `go test ./internal/runtime/checkpoints`
Expected: PASS

### Task 4: Update docs, verify, and commit

**Files:**
- Modify: `README.md`
- Modify: `docs/contracts/runtime-events.md`
- Modify: `internal/runtime/projections/projections.go`
- Test: `internal/store/sqlite/store_test.go`
- Test: `internal/runtime/checkpoints/service_test.go`

**Step 1: Add any missing read-only projection helpers**

If needed, add a latest wake-packet projection helper rather than querying raw SQL from tests or CLI code.

**Step 2: Update docs**

Reflect the richer context-packet event contract and mark Phase 08 in the README.

**Step 3: Run focused verification**

Run: `go test ./internal/store/sqlite ./internal/runtime/checkpoints ./internal/runtime/projections`
Expected: PASS

**Step 4: Run full verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: PASS

**Step 5: Commit**

```bash
git add README.md docs/contracts/context-compaction.md docs/contracts/runtime-events.md docs/plans/2026-04-09-phase-08-context-compaction.md internal/runtime/checkpoints internal/runtime/events/events.go internal/runtime/projections/projections.go internal/store/sqlite/models.go internal/store/sqlite/migrations/0002_context_packets_envelope.sql internal/store/sqlite/store.go internal/store/sqlite/store_test.go
git commit -m "feat: add context compaction and wake packets for phase 08"
```

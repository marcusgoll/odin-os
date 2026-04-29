# Action Evidence Runtime Substrate Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a generic Odin action evidence runtime substrate, proven first with Marcus FLICA fixture data, so a prepared payload, approval, submission, and readback proof can be audited as one append-only lifecycle.

**Domain Source of Truth:** `CONTEXT.md`, `docs/plans/2026-04-29-action-evidence-runtime-substrate-design.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/runtime-events.md`, `docs/contracts/registry-format.md`, `docs/contracts/verification-model.md`, `docs/contracts/homelab-operations.md`, and `registry/workflows/flica-*.md`.

**Context:** Odin runtime action evidence. This is a generic Odin runtime substrate with Marcus FLICA as the first proof case.

**Owns / Does Not Own:** Owns generic Action Record persistence, immutable Prepared Action Payload identity, Action Evidence Events, lifecycle/proof validation, and action-bound approval checks. Does not own PBS airline semantics, FLICA browser/session behavior, seniority ranking, FCFS interpretation, vacation rules, or Huginn browser automation.

**Invariants:** Actions are tied to exactly one workflow run and one workflow registry key; payload hashes are immutable; approvals bind to exactly one `action_id` and `payload_hash`; submission requires a resolved matching approval; completion requires declared proof; evidence is append-only; FLICA live writes remain operator-invoked and Huginn-backed when live browser proof/auth/readback is required.

**Architecture:** Add `internal/runtime/actions` as the action service, backed by SQLite migrations and store methods. Keep `/actions` as a thin read-only REPL surface. Extend existing approvals instead of creating a parallel approval lifecycle.

**Tech Stack:** Go, SQLite, existing Odin REPL, existing registry snapshot, existing runtime events, focused Go tests, real `./bin/odin` E2E checks.

---

## Context Mapping

Context: `odin-os` runtime action evidence.

Owns:

- `Action Record`
- `Prepared Action Payload`
- `Action Evidence Event`
- lifecycle transition validation
- proof requirement evaluation
- action-bound approval checks
- `/actions` read-only operator surface

Depends on:

- SQLite runtime authority in `internal/store/sqlite`
- registry workflow keys loaded from `registry/workflows`
- existing approval lifecycle and `/approvals`
- existing run/task/project rows
- Huginn and PBS only as downstream evidence sources in later FLICA integration slices

Does not own:

- PBS FLICA browser/session implementation
- airline-domain bidding or TradeBoard semantics
- live AA SSO or Duo behavior
- Annual Vacation command behavior

Boundary crossings:

- Approval binding crosses from `internal/runtime/actions` into existing `approvals`.
- Registry validation crosses through `workflow_key` references, not by embedding registry files in action state.
- Future `/tradeboard` integration will call `internal/runtime/actions`; `/tradeboard` must not become the state owner.

## Task 1: Store Migration For Action Records And Approval Binding

**Domain Goal:** Persist Action Records, immutable Prepared Action Payloads, Action Evidence Events, and action-bound approval fields in SQLite.

**Domain Rules Enforced:**

- SQLite remains canonical runtime authority.
- Payload hashes are immutable per action.
- Existing approvals remain the only approval system.

**Why this matters:**

- The rest of the substrate cannot prove approval or readback unless the runtime store has durable action rows and approval binding fields.

**Files:**

- Create: `internal/store/sqlite/migrations/0018_actions.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/store/sqlite/migrations_test.go` if migration-count expectations exist

**Step 1: Write the failing tests**

Add tests in `internal/store/sqlite/store_test.go`:

```go
func TestStorePersistsActionPayloadAndEvidence(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t)
	_, _, run := createProjectTaskRunFixture(t, ctx, store)

	action, payload, err := store.CreateActionWithPayload(ctx, CreateActionWithPayloadParams{
		WorkflowKey:          "flica-tradeboard",
		WorkflowRunID:        run.ID,
		ActionType:           "tradeboard_action",
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadHash:          "sha256:test",
		PayloadJSON:          `{"pairing":"W7084C"}`,
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     "external_readback",
	})
	if err != nil {
		t.Fatalf("CreateActionWithPayload() error = %v", err)
	}

	if action.WorkflowKey != "flica-tradeboard" || payload.PayloadHash != "sha256:test" {
		t.Fatalf("action=%+v payload=%+v", action, payload)
	}

	if _, err := store.AppendActionEvidence(ctx, AppendActionEvidenceParams{
		ActionID:     action.ID,
		EventType:    "action.prepared",
		EventVersion: 1,
		PayloadHash:  "sha256:test",
		RunID:        &run.ID,
		Source:       "test",
		EvidenceJSON: `{"status":"prepared"}`,
	}); err != nil {
		t.Fatalf("AppendActionEvidence() error = %v", err)
	}
}
```

Add `openMigratedTestStore` and `createProjectTaskRunFixture` as local test
helpers in `store_test.go` by extracting the existing project/task/run setup
pattern from `TestStoreMigrateLifecycleAndReopen`.

Add a second test:

```go
func TestStoreRejectsDuplicateActionPayloadHash(t *testing.T) {
	// Create the same action payload hash twice for one action.
	// Expected: second insert fails on unique (action_id, payload_hash).
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store/sqlite -run 'TestStorePersistsActionPayloadAndEvidence|TestStoreRejectsDuplicateActionPayloadHash' -count=1
```

Expected: FAIL because the action tables and methods do not exist.

**Step 3: Write minimal implementation**

Create `0018_actions.sql`:

```sql
CREATE TABLE IF NOT EXISTS actions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workflow_key TEXT NOT NULL,
  workflow_run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  lifecycle_state TEXT NOT NULL,
  current_payload_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS action_payloads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
  payload_schema TEXT NOT NULL,
  payload_schema_version INTEGER NOT NULL,
  payload_hash TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  submit_path TEXT NOT NULL,
  readback_path TEXT NOT NULL,
  proof_requirement TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(action_id, payload_hash)
);

CREATE TABLE IF NOT EXISTS action_evidence_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  event_version INTEGER NOT NULL,
  payload_hash TEXT,
  approval_id INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  source TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  occurred_at TEXT NOT NULL
);

ALTER TABLE approvals ADD COLUMN action_id INTEGER REFERENCES actions(id) ON DELETE SET NULL;
ALTER TABLE approvals ADD COLUMN payload_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_actions_workflow_run_id ON actions(workflow_run_id);
CREATE INDEX IF NOT EXISTS idx_actions_workflow_key ON actions(workflow_key);
CREATE INDEX IF NOT EXISTS idx_action_payloads_action_id ON action_payloads(action_id);
CREATE INDEX IF NOT EXISTS idx_action_evidence_action_id ON action_evidence_events(action_id, id);
CREATE INDEX IF NOT EXISTS idx_approvals_action_id ON approvals(action_id);
```

If SQLite migration re-runs fail on `ALTER TABLE ADD COLUMN`, split the approval binding into an idempotent migration helper in Go following existing migration style.

Add `Action`, `ActionPayload`, `ActionEvidenceEvent`, and params types in `models.go`.

Add store methods in `store.go`:

- `CreateActionWithPayload`
- `AppendActionEvidence`
- `ListActionEvidence`
- `GetAction`
- `ListActions`

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/store/sqlite -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/store/sqlite
git commit -m "feat(actions): add action evidence store schema"
```

## Task 2: Runtime Action Service For Payload Identity And Lifecycle Rules

**Domain Goal:** Add the domain service that makes payload identity, lifecycle transitions, and proof checks explicit before any command can submit external work.

**Domain Rules Enforced:**

- Payload identity is immutable and hash-backed.
- Invalid lifecycle transitions fail before evidence is appended.
- Completion requires declared proof.

**Why this matters:**

- Store methods persist rows; the runtime service enforces domain meaning.

**Files:**

- Create: `internal/runtime/actions/types.go`
- Create: `internal/runtime/actions/service.go`
- Create: `internal/runtime/actions/service_test.go`

**Step 1: Write the failing tests**

Add tests:

```go
func TestPreparedPayloadHashChangesWhenSubmitPathChanges(t *testing.T) {
	service := actions.Service{}
	first, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:      json.RawMessage(`{"pairing":"W7084C"}`),
		SubmitPath:       "command:/tradeboard post",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: "external_readback",
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	second, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:      json.RawMessage(`{"pairing":"W7084C"}`),
		SubmitPath:       "command:/tradeboard pickup",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: "external_readback",
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	if first == second {
		t.Fatalf("hash did not change when submit path changed")
	}
}
```

```go
func TestLifecycleRejectsCompletionWithoutReadback(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: "external_readback",
		Events: []actions.EvidenceSummary{
			{Type: actions.EventSubmitted},
			{Type: actions.EventInternallyRecorded},
		},
	})
	if !errors.Is(err, actions.ErrExternalReadbackMissing) {
		t.Fatalf("err = %v, want ErrExternalReadbackMissing", err)
	}
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/runtime/actions -count=1
```

Expected: FAIL because the package does not exist.

**Step 3: Write minimal implementation**

Define canonical constants:

- lifecycle states: `prepared`, `preflighted`, `approved`, `submitted`, `internally_recorded`, `externally_read_back`, `completed`, `failed`, `abandoned`
- event types: `action.prepared`, `action.preflighted`, `action.approved`, `action.submitted`, `action.internally_recorded`, `action.externally_read_back`, `action.completed`, `action.failed`, `action.abandoned`, `action.corrected`
- error codes from the design doc

Implement:

- deterministic JSON canonicalization for `payload_json`
- `HashPreparedPayload`
- `ValidateTransition`
- `ValidateApprovalBinding`
- `ValidateCompletion`

Keep the service independent of FLICA terms except test fixture names.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/runtime/actions -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/actions
git commit -m "feat(actions): add action lifecycle service"
```

## Task 3: Action-Bound Approval Store Integration

**Domain Goal:** Reuse existing approvals while proving each action approval authorizes exactly one immutable payload hash.

**Domain Rules Enforced:**

- No second approval system.
- Approval must bind to `action_id` and `payload_hash`.
- Submission cannot rely on stale approval.

**Why this matters:**

- FLICA safety depends on the operator approving the exact payload that later submits.

**Files:**

- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/cli/repl/shell.go`

**Step 1: Write the failing tests**

Add store tests:

```go
func TestRequestApprovalCanBindActionPayload(t *testing.T) {
	// Create project/task/run/action/payload.
	// Request approval with ActionID and PayloadHash.
	// Assert approval row and approval.requested event include both fields.
}
```

```go
func TestResolveActionApprovalRejectsPayloadMismatch(t *testing.T) {
	// Create action with payload hash A.
	// Request approval with hash A.
	// Create new payload hash B and make it current.
	// Resolve approval expecting ErrApprovalPayloadMismatch.
}
```

Add shell test:

```go
func TestShellApprovalsShowsActionBinding(t *testing.T) {
	// Seed pending action-bound approval.
	// /approvals output includes action_id= and payload_hash=.
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store/sqlite ./internal/cli/repl -run 'ActionApproval|ApprovalsShowsActionBinding' -count=1
```

Expected: FAIL because approvals do not expose or validate action binding.

**Step 3: Write minimal implementation**

Extend:

- `Approval` with `ActionID *int64` and `PayloadHash string`
- `RequestApprovalParams` with `ActionID *int64` and `PayloadHash string`
- `ResolveApproval` to reject approving an action-bound approval when the action current payload hash differs
- `ApprovalRequestedPayload` and `ApprovalResolvedPayload` with action binding fields
- REPL pending approval query/rendering to include action binding when present

Use stable operator-visible error text containing `approval_payload_mismatch`.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/store/sqlite ./internal/cli/repl -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/store/sqlite internal/runtime/events internal/cli/repl
git commit -m "feat(actions): bind approvals to action payloads"
```

## Task 4: Read-Only `/actions` Operator Surface

**Domain Goal:** Make action state inspectable through the canonical Odin operator shell.

**Domain Rules Enforced:**

- Operators do not query SQLite manually.
- Action lifecycle and proof state are visible through `odin`.
- `/actions` is read-only in this slice.

**Why this matters:**

- The user-visible proof surface must be the real Odin command path.

**Files:**

- Create: `internal/cli/repl/actions.go`
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/runtime/actions/service.go` if a read projection helper is needed
- Modify: `internal/store/sqlite/store.go`

**Step 1: Write the failing tests**

Add shell tests:

```go
func TestShellActionsListsRecentActions(t *testing.T) {
	// Seed one action.
	// Run /actions.
	// Assert output includes action_id, workflow=flica-tradeboard,
	// type=tradeboard_action, lifecycle=prepared, payload_hash=sha256:test.
}
```

```go
func TestShellActionsShowsActionDetail(t *testing.T) {
	// Seed action, payload, action-bound approval, evidence.
	// Run /actions <id>.
	// Assert output includes workflow_run_id, registry workflow key,
	// submit_path, readback_path, proof_requirement, approval binding.
}
```

```go
func TestShellActionsShowsEvidenceTimeline(t *testing.T) {
	// Seed ordered evidence events.
	// Run /actions <id> evidence.
	// Assert evidence output is ordered and append-only.
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli/repl -run 'TestShellActions' -count=1
```

Expected: FAIL because `/actions` is not registered.

**Step 3: Write minimal implementation**

Add `/actions` to shell help and dispatch.

Implement:

- `/actions`
- `/actions <id>`
- `/actions <id> evidence`

Render compact stable key/value output suitable for E2E assertions.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/cli/repl -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/repl internal/store/sqlite internal/runtime/actions
git commit -m "feat(actions): expose action evidence in shell"
```

## Task 5: Runtime Event Contract Alignment

**Domain Goal:** Keep action evidence compatible with Odin's append-only runtime event model.

**Domain Rules Enforced:**

- Important runtime mutations append events in the same SQL transaction.
- Event names are stable typed contracts.
- Action evidence remains append-only.

**Why this matters:**

- ADR 0001 and the runtime event contract require auditable mutations, not hidden table changes.

**Files:**

- Modify: `internal/runtime/events/events.go`
- Modify: `docs/contracts/runtime-events.md`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/projections/replay_test.go` only if replay coverage is extended in this slice

**Step 1: Write the failing tests**

Add store test:

```go
func TestAppendActionEvidenceMirrorsRuntimeEvent(t *testing.T) {
	// Append action evidence.
	// List generic events.
	// Assert stream_type=action, stream_id=action.ID,
	// event_type=action.prepared, run_id matches, payload contains evidence ID.
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store/sqlite -run TestAppendActionEvidenceMirrorsRuntimeEvent -count=1
```

Expected: FAIL until `StreamAction` and action event mirroring exist.

**Step 3: Write minimal implementation**

Add:

- `StreamAction`
- action event constants
- payload structs with `action_id`, `payload_hash`, `approval_id`, `source`, and `evidence_id`
- event append in the same transaction as `AppendActionEvidence`

Update `docs/contracts/runtime-events.md` with the action stream and events.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/projections -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/events internal/store/sqlite docs/contracts/runtime-events.md
git commit -m "feat(actions): mirror action evidence into runtime events"
```

## Task 6: Fixture-Backed Real Odin E2E Proof

**Domain Goal:** Prove the new operator-visible substrate through the real repo-owned `odin` binary without touching live FLICA.

**Domain Rules Enforced:**

- Real operator truth comes from `./bin/odin`.
- Fixture proof does not overclaim live FLICA behavior.
- Live FLICA proof remains a later operator-attended Huginn/PBS slice.

**Why this matters:**

- The action substrate is not complete until the actual shell can show actions and evidence.

**Files:**

- Create: `tests/e2e/action_fixture_test.go` or extend the nearest existing E2E harness if one exists
- Create: `internal/runtime/actions/fixture_test.go` only for reusable fixture helpers if needed
- Modify: `internal/cli/repl/shell_test.go` only if shared fixture helpers are local to REPL tests

**Step 1: Write the failing E2E test**

Add an E2E test that:

1. Builds or invokes `./bin/odin`.
2. Creates a temp `ODIN_ROOT`.
3. Seeds the temp SQLite DB through store APIs with:
   - project
   - task
   - run
   - action
   - payload
   - approval binding
   - ordered evidence
4. Runs:

```bash
printf '/actions\n/actions 1\n/actions 1 evidence\n/exit\n' | ODIN_ROOT="$tmp" ./bin/odin
```

5. Asserts stdout includes:
   - `/actions` command in help if help is included
   - `workflow=flica-tradeboard`
   - `payload_hash=`
   - `approval_id=`
   - `action.prepared`
   - `action.externally_read_back`

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./tests/e2e -run TestActionsShellShowsFixtureEvidence -count=1
```

Expected: FAIL until `/actions` is wired through the real binary and fixture seeding is complete.

**Step 3: Write minimal implementation**

Use only test harness code for fixture seeding. Do not add a production seed command.

Ensure the E2E test rebuilds the binary or calls:

```bash
go build -o ./bin/odin ./cmd/odin
```

before invoking the shell.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./tests/e2e -run TestActionsShellShowsFixtureEvidence -count=1
```

Expected: PASS.

Also run a manual smoke:

```bash
go build -o ./bin/odin ./cmd/odin
tmp=$(mktemp -d)
printf '/actions\n/exit\n' | ODIN_ROOT="$tmp" ./bin/odin
rm -rf "$tmp"
```

Expected: the command exits 0 and prints `no actions` for an empty runtime root.

**Step 5: Commit**

```bash
git add tests internal/cli/repl internal/runtime/actions internal/store/sqlite
git commit -m "test(actions): prove action evidence through odin shell"
```

## Task 7: FLICA Fixture Payload Contract

**Domain Goal:** Prove that the generic substrate can carry a FLICA TradeBoard payload without Odin taking ownership of PBS airline semantics.

**Domain Rules Enforced:**

- FLICA-specific fields stay in schema-versioned JSON.
- Odin records submit/readback/proof fields generically.
- No live FLICA write occurs in this task.

**Why this matters:**

- The first proof case should validate FLICA constraints without requiring Huginn, Duo, or live airline UI.

**Files:**

- Create: `internal/runtime/actions/flica_payload_test.go`
- Modify: `internal/runtime/actions/types.go` only if schema constants are useful
- Modify: `docs/plans/2026-04-29-action-evidence-runtime-substrate-design.md` only if a discovered gap changes the approved design

**Step 1: Write the failing test**

Add:

```go
func TestFLICATradeBoardFixturePayloadIncludesProofFields(t *testing.T) {
	payload := actions.PreparedPayload{
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadJSON: json.RawMessage(`{
			"action_type":"tradeboard_action",
			"operation":"post",
			"pairing":"W7084C",
			"bcid":"2026-04",
			"split_legs":[9,10],
			"comment":"DFW-DAY-DFW_turn_report_1351_end_2030"
		}`),
		SubmitPath:       "command:/tradeboard post",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: "external_readback",
	}
	hash, err := actions.Service{}.HashPreparedPayload(payload)
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	if hash == "" {
		t.Fatalf("hash is empty")
	}
}
```

**Step 2: Run test to verify it fails if schema support is missing**

Run:

```bash
go test ./internal/runtime/actions -run TestFLICATradeBoardFixturePayloadIncludesProofFields -count=1
```

Expected: FAIL only if required fields are not validated yet. If the generic hashing test already passes, add validation for required FLICA fixture fields and rerun.

**Step 3: Write minimal implementation**

Add schema constants or validation only where they enforce the approved design. Do not encode PBS airline rules or FLICA UI behavior.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/runtime/actions -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/actions docs/plans/2026-04-29-action-evidence-runtime-substrate-design.md
git commit -m "test(actions): validate flica tradeboard fixture payload"
```

## Task 8: Final Verification And Report

**Domain Goal:** Close the implementation with clear proof and unproven live boundaries.

**Domain Rules Enforced:**

- Real `odin` proof is required for operator-visible behavior.
- Fixture proof must not be described as live FLICA proof.
- Remaining live Huginn/PBS/FLICA risks stay explicit.

**Why this matters:**

- This substrate is safety-critical for future FLICA writes. The final report must be honest about what is proven.

**Files:**

- Modify: `docs/plans/2026-04-29-action-evidence-runtime-substrate.md` only if implementation reveals plan corrections
- Modify: PR body or completion note outside repo as appropriate

**Step 1: Run targeted checks**

Run:

```bash
go test ./internal/runtime/actions ./internal/store/sqlite ./internal/cli/repl ./internal/runtime/projections -count=1
```

Expected: PASS.

**Step 2: Run E2E checks**

Run:

```bash
go test ./tests/e2e -run TestActionsShellShowsFixtureEvidence -count=1
```

Expected: PASS.

Run:

```bash
go build -o ./bin/odin ./cmd/odin
tmp=$(mktemp -d)
printf '/help\n/actions\n/exit\n' | ODIN_ROOT="$tmp" ./bin/odin
status=$?
rm -rf "$tmp"
exit "$status"
```

Expected: exit 0; `/help` includes `/actions`; empty runtime prints `no actions`.

**Step 3: Run baseline package checks**

Run:

```bash
go test ./internal/... -count=1
```

Expected: PASS, or document unrelated pre-existing failures separately.

**Step 4: Commit final adjustments**

```bash
git status --short
git add <only files changed by this implementation>
git commit -m "chore(actions): finalize action evidence verification"
```

Only create this commit if there are final code or doc adjustments not already committed.

## Review Checklist

- Domain naming matches `CONTEXT.md`.
- `actions`, `action_payloads`, and `action_evidence_events` implement Action Record, Prepared Action Payload, and Action Evidence Event without introducing alternate vocabulary.
- Approval binding reuses existing approvals and does not create a parallel approval lifecycle.
- Payload hash tests prove material payload field changes alter identity.
- Lifecycle tests reject invalid completion, stale approval, and terminal mutation.
- Store tests prove append-only evidence and payload hash uniqueness.
- `/actions` shell tests prove operator-visible read behavior.
- Real `./bin/odin` E2E proof exists for `/actions`.
- Runtime event contract is updated if action evidence is mirrored into generic events.
- FLICA fixture proof is clearly labeled fixture-backed, not live airline proof.
- Live Huginn, Duo, PBS, and FLICA readback remain unproven until a later operator-attended run.

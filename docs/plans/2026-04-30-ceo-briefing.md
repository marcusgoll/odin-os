# CEO Briefing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the canonical `odin brief ceo` workflow that creates approval-gated CEO briefing proposals and publishes Daily Priority Packets through Odin OS runtime state.

**Domain Source of Truth:** `CONTEXT.md`, `docs/adr/0001-canonical-authority.md`, `docs/adr/0002-migration-policy.md`, `docs/contracts/runtime-events.md`, `docs/contracts/verification-model.md`, `docs/plans/2026-04-30-ceo-briefing-design.md`

**Context:** Odin OS governed portfolio-priority workflow.

**Owns / Does Not Own:** Odin owns the **CEO Briefing Workflow**, **Briefing Proposals**, **Daily Priority Packets**, approval-gated publication, and CLI projections. It does not own queue mutation, scheduler decisions, Telegram/email/Sabbatic delivery, or legacy `odin-orchestrator` runtime state.

**Invariants:**
- `odin brief ceo` is operator-invoked; scheduled or legacy sidecar execution cannot publish an Odin-owned packet.
- Proposal generation must not mutate queue/task state or publish priorities.
- Approval is the only publication boundary.
- Only one Daily Priority Packet may be active per business date.
- Supersession is append-only history, not in-place overwrite.
- SQLite runtime state and runtime events are the authority.

**Architecture:** Add a small `internal/runtime/briefing` service over existing SQLite, approval, health, registry, and projection primitives. Add top-level CLI dispatch for `odin brief ceo` and a minimal generic approval-resolution operator path so publication can happen through existing approval semantics instead of `brief --approve`.

**Tech Stack:** Go, SQLite migrations, existing `internal/store/sqlite`, existing runtime events, existing `internal/app/lifecycle` CLI dispatch, focused Go tests, real `./bin/odin` E2E checks.

---

## Context Mapping

- **Owns:** `CEO Briefing Workflow`, `Briefing Proposal`, `Daily Priority Packet`, `Priority Packet Supersession`, `odin brief ceo` surface.
- **Depends on:** SQLite store, runtime events, approvals, health doctor, projections, project registry, executor health.
- **Does not own:** task queue mutation, scheduler mutation, external notification delivery, legacy `/var/odin/engine.db`, `odin-orchestrator` scripts.
- **Boundary crossings:** approval resolution triggers packet publication; evidence collector reads runtime projections but must not mutate them.

## Preflight

Before implementation, create a dedicated worktree or confirm the branch is clean enough for the slice:

```bash
git -C /home/orchestrator/odin-os status --short
```

If unrelated changes remain in `CONTEXT.md` or other files, do not overwrite them. Use a task-owned worktree for implementation if available.

## Task 1: Briefing Runtime Event Contract

**Domain Goal:** Make Briefing Proposal and Daily Priority Packet events first-class Odin runtime events.

**Domain Rules Enforced:**
- SQLite events are append-only authority.
- Supersession is represented by events, not hidden mutation.

**Why this matters:**
- Store and CLI behavior need typed event names before persistence and projections can be tested.

**Files:**
- Modify: `internal/runtime/events/events.go`
- Test: `internal/runtime/projections/replay_test.go` or `internal/store/sqlite/store_test.go`

**Step 1: Write the failing event constant test**

Add a focused assertion where runtime event constants are already exercised, or add a new small test if no good home exists:

```go
func TestBriefingEventTypesAreCanonical(t *testing.T) {
	if runtimeevents.StreamBriefing != runtimeevents.StreamType("briefing") {
		t.Fatalf("StreamBriefing = %q", runtimeevents.StreamBriefing)
	}
	for _, eventType := range []runtimeevents.Type{
		runtimeevents.EventBriefingProposalCreated,
		runtimeevents.EventBriefingApprovalRequested,
		runtimeevents.EventBriefingProposalRejected,
		runtimeevents.EventBriefingPacketPublished,
		runtimeevents.EventBriefingPacketSuperseded,
	} {
		if !strings.HasPrefix(string(eventType), "briefing.") {
			t.Fatalf("briefing event %q should use briefing prefix", eventType)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/projections ./internal/store/sqlite
```

Expected: FAIL for missing briefing stream/event constants.

**Step 3: Add constants and payload types**

Add to `internal/runtime/events/events.go`:

```go
const (
	StreamBriefing StreamType = "briefing"
)

const (
	EventBriefingProposalCreated    Type = "briefing.proposal_created"
	EventBriefingApprovalRequested  Type = "briefing.approval_requested"
	EventBriefingProposalRejected   Type = "briefing.proposal_rejected"
	EventBriefingPacketPublished    Type = "briefing.packet_published"
	EventBriefingPacketSuperseded   Type = "briefing.packet_superseded"
)

type BriefingProposalCreatedPayload struct {
	ProposalID   int64  `json:"proposal_id"`
	BusinessDate string `json:"business_date"`
	Status       string `json:"status"`
}

type BriefingApprovalRequestedPayload struct {
	ProposalID int64 `json:"proposal_id"`
	ApprovalID int64 `json:"approval_id"`
}

type BriefingProposalRejectedPayload struct {
	ProposalID int64  `json:"proposal_id"`
	ApprovalID int64  `json:"approval_id"`
	Reason     string `json:"reason"`
}

type BriefingPacketPublishedPayload struct {
	PacketID     int64  `json:"packet_id"`
	ProposalID   int64  `json:"proposal_id"`
	BusinessDate string `json:"business_date"`
}

type BriefingPacketSupersededPayload struct {
	PacketID           int64 `json:"packet_id"`
	SupersededByPacket int64 `json:"superseded_by_packet"`
}
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/runtime/events ./internal/runtime/projections ./internal/store/sqlite
```

Expected: PASS for event compilation.

**Step 5: Commit**

```bash
git add internal/runtime/events/events.go internal/runtime/projections/replay_test.go internal/store/sqlite/store_test.go
git commit -m "feat(briefing): add runtime event contract"
```

## Task 2: Briefing Store Schema

**Domain Goal:** Persist Briefing Proposals and Daily Priority Packets in SQLite.

**Domain Rules Enforced:**
- SQLite is authoritative.
- Only one active packet per business date.

**Why this matters:**
- Generated files and legacy `/var/odin/state/priorities.json` must not become authority.

**Files:**
- Create: `internal/store/sqlite/migrations/0021_ceo_briefing.sql`
- Modify: `internal/store/sqlite/migrations_test.go`
- Modify: `internal/store/sqlite/models.go`

**Step 1: Write the failing migration test**

Add:

```go
func TestCEOBriefingMigrationCreatesProposalAndPacketTables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	for _, table := range []string{"briefing_proposals", "daily_priority_packets"} {
		var tableName string
		if err := store.DB().QueryRowContext(ctx, `
			SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?
		`, table).Scan(&tableName); err != nil {
			t.Fatalf("%s table query error = %v", table, err)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/store/sqlite -run TestCEOBriefingMigrationCreatesProposalAndPacketTables -v
```

Expected: FAIL because tables do not exist.

**Step 3: Add migration**

Create `internal/store/sqlite/migrations/0021_ceo_briefing.sql`. If a newer migration already exists in the active worktree, use the next available number and update this plan reference.

```sql
CREATE TABLE IF NOT EXISTS briefing_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  business_date TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT 'global',
  scope_key TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('drafted', 'approval_requested', 'rejected', 'published')),
  evidence_json TEXT NOT NULL,
  proposal_json TEXT NOT NULL,
  approval_id INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_briefing_proposals_date ON briefing_proposals(business_date, id);
CREATE INDEX IF NOT EXISTS idx_briefing_proposals_approval ON briefing_proposals(approval_id);

CREATE TABLE IF NOT EXISTS daily_priority_packets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  business_date TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'superseded')),
  proposal_id INTEGER NOT NULL REFERENCES briefing_proposals(id) ON DELETE RESTRICT,
  supersedes_packet_id INTEGER REFERENCES daily_priority_packets(id) ON DELETE SET NULL,
  packet_json TEXT NOT NULL,
  approved_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_priority_packets_one_active
  ON daily_priority_packets(business_date)
  WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_daily_priority_packets_proposal ON daily_priority_packets(proposal_id);
```

**Step 4: Add model structs**

Add to `internal/store/sqlite/models.go`:

```go
type BriefingProposal struct {
	ID           int64
	BusinessDate string
	Scope        string
	ScopeKey     string
	Status       string
	EvidenceJSON string
	ProposalJSON string
	ApprovalID   *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type DailyPriorityPacket struct {
	ID                  int64
	BusinessDate        string
	Status              string
	ProposalID          int64
	SupersedesPacketID  *int64
	PacketJSON          string
	ApprovedAt          time.Time
	CreatedAt           time.Time
}
```

**Step 5: Run tests**

Run:

```bash
go test ./internal/store/sqlite -run 'TestCEOBriefingMigration|TestStoreMigrateLifecycleAndReopen' -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/store/sqlite/migrations/0021_ceo_briefing.sql internal/store/sqlite/migrations_test.go internal/store/sqlite/models.go
git commit -m "feat(briefing): add priority packet schema"
```

## Task 3: Store Methods For Proposal And Packet Lifecycle

**Domain Goal:** Enforce proposal creation, approval linkage, publication, idempotency, and supersession in one transactional store boundary.

**Domain Rules Enforced:**
- Proposal generation does not publish a packet.
- Approval publication is idempotent.
- Only one active packet per business date.

**Why this matters:**
- This is the core governance boundary.

**Files:**
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/models.go`
- Test: `internal/store/sqlite/store_test.go`

**Step 1: Write failing store tests**

Add tests:

```go
func TestBriefingProposalDoesNotCreatePacket(t *testing.T) {
	store := openMigratedTestStore(t, "briefing-proposal-no-packet.db")
	ctx := context.Background()

	proposal, err := store.CreateBriefingProposal(ctx, sqlite.CreateBriefingProposalParams{
		BusinessDate: "2026-04-30",
		Scope: "global",
		EvidenceJSON: `{"sources":[]}`,
		ProposalJSON: `{"focus_weights":{}}`,
	})
	if err != nil {
		t.Fatalf("CreateBriefingProposal() error = %v", err)
	}
	if proposal.Status != "drafted" {
		t.Fatalf("proposal.Status = %q, want drafted", proposal.Status)
	}
	if packet, err := store.GetActiveDailyPriorityPacket(ctx, "2026-04-30"); err == nil {
		t.Fatalf("GetActiveDailyPriorityPacket() = %+v, want no row", packet)
	}
}

func TestPublishDailyPriorityPacketSupersedesPriorActivePacket(t *testing.T) {
	store := openMigratedTestStore(t, "briefing-supersession.db")
	ctx := context.Background()

	first := seedApprovedBriefingProposal(t, ctx, store, "2026-04-30")
	firstPacket, err := store.PublishDailyPriorityPacket(ctx, sqlite.PublishDailyPriorityPacketParams{
		ProposalID: first.ID,
		ApprovalID: *first.ApprovalID,
		DecisionBy: "operator",
		Reason: "approved",
	})
	if err != nil {
		t.Fatalf("PublishDailyPriorityPacket(first) error = %v", err)
	}

	second := seedApprovedBriefingProposal(t, ctx, store, "2026-04-30")
	secondPacket, err := store.PublishDailyPriorityPacket(ctx, sqlite.PublishDailyPriorityPacketParams{
		ProposalID: second.ID,
		ApprovalID: *second.ApprovalID,
		DecisionBy: "operator",
		Reason: "approved replacement",
	})
	if err != nil {
		t.Fatalf("PublishDailyPriorityPacket(second) error = %v", err)
	}
	if secondPacket.SupersedesPacketID == nil || *secondPacket.SupersedesPacketID != firstPacket.ID {
		t.Fatalf("second supersedes = %+v, want %d", secondPacket.SupersedesPacketID, firstPacket.ID)
	}
	active, err := store.GetActiveDailyPriorityPacket(ctx, "2026-04-30")
	if err != nil {
		t.Fatalf("GetActiveDailyPriorityPacket() error = %v", err)
	}
	if active.ID != secondPacket.ID {
		t.Fatalf("active packet = %d, want %d", active.ID, secondPacket.ID)
	}
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store/sqlite -run 'TestBriefingProposal|TestPublishDailyPriorityPacket' -v
```

Expected: FAIL for missing store methods.

**Step 3: Implement store methods**

Add params:

```go
type CreateBriefingProposalParams struct {
	BusinessDate string
	Scope        string
	ScopeKey     string
	EvidenceJSON string
	ProposalJSON string
}

type RequestBriefingApprovalParams struct {
	ProposalID  int64
	TaskID      int64
	RequestedBy string
}

type PublishDailyPriorityPacketParams struct {
	ProposalID int64
	ApprovalID int64
	DecisionBy string
	Reason     string
}
```

Implement:

- `CreateBriefingProposal`
- `RequestBriefingApproval`
- `RejectBriefingProposal`
- `PublishDailyPriorityPacket`
- `GetBriefingProposal`
- `GetBriefingProposalByApproval`
- `GetActiveDailyPriorityPacket`
- `ListBriefingProposalsByDate`

`PublishDailyPriorityPacket` must:

1. run in one transaction
2. load proposal and approval
3. return existing packet when proposal is already `published`
4. update old active packet to `superseded`
5. insert new active packet
6. update proposal to `published`
7. append packet-published and superseded events

**Step 4: Run targeted tests**

Run:

```bash
go test ./internal/store/sqlite -run 'TestBriefingProposal|TestPublishDailyPriorityPacket|TestStoreMigrateLifecycleAndReopen' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/store/sqlite/store.go internal/store/sqlite/models.go internal/store/sqlite/store_test.go
git commit -m "feat(briefing): persist proposal and packet lifecycle"
```

## Task 4: Briefing Service And Evidence Collector

**Domain Goal:** Generate evidence-backed Briefing Proposals from current Odin OS runtime state only.

**Domain Rules Enforced:**
- No legacy `/var/odin/engine.db` or `odin-orchestrator` scripts.
- Evidence gaps are recorded instead of hidden.
- Proposal generation does not mutate queue state.

**Why this matters:**
- The old briefing used ad hoc shell reads. Odin OS needs a bounded service with provenance.

**Files:**
- Create: `internal/runtime/briefing/types.go`
- Create: `internal/runtime/briefing/service.go`
- Create: `internal/runtime/briefing/evidence.go`
- Test: `internal/runtime/briefing/service_test.go`

**Step 1: Write failing service test**

```go
func TestServiceGenerateCEOProposalRecordsEvidenceAndApproval(t *testing.T) {
	store := openBriefingTestStore(t)
	service := briefing.Service{
		Store: store,
		Now: func() time.Time { return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC) },
		Evidence: briefing.StaticEvidenceCollector{
			Snapshot: briefing.EvidenceSnapshot{
				Sources: []briefing.EvidenceSource{{Name: "doctor", Status: "healthy"}},
			},
		},
	}

	result, err := service.GenerateCEOProposal(context.Background(), briefing.GenerateInput{})
	if err != nil {
		t.Fatalf("GenerateCEOProposal() error = %v", err)
	}
	if result.Proposal.BusinessDate != "2026-04-30" {
		t.Fatalf("business date = %q", result.Proposal.BusinessDate)
	}
	if result.Approval.ID == 0 {
		t.Fatalf("approval not created")
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/briefing -v
```

Expected: FAIL because package does not exist.

**Step 3: Implement minimal service**

Define:

```go
type GenerateInput struct {
	BusinessDate string
	ProjectKey   string
	JSON         bool
}

type GenerateResult struct {
	Proposal sqlite.BriefingProposal
	Approval sqlite.Approval
	Summary  ProposalSummary
}

type EvidenceCollector interface {
	Collect(ctx context.Context, input GenerateInput) (EvidenceSnapshot, error)
}
```

The real collector should read:

- `health.Service.Doctor`
- `projections.ListPendingApprovalViews`
- `jobs.Service.List` or direct projections for task/job state
- `runs.Service.List`
- registry snapshot diagnostics passed from bootstrap/lifecycle
- executor health from store/projections
- projection freshness rows
- incidents/recoveries where current store helpers exist

If a source is missing, add an evidence gap:

```json
{"name":"incidents","status":"missing","reason":"projection not implemented"}
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/runtime/briefing ./internal/store/sqlite
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/briefing internal/store/sqlite/store_test.go
git commit -m "feat(briefing): generate CEO briefing proposals"
```

## Task 5: Generic Approval Resolution Surface

**Domain Goal:** Resolve approvals through an Odin-owned operator path without adding `odin brief ceo --approve`.

**Domain Rules Enforced:**
- Approval remains the publication boundary.
- CEO briefing does not own a special approval shortcut.

**Why this matters:**
- Current code has `Store.ResolveApproval`, but no obvious top-level operator command for resolving approvals.

**Files:**
- Create: `internal/cli/commands/approvals.go`
- Modify: `internal/app/lifecycle/run.go`
- Test: `internal/cli/commands/commands_test.go`
- Test: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing command test**

Add a lifecycle test:

```go
func TestRunResolvesApprovalFromTopLevelCommand(t *testing.T) {
	root := seedLifecycleRoot(t)
	approvalID := seedPendingApproval(t, root)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"approvals", "resolve", strconv.FormatInt(approvalID, 10), "approved", "because", "operator approved"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(approvals resolve) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "approval=") {
		t.Fatalf("output = %q, want approval result", stdout.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/app/lifecycle -run TestRunResolvesApprovalFromTopLevelCommand -v
```

Expected: FAIL for unknown command.

**Step 3: Implement command**

Add top-level dispatch:

```go
case "approvals":
	return commands.RunApprovals(ctx, app.Store, args[1:], stdout)
```

Support:

- `odin approvals list [--json]`
- `odin approvals resolve <id> approved|rejected because <reason...> [--json]`

Keep this generic. Do not publish packets here yet; that happens in Task 6 through a resolver service wrapper.

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands ./internal/app/lifecycle -run 'Approvals|Approval' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/approvals.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/cli/commands/commands_test.go
git commit -m "feat(approvals): add operator resolution command"
```

## Task 6: Approval Resolution Publishes Packets

**Domain Goal:** Positive approval for a Briefing Proposal publishes the Daily Priority Packet exactly once.

**Domain Rules Enforced:**
- Approval is the only publication boundary.
- Publication is idempotent.
- Rejection does not create or change packets.

**Why this matters:**
- Without this, `odin brief ceo` can create proposals but cannot replace legacy priority publication.

**Files:**
- Modify: `internal/runtime/briefing/service.go`
- Modify: `internal/cli/commands/approvals.go`
- Test: `internal/runtime/briefing/service_test.go`
- Test: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing publication test**

```go
func TestResolveBriefingApprovalPublishesDailyPriorityPacket(t *testing.T) {
	service, store := seedBriefingServiceWithPendingApproval(t)

	result, err := service.ResolveApproval(context.Background(), briefing.ResolveApprovalInput{
		ApprovalID: pendingApprovalID,
		Status: "approved",
		DecisionBy: "operator",
		Reason: "approved",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if result.Packet == nil || result.Packet.Status != "active" {
		t.Fatalf("packet = %+v, want active packet", result.Packet)
	}

	again, err := service.ResolveApproval(context.Background(), sameInput)
	if err != nil {
		t.Fatalf("ResolveApproval(second) error = %v", err)
	}
	if again.Packet.ID != result.Packet.ID {
		t.Fatalf("idempotent packet id = %d, want %d", again.Packet.ID, result.Packet.ID)
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/runtime/briefing -run TestResolveBriefingApprovalPublishesDailyPriorityPacket -v
```

Expected: FAIL for missing resolver behavior.

**Step 3: Implement resolver**

In `briefing.Service.ResolveApproval`:

1. call `Store.ResolveApproval`
2. check whether approval belongs to a briefing proposal via `GetBriefingProposalByApproval`
3. if no proposal, return generic approval result
4. if status is approved, call `PublishDailyPriorityPacket`
5. if status is rejected, call `RejectBriefingProposal`
6. return packet/proposal summary

Wire `commands.RunApprovals` to use briefing resolver when initialized from lifecycle. If package cycles appear, add a small `internal/runtime/approvals` coordinator instead of importing CLI from runtime.

**Step 4: Run tests**

Run:

```bash
go test ./internal/runtime/briefing ./internal/cli/commands ./internal/app/lifecycle
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/briefing internal/cli/commands/approvals.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go
git commit -m "feat(briefing): publish packets from approval resolution"
```

## Task 7: `odin brief ceo` Operator Surface

**Domain Goal:** Expose the canonical operator-invoked CEO Briefing Workflow.

**Domain Rules Enforced:**
- Real `odin brief ceo` is the proof path.
- No `--approve` shortcut.
- Legacy sidecar state is not used as authority.

**Why this matters:**
- Without this, the feature remains internal runtime code.

**Files:**
- Create: `internal/cli/commands/brief.go`
- Modify: `internal/app/lifecycle/run.go`
- Test: `internal/cli/commands/commands_test.go`
- Test: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing CLI tests**

```go
func TestRunBriefCEOCreatesProposalAndApproval(t *testing.T) {
	root := seedLifecycleRoot(t)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"brief", "ceo", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(brief ceo) error = %v", err)
	}
	var result struct {
		ProposalID int64 `json:"proposal_id"`
		ApprovalID int64 `json:"approval_id"`
		BusinessDate string `json:"business_date"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json output: %v\n%s", err, stdout.String())
	}
	if result.ProposalID == 0 || result.ApprovalID == 0 {
		t.Fatalf("result = %+v, want proposal and approval ids", result)
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/app/lifecycle -run TestRunBriefCEOCreatesProposalAndApproval -v
```

Expected: FAIL for unknown command.

**Step 3: Implement command**

Add:

```go
case "brief":
	return commands.RunBrief(ctx, commands.BriefDependencies{
		Store: app.Store,
		Registry: app.Registry,
		RegistrySnapshot: app.RegistrySnapshot,
		RegistryDiagnostics: app.RegistryDiagnostics,
		Executors: app.Executors,
	}, args[1:], stdout)
```

Support:

- `odin brief ceo`
- `odin brief ceo --json`
- `odin brief ceo --date YYYY-MM-DD`
- `odin brief ceo --project <key>`
- `odin brief ceo status [--date YYYY-MM-DD] [--json]`
- `odin brief ceo show <id> [--json]`

**Step 4: Run tests**

Run:

```bash
go test ./internal/cli/commands ./internal/app/lifecycle -run 'Brief|CEO' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/commands/brief.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go internal/cli/commands/commands_test.go
git commit -m "feat(briefing): add ceo briefing command"
```

## Task 8: Registry Workflow Entry

**Domain Goal:** Document the CEO Briefing Workflow as an authored workflow while keeping runtime enforcement in the command/service.

**Domain Rules Enforced:**
- Registry declares purpose and constraints.
- Runtime remains authority for proposal/packet state.

**Why this matters:**
- Odin workflows should be reviewable under `registry/workflows/`, but registry text must not become a parallel runtime.

**Files:**
- Create: `registry/workflows/ceo-briefing.md`
- Test: `internal/registry/validator/validate_test.go` or existing registry loader tests

**Step 1: Write failing registry validation test**

Add a test or fixture assertion that `ceo-briefing` loads as a valid workflow.

**Step 2: Run test**

```bash
go test ./internal/registry/... -run 'Workflow|Registry' -v
```

Expected: FAIL until registry file exists.

**Step 3: Add workflow entry**

Frontmatter:

```yaml
---
kind: workflow
key: ceo-briefing
title: CEO Briefing Workflow
summary: Operator-invoked workflow for producing approval-gated Daily Priority Packets.
status: active
entrypoint: command:odin brief ceo
composes: []
tags:
  - portfolio_priority
  - approval_gated
owners:
  - odin-core
---
```

Required sections must cover purpose, inputs, procedure, outputs, constraints, and success criteria. State explicitly that v1 does not send email, Telegram, Sabbatic posts, scheduled runs, or queue mutations.

**Step 4: Run registry tests**

```bash
go test ./internal/registry/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add registry/workflows/ceo-briefing.md internal/registry/validator/validate_test.go
git commit -m "docs(registry): add ceo briefing workflow"
```

## Task 9: Real Odin E2E Proof

**Domain Goal:** Prove the operator-visible workflow through the repo-owned `odin` command path.

**Domain Rules Enforced:**
- Internal tests are not enough.
- Proposal generation alone does not mutate queue/task state.

**Why this matters:**
- The legacy defect was an invisible sidecar path; proof must come from `odin`.

**Files:**
- Modify: `tests/integration/alpha_acceptance_test.go` or create `tests/integration/ceo_briefing_test.go`
- Modify: `Makefile` only if a new targeted test command is warranted

**Step 1: Add integration/E2E test**

Test flow:

1. create temp Odin root
2. run built binary or lifecycle command equivalent for `brief ceo --json`
3. capture proposal and approval ids
4. inspect queue/task rows before and after proposal generation
5. run `approvals resolve <id> approved because test`
6. run `brief ceo status --json`
7. assert one active packet for the date

**Step 2: Run integration test**

```bash
go test ./tests/integration -run TestCEOBriefingCommandLifecycle -v
```

Expected: PASS.

**Step 3: Build binary**

```bash
make build
```

Expected: `bin/odin` builds successfully.

**Step 4: Run real command smoke**

Use a fresh runtime:

```bash
tmp_root="$(mktemp -d)"
cp -R config registry "$tmp_root/"
mkdir -p "$tmp_root/data" "$tmp_root/state/cache" "$tmp_root/.git"
ODIN_ROOT="$tmp_root" ./bin/odin brief ceo --json
ODIN_ROOT="$tmp_root" ./bin/odin approvals list --json
ODIN_ROOT="$tmp_root" ./bin/odin approvals resolve <approval_id> approved because test
ODIN_ROOT="$tmp_root" ./bin/odin brief ceo status --json
```

Expected:

- first command returns `proposal_id`, `approval_id`, and `business_date`
- approval list shows the pending approval
- resolve command publishes a packet
- status shows one active packet
- no task/queue statuses changed from generation alone

**Step 5: Commit**

```bash
git add tests/integration/ceo_briefing_test.go Makefile
git commit -m "test(briefing): prove ceo briefing command lifecycle"
```

## Task 10: Legacy Boundary And Documentation Closeout

**Domain Goal:** Make the legacy boundary explicit so future operators do not treat old CEO briefing artifacts as current authority.

**Domain Rules Enforced:**
- `odin-orchestrator` is migration source only.
- Legacy CEO artifacts are reference evidence only.

**Why this matters:**
- The original audit found live legacy artifacts that looked official.

**Files:**
- Modify: `docs/plans/2026-04-30-ceo-briefing-design.md` if implementation changes any command details
- Modify: `README.md` only if operator command list is maintained there
- Modify: `docs/contracts/runtime-events.md` if briefing events are now part of the active contract

**Step 1: Update contract docs**

Add briefing stream/events to `docs/contracts/runtime-events.md` once implemented:

```markdown
- `briefing`
- `briefing.proposal_created`
- `briefing.approval_requested`
- `briefing.proposal_rejected`
- `briefing.packet_published`
- `briefing.packet_superseded`
```

**Step 2: Run docs/contract checks**

```bash
go test ./internal/runtime/events ./internal/store/sqlite ./internal/registry/...
```

Expected: PASS.

**Step 3: Commit**

```bash
git add docs/contracts/runtime-events.md README.md docs/plans/2026-04-30-ceo-briefing-design.md
git commit -m "docs(briefing): document ceo briefing runtime contract"
```

## Final Verification

Run:

```bash
go test ./internal/runtime/briefing ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle ./internal/registry/... ./tests/integration
make build
tmp_root="$(mktemp -d)"
cp -R config registry "$tmp_root/"
mkdir -p "$tmp_root/data" "$tmp_root/state/cache" "$tmp_root/.git"
ODIN_ROOT="$tmp_root" ./bin/odin brief ceo --json
ODIN_ROOT="$tmp_root" ./bin/odin approvals list --json
ODIN_ROOT="$tmp_root" ./bin/odin approvals resolve <approval_id> approved because final-smoke
ODIN_ROOT="$tmp_root" ./bin/odin brief ceo status --json
```

Expected final proof:

- `odin brief ceo --json` creates proposal and approval ids.
- Proposal generation does not change queue/task statuses.
- Approval resolution publishes exactly one active Daily Priority Packet.
- Re-resolving the same approval is idempotent.
- A second approved packet for the same date supersedes the first.
- No code path calls legacy `odin-orchestrator` scripts, `/var/odin/engine.db`, email, Telegram, or Sabbatic.

## Review Checklist

- Domain naming matches `CONTEXT.md`: CEO Briefing Workflow, Briefing Proposal, Daily Priority Packet, Priority Packet Supersession.
- Invariant coverage exists for no queue mutation, approval-only publication, single active packet, supersession, and idempotency.
- ADR 0001 is honored: SQLite and events are authority.
- ADR 0002 is honored: legacy `odin-orchestrator` is reference input only.
- Boundary crossings are explicit: approval resolution is the only publish boundary.
- Reused repo structures are named: lifecycle command dispatch, SQLite store, approvals, runtime events, registry workflows, verification model.
- Open blockers: exact JSON response envelope and id style can be finalized during Task 7, but must remain stable once tests land.

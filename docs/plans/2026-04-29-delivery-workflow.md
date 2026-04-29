# Delivery Workflow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the v1 `odin work ...` Delivery Workflow surface over existing tasks, runs, registry workflows, and runtime events.

**Domain Source of Truth:** `CONTEXT.md`, `docs/plans/2026-04-29-delivery-workflow-design.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/registry-format.md`, `docs/contracts/verification-model.md`

**Context:** Odin OS governed work-control plane

**Owns / Does Not Own:** Odin owns Initiatives, Work Items, Run Attempts, Delivery Gates, Delivery Profiles, Feedback Loops, evidence, and command proof. It does not create a new feature aggregate, a fifth registry kind, or a sidecar pipeline.

**Invariants:**
- Delivery hierarchy is Initiative -> Work Item -> Run Attempt.
- V1 Delivery Profiles are `workflow` registry entries tagged `delivery_profile`.
- Delivery Workflow gates advance in order unless the failure branch explicitly returns to an earlier gate.
- A gate cannot complete without Odin-owned evidence or a linked artifact.
- Real E2E proof for user-visible delivery behavior must exercise `odin work ...`.

**Architecture:** Add a small `internal/runtime/delivery` service that treats existing task rows as Work Items, run rows as Run Attempts, registry workflow entries tagged `delivery_profile` as Delivery Profiles, and append-only runtime events as gate evidence. Expose it through a top-level `odin work ...` command family, with later REPL aliases as thin adapters.

**Tech Stack:** Go, SQLite events, Markdown workflow registry, existing lifecycle dispatch, existing registry loader, existing integration helpers

---

### Task 1: Add Delivery Profile registry fixtures

**Domain Goal:** Represent flexible feedback loops through existing workflow registry entries without adding a fifth registry kind.

**Domain Rules Enforced:**
- V1 Delivery Profiles are workflow entries tagged `delivery_profile`.
- Profiles declare gates, skills, agents, proof, and failure branches in authored Markdown.

**Why this matters:**
- The feedback loop must be flexible across task shapes without becoming a hardcoded feature-only pipeline.

**Files:**
- Create: `registry/workflows/delivery-profile-feature-delivery.md`
- Create: `registry/workflows/delivery-profile-bugfix-debugging.md`
- Create: `registry/workflows/delivery-profile-audit-only.md`
- Modify: `docs/contracts/registry-format.md`
- Test: `internal/registry/loader/load_test.go`

**Step 1: Write the failing registry test**

Add a test in `internal/registry/loader/load_test.go`:

```go
func TestLoadDirLoadsDeliveryProfilesAsWorkflows(t *testing.T) {
	snapshot, err := LoadDir(filepath.Join(projectRoot(t), "registry"))
	if err != nil {
		t.Fatalf("LoadDir(registry) error = %v", err)
	}
	var profiles []registry.Item
	for _, item := range snapshot.ByKind[registry.KindWorkflow] {
		if slices.Contains(item.Tags, "delivery_profile") {
			profiles = append(profiles, item)
		}
	}
	if len(profiles) < 3 {
		t.Fatalf("delivery profile count = %d, want at least 3", len(profiles))
	}
	for _, profile := range profiles {
		if profile.Entrypoint != "command:work" {
			t.Fatalf("%s entrypoint = %q, want command:work", profile.Key, profile.Entrypoint)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/loader -run TestLoadDirLoadsDeliveryProfilesAsWorkflows -v`

Expected: FAIL because the profile workflow files do not exist.

**Step 3: Add profile workflow entries**

Each profile must use `kind: workflow`, `entrypoint: command:work`, `tags: [delivery_profile, ...]`, and required sections. The `Procedure` section must list gates. Example for `feature_delivery`:

```markdown
---
kind: workflow
key: delivery-profile-feature-delivery
title: Feature Delivery Profile
summary: Delivery profile for software feature work requiring domain lock, design, plan, execution, verification, branch finish, and learning review.
status: active
tags:
  - delivery_profile
  - feature_delivery
owners:
  - odin-core
entrypoint: command:work
composes:
  - domain-model
  - superpowers:brainstorming
  - superpowers:writing-plans
  - superpowers:subagent-driven-development
  - superpowers:verification-before-completion
  - superpowers:finishing-a-development-branch
---
```

Include gates in body text:

```text
domain_locked, design_approved, plan_ready, execution_selected, execution_complete, verified, branch_finished, learning_reviewed
```

**Step 4: Document profile convention**

Update `docs/contracts/registry-format.md` with a short subsection under workflow explaining `delivery_profile` tagged workflows.

**Step 5: Run registry tests**

Run: `go test ./internal/registry/...`

Expected: PASS.

**Step 6: Commit**

```bash
git add registry/workflows/delivery-profile-*.md docs/contracts/registry-format.md internal/registry/loader/load_test.go
git commit -m "feat(registry): add delivery profile workflows"
```

### Task 2: Add Delivery runtime service over existing task/run/events

**Domain Goal:** Make Delivery Gates durable through Odin-owned runtime events without adding a parallel gate store.

**Domain Rules Enforced:**
- Gate completion requires evidence.
- Gates advance in canonical order.
- Work Item and Run Attempt language maps onto existing task/run substrate.

**Why this matters:**
- Clean feedback loops need state and evidence that survive the executor session.

**Files:**
- Create: `internal/runtime/delivery/types.go`
- Create: `internal/runtime/delivery/service.go`
- Create: `internal/runtime/delivery/service_test.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Write failing service tests**

Add tests for:

```go
func TestDeliveryServiceListsProfilesFromWorkflowRegistry(t *testing.T)
func TestDeliveryServiceStartsWorkItemFromProfile(t *testing.T)
func TestDeliveryServiceRejectsGateAdvanceWithoutEvidence(t *testing.T)
func TestDeliveryServiceAdvancesGatesInOrder(t *testing.T)
```

The test should create a temp store, load registry, create/select `odin-core`, and assert canonical terms in returned structs:

```go
if got.WorkItemKey == "" { t.Fatal("WorkItemKey missing") }
if got.CurrentGate != delivery.GateDomainLocked { t.Fatalf(...) }
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/delivery -v`

Expected: FAIL because the package does not exist.

**Step 3: Add delivery event types**

Add to `internal/runtime/events/events.go`:

```go
EventDeliveryWorkStarted Type = "delivery.work_started"
EventDeliveryGateAdvanced Type = "delivery.gate_advanced"
EventDeliveryEvidenceRecorded Type = "delivery.evidence_recorded"
```

Add payload structs:

```go
type DeliveryWorkStartedPayload struct {
	ProfileKey string `json:"profile_key"`
	WorkItemKey string `json:"work_item_key"`
	InitialGate string `json:"initial_gate"`
}

type DeliveryGateAdvancedPayload struct {
	PreviousGate string `json:"previous_gate"`
	Gate string `json:"gate"`
	EvidenceSummary string `json:"evidence_summary"`
}

type DeliveryEvidenceRecordedPayload struct {
	Gate string `json:"gate"`
	Kind string `json:"kind"`
	Summary string `json:"summary"`
	Ref string `json:"ref,omitempty"`
}
```

**Step 4: Implement service**

`internal/runtime/delivery/types.go` should define:

```go
type Gate string

const (
	GateDomainLocked Gate = "domain_locked"
	GateDesignApproved Gate = "design_approved"
	GatePlanReady Gate = "plan_ready"
	GateExecutionSelected Gate = "execution_selected"
	GateExecutionComplete Gate = "execution_complete"
	GateVerified Gate = "verified"
	GateBranchFinished Gate = "branch_finished"
	GateLearningReviewed Gate = "learning_reviewed"
)
```

`service.go` should expose:

```go
type Service struct {
	Store *sqlite.Store
	RegistrySnapshot registry.Snapshot
	Now func() time.Time
}

func (s Service) ListProfiles() []Profile
func (s Service) Start(ctx context.Context, params StartParams) (Status, error)
func (s Service) Status(ctx context.Context, workItemKey string) (Status, error)
func (s Service) RecordEvidence(ctx context.Context, params EvidenceParams) (Status, error)
func (s Service) Advance(ctx context.Context, params AdvanceParams) (Status, error)
```

Use `Store.CreateTask` for v1 Work Items and append delivery events to the task stream. If `Store` has no public generic append-event method, add the narrow store method needed for delivery events rather than writing SQL in the service.

**Step 5: Run service tests**

Run: `go test ./internal/runtime/delivery ./internal/runtime/events ./internal/store/sqlite -run 'TestDelivery|TestStore.*Delivery' -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/runtime/delivery internal/runtime/events/events.go internal/store/sqlite
git commit -m "feat(runtime): add delivery gate service"
```

### Task 3: Add top-level `odin work ...` command

**Domain Goal:** Make the Delivery Workflow provable through the canonical real `odin` command surface.

**Domain Rules Enforced:**
- `odin work ...` is the proof authority.
- REPL aliases are not required for v1 proof.

**Why this matters:**
- The operator feedback loop must be scriptable and testable without entering a bespoke session.

**Files:**
- Create: `internal/cli/commands/work.go`
- Create: `internal/cli/commands/work_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Test: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write failing command tests**

Add tests for:

```go
func TestRunWorkProfilesListsDeliveryProfiles(t *testing.T)
func TestRunWorkStatusReportsMissingWorkItem(t *testing.T)
func TestRunWorkAdvanceRequiresEvidence(t *testing.T)
```

Expected output examples:

```text
delivery-profile-feature-delivery gates=domain_locked,design_approved,plan_ready,execution_selected,execution_complete,verified,branch_finished,learning_reviewed
```

```text
work_item=unknown status=missing
```

```text
gate=domain_locked status=blocked reason=evidence_required
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/cli/commands -run TestRunWork -v`

Expected: FAIL because `RunWork` does not exist.

**Step 3: Implement command adapter**

`work.go` should parse:

```text
profiles
start --profile <key> --project <key> --title <title>
status <work-item-key>
evidence <work-item-key> --gate <gate> --kind <kind> --summary <summary> [--ref <ref>]
advance <work-item-key> --gate <gate> --evidence <summary>
verify <work-item-key> --summary <summary> [--ref <ref>]
```

Keep parsing simple and deterministic; no interactive prompts in top-level v1.

**Step 4: Wire lifecycle dispatch**

In `internal/app/lifecycle/run.go`, add:

```go
case "work":
	return commands.RunWork(ctx, commands.WorkEnvironment{
		Store: app.Store,
		Registry: app.Registry,
		RegistrySnapshot: app.RegistrySnapshot,
	}, args[1:], stdout)
```

**Step 5: Add integration acceptance**

Extend `tests/integration/alpha_acceptance_test.go` with a subtest that builds `odin` and runs:

```bash
ODIN_ROOT="$runtimeRoot" ./bin/odin work profiles
ODIN_ROOT="$runtimeRoot" ./bin/odin work start --profile delivery-profile-audit-only --project odin-core --title "Audit delivery loop"
ODIN_ROOT="$runtimeRoot" ./bin/odin work status <work-item-key>
```

For `odin-core`, this start path should create a Work Item only and must not execute a mutating run.

**Step 6: Run command and integration tests**

Run:

```bash
go test ./internal/cli/commands ./internal/app/lifecycle -run 'TestRunWork|TestRunDispatchesWork' -v
go test ./tests/integration -run TestAlphaAcceptance -v
```

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/cli/commands/work.go internal/cli/commands/work_test.go internal/app/lifecycle/run.go tests/integration/alpha_acceptance_test.go
git commit -m "feat(cli): add odin work delivery surface"
```

### Task 4: Add clean operator output and missing-command failure proof

**Domain Goal:** Keep the feedback loop high-signal and make failures actionable.

**Domain Rules Enforced:**
- Output includes current gate, evidence, decision, next action, and remaining risk where applicable.
- Missing or invalid profile/gate states fail clearly.

**Why this matters:**
- Clean output is the point of the Delivery Workflow; noisy signal causes agents and operators to get lost.

**Files:**
- Modify: `internal/cli/commands/work.go`
- Modify: `internal/runtime/delivery/service.go`
- Modify: `internal/cli/commands/work_test.go`
- Test: `tests/integration/alpha_acceptance_test.go`

**Step 1: Add failing output contract tests**

Assert these exact fragments:

```text
current_gate=domain_locked
decision=blocked
next_action=record_evidence
remaining_risk=evidence_missing
```

For invalid transitions:

```text
status=blocked reason=gate_order_violation expected=design_approved got=verified
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/cli/commands ./internal/runtime/delivery -run 'Test.*Output|Test.*Gate' -v`

Expected: FAIL until output contract is implemented.

**Step 3: Implement deterministic rendering**

Keep output line-oriented `key=value` for v1. Avoid prose paragraphs in machine-oriented command output.

**Step 4: Run focused and real command proof**

Run:

```bash
go test ./internal/runtime/delivery ./internal/cli/commands -v
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin work profiles
ODIN_ROOT="$tmp" ./bin/odin work status missing
```

Expected: profile list succeeds; missing status returns clear missing output without panic.

**Step 5: Commit**

```bash
git add internal/runtime/delivery internal/cli/commands/work.go internal/cli/commands/work_test.go tests/integration/alpha_acceptance_test.go
git commit -m "feat(cli): render clean delivery feedback"
```

### Task 5: Add REPL `/work` alias as a thin adapter

**Domain Goal:** Keep the shell ergonomic while preserving top-level `odin work ...` as proof authority.

**Domain Rules Enforced:**
- REPL alias delegates to the same command/service behavior.
- Business logic stays outside the shell.

**Why this matters:**
- Operators can use the shell without creating a second Delivery Workflow implementation.

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/cli/commands/commands.go`

**Step 1: Write failing shell test**

Add a test proving:

```text
/work profiles
```

prints the same profile keys as `odin work profiles`.

**Step 2: Run test to verify failure**

Run: `go test ./internal/cli/repl -run TestShellWorkAlias -v`

Expected: FAIL because `/work` is unknown.

**Step 3: Add alias**

Wire `/work` to call `commands.RunWork` with shell environment dependencies. Do not duplicate parsing or delivery behavior in `shell.go`.

**Step 4: Run tests**

Run: `go test ./internal/cli/repl ./internal/cli/commands -run 'TestShellWorkAlias|TestRunWork' -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/cli/commands/commands.go
git commit -m "feat(repl): add work alias"
```

### Task 6: Final verification and docs closeout

**Domain Goal:** Prove the Delivery Workflow through the real command path and document remaining limits.

**Domain Rules Enforced:**
- Real `odin work ...` E2E proof exists.
- Proven and unproven behavior are separated.

**Why this matters:**
- Completion cannot be claimed from internal tests alone.

**Files:**
- Modify: `README.md`
- Modify: `docs/contracts/verification-model.md` if command examples need updating
- Modify: `docs/plans/2026-04-29-delivery-workflow-design.md` only if implementation discovers a design correction

**Step 1: Update operator docs**

Add a short README or contract note:

```text
Use `odin work profiles` to inspect Delivery Profiles.
Use `odin work start` to create a Work Item under a profile.
Use `odin work evidence` and `odin work advance` to move gates with evidence.
```

**Step 2: Run full focused verification**

Run:

```bash
go test ./internal/registry/... ./internal/runtime/delivery ./internal/cli/commands ./internal/cli/repl ./internal/app/lifecycle -v
go test ./tests/integration -run TestAlphaAcceptance -v
go build -o ./bin/odin ./cmd/odin
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" ./bin/odin work profiles
ODIN_ROOT="$tmp" ./bin/odin work start --profile delivery-profile-audit-only --project odin-core --title "Audit delivery loop"
ODIN_ROOT="$tmp" ./bin/odin work status <created-work-item-key>
ODIN_ROOT="$tmp" ./bin/odin work evidence <created-work-item-key> --gate domain_locked --kind doc --summary "CONTEXT.md checked"
ODIN_ROOT="$tmp" ./bin/odin work advance <created-work-item-key> --gate domain_locked --evidence "CONTEXT.md checked"
```

Expected: tests pass; real command output shows profile list, Work Item creation, status, evidence recording, and gate advancement.

**Step 3: Record unproven limits**

Explicitly document if any are deferred:

- automatic profile recommendation
- full Initiative schema
- branch-finish automation
- `writing_skills` follow-up automation
- provider-backed execution beyond deterministic alpha lane

**Step 4: Commit**

```bash
git add README.md docs/contracts/verification-model.md docs/plans/2026-04-29-delivery-workflow-design.md
git commit -m "docs: document delivery workflow surface"
```

## Review Checklist

- Domain naming matches `CONTEXT.md`: Initiative, Work Item, Run Attempt, Delivery Workflow, Delivery Gate, Delivery Profile, Feedback Loop.
- Delivery Profiles are workflow entries tagged `delivery_profile`, not a new registry kind.
- Gate evidence is Odin-owned and append-only.
- Real `odin work ...` proof is required before completion.
- Existing registry, task, run, event, executor, worktree, and shell structures are reused.
- No sidecar pipeline or direct Codex-session authority is introduced.

## Execution Handoff

- Domain artifacts used: `CONTEXT.md`, `docs/plans/2026-04-29-delivery-workflow-design.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/registry-format.md`, `docs/contracts/verification-model.md`.
- Reused components: workflow registry, skill/agent composition, tasks, runs, events, registry loader, lifecycle dispatch, REPL, integration helpers, verification model.
- New components proposed: delivery profile workflow files, `internal/runtime/delivery`, `odin work ...` command adapter, optional `/work` REPL alias.
- Why new components are necessary: current Odin can create tasks and runs but cannot represent ordered delivery gates, profile-selected feedback loops, or canonical `odin work ...` proof.
- Invariants and boundary checks: no new registry kind in v1, no feature aggregate, no gate advancement without evidence, no REPL-only proof, no sidecar pipeline.
